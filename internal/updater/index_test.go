package updater

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/wat"
)

type indexEnv struct {
	u          *Updater
	st         *store.Store
	srv        *httptest.Server
	indexHits  atomic.Int64
	planHits   sync.Map
	indexMode  atomic.Value
	indexDate  atomic.Value
	planEvents atomic.Int64
}

func newIndexEnv(t *testing.T) *indexEnv {
	t.Helper()
	e := &indexEnv{}
	e.indexMode.Store("date")
	e.indexDate.Store(time.Now().Add(-48 * time.Hour))
	e.planEvents.Store(12)
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/index.xml") {
			e.indexHits.Add(1)
			switch e.indexMode.Load().(string) {
			case "fail":
				w.WriteHeader(http.StatusInternalServerError)
				return
			case "nodate":
				w.Write([]byte(`<?xml version="1.0"?><xml><data></data></xml>`))
				return
			}
			d := e.indexDate.Load().(time.Time).In(warsawLoc).Format("02.01.2006 15:04:05")
			fmt.Fprintf(w, `<?xml version="1.0"?><xml><data><gro href="A.htm" text="A"/></data><who lastupdatetext="Aktualizacja: %s"></who></xml>`, d)
			return
		}
		n, _ := e.planHits.LoadOrStore(r.URL.Path, new(atomic.Int64))
		n.(*atomic.Int64).Add(1)
		w.Write([]byte(planHTML(int(e.planEvents.Load()))))
	}))
	t.Cleanup(e.srv.Close)
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	t.Setenv("PLANWAT_PROXY", "")
	t.Setenv("PLANWAT_PROXY_HOST", "")
	t.Setenv("PLANWAT_SCRAPER_PROVIDER", "")
	fetcher, err := wat.NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	e.u = NewUpdater(st, fetcher)
	e.st = st
	e.u.SetIndexSources([]config.Source{{Faculty: "TST", IndexURL: e.srv.URL + "/zima/index.xml"}, {Faculty: "WCY", IndexURL: e.srv.URL + "/wcy/rozklad", Format: "wcy"}}, 30*time.Minute)
	return e
}

func (e *indexEnv) plan(id string) config.PlanConfig {
	return config.PlanConfig{ID: id, URL: e.srv.URL + "/zima/" + id + ".htm", StartYear: 2026}
}

func (e *indexEnv) save(t *testing.T, id string, fetched, checked, modified time.Duration) {
	t.Helper()
	events := make([]model.Event, 12)
	for i := range events {
		events[i] = model.Event{Date: fmt.Sprintf("2026-01-%02d", i+1), Start: "08:00", End: "09:35", Subject: "X"}
	}
	if err := e.st.Save(id, model.Plan{Events: events, FetchedAt: stamp(fetched), CheckedAt: stamp(checked), ModifiedAt: stamp(modified)}); err != nil {
		t.Fatal(err)
	}
}

func (e *indexEnv) hits(id string) int64 {
	if n, ok := e.planHits.Load("/zima/" + id + ".htm"); ok {
		return n.(*atomic.Int64).Load()
	}
	return 0
}

func TestIndexUnchangedSkipsPlanFetch(t *testing.T) {
	e := newIndexEnv(t)
	e.save(t, "A", 5*time.Hour, 5*time.Hour, 240*time.Hour)
	if err := e.u.CheckPlan(e.plan("A")); err != nil {
		t.Fatal(err)
	}
	if e.indexHits.Load() != 1 || e.hits("A") != 0 {
		t.Fatalf("indeks %d, plan %d; chcę 1 i 0", e.indexHits.Load(), e.hits("A"))
	}
	stored, _ := e.st.Load("A")
	if at, _ := time.ParseInLocation(timeLayout, stored.CheckedAt, warsawLoc); time.Since(at) > time.Minute {
		t.Fatalf("checked_at nie zaktualizowany: %s", stored.CheckedAt)
	}
	if stored.FetchedAt != stamp(5*time.Hour) {
		t.Fatalf("fetched_at zmieniony bez pobrania: %s", stored.FetchedAt)
	}
	if _, saved := e.u.IndexStatus(); saved != 1 {
		t.Fatalf("zaoszczędzone sprawdzenia = %d, chcę 1", saved)
	}
}

