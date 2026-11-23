package store

import (
	"encoding/json"
	"sync"
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

func TestMetaSurvivesRestore(t *testing.T) {
	remote := durable.NewMemory()
	first, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first.SetMirror(remote)
	if err := first.SaveMeta("usage", []byte(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	if data, err := first.LoadMeta("usage"); err != nil || string(data) != `{"x":1}` {
		t.Fatalf("LoadMeta = %q, %v", data, err)
	}

	second, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	restored, uploaded, err := second.Restore(remote)
	if err != nil {
		t.Fatal(err)
	}
	if restored != 0 || uploaded != 0 {
		t.Fatalf("plik meta potraktowany jak plan: odtworzono %d, wysłano %d", restored, uploaded)
	}
	if data, err := second.LoadMeta("usage"); err != nil || string(data) != `{"x":1}` {
		t.Fatalf("po odtworzeniu LoadMeta = %q, %v", data, err)
	}
}

func TestPublisherAndPlanIDs(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	published := map[string]string{}
	st.SetPublisher(func(id string, data []byte) {
		mu.Lock()
		published[id] = string(data)
		mu.Unlock()
	})
	if err := st.Save("A1", model.Plan{FetchedAt: "2026-11-23 12:00:00"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveMeta("usage", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := st.AddHistory("A1", model.Change{}, 10); err != nil {
		t.Fatal(err)
	}
	raw, _ := st.Raw("A1")
	mu.Lock()
	if published["A1"] != string(raw) || len(published) != 1 {
		t.Fatalf("opublikowano %v", published)
	}
	mu.Unlock()
	if ids := st.PlanIDs(); len(ids) != 1 || ids[0] != "A1" {
		t.Fatalf("PlanIDs %v, chcę tylko A1 (bez .meta i .history.json)", ids)
	}
}
