package wat

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type scraperConfig struct {
	provider  string
	apiKey    string
	proxyType string
	country   string
	browser   bool
	baseURL   string
}

func loadScraperConfig() (*scraperConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("PLANWAT_SCRAPER_PROVIDER")))
	if provider == "" {
		return nil, nil
	}

	apiKey := strings.TrimSpace(os.Getenv("PLANWAT_SCRAPER_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("PLANWAT_SCRAPER_PROVIDER ustawione, ale brak PLANWAT_SCRAPER_KEY")
	}

	cfg := &scraperConfig{
		provider:  provider,
		apiKey:    apiKey,
		proxyType: strings.ToLower(envOrDefault("PLANWAT_SCRAPER_PROXY_TYPE", "datacenter")),
		country:   strings.ToUpper(strings.TrimSpace(os.Getenv("PLANWAT_SCRAPER_COUNTRY"))),
		browser:   scraperBool(os.Getenv("PLANWAT_SCRAPER_BROWSER")),
	}

	switch provider {
	case "scrapingant":
		cfg.baseURL = envOrDefault("PLANWAT_SCRAPER_API_URL", "https://api.scrapingant.com/v2/general")
		if cfg.country == "" {
			cfg.country = "PL"
		}
		if cfg.proxyType != "datacenter" && cfg.proxyType != "residential" {
			return nil, fmt.Errorf("zły PLANWAT_SCRAPER_PROXY_TYPE %q (datacenter/residential)", cfg.proxyType)
		}
	case "scrapingbee":
		cfg.baseURL = envOrDefault("PLANWAT_SCRAPER_API_URL", "https://app.scrapingbee.com/api/v1/")
		if cfg.country == "" {
			cfg.country = "PL"
		}
	case "scraperapi":
		cfg.baseURL = envOrDefault("PLANWAT_SCRAPER_API_URL", "https://api.scraperapi.com")
		if cfg.country == "" {
			cfg.country = "EU"
		}
	default:
		return nil, fmt.Errorf("nieznany PLANWAT_SCRAPER_PROVIDER %q", provider)
	}

	return cfg, nil
}

func (c *scraperConfig) request(target string) (*http.Request, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()

	switch c.provider {
	case "scrapingant":
		q.Set("url", target)
		q.Set("browser", strconv.FormatBool(c.browser))
		q.Set("proxy_type", c.proxyType)
		if c.country != "" {
			q.Set("proxy_country", c.country)
		}
	case "scrapingbee":
		q.Set("url", target)
		q.Set("render_js", strconv.FormatBool(c.browser))
		if c.proxyType == "residential" {
			q.Set("premium_proxy", "true")
		}
		if c.country != "" {
			q.Set("country_code", strings.ToLower(c.country))
		}
	case "scraperapi":
		q.Set("api_key", c.apiKey)
		q.Set("url", target)
		if c.proxyType == "residential" {
			q.Set("premium", "true")
		}
		if c.country != "" {
			q.Set("country_code", strings.ToLower(c.country))
		}
	}

	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	switch c.provider {
	case "scrapingant":
		req.Header.Set("x-api-key", c.apiKey)
	case "scrapingbee":
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return req, nil
}

type redactedError struct {
	text string
	err  error
}

func (e *redactedError) Error() string { return e.text }

func (e *redactedError) Unwrap() error { return e.err }

func (c *scraperConfig) redact(err error) error {
	if err == nil || c.apiKey == "" {
		return err
	}
	text := err.Error()
	clean := strings.ReplaceAll(text, c.apiKey, "***")
	clean = strings.ReplaceAll(clean, url.QueryEscape(c.apiKey), "***")
	if clean == text {
		return err
	}
	return &redactedError{text: clean, err: err}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func scraperBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "tak", "on":
		return true
	}
	return false
}
