package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/wat"
)

func TestUpdateAllEmpty(t *testing.T) {
	u := NewUpdater(nil, nil)
	if err := u.UpdateAll(context.Background(), nil); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
}

func TestDiscoverDropsPlansFromRemovedSources(t *testing.T) {
	u := NewUpdater(nil, nil)
	static := []config.PlanConfig{
		{ID: "WME22MC1S0", Faculty: "WIM"},
		{ID: "WCY22AB1S0", Faculty: "WCY"},
	}

	got := u.Discover(nil, static, nil)
	if len(got) != 0 {
		t.Fatalf("Discover = %d planów, chcę 0", len(got))
	}
}

func TestSuspiciousDrop(t *testing.T) {
	cases := []struct {
		name     string
		oldCount int
		newCount int
		want     bool
	}{
		{"bez zmian", 100, 100, false},
		{"niewielki spadek", 100, 60, false},
		{"spadek o połowę", 100, 50, false},
		{"spadek poniżej połowy", 100, 49, true},
		{"pusty plan", 100, 0, true},
		{"mały plan bez ochrony", 7, 1, false},
		{"próg działa od 8", 8, 3, true},
		{"duży plan stabilny", 8, 4, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := suspiciousDrop(c.oldCount, c.newCount); got != c.want {
				t.Errorf("suspiciousDrop(%d, %d) = %v, chcę %v", c.oldCount, c.newCount, got, c.want)
			}
		})
	}
}

var romanMonths = []string{"", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII"}

func planHTML(events int) string {
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

	var b strings.Builder
	b.WriteString("<table><tr><td>pon.</td><td>&nbsp</td>")
	for i := 0; i < events; i++ {
		d := start.AddDate(0, 0, 7*i)
		fmt.Fprintf(&b, "<td>%02d %s</td>", d.Day(), romanMonths[int(d.Month())])
	}
	b.WriteString("<td>&nbsp</td><td>&nbsp</td></tr>")
	b.WriteString("<tr><td>pon.</td><td>1-2</td>")
	for i := 0; i < events; i++ {
		b.WriteString("<td>Przedmiot</td>")
	}
	b.WriteString("<td>&nbsp</td><td>&nbsp</td></tr></table>")
	return b.String()
}

func newTestUpdater(t *testing.T, body *atomic.Value) (*Updater, *store.Store, config.PlanConfig) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, body.Load().(string))
	}))
	t.Cleanup(srv.Close)

	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	fetcher, err := wat.NewFetcher()
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}
	return NewUpdater(st, fetcher), st, config.PlanConfig{ID: "TST26XX1S0", URL: srv.URL, StartYear: 2026}
}

func loadPlan(t *testing.T, st *store.Store, id string) model.Plan {
	t.Helper()
	plan, err := st.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return plan
}

func TestSuspiciousDropNeedsConfirmation(t *testing.T) {
	var body atomic.Value
	body.Store(planHTML(10))
	u, st, cfg := newTestUpdater(t, &body)

	if err := u.Update(cfg); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := len(loadPlan(t, st, cfg.ID).Events); got != 10 {
		t.Fatalf("zdarzeń = %d, chcę 10", got)
	}

	body.Store(planHTML(4))
	for i := 1; i < dropConfirmations; i++ {
		if err := u.Update(cfg); err != nil {
			t.Fatalf("Update %d: %v", i, err)
		}
		plan := loadPlan(t, st, cfg.ID)
		if len(plan.Events) != 10 {
			t.Fatalf("próba %d: zdarzeń = %d, chcę 10 (stare dane)", i, len(plan.Events))
		}
		if plan.DropStreak != i || plan.DropCount != 4 {
			t.Fatalf("próba %d: licznik spadku = %d/%d, chcę %d/4", i, plan.DropStreak, plan.DropCount, i)
		}
	}

	if err := u.Update(cfg); err != nil {
		t.Fatalf("Update potwierdzający: %v", err)
	}
	plan := loadPlan(t, st, cfg.ID)
	if len(plan.Events) != 4 {
		t.Fatalf("zdarzeń = %d, chcę 4", len(plan.Events))
	}
	if plan.DropStreak != 0 || plan.DropCount != 0 {
		t.Fatalf("licznik spadku = %d/%d, chcę wyczyszczonego", plan.DropStreak, plan.DropCount)
	}
}

func TestSuspiciousDropResetsOnRecovery(t *testing.T) {
	var body atomic.Value
	body.Store(planHTML(10))
	u, st, cfg := newTestUpdater(t, &body)

	if err := u.Update(cfg); err != nil {
		t.Fatalf("Update: %v", err)
	}

	body.Store(planHTML(4))
	if err := u.Update(cfg); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := loadPlan(t, st, cfg.ID).DropStreak; got != 1 {
		t.Fatalf("licznik spadku = %d, chcę 1", got)
	}

	body.Store(planHTML(10))
	if err := u.Update(cfg); err != nil {
		t.Fatalf("Update: %v", err)
	}
	plan := loadPlan(t, st, cfg.ID)
	if len(plan.Events) != 10 {
		t.Fatalf("zdarzeń = %d, chcę 10", len(plan.Events))
	}
	if plan.DropStreak != 0 || plan.DropCount != 0 {
		t.Fatalf("licznik spadku = %d/%d, chcę wyczyszczonego", plan.DropStreak, plan.DropCount)
	}
}
