package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/alert"
	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/wat"
)

func newMaintainFixture(t *testing.T, handler http.HandlerFunc) (*Updater, *store.Store, string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	t.Setenv("PLANWAT_BLOCK_BACKOFF", "1h")
	fetcher, err := wat.NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	return NewUpdater(st, fetcher), st, srv.URL
}

func TestMaintainAddsNewGroupsAndFetchesMissing(t *testing.T) {
	var planHits atomic.Int64
	u, st, base := newMaintainFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plany/index.xml":
			fmt.Fprint(w, `<a href="WEL26EX1S0.htm">x</a><a href="WEL26EX2S0.htm">y</a><a href="dr_Kowalski_Jan.htm">z</a>`)
		case "/plany/WEL26EX1S0.htm", "/plany/WEL26EX2S0.htm":
			planHits.Add(1)
			fmt.Fprint(w, planHTML(5))
		default:
			http.NotFound(w, r)
		}
	})

	known := config.PlanConfig{ID: "WEL26EX1S0", Name: "WEL26EX1S0", URL: base + "/plany/WEL26EX1S0.htm", StartYear: 2026, Faculty: "WEL"}
	if err := st.Save(known.ID, model.Plan{Events: []model.Event{{Date: "2026-10-01"}}, FetchedAt: "2026-10-01 08:00:00"}); err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{known})
	sources := []config.Source{{Faculty: "WEL", IndexURL: base + "/plany/index.xml", StartYear: 2026}}

	u.checkSources(sources, reg)
	if _, ok := reg.Get("WEL26EX2S0"); !ok {
		t.Fatalf("nowa grupa nie trafiła do listy: %+v", reg.List())
	}
	if got := u.SourceStatus()["WEL"]; got.Groups != 2 || got.New != 1 || got.Error != "" {
		t.Fatalf("status WEL = %+v", got)
	}

	if !u.fillMissing(context.Background(), reg, map[string]time.Time{}, MaintainOptions{FetchGap: time.Millisecond}.withDefaults()) {
		t.Fatal("fillMissing przerwane")
	}
	if planHits.Load() != 1 {
		t.Fatalf("pobrań planów %d, chcę 1 (tylko brakujący)", planHits.Load())
	}
	if got := loadPlan(t, st, "WEL26EX2S0"); len(got.Events) != 5 {
		t.Fatalf("brakujący plan: %d zajęć", len(got.Events))
	}
	if u.MissingPlans() != 0 {
		t.Fatalf("MissingPlans = %d", u.MissingPlans())
	}
}

func TestMaintainReportsMovedIndexLoudly(t *testing.T) {
	u, _, base := newMaintainFixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	reg := config.NewRegistry([]config.PlanConfig{{ID: "WME26MX1S0", Faculty: "WIM", URL: base + "/WME26MX1S0.htm"}})
	src := config.Source{Faculty: "WIM", IndexURL: base + "/rozklady/zima/index.xml", StartYear: 2026}

	u.checkSources([]config.Source{src}, reg)
	got := u.SourceStatus()["WIM"]
	if !strings.Contains(got.Error, "404") || !strings.Contains(got.Error, src.IndexURL) {
		t.Fatalf("status WIM = %+v, chcę głośny błąd o przeniesionej liście", got)
	}
	if len(reg.List()) != 1 {
		t.Fatalf("lista grup zmieniona mimo błędu: %+v", reg.List())
	}
}

func TestFillMissingSkipsBlockedHostAndRemembersFailures(t *testing.T) {
	var hits atomic.Int64
	u, _, base := newMaintainFixture(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Request unsuccessful. Incapsula incident ID: 1")
	})
	reg := config.NewRegistry([]config.PlanConfig{
		{ID: "WEL26EX1S0", URL: base + "/a.htm", StartYear: 2026},
		{ID: "WEL26EX2S0", URL: base + "/b.htm", StartYear: 2026},
	})
	failed := map[string]time.Time{}
	opts := MaintainOptions{FetchGap: time.Millisecond}.withDefaults()

	u.fillMissing(context.Background(), reg, failed, opts)
	if hits.Load() != 1 {
		t.Fatalf("zapytań %d, chcę 1 (po blokadzie host jest pomijany)", hits.Load())
	}
	u.fillMissing(context.Background(), reg, failed, opts)
	if hits.Load() != 1 {
		t.Fatalf("druga runda zapytań %d, chcę dalej 1", hits.Load())
	}
	if u.MissingPlans() != 2 {
		t.Fatalf("MissingPlans = %d", u.MissingPlans())
	}
}

