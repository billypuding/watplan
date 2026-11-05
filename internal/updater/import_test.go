package updater

import (
	"os"
	"testing"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/store"
)

func TestImportPlanFromHTML(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	u := NewUpdater(st, nil)

	f, err := os.Open("../wat/testdata/plan_wat.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	id := "WMT26AX3S1"
	n, err := u.Import(config.PlanConfig{ID: id, StartYear: 2026}, f)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if n == 0 {
		t.Fatal("Import zwrócił 0 zajęć")
	}

	plan := loadPlan(t, st, id)
	if len(plan.Events) != n {
		t.Fatalf("zapisanych zajęć = %d, chcę %d", len(plan.Events), n)
	}
	if plan.FetchedAt == "" || plan.ModifiedAt == "" {
		t.Fatalf("brak dat w planie: fetched=%q modified=%q", plan.FetchedAt, plan.ModifiedAt)
	}
}
