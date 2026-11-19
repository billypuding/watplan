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
