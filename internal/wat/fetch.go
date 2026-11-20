package wat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html/charset"

	"github.com/billypuding/watplan/internal/alert"
)

const (
	fetchTimeout   = 15 * time.Second
	maxBodyBytes   = 10 << 20
	fetchAttempts  = 3
	fetchRetryBase = 400 * time.Millisecond

	defaultMinInterval = time.Second
	userAgent          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"
)

var ErrBlocked = errors.New("zapytanie zablokowane przez WAF (Incapsula) - wstrzymuję pobieranie")

var ErrProxy = errors.New("serwer proxy (PLANWAT_PROXY) odrzucił połączenie")

var ErrNotModified = errors.New("strona bez zmian od ostatniego pobrania")

var ErrGone = errors.New("strony już nie ma (404/410)")

type Validators struct {
	ETag         string
	LastModified string
}

func (v Validators) empty() bool {
	return v.ETag == "" && v.LastModified == ""
}

type proxyKey struct{}

type Fetcher struct {
	client *http.Client

	minInterval time.Duration
	paceMu      sync.Mutex
	nextAt      time.Time

	blockBackoff time.Duration
	blockMu      sync.Mutex
	blockedUntil map[string]time.Time

	listMu     sync.Mutex
	listings   map[string]listing
	staleUntil map[string]time.Time

	alert *alert.Notifier

	scraper       *scraperConfig
	scraperClient *http.Client
	scraperMu     sync.Mutex

	proxyMu    sync.Mutex
	proxies    []*url.URL
	active     int
	proxyBad   []time.Time
	proxyPause time.Duration
	proxyError string
	proxyAt    time.Time
}

func (f *Fetcher) SetAlert(n *alert.Notifier) {
	f.alert = n
}

func (f *Fetcher) ProxyError() (string, time.Time) {
	f.proxyMu.Lock()
	defer f.proxyMu.Unlock()
	return f.proxyError, f.proxyAt
}

const defaultBlockBackoff = time.Hour

func NewFetcher() (*Fetcher, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()

	var proxies []*url.URL
	for _, name := range []string{"PLANWAT_PROXY", "PLANWAT_PROXY_BACKUP"} {
		raw := os.Getenv(name)
		if raw == "" {
			continue
		}
		proxyURL, err := url.Parse(raw)
		if err != nil || proxyURL.Host == "" {
			return nil, fmt.Errorf("zły adres proxy w %s", name)
		}
		proxies = append(proxies, proxyURL)
		log.Printf("proxy %s (%s)", proxyURL.Host, name)
	}

	scraper, err := loadScraperConfig()
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout:   fetchTimeout,
		Jar:       jar,
		Transport: transport,
	}

	var scraperClient *http.Client
	if scraper != nil {
		log.Printf("pobieranie w tle przez %s (proxy_type=%s, country=%s, browser=%t)", scraper.provider, scraper.proxyType, scraper.country, scraper.browser)
		if len(proxies) > 0 {
			log.Printf("ręczne pobieranie przez proxy %s", proxies[0].Host)
		}
		scraperTransport := http.DefaultTransport.(*http.Transport).Clone()
		scraperTransport.Proxy = nil
		scraperClient = &http.Client{
			Timeout:   60 * time.Second,
			Jar:       jar,
			Transport: scraperTransport,
		}
	}

	minInterval := defaultMinInterval
	if v := os.Getenv("PLANWAT_FETCH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("zły PLANWAT_FETCH_INTERVAL %q (np. 1s, 500ms)", v)
		}
		minInterval = d
	}

	blockBackoff := defaultBlockBackoff
	if v := os.Getenv("PLANWAT_BLOCK_BACKOFF"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("zły PLANWAT_BLOCK_BACKOFF %q (np. 1h, 30m)", v)
		}
		blockBackoff = d
	}

	proxyPause := defaultProxyPause
	if v := os.Getenv("PLANWAT_PROXY_BLOCK_BACKOFF"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("zły PLANWAT_PROXY_BLOCK_BACKOFF %q (np. 30m, 1h)", v)
		}
		proxyPause = d
	}

	f := &Fetcher{
		client:        client,
		minInterval:   minInterval,
		blockBackoff:  blockBackoff,
		blockedUntil:  make(map[string]time.Time),
		scraper:       scraper,
		scraperClient: scraperClient,
		proxies:       proxies,
		proxyBad:      make([]time.Time, len(proxies)),
		proxyPause:    proxyPause,
	}
	if len(proxies) > 0 {
		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			if p, ok := req.Context().Value(proxyKey{}).(*url.URL); ok {
				return p, nil
			}
			return f.currentProxy(), nil
		}
	}
	return f, nil
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func (f *Fetcher) hostBlocked(host string) (time.Time, bool) {
	f.blockMu.Lock()
	defer f.blockMu.Unlock()
	until, ok := f.blockedUntil[host]
	if !ok {
		return time.Time{}, false
	}
	if time.Now().After(until) {
		delete(f.blockedUntil, host)
		return time.Time{}, false
	}
	return until, true
}

