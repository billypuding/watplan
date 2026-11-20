package wat

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestScrapingAntGateway(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte("<html>plan</html>"))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("PLANWAT_SCRAPER_PROVIDER", "scrapingant")
	t.Setenv("PLANWAT_SCRAPER_KEY", "test-key")
	t.Setenv("PLANWAT_SCRAPER_API_URL", srv.URL)
	t.Setenv("PLANWAT_SCRAPER_PROXY_TYPE", "datacenter")
	t.Setenv("PLANWAT_SCRAPER_COUNTRY", "PL")
	t.Setenv("PLANWAT_SCRAPER_BROWSER", "false")
	t.Setenv("PLANWAT_PROXY", "")
	t.Setenv("PLANWAT_PROXY_BACKUP", "")
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")

	f, err := NewFetcher()
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}

	body, err := f.FetchHTML("https://wml.wat.edu.pl/plan.htm?grupa_id=WMT26AX3S1")
	if err != nil {
		t.Fatalf("FetchHTML: %v", err)
	}
	if !strings.Contains(string(body), "plan") {
		t.Fatalf("body = %q", body)
	}

	want := map[string]string{
		"url":           "https://wml.wat.edu.pl/plan.htm?grupa_id=WMT26AX3S1",
		"x-api-key":     "test-key",
		"browser":       "false",
		"proxy_type":    "datacenter",
		"proxy_country": "PL",
	}
	for key, value := range want {
		if got.Get(key) != value {
			t.Errorf("%s = %q, chcę %q", key, got.Get(key), value)
		}
	}
}

func TestScraperRequiresKey(t *testing.T) {
	t.Setenv("PLANWAT_SCRAPER_PROVIDER", "scrapingant")
	t.Setenv("PLANWAT_SCRAPER_KEY", "")
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")

	if _, err := NewFetcher(); err == nil {
		t.Fatal("NewFetcher bez klucza powinien zwrócić błąd")
	}
}
