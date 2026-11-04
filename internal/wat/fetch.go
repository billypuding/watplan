package wat

import (
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
)

const (
	fetchTimeout   = 15 * time.Second
	maxBodyBytes   = 10 << 20
	fetchAttempts  = 3
	fetchRetryBase = 400 * time.Millisecond
	// defaultMinInterval to minimalny odstęp między kolejnymi zapytaniami do
	// serwerów WAT (wspólny dla wszystkich workerów). Serwery stoją za WAF-em
	// (Incapsula), który blokuje IP po zbyt gęstym ruchu.
	defaultMinInterval = time.Second
	userAgent          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"
)

// ErrBlocked oznacza, że WAF (Incapsula) zablokował zapytanie. Ponawianie
// nic nie da i tylko przedłuża blokadę, więc wywołujący powinien przerwać
// bieżący cykl odświeżania.
var ErrBlocked = errors.New("zapytanie zablokowane przez WAF (Incapsula) - wstrzymuję pobieranie")

type Fetcher struct {
	client *http.Client

	minInterval time.Duration
	paceMu      sync.Mutex
	nextAt      time.Time
}

func NewFetcher() (*Fetcher, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()

	if proxyStr := os.Getenv("PLANWAT_PROXY"); proxyStr != "" {
		proxyURL, err := url.Parse(proxyStr)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy url: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	client := &http.Client{
		Timeout:   fetchTimeout,
		Jar:       jar,
		Transport: transport,
	}

	minInterval := defaultMinInterval
	if v := os.Getenv("PLANWAT_FETCH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("zły PLANWAT_FETCH_INTERVAL %q (np. 1s, 500ms)", v)
		}
		minInterval = d
	}

	return &Fetcher{client: client, minInterval: minInterval}, nil
}

// pace czeka, aż minie minInterval od poprzedniego zapytania (globalnie dla
// tego Fetchera, niezależnie od liczby workerów).
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

func (f *Fetcher) FetchHTML(targetUrl string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		body, err := f.fetchOnce(targetUrl)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if attempt == fetchAttempts || !retryable(err) {
			break
		}
		delay := fetchRetryBase * time.Duration(1<<uint(attempt-1))
		delay += time.Duration(rand.Int63n(int64(fetchRetryBase)))
		log.Printf("fetch %s: %v (próba %d/%d, ponawiam za %v)", targetUrl, err, attempt, fetchAttempts, delay.Round(time.Millisecond))
		time.Sleep(delay)
	}
	return nil, lastErr
}

type statusError struct {
	status int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("status code error: %d", e.status)
}

func retryable(err error) bool {
	if errors.Is(err, ErrBlocked) {
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

func (f *Fetcher) fetchOnce(targetUrl string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, targetUrl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "pl-PL,pl;q=0.9,en-US;q=0.8,en;q=0.7")

	f.pace()
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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
			return nil, fmt.Errorf("%w (status %d)", ErrBlocked, resp.StatusCode)
		}
		return nil, &statusError{status: resp.StatusCode}
	}

	body, err := charset.NewReader(io.LimitReader(resp.Body, maxBodyBytes), resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(body)
}

func isWAFBlock(resp *http.Response, snippet []byte) bool {
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	if resp.Header.Get("X-Iinfo") != "" {
		return true
	}
	return strings.Contains(string(snippet), "Incapsula")
}