func TestIndexChangedFetchesPlan(t *testing.T) {
	e := newIndexEnv(t)
	e.save(t, "A", 5*time.Hour, 5*time.Hour, 240*time.Hour)
	e.indexDate.Store(time.Now().Add(-time.Hour))
	if err := e.u.CheckPlan(e.plan("A")); err != nil {
		t.Fatal(err)
	}
	if e.hits("A") != 1 {
		t.Fatalf("pobrań planu po zmianie wydziału = %d, chcę 1", e.hits("A"))
	}
}

func TestIndexFreshDecidesWithoutAdaptiveWait(t *testing.T) {
	e := newIndexEnv(t)
	e.save(t, "A", 5*time.Hour, 5*time.Hour, 240*time.Hour)
	e.save(t, "B", 3*time.Hour, 30*time.Minute, 240*time.Hour)
	e.indexDate.Store(time.Now().Add(-time.Hour))
	if err := e.u.CheckPlan(e.plan("A")); err != nil {
		t.Fatal(err)
	}
	stored, _ := e.st.Load("B")
	d := e.u.Decide(e.plan("B"), TimesOf(stored), time.Now())
	if !d.Due {
		t.Fatalf("B po zmianie wydziału: %+v, chcę Due mimo świeżego sprawdzenia", d)
	}
}

func TestIndexSafetyNet(t *testing.T) {
	e := newIndexEnv(t)
	e.save(t, "A", 25*time.Hour, 5*time.Minute, 240*time.Hour)
	if err := e.u.CheckPlan(e.plan("A")); err != nil {
		t.Fatal(err)
	}
	if e.hits("A") != 1 {
		t.Fatalf("pobrań planu po 25 h = %d, chcę 1", e.hits("A"))
	}
}

func TestIndexWithoutDateFallsBack(t *testing.T) {
	e := newIndexEnv(t)
	e.indexMode.Store("nodate")
	e.save(t, "A", 5*time.Hour, 5*time.Hour, 240*time.Hour)
	e.save(t, "B", 2*time.Hour, 2*time.Hour, 240*time.Hour)
	for _, id := range []string{"A", "B"} {
		if err := e.u.CheckPlan(e.plan(id)); err != nil {
			t.Fatal(err)
		}
	}
	if e.hits("A") != 1 || e.hits("B") != 0 {
		t.Fatalf("A %d (adaptacyjnie do sprawdzenia), B %d (świeży); chcę 1 i 0", e.hits("A"), e.hits("B"))
	}
	st, _ := e.u.IndexStatus()
	if !st[e.srv.URL+"/zima/index.xml"].NoDate {
		t.Fatalf("stan indeksu: %+v", st)
	}
}

func TestIndexFailureFallsBack(t *testing.T) {
	e := newIndexEnv(t)
	e.indexMode.Store("fail")
	e.save(t, "A", 5*time.Hour, 5*time.Hour, 240*time.Hour)
	if err := e.u.CheckPlan(e.plan("A")); err != nil {
		t.Fatal(err)
	}
	if e.hits("A") != 1 {
		t.Fatalf("pobrań planu przy niedziałającym indeksie = %d, chcę 1", e.hits("A"))
	}
}

func TestStaleIndexNotFetchedWhenPlanNotDue(t *testing.T) {
	e := newIndexEnv(t)
	e.save(t, "A", 2*time.Hour, 2*time.Hour, 240*time.Hour)
	stored, _ := e.st.Load("A")
	if d := e.u.Decide(e.plan("A"), TimesOf(stored), time.Now()); d.Due || d.NeedIndex {
		t.Fatalf("decyzja: %+v, chcę nic", d)
	}
	if e.indexHits.Load() != 0 {
		t.Fatal("indeks pobrany bez potrzeby")
	}
}

func TestIndexFetchedOncePerInterval(t *testing.T) {
	e := newIndexEnv(t)
	for _, id := range []string{"A", "B", "C"} {
		e.save(t, id, 5*time.Hour, 5*time.Hour, 240*time.Hour)
	}
	var wg sync.WaitGroup
	for _, id := range []string{"A", "B", "C"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			e.u.CheckPlan(e.plan(id))
		}(id)
	}
	wg.Wait()
	if e.indexHits.Load() != 1 {
		t.Fatalf("pobrań indeksu = %d, chcę 1", e.indexHits.Load())
	}
	for _, id := range []string{"A", "B", "C"} {
		if e.hits(id) != 0 {
			t.Fatalf("plan %s pobrany mimo braku zmian", id)
		}
	}
}

