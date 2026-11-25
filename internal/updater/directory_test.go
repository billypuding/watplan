package updater

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/wat"
)

func shortTeacherHTML() string {
	return strings.ReplaceAll(planHTML(3), "<td>Przedmiot</td>", "<td>HP(w)<br>WLO26FW4S0; WLO26FW3S0<br>T.Szem<br>255 S</td>")
}

func TestResolveStoredTeachersUsesOtherPlans(t *testing.T) {
	var body atomic.Value
	body.Store(shortTeacherHTML())
	u, st, _ := newTestUpdater(t, &body)
	st.Save("WME26MX1S0", model.Plan{Events: []model.Event{{Teacher: "T.Szem", Subject: "HP"}, {Teacher: "M.KOCZ"}}, ModifiedAt: "2026-11-01 10:00:00"})
	st.Save("WCY26KY4S1", model.Plan{Events: []model.Event{{Teacher: "mgr Szemraj Tomasz", Subject: "HP"}}})
	u.ResolveStoredTeachers(context.Background())
	got := loadPlan(t, st, "WME26MX1S0")
	if got.Events[0].Teacher != "mgr Szemraj Tomasz" || got.Events[1].Teacher != "M.KOCZ" || got.ModifiedAt != "2026-11-01 10:00:00" {
		t.Fatalf("%+v", got)
	}
}

func TestUpdateResolvesTeachersWithoutFakeChange(t *testing.T) {
	var body atomic.Value
	body.Store(shortTeacherHTML())
	u, st, cfg := newTestUpdater(t, &body)
	u.clock = func() time.Time { return time.Date(2025, 12, 1, 12, 0, 0, 0, time.UTC) }
	var notified atomic.Int32
	u.OnChange(func(config.PlanConfig, model.Change) { notified.Add(1) })
	if err := u.Update(cfg); err != nil {
		t.Fatal(err)
	}
	if loadPlan(t, st, cfg.ID).Events[0].Teacher != "T.Szem" {
		t.Fatal("bez katalogu skrót powinien zostać")
	}
	st.Save("WCY26KY4S1", model.Plan{Events: []model.Event{{Teacher: "mgr Szemraj Tomasz"}}})
	u.dirMu.Lock()
	u.dir = nil
	u.dirMu.Unlock()
	if err := u.Update(cfg); err != nil {
		t.Fatal(err)
	}
	plan := loadPlan(t, st, cfg.ID)
	if plan.Events[0].Teacher != "mgr Szemraj Tomasz" {
		t.Fatalf("skrót nie rozwinięty: %q", plan.Events[0].Teacher)
	}
	if h, _ := st.History(cfg.ID); len(h) != 0 || notified.Load() != 0 {
		t.Fatalf("fałszywa zmiana: historia %d, powiadomień %d", len(h), notified.Load())
	}
	if plan.Parser != wat.ParserVersion {
		t.Fatal("brak wersji parsera")
	}
}
