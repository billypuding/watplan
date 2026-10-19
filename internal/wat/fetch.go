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
	"time"

	"golang.org/x/net/html/charset"
)

const (
	fetchTimeout   = 15 * time.Second
	maxBodyBytes   = 10 << 20
	fetchAttempts  = 3
	fetchRetryBase = 400 * time.Millisecond
	userAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"
)

type Fetcher struct {
	client *http.Client
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

	return &Fetcher{client: client}, nil
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
	var se *statusError
	if errors.As(err, &se) {
		switch se.status {
		case http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooManyRequests,
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
		return nil, &statusError{status: resp.StatusCode}
	}

	body, err := charset.NewReader(io.LimitReader(resp.Body, maxBodyBytes), resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(body)
}