func (f *Fetcher) markBlocked(host string) {
	if host == "" || f.blockBackoff <= 0 {
		return
	}
	until := time.Now().Add(f.blockBackoff)
	f.blockMu.Lock()
	f.blockedUntil[host] = until
	f.blockMu.Unlock()
	log.Printf("WAF blokuje %s - wstrzymuję zapytania do tej domeny do %s", host, until.Format("15:04"))
	f.alert.Problem("waf:"+host, fmt.Sprintf("WAT (Incapsula) blokuje pobieranie z %s. Serwer ponawia co godzinę i da znać, gdy blokada zejdzie.", host))
}

func (f *Fetcher) BlockedHosts() map[string]time.Time {
	f.blockMu.Lock()
	defer f.blockMu.Unlock()
	out := make(map[string]time.Time, len(f.blockedUntil))
	now := time.Now()
	for h, until := range f.blockedUntil {
		if now.Before(until) {
			out[h] = until
		}
	}
	return out
}

func (f *Fetcher) pace() {
	if f.minInterval <= 0 {
		return
	}
	f.paceMu.Lock()
	now := time.Now()
	wait := f.nextAt.Sub(now)
	if wait < 0 {
		wait = 0
	}
	f.nextAt = now.Add(wait + f.minInterval)
	f.paceMu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

type FetchMode int

const (
	FetchAuto FetchMode = iota
	FetchViaProxy
)

func (f *Fetcher) usesScraper(mode FetchMode) bool {
	return f.scraper != nil && mode != FetchViaProxy
}

func (f *Fetcher) FetchHTML(targetUrl string) ([]byte, error) {
	body, _, err := f.FetchPage(targetUrl, Validators{})
	return body, err
}

func (f *Fetcher) Blocked(targetUrl string) bool {
	if f.scraper == nil {
		if _, paused := f.proxyPaused(); paused {
			return true
		}
	}
	_, blocked := f.hostBlocked(hostOf(targetUrl))
	return blocked
}

func (f *Fetcher) FetchPage(targetUrl string, prev Validators) ([]byte, Validators, error) {
	return f.fetchPage(targetUrl, prev, FetchAuto)
}

func (f *Fetcher) FetchPageViaProxy(targetUrl string, prev Validators) ([]byte, Validators, error) {
	return f.fetchPage(targetUrl, prev, FetchViaProxy)
}

func (f *Fetcher) fetchPage(targetUrl string, prev Validators, mode FetchMode) ([]byte, Validators, error) {
	host := hostOf(targetUrl)
	if !f.usesScraper(mode) {
		if until, paused := f.proxyPaused(); paused {
			status, _ := f.ProxyError()
			return nil, Validators{}, fmt.Errorf("%w: %s (pobieranie wstrzymane do %s)", ErrProxy, status, until.Format("15:04"))
		}
	}
	if mode != FetchViaProxy {
		if until, blocked := f.hostBlocked(host); blocked {
			return nil, Validators{}, fmt.Errorf("%w (%s wstrzymane do %s)", ErrBlocked, host, until.Format("15:04"))
		}
	}

	var lastErr error
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		body, got, err := f.fetchOnce(targetUrl, prev, mode)
		if err == nil {
			return body, got, nil
		}
		lastErr = err
		if errors.Is(err, ErrNotModified) {
			return nil, prev, err
		}
		if errors.Is(err, errProxySwitched) {
			continue
		}
		if mode == FetchViaProxy && isTimeout(err) {
			break
		}
		if errors.Is(err, ErrBlocked) {
			if mode != FetchViaProxy {
				f.markBlocked(host)
			}
			break
		}
		if attempt == fetchAttempts || !retryable(err) {
			break
		}
		delay := fetchRetryBase * time.Duration(1<<uint(attempt-1))
		delay += time.Duration(rand.Int63n(int64(fetchRetryBase)))
		log.Printf("fetch %s: %v (próba %d/%d, ponawiam za %v)", targetUrl, err, attempt, fetchAttempts, delay.Round(time.Millisecond))
		time.Sleep(delay)
	}
	return nil, Validators{}, lastErr
}

type statusError struct {
	status int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("status code error: %d", e.status)
}

func (e *statusError) Is(target error) bool {
	return target == ErrGone && (e.status == http.StatusNotFound || e.status == http.StatusGone)
}

func (f *Fetcher) proxyRejection(err error) (string, bool) {
	if len(f.proxies) == 0 {
		return "", false
	}
	var ue *url.Error
	if !errors.As(err, &ue) || ue.Err == nil {
		return "", false
	}
	text := ue.Err.Error()
	for code := 400; code < 600; code++ {
		if st := http.StatusText(code); st != "" && st == text {
			return fmt.Sprintf("%d %s", code, st), true
		}
	}
	if strings.Contains(text, "proxyconnect") {
		return "brak połączenia z proxy", true
	}
	return "", false
}

