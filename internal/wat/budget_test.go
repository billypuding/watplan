package wat

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/alert"
)

func TestScraperBudgetStopsRequests(t *testing.T) {
	var calls atomic.Int64
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Ant-Credits-Cost", "1")
		w.Write([]byte("<html>plan</html>"))
	}))
	defer api.Close()
	t.Setenv("PLANWAT_PROXY", "")
	t.Setenv("PLANWAT_PROXY_HOST", "")
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	t.Setenv("PLANWAT_SCRAPER_PROVIDER", "scrapingant")
	t.Setenv("PLANWAT_SCRAPER_KEY", "test-key")
	t.Setenv("PLANWAT_SCRAPER_API_URL", api.URL)
	t.Setenv("PLANWAT_BUDGET_SCRAPER_DAILY", "2")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	alerts := make(chan string, 10)
	f.SetAlert(alert.NewFunc(func(text string) error { alerts <- text; return nil }))

	for i := 0; i < 2; i++ {
		if _, _, err := f.FetchPage("https://example.test/p.htm", Validators{}); err != nil {
			t.Fatalf("zapytanie %d: %v", i+1, err)
		}
	}
	if f.BudgetLeft(FetchAuto) {
		t.Fatal("BudgetLeft = true po wyczerpaniu limitu")
	}
	for i := 0; i < 3; i++ {
		if _, _, err := f.FetchPage("https://example.test/p.htm", Validators{}); !errors.Is(err, ErrBudget) {
			t.Fatalf("po limicie: err = %v, chcę ErrBudget", err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("zapytań do API = %d, chcę 2", got)
	}
	if b := f.Budget()["scrapingant"]; b.Used != 2 || b.Limit != 2 {
		t.Fatalf("Budget = %+v", b)
	}
	select {
	case a := <-alerts:
		if !strings.Contains(a, "limit") {
			t.Fatalf("alert = %q", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("brak alertu o limicie")
	}
	select {
	case a := <-alerts:
		t.Fatalf("drugi alert o tym samym limicie: %q", a)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestBudgetFromEnv(t *testing.T) {
	t.Setenv("X_BUDGET", "")
	if n, err := budgetFromEnv("X_BUDGET", 7); n != 7 || err != nil {
		t.Fatalf("domyślny: %d %v", n, err)
	}
	t.Setenv("X_BUDGET", "0")
	if n, err := budgetFromEnv("X_BUDGET", 7); n != 0 || err != nil {
		t.Fatalf("0: %d %v", n, err)
	}
	t.Setenv("X_BUDGET", "-3")
	if _, err := budgetFromEnv("X_BUDGET", 7); err == nil {
		t.Fatal("chcę błąd dla -3")
	}
}
