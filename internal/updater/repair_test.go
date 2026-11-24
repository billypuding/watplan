package updater

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/wat"
)

func wloFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../wat/testdata/plan_wlo.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func misparsedWLO() model.Plan {
	return model.Plan{
		FetchedAt:  "2026-11-20 10:00:00",
		ModifiedAt: "2026-11-01 08:00:00",
		ETag:       `"stary"`,
		Events: []model.Event{
			{Date: "2026-10-01", Start: "08:00", End: "09:35", Subject: "ZCzajko", Teacher: "WF(ć)", Room: "2PL/21KP ; WLO26FW4S0 ; WLO26FW5S0"},
			{Date: "2026-10-01", Start: "11:40", End: "13:15", Subject: "OleM", Teacher: "TW(w)", Room: "WLO26FW4S0, 111 58, T1W"},
		},
	}
}

func TestMisparsedDetection(t *testing.T) {
	if !wat.Misparsed(misparsedWLO().Events) {
		t.Fatal("zepsuty plan WLO nie został rozpoznany")
	}
	ok := []model.Event{{Room: "111 58"}, {Room: "aula/ 135"}, {Room: "128 K ; 127 K"}, {Room: "032 S"}, {Room: "AW 3/038"}}
	if wat.Misparsed(ok) {
		t.Fatal("poprawne sale uznane za zepsute")
	}
}

func TestReparseIsSilentAndFetchesFullPage(t *testing.T) {
	var body atomic.Value
	body.Store(wloFixture(t))
	u, st, cfg := newTestUpdater(t, &body)
	cfg.ID = "WLO26FW4S0"
	if err := st.Save(cfg.ID, misparsedWLO()); err != nil {
		t.Fatal(err)
	}
	var notified atomic.Int32
	u.OnChange(func(config.PlanConfig, model.Change) { notified.Add(1) })

	if err := u.Update(cfg); err != nil {
		t.Fatal(err)
	}
	plan := loadPlan(t, st, cfg.ID)
	if plan.Parser != wat.ParserVersion || wat.Misparsed(plan.Events) || len(plan.Events) < 250 {
		t.Fatalf("po naprawie: parser %d, zepsuty %v, zajęć %d", plan.Parser, wat.Misparsed(plan.Events), len(plan.Events))
	}
	if plan.ModifiedAt != "2026-11-01 08:00:00" {
		t.Fatalf("data zmiany przesunięta na %s, choć WAT nic nie zmienił", plan.ModifiedAt)
	}
	if h, _ := st.History(cfg.ID); len(h) != 0 {
		t.Fatalf("naprawa odczytu zapisała historię: %d wpisów", len(h))
	}
	time.Sleep(50 * time.Millisecond)
	if notified.Load() != 0 {
		t.Fatal("naprawa odczytu wysłała powiadomienie")
	}
}

func TestNormalChangeAfterRepairStillNotifies(t *testing.T) {
	var body atomic.Value
	body.Store(planHTML(10))
	u, st, cfg := newTestUpdater(t, &body)
	u.clock = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }
	if err := u.Update(cfg); err != nil {
		t.Fatal(err)
	}
	if p := loadPlan(t, st, cfg.ID); p.Parser != wat.ParserVersion {
		t.Fatalf("nowy plan bez wersji parsera: %d", p.Parser)
	}
	body.Store(planHTML(9))
	if err := u.Update(cfg); err != nil {
		t.Fatal(err)
	}
	if h, _ := st.History(cfg.ID); len(h) != 1 {
		t.Fatalf("zwykła zmiana bez historii: %d wpisów", len(h))
	}
}

func TestRepairMisparsedFixesOnlyBrokenPlans(t *testing.T) {
	var body atomic.Value
	body.Store(wloFixture(t))
	u, st, cfg := newTestUpdater(t, &body)
	broken := config.PlanConfig{ID: "WLO26FW4S0", URL: cfg.URL, StartYear: 2026}
	fine := config.PlanConfig{ID: "WML26AX1S1", URL: cfg.URL + "/inny", StartYear: 2026}
	st.Save(broken.ID, misparsedWLO())
	st.Save(fine.ID, model.Plan{FetchedAt: "2026-11-20 10:00:00", Events: []model.Event{{Date: "2026-10-01", Start: "08:00", Subject: "Mat", Room: "19 36"}}})
	reg := config.NewRegistry([]config.PlanConfig{broken, fine})
	if got := u.misparsedPlans(reg); len(got) != 1 || got[0].ID != broken.ID {
		t.Fatalf("do naprawy: %+v", got)
	}
	u.RepairMisparsed(context.Background(), reg, time.Millisecond, time.Millisecond)
	if p := loadPlan(t, st, broken.ID); wat.Misparsed(p.Events) {
		t.Fatal("plan nie został naprawiony")
	}
	if p := loadPlan(t, st, fine.ID); p.FetchedAt != "2026-11-20 10:00:00" {
		t.Fatal("poprawny plan został pobrany ponownie")
	}
	if len(u.misparsedPlans(reg)) != 0 {
		t.Fatal("po naprawie nadal są plany do naprawy")
	}
}

func TestRepairRetriesFailedPlansLater(t *testing.T) {
	var body atomic.Value
	body.Store("<html>chwilowo zła strona</html>")
	u, st, cfg := newTestUpdater(t, &body)
	broken := config.PlanConfig{ID: "WLO26FW4S0", URL: cfg.URL, StartYear: 2026}
	st.Save(broken.ID, misparsedWLO())
	reg := config.NewRegistry([]config.PlanConfig{broken})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		u.RepairMisparsed(ctx, reg, time.Millisecond, 20*time.Millisecond)
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	if !wat.Misparsed(loadPlan(t, st, broken.ID).Events) {
		t.Fatal("zepsuta strona nie powinna naprawić planu")
	}
	body.Store(wloFixture(t))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("naprawa nie ponowiła nieudanego planu")
	}
	if wat.Misparsed(loadPlan(t, st, broken.ID).Events) {
		t.Fatal("plan nadal zepsuty po ponowieniu")
	}
}