func TestPlanOutsideIndexDirFallsBack(t *testing.T) {
	e := newIndexEnv(t)
	cfg := e.plan("A")
	cfg.Extra = []config.PlanSource{{URL: e.srv.URL + "/lato/A.htm"}}
	e.save(t, "A", 5*time.Hour, 5*time.Hour, 240*time.Hour)
	stored, _ := e.st.Load("A")
	if d := e.u.Decide(cfg, TimesOf(stored), time.Now()); !d.Due || d.NeedIndex || d.ViaIndex {
		t.Fatalf("plan z częścią spoza indeksu: %+v, chcę adaptacyjnie Due", d)
	}
	wcy := config.PlanConfig{ID: "W", URL: e.srv.URL + "/wcy/rozklad?grupa=W"}
	if d := e.u.Decide(wcy, TimesOf(stored), time.Now()); !d.Due || d.NeedIndex {
		t.Fatalf("WCY (bez daty w indeksie): %+v", d)
	}
}

func TestIndexAssumptionAlert(t *testing.T) {
	e := newIndexEnv(t)
	n, drain := collect(t)
	e.u.SetAlert(n)
	e.save(t, "A", 5*time.Hour, 5*time.Hour, 240*time.Hour)
	e.u.refreshIndexes(e.plan("A"))
	e.planEvents.Store(13)
	if _, err := e.u.update(e.plan("A")); err != nil {
		t.Fatal(err)
	}
	got := drain()
	if len(got) != 1 || !strings.Contains(got[0], "Założenie o dacie aktualizacji wydziału nie działa") {
		t.Fatalf("alerty: %q", got)
	}
}

func TestIndexStatePersists(t *testing.T) {
	e := newIndexEnv(t)
	saved := make(chan []byte, 10)
	if err := e.u.LoadIndexState(nil, func(b []byte) { saved <- b }); err != nil {
		t.Fatal(err)
	}
	e.save(t, "A", 5*time.Hour, 5*time.Hour, 240*time.Hour)
	if err := e.u.CheckPlan(e.plan("A")); err != nil {
		t.Fatal(err)
	}
	var data []byte
	timeout := time.After(2 * time.Second)
	for data == nil || !strings.Contains(string(data), `"saved":1`) {
		select {
		case data = <-saved:
		case <-timeout:
			t.Fatalf("stan nie zapisany: %s", data)
		}
	}
	other := newIndexEnv(t)
	other.u.SetIndexSources([]config.Source{{Faculty: "TST", IndexURL: e.srv.URL + "/zima/index.xml"}}, 30*time.Minute)
	if err := other.u.LoadIndexState(data, nil); err != nil {
		t.Fatal(err)
	}
	st, n := other.u.IndexStatus()
	if n != 1 || st[e.srv.URL+"/zima/index.xml"].Updated == "" {
		t.Fatalf("po wczytaniu: %+v, %d", st, n)
	}
}

func TestFailedIndexNotRetriedWithinInterval(t *testing.T) {
	e := newIndexEnv(t)
	e.indexMode.Store("fail")
	for _, id := range []string{"A", "B"} {
		e.save(t, id, 5*time.Hour, 5*time.Hour, 240*time.Hour)
		if err := e.u.CheckPlan(e.plan(id)); err != nil {
			t.Fatal(err)
		}
	}
	first := e.indexHits.Load()
	e.save(t, "C", 5*time.Hour, 5*time.Hour, 240*time.Hour)
	if err := e.u.CheckPlan(e.plan("C")); err != nil {
		t.Fatal(err)
	}
	if e.indexHits.Load() != first {
		t.Fatalf("indeks ponawiany po błędzie: %d → %d zapytań", first, e.indexHits.Load())
	}
	if e.hits("A") != 1 || e.hits("B") != 1 || e.hits("C") != 1 {
		t.Fatalf("plany przy błędzie indeksu: A %d, B %d, C %d; chcę po 1", e.hits("A"), e.hits("B"), e.hits("C"))
	}
}
