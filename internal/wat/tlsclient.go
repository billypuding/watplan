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
	mu      sync.Mutex
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

	c := &tlsProxyClient{host: host, user: user, pass: pass}
	if err := c.rotate(); err != nil {
		return nil, err
	}

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

func (c *tlsProxyClient) proxyURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.proxyURLLocked()
}

func (c *tlsProxyClient) proxyURLLocked() string {
	login := proxyLogin(c.user) + "-sessid-" + c.session + "-sesstime-10"
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

func (c *tlsProxyClient) rotate() error {
	session, err := randomSession()
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.session = session
	proxy := c.proxyURLLocked()
	c.mu.Unlock()

	if c.client != nil {
		if err := c.client.SetProxy(proxy); err != nil {
			return err
		}
		c.client.SetCookieJar(tls_client.NewCookieJar())
	}
	return nil
}

func (c *tlsProxyClient) fetch(target string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		body, err := c.do(target)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if attempt == 1 || !retryNewSession(err) {
			break
		}
		if err := c.rotate(); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
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

func (c *tlsProxyClient) do(target string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
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
		},
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}

	switch {
	case resp.StatusCode == http.StatusProxyAuthRequired:
		return nil, fmt.Errorf("%w: HTTP 407", ErrProxy)
	case resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w (HTTP 403)", ErrBlocked)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, &statusError{status: resp.StatusCode}
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("%w: pusta odpowiedź", ErrProxy)
	}
	if isWAFBlockBody(body) {
		return nil, fmt.Errorf("%w (tls-client)", ErrBlocked)
	}
	return body, nil
}