func TestCheckSourcesAttachesSummerPlans(t *testing.T) {
	u, _, base := newMaintainFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zima/index.xml":
			fmt.Fprint(w, `<a href="WEL26EX1S0.htm">x</a>`)
		case "/lato/index.xml":
			fmt.Fprint(w, `<a href="WEL26EX1S0.htm">x</a><a href="WEL26EX9S0.htm">y</a>`)
		case "/lato/WEL26EX1S0.htm", "/lato/WEL26EX9S0.htm":
			fmt.Fprint(w, summerHTML)
		default:
			http.NotFound(w, r)
		}
	})
	reg := config.NewRegistry([]config.PlanConfig{{ID: "WEL26EX1S0", URL: base + "/zima/WEL26EX1S0.htm", Faculty: "WEL", StartYear: 2026}})
	sources := []config.Source{
		{Faculty: "WEL", IndexURL: base + "/zima/index.xml", StartYear: 2026},
		{Faculty: "WEL", IndexURL: base + "/lato/index.xml", StartYear: 2026, Semester: "lato", Optional: true},
		{Faculty: "WIM", IndexURL: base + "/brak/index.xml", StartYear: 2026, Semester: "lato", Optional: true},
	}
	u.checkSources(sources, reg)

	p, _ := reg.Get("WEL26EX1S0")
	if len(p.Extra) != 1 || p.Extra[0].URL != base+"/lato/WEL26EX1S0.htm" {
		t.Fatalf("plan zimowy bez źródła letniego: %+v", p)
	}
	if n, ok := reg.Get("WEL26EX9S0"); !ok || n.URL != base+"/lato/WEL26EX9S0.htm" {
		t.Fatalf("nowa grupa letnia: %+v %v", n, ok)
	}
	status := u.SourceStatus()
	if status["WEL lato"].Groups != 2 || status["WIM lato"].Error != "jeszcze nie opublikowano (404)" {
		t.Fatalf("status %+v", status)
	}

	u.checkSources(sources, reg)
	if p, _ := reg.Get("WEL26EX1S0"); len(p.Extra) != 1 {
		t.Fatalf("źródło letnie dodane dwa razy: %+v", p.Extra)
	}
}

func TestCheckSourcesRejectsLastYearSummer(t *testing.T) {
	oldSummer := `<table><tr><td>pon.</td><td>&nbsp</td><td>02 III</td><td>09 III</td><td>&nbsp</td><td>&nbsp</td></tr>` +
		`<tr><td>pon.</td><td>1-2</td><td>Fizyka</td><td>Fizyka</td><td>&nbsp</td><td>&nbsp</td></tr></table>`
	u, _, base := newMaintainFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lato/index.xml":
			fmt.Fprint(w, `<a href="WME21MC1S0.htm">x</a><a href="WEL26EX1S0.htm">y</a>`)
		case "/lato/WME21MC1S0.htm", "/lato/WEL26EX1S0.htm":
			fmt.Fprint(w, oldSummer)
		default:
			http.NotFound(w, r)
		}
	})
	reg := config.NewRegistry([]config.PlanConfig{{ID: "WEL26EX1S0", URL: base + "/zima/WEL26EX1S0.htm", Faculty: "WIM", StartYear: 2026}})
	u.checkSources([]config.Source{{Faculty: "WIM", IndexURL: base + "/lato/index.xml", StartYear: 2026, Semester: "lato", Optional: true}}, reg)

	if len(reg.List()) != 1 {
		t.Fatalf("dodano grupy ze starego semestru: %+v", reg.List())
	}
	if p, _ := reg.Get("WEL26EX1S0"); len(p.Extra) != 0 {
		t.Fatalf("doczepiono stare źródło: %+v", p.Extra)
	}
	if got := u.SourceStatus()["WIM lato"].Error; got != "na stronie są jeszcze plany z poprzedniego roku" {
		t.Fatalf("status %q", got)
	}
}

func TestFacultyStatusSendsAlerts(t *testing.T) {
	var mu sync.Mutex
	var msgs []string
	u := NewUpdater(nil, nil)
	u.SetAlert(alert.NewFunc(func(text string) error { mu.Lock(); msgs = append(msgs, text); mu.Unlock(); return nil }))

	u.recordSource("WEL lato", FacultyStatus{Checked: "a", Error: "jeszcze nie opublikowano (404)"})
	u.recordSource("WIM lato", FacultyStatus{Checked: "a", Error: "na stronie są jeszcze plany z poprzedniego roku"})
	u.recordSource("WCY", FacultyStatus{Checked: "a", Error: "WAT (Incapsula) blokuje pobieranie listy grup"})
	u.recordSource("WCY", FacultyStatus{Checked: "b", Groups: 180})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(msgs)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) != 2 || !strings.Contains(msgs[0], "WCY") || !strings.Contains(msgs[1], "180 grup") {
		t.Fatalf("wiadomości %q", msgs)
	}
}
