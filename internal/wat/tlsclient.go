package wat

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

type tlsProxyClient struct {
	client  tls_client.HttpClient
	host    string
	user    string
	pass    string
	mu      sync.RWMutex
	session string
}

func newTLSProxyClient() (*tlsProxyClient, error) {
	host := strings.TrimSpace(os.Getenv("PLANWAT_PROXY_HOST"))
	if host == "" {
		return nil, nil
	}
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimSuffix(host, "/")
	if host == "" {
		return nil, fmt.Errorf("zły PLANWAT_PROXY_HOST")
	}

	user := strings.TrimSpace(os.Getenv("PLANWAT_PROXY_USER"))
	pass := os.Getenv("PLANWAT_PROXY_PASS")
	if user == "" || pass == "" {
		return nil, fmt.Errorf("PLANWAT_PROXY_HOST wymaga PLANWAT_PROXY_USER i PLANWAT_PROXY_PASS")
	}

	session, err := randomSession()
	if err != nil {
		return nil, err
	}
	c := &tlsProxyClient{host: host, user: user, pass: pass, session: session}

	jar := tls_client.NewCookieJar()
	client, err := tls_client.NewHttpClient(nil,
		tls_client.WithTimeoutSeconds(10),
		tls_client.WithClientProfile(profiles.Chrome_150),
		tls_client.WithCookieJar(jar),
		tls_client.WithProxyUrl(c.proxyURL()),
	)
	if err != nil {
		return nil, err
	}
	client.SetFollowRedirect(true)
	c.client = client
	return c, nil
}

const proxySessionMinutes = "1440"

func (c *tlsProxyClient) proxyURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.proxyURLFor(c.session)
}

func (c *tlsProxyClient) proxyURLFor(session string) string {
	login := proxyLogin(c.user) + "-sessid-" + session + "-sesstime-" + proxySessionMinutes
	u := url.URL{Scheme: "http", Host: c.host, User: url.UserPassword(login, c.pass)}
	return u.String()
}

func proxyLogin(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "customer-")
	if i := strings.Index(raw, "-sessid-"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.Index(raw, "-sesstime-"); i >= 0 {
		raw = raw[:i]
	}
	if !strings.Contains(raw, "-cc-") {
		raw += "-cc-PL"
	}
	return "customer-" + raw
}

func randomSession() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (c *tlsProxyClient) rotateFrom(seen string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != seen {
		return nil
	}
	session, err := randomSession()
	if err != nil {
		return err
	}
	if c.client != nil {
		if err := c.client.SetProxy(c.proxyURLFor(session)); err != nil {
			return err
		}
		c.client.SetCookieJar(tls_client.NewCookieJar())
	}
	c.session = session
	return nil
}

func (c *tlsProxyClient) fetch(target string, prev Validators) ([]byte, Validators, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		c.mu.RLock()
		session := c.session
		body, got, err := c.do(target, prev)
		c.mu.RUnlock()
		if err == nil {
			return body, got, nil
		}
		if errors.Is(err, ErrNotModified) {
			return nil, prev, err
		}
		lastErr = err
		if attempt == 1 || !retryNewSession(err) {
			break
		}
		if err := c.rotateFrom(session); err != nil {
			return nil, Validators{}, err
		}
	}
	return nil, Validators{}, lastErr
}

func retryNewSession(err error) bool {
	if errors.Is(err, ErrProxy) || errors.Is(err, ErrBlocked) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "407")
}

func (c *tlsProxyClient) do(target string, prev Validators) ([]byte, Validators, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, Validators{}, err
	}
	req.Header = http.Header{
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		"accept-language":           {"pl-PL,pl;q=0.9,en-US;q=0.8,en;q=0.7"},
		"user-agent":                {"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"},
		"upgrade-insecure-requests": {"1"},
		"sec-ch-ua":                 {`"Chromium";v="150", "Google Chrome";v="150", "Not.A/Brand";v="99"`},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {`"Windows"`},
		"sec-fetch-site":            {"none"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-user":            {"?1"},
		"sec-fetch-dest":            {"document"},
		http.HeaderOrderKey: {
			"accept",
			"accept-language",
			"user-agent",
			"upgrade-insecure-requests",
			"sec-ch-ua",
			"sec-ch-ua-mobile",
			"sec-ch-ua-platform",
			"sec-fetch-site",
			"sec-fetch-mode",
			"sec-fetch-user",
			"sec-fetch-dest",
			"if-none-match",
			"if-modified-since",
		},
	}
	if prev.ETag != "" {
		req.Header.Set("if-none-match", prev.ETag)
	}
	if prev.LastModified != "" {
		req.Header.Set("if-modified-since", prev.LastModified)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, Validators{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, Validators{}, err
	}

	switch {
	case resp.StatusCode == http.StatusNotModified && !prev.empty():
		return nil, prev, ErrNotModified
	case resp.StatusCode == http.StatusProxyAuthRequired:
		return nil, Validators{}, fmt.Errorf("%w: HTTP 407", ErrProxy)
	case resp.StatusCode == http.StatusForbidden:
		return nil, Validators{}, fmt.Errorf("%w (HTTP 403)", ErrBlocked)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, Validators{}, &statusError{status: resp.StatusCode}
	}
	if len(body) == 0 {
		return nil, Validators{}, fmt.Errorf("%w: pusta odpowiedź", ErrProxy)
	}
	if isWAFBlockBody(body) {
		return nil, Validators{}, fmt.Errorf("%w (tls-client)", ErrBlocked)
	}
	got := Validators{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
	return body, got, nil
}
