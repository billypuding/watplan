package store

import (
	"encoding/json"
	"testing"

	"github.com/billypuding/watplan/internal/durable"
	"github.com/billypuding/watplan/internal/model"
)

func TestRestoreBringsBackNewerPlansAndHistory(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Save("A", model.Plan{FetchedAt: "2026-11-19 10:00:00", Events: []model.Event{{Date: "2026-11-20"}}})
	_ = st.Save("C", model.Plan{FetchedAt: "2026-11-19 10:00:00"})

	remote := durable.NewMemory()
	newer, _ := json.Marshal(model.Plan{FetchedAt: "2026-11-19 12:00:00", Events: []model.Event{{Date: "2026-11-21"}}})
	onlyRemote, _ := json.Marshal(model.Plan{FetchedAt: "2026-11-18 09:00:00"})
	history, _ := json.Marshal([]model.Change{{At: "2026-11-19 12:00:00"}})
	_ = remote.Put("plan/A", newer)
	_ = remote.Put("plan/B", onlyRemote)
	_ = remote.Put("history/A", history)

	restored, uploaded, err := st.Restore(remote)
	if err != nil {
		t.Fatal(err)
	}
	if restored != 2 || uploaded != 1 {
		t.Fatalf("odtworzono %d, wysłano %d", restored, uploaded)
	}
	if a, _ := st.Load("A"); a.FetchedAt != "2026-11-19 12:00:00" {
		t.Fatalf("A nie zostało podmienione na nowsze: %+v", a)
	}
	if _, err := st.Load("B"); err != nil {
		t.Fatalf("B nie zostało odtworzone: %v", err)
	}
	if h, _ := st.History("A"); len(h) != 1 {
		t.Fatalf("historia A: %+v", h)
	}
	if all, _ := remote.All("plan/"); all["plan/C"] == nil {
		t.Fatal("lokalny plan C nie trafił do bazy")
	}

	st.SetMirror(remote)
	_ = st.Save("D", model.Plan{FetchedAt: "2026-11-19 13:00:00"})
	_ = st.AddHistory("D", model.Change{At: "2026-11-19 13:00:00"}, 10)
	all, _ := remote.All("")
	if all["plan/D"] == nil || all["history/D"] == nil {
		t.Fatalf("zapis nie trafił do bazy: %v", keys(all))
	}
}

func TestRestoreKeepsNewerLocalPlan(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	_ = st.Save("A", model.Plan{FetchedAt: "2026-11-19 15:00:00"})
	remote := durable.NewMemory()
	older, _ := json.Marshal(model.Plan{FetchedAt: "2026-11-19 09:00:00"})
	_ = remote.Put("plan/A", older)
	if restored, _, _ := st.Restore(remote); restored != 0 {
		t.Fatalf("starsza kopia z bazy nadpisała lokalną")
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
