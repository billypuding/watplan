package wat

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"time"

	"golang.org/x/net/html/charset"
)

const (
	fetchTimeout = 15 * time.Second
	maxBodyBytes = 10 << 20
	userAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"
)

func FetchHTML(targetUrl string) ([]byte, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	client := http.Client{
		Timeout: fetchTimeout,
		Jar:     jar,
	}

	if proxyStr := os.Getenv("PLANWAT_PROXY"); proxyStr != "" {
		if proxyURL, err := url.Parse(proxyStr); err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			}
		}
	}

	req, err := http.NewRequest(http.MethodGet, targetUrl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "pl-PL,pl;q=0.9,en-US;q=0.8,en;q=0.7")

	resp, err := client.Do(req)
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
		return nil, fmt.Errorf("status code error: %d", resp.StatusCode)
	}

	body, err := charset.NewReader(io.LimitReader(resp.Body, maxBodyBytes), resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(body)
}
