package wat

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

func TestUsageWindows(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 30, 0, 0, time.UTC)
	u := NewUsage()
	at := func(d time.Duration) { u.now = func() time.Time { return now.Add(-d) } }

	at(40 * 24 * time.Hour)
	u.Record("scrapingant", 1, 0)
	at(10 * 24 * time.Hour)
	u.Record("scrapingant", 1, 0)
	u.Record("scrapingant", 1, 0)
	at(2 * time.Hour)
	u.Record("scrapingant", 1, 0)
	u.Record("oxylabs", 0, 6000)
	at(0)
	u.Record("oxylabs", 0, 4000)

	s := u.Snapshot()
	if got := s["scrapingant"]; got.Requests24h != 1 || got.Requests30d != 3 || got.Credits24h != 1 || got.Credits30d != 3 {
		t.Fatalf("scrapingant = %+v", got)
	}
	if got := s["oxylabs"]; got.Requests24h != 2 || got.Requests30d != 2 || got.Bytes24h != 10000 {
		t.Fatalf("oxylabs = %+v", got)
	}
	for _, hours := range u.hours {
		for h := range hours {
			if h < now.Add(-usageKeep).Unix() {
				t.Fatal("wpisy starsze niż 31 dni nie zostały usunięte")
			}
		}
	}
}

func TestUsagePersistAndLoad(t *testing.T) {
	saved := make(chan []byte, 4)
	u := NewUsage()
	u.SetPersist(func(data []byte) { saved <- data })
	u.Record("scrapingant", 1, 0)
	data := <-saved

	restarted := NewUsage()
	if err := restarted.Load(data); err != nil {
		t.Fatal(err)
	}
	restarted.Record("scrapingant", 1, 0)
	if got := restarted.Snapshot()["scrapingant"]; got.Requests24h != 2 || got.Credits30d != 2 {
		t.Fatalf("po restarcie = %+v", got)
	}
}

func TestFetcherRecordsScraperCredits(t *testing.T) {
	var calls atomic.Int64
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Ant-Credits-Cost", "1")
		w.Header().Set("Ant-Page-Status-Code", "200")
		w.Write([]byte("<html><body>plan</body></html>"))
	}))
	defer api.Close()
	t.Setenv("PLANWAT_PROXY", "")
	t.Setenv("PLANWAT_PROXY_HOST", "")
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	t.Setenv("PLANWAT_SCRAPER_PROVIDER", "scrapingant")
	t.Setenv("PLANWAT_SCRAPER_KEY", "test-key")
	t.Setenv("PLANWAT_SCRAPER_API_URL", api.URL)
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := f.FetchPage("https://example.test/plan.htm", Validators{}); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.Usage().Snapshot()["scrapingant"]; got.Requests24h != 3 || got.Credits24h != 3 {
		t.Fatalf("scrapingant = %+v, chcę 3 zapytania i 3 kredyty", got)
	}
}

func TestTLSProxyRecordsUsage(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("<html>plan</html>"))
	}))
	defer target.Close()
	proxy := startTestProxy(t)
	client, err := tls_client.NewHttpClient(nil,
		tls_client.WithClientProfile(profiles.Chrome_150),
		tls_client.WithInsecureSkipVerify(),
		tls_client.WithProxyUrl("http://"+proxy),
		tls_client.WithBandwidthTracker(),
	)
	if err != nil {
		t.Fatal(err)
	}
	c := &tlsProxyClient{client: client, host: "pr.oxylabs.io:7777", usage: NewUsage()}
	for i := 0; i < 2; i++ {
		if _, _, err := c.fetch(target.URL, Validators{}); err != nil {
			t.Fatal(err)
		}
	}
	got := c.usage.Snapshot()["oxylabs"]
	if got.Requests24h != 2 || got.Bytes24h <= 0 {
		t.Fatalf("oxylabs = %+v, chcę 2 zapytania i niezerowe bajty", got)
	}
}
