package updater

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/wat"
)

func stamp(d time.Duration) string {
	return time.Now().Add(-d).In(warsawLoc).Format(timeLayout)
}

func TestCheckDue(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name              string
		modified, checked time.Duration
		due               bool
	}{
		{"zmieniony 2 h temu, sprawdzony 15 min temu", 2 * time.Hour, 15 * time.Minute, true},
		{"zmieniony 2 h temu, sprawdzony 5 min temu", 2 * time.Hour, 5 * time.Minute, false},
		{"bez zmian 3 dni, sprawdzony 30 min temu", 72 * time.Hour, 30 * time.Minute, false},
		{"bez zmian 3 dni, sprawdzony 2 h temu", 72 * time.Hour, 2 * time.Hour, true},
		{"bez zmian 10 dni, sprawdzony 3 h temu", 240 * time.Hour, 3 * time.Hour, false},
		{"bez zmian 10 dni, sprawdzony 5 h temu", 240 * time.Hour, 5 * time.Hour, true},
	}
	for _, c := range cases {
		p := model.Plan{ModifiedAt: stamp(c.modified), CheckedAt: stamp(c.checked), FetchedAt: stamp(c.checked)}
		if got := checkDue(p, now); got != c.due {
			t.Errorf("%s: due = %v, chcę %v", c.name, got, c.due)
		}
	}
	if !checkDue(model.Plan{}, now) {
		t.Error("plan bez dat powinien być do sprawdzenia")
	}
}

func TestRefreshWatchedUsesAdaptiveInterval(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write([]byte(planHTML(3)))
	}))
	defer srv.Close()
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	fetcher, err := wat.NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	u := NewUpdater(st, fetcher)
	plans := []config.PlanConfig{
		{ID: "STALE10D", URL: srv.URL, StartYear: 2026},
		{ID: "FRESH10D", URL: srv.URL, StartYear: 2026},
		{ID: "CHANGED", URL: srv.URL, StartYear: 2026},
	}
	events := []model.Event{{Date: "2026-01-05", Start: "08:00", End: "09:35", Subject: "X"}}
	save := func(id string, modified, checked time.Duration) {
		if err := st.Save(id, model.Plan{Events: events, ModifiedAt: stamp(modified), CheckedAt: stamp(checked), FetchedAt: stamp(checked)}); err != nil {
			t.Fatal(err)
		}
	}
	save("STALE10D", 240*time.Hour, 5*time.Hour)
	save("FRESH10D", 240*time.Hour, 2*time.Hour)
	save("CHANGED", 2*time.Hour, 15*time.Minute)

	opts := MaintainOptions{FetchGap: time.Millisecond, Watched: func() []string { return []string{"STALE10D", "FRESH10D", "CHANGED"} }}.withDefaults()
	if !u.refreshWatched(context.Background(), config.NewRegistry(plans), opts) {
		t.Fatal("refreshWatched przerwane")
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("pobrań = %d, chcę 2 (STALE10D i CHANGED, bez FRESH10D)", got)
	}
}

func TestFetchHealthy(t *testing.T) {
	u := &Updater{started: time.Now()}
	if !u.FetchHealthy(time.Hour) {
		t.Fatal("bez prób pobierania ma być zdrowo")
	}
	u.recordFetch(fmt.Errorf("pobieranie: %w", wat.ErrGone))
	if u.lastFail.Load() != 0 {
		t.Fatal("404 grupy nie powinno liczyć się jako awaria pobierania")
	}
	u.lastOK.Store(time.Now().Add(-4 * time.Hour).UnixNano())
	u.recordFetch(errors.New("timeout"))
	if u.FetchHealthy(3 * time.Hour) {
		t.Fatal("błędy od 4 h bez sukcesu: chcę niezdrowo")
	}
	if !u.FetchHealthy(5 * time.Hour) {
		t.Fatal("przy progu 5 h jeszcze zdrowo")
	}
	u.recordFetch(nil)
	if !u.FetchHealthy(3 * time.Hour) {
		t.Fatal("po udanym pobraniu znów zdrowo")
	}
	fresh := &Updater{started: time.Now().Add(-4 * time.Hour)}
	fresh.recordFetch(errors.New("blokada"))
	if fresh.FetchHealthy(3 * time.Hour) {
		t.Fatal("same błędy od startu 4 h temu: chcę niezdrowo")
	}
}

func TestCacheOnlyDoesNotFetchFromScratch(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write([]byte(planHTML(3)))
	}))
	defer srv.Close()
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	fetcher, err := wat.NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	u := NewUpdater(st, fetcher)
	u.SetCacheOnly(true)
	plan := config.PlanConfig{ID: "NOWY", URL: srv.URL, StartYear: 2026}
	if err := u.Ensure(plan); !errors.Is(err, ErrCacheOnly) {
		t.Fatalf("Ensure: %v, chcę ErrCacheOnly", err)
	}
	opts := MaintainOptions{FetchGap: time.Millisecond}.withDefaults()
	if !u.fillMissing(context.Background(), config.NewRegistry([]config.PlanConfig{plan}), map[string]time.Time{}, opts) {
		t.Fatal("fillMissing przerwane")
	}
	if hits.Load() != 0 {
		t.Fatalf("pobrań = %d w trybie tylko cache, chcę 0", hits.Load())
	}
	u.SetCacheOnly(false)
	if err := u.Ensure(plan); err != nil || hits.Load() != 1 {
		t.Fatalf("po wyjściu z trybu: err = %v, pobrań = %d", err, hits.Load())
	}
}
