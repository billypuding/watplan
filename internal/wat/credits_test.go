package wat

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/alert"
)

func TestCreditAlertsOncePerLevel(t *testing.T) {
	var remaining atomic.Int64
	var sawKey atomic.Value
	usage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawKey.Store(r.URL.Query().Get("x-api-key"))
		fmt.Fprintf(w, `{"plan_name":"free","plan_total_credits":10000,"remained_credits":%d}`, remaining.Load())
	}))
	defer usage.Close()
	t.Setenv("PLANWAT_PROXY", "")
	t.Setenv("PLANWAT_PROXY_HOST", "")
	t.Setenv("PLANWAT_SCRAPER_PROVIDER", "scrapingant")
	t.Setenv("PLANWAT_SCRAPER_KEY", "secret-key")
	t.Setenv("PLANWAT_SCRAPER_USAGE_URL", usage.URL)
	t.Setenv("PLANWAT_CREDITS_ALERT", "10,30")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	sent := make(chan string, 20)
	f.SetAlert(alert.NewFunc(func(text string) error { sent <- text; return nil }))
	drain := func() []string {
		var out []string
		for {
			select {
			case s := <-sent:
				out = append(out, s)
			case <-time.After(150 * time.Millisecond):
				return out
			}
		}
	}

	steps := []struct {
		remaining int
		want      []string
	}{
		{5000, nil},
		{2500, []string{"poniżej 30%"}},
		{2400, nil},
		{800, []string{"poniżej 10%"}},
		{700, nil},
		{6000, []string{"powyżej", "powyżej"}},
		{2500, []string{"poniżej 30%"}},
	}
	for i, step := range steps {
		remaining.Store(int64(step.remaining))
		f.CheckCredits()
		got := drain()
		if len(got) != len(step.want) {
			t.Fatalf("krok %d (%d kredytów): alerty %q, chcę %d", i+1, step.remaining, got, len(step.want))
		}
		for j, w := range step.want {
			if !strings.Contains(got[j], w) {
				t.Fatalf("krok %d: alert %q nie zawiera %q", i+1, got[j], w)
			}
		}
	}
	if sawKey.Load() != "secret-key" {
		t.Fatalf("klucz w zapytaniu = %v", sawKey.Load())
	}
	if c, ok := f.Credits(); !ok || c.Remaining != 2500 || c.Total != 10000 {
		t.Fatalf("Credits = %+v, %v", c, ok)
	}
}

func TestCreditWatchOnlyForScrapingAnt(t *testing.T) {
	t.Setenv("PLANWAT_PROXY", "")
	t.Setenv("PLANWAT_PROXY_HOST", "")
	t.Setenv("PLANWAT_SCRAPER_PROVIDER", "")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Credits(); ok || f.credits != nil {
		t.Fatal("bez ScrapingAnt nie powinno być sprawdzania kredytów")
	}
	t.Setenv("PLANWAT_SCRAPER_PROVIDER", "scrapingant")
	t.Setenv("PLANWAT_SCRAPER_KEY", "k")
	t.Setenv("PLANWAT_CREDITS_ALERT", "150")
	if _, err := NewFetcher(); err == nil {
		t.Fatal("chcę błąd dla progu 150%")
	}
}
