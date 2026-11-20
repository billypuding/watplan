package updater

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/wat"
)

func manualTestFetcher(t *testing.T, proxyHits, apiHits *atomic.Int64) (*Updater, *store.Store, config.PlanConfig) {
	t.Helper()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyHits.Add(1)
		w.Header().Set("X-Iinfo", "62-966453-0 0NNN")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Request unsuccessful. Incapsula incident ID: 1"))
	}))
	t.Cleanup(proxy.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiHits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(planHTML(2)))
	}))
	t.Cleanup(api.Close)

	t.Setenv("PLANWAT_PROXY", proxy.URL)
	t.Setenv("PLANWAT_PROXY_BACKUP", "")
	t.Setenv("PLANWAT_SCRAPER_PROVIDER", "scrapingant")
	t.Setenv("PLANWAT_SCRAPER_KEY", "test-key")
	t.Setenv("PLANWAT_SCRAPER_API_URL", api.URL)
	t.Setenv("PLANWAT_SCRAPER_PROXY_TYPE", "datacenter")
	t.Setenv("PLANWAT_SCRAPER_COUNTRY", "PL")
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")

	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fetcher, err := wat.NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	u := NewUpdater(st, fetcher)
	return u, st, config.PlanConfig{ID: "TST26XX1S0", URL: "http://example.test/plan", StartYear: 2026}
}

func TestUpdateManualFallsBackToAPI(t *testing.T) {
	var proxyHits, apiHits atomic.Int64
	u, st, cfg := manualTestFetcher(t, &proxyHits, &apiHits)

	if err := u.UpdateManual(cfg); err != nil {
		t.Fatalf("UpdateManual: %v", err)
	}
	if got := len(loadPlan(t, st, cfg.ID).Events); got != 2 {
		t.Fatalf("zdarzeń = %d, chcę 2", got)
	}
	if proxyHits.Load() != 1 {
		t.Fatalf("zapytań do proxy = %d, chcę 1", proxyHits.Load())
	}
	if apiHits.Load() != 1 {
		t.Fatalf("zapytań do API = %d, chcę 1", apiHits.Load())
	}
}

func TestUpdateBackgroundUsesAPIOnly(t *testing.T) {
	var proxyHits, apiHits atomic.Int64
	u, st, cfg := manualTestFetcher(t, &proxyHits, &apiHits)

	if err := u.Update(cfg); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := len(loadPlan(t, st, cfg.ID).Events); got != 2 {
		t.Fatalf("zdarzeń = %d, chcę 2", got)
	}
	if proxyHits.Load() != 0 {
		t.Fatalf("tło poszło przez proxy (%d), chcę 0", proxyHits.Load())
	}
	if apiHits.Load() != 1 {
		t.Fatalf("zapytań do API = %d, chcę 1", apiHits.Load())
	}
}