func retryable(err error) bool {
	if errors.Is(err, ErrBlocked) || errors.Is(err, ErrProxy) || errors.Is(err, ErrNotModified) {
		return false
	}
	var se *statusError
	if errors.As(err, &se) {
		switch se.status {
		case http.StatusRequestTimeout, http.StatusTooManyRequests,
			http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
		return false
	}
	return true
}

func (f *Fetcher) fetchOnce(targetUrl string, prev Validators, mode FetchMode) ([]byte, Validators, error) {
	body, err := f.fetchBody(targetUrl, prev, mode)
	return body.data, body.validators, err
}

type fetched struct {
	data       []byte
	validators Validators
}

func withoutRequestURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}

func (f *Fetcher) fetchBody(targetUrl string, prev Validators, mode FetchMode) (fetched, error) {
	useScraper := f.usesScraper(mode)
	var (
		req *http.Request
		err error
	)
	if useScraper {
		req, err = f.scraper.request(targetUrl)
	} else {
		req, err = http.NewRequest(http.MethodGet, targetUrl, nil)
	}
	if err != nil {
		return fetched{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "pl-PL,pl;q=0.9,en-US;q=0.8,en;q=0.7")
	if !useScraper {
		if prev.ETag != "" {
			req.Header.Set("If-None-Match", prev.ETag)
		}
		if prev.LastModified != "" {
			req.Header.Set("If-Modified-Since", prev.LastModified)
		}
	}

	f.pace()
	var used *url.URL
	if !useScraper {
		used = f.currentProxy()
		if used != nil {
			req = req.WithContext(context.WithValue(req.Context(), proxyKey{}, used))
		}
	}
	if useScraper {
		f.scraperMu.Lock()
		defer f.scraperMu.Unlock()
	}
	httpClient := f.client
	if useScraper {
		httpClient = f.scraperClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if used != nil {
			if status, ok := f.proxyRejection(err); ok {
				return fetched{}, f.proxyFailed(used, status)
			}
		}
		if useScraper {
			return fetched{}, fmt.Errorf("scraper %s: %w", f.scraper.provider, f.scraper.redact(withoutRequestURL(err)))
		}
		if used != nil && isTimeout(err) {
			return fetched{}, f.proxyTimedOut(used, err)
		}
		return fetched{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && !prev.empty() {
		if used != nil {
			f.proxyWorked(used)
		}
		return fetched{validators: prev}, ErrNotModified
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		log.Printf(
			"fetch %s: status=%d server=%q content-type=%q body=%q",
			targetUrl, resp.StatusCode,
			resp.Header.Get("Server"),
			resp.Header.Get("Content-Type"),
			snippet,
		)
		if isWAFBlock(resp, snippet) {
			if used != nil {
				return fetched{}, f.proxyBlockedByWAF(used, fmt.Sprintf("HTTP %d", resp.StatusCode))
			}
			return fetched{}, fmt.Errorf("%w (status %d)", ErrBlocked, resp.StatusCode)
		}
		if used != nil {
			f.proxyWorked(used)
		}
		return fetched{}, &statusError{status: resp.StatusCode}
	}

	got := Validators{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return fetched{}, err
	}
	if len(raw) == 0 || bytes.HasPrefix(raw, []byte("%PDF")) {
		return fetched{data: raw, validators: got}, nil
	}
	reader, err := charset.NewReader(bytes.NewReader(raw), resp.Header.Get("Content-Type"))
	if err != nil {
		return fetched{}, err
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return fetched{}, err
	}

	if isWAFBlockBody(body) {
		log.Printf("fetch %s: strona blokady WAF (status %d)", targetUrl, resp.StatusCode)
		if used != nil {
			return fetched{}, f.proxyBlockedByWAF(used, "strona blokady")
		}
		return fetched{}, fmt.Errorf("%w (status %d)", ErrBlocked, resp.StatusCode)
	}
	if used != nil {
		f.proxyWorked(used)
	}
	f.alert.Resolved("waf:"+hostOf(targetUrl), "WAT znów wpuszcza pobieranie z "+hostOf(targetUrl)+".")
	return fetched{data: body, validators: got}, nil
}

func isWAFBlock(resp *http.Response, snippet []byte) bool {
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
	default:
		return false
	}
	if resp.Header.Get("X-Iinfo") != "" {
		return true
	}
	return isWAFBlockBody(snippet)
}

func isWAFBlockBody(body []byte) bool {
	const maxInspect = 64 << 10
	if len(body) > maxInspect {
		body = body[:maxInspect]
	}
	text := strings.ToLower(string(body))
	return strings.Contains(text, "incapsula incident id") ||
		strings.Contains(text, "request unsuccessful. incapsula") ||
		(strings.Contains(text, "access denied") && strings.Contains(text, "error 17"))
}
