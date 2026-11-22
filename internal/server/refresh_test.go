package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/updater"
	"github.com/billypuding/watplan/internal/wat"
)

const refreshTestPlan = "TST26XX1S0"

type refreshEnv struct {
	srv       *Server
	handler   http.Handler
	store     *store.Store
	proxyHits atomic.Int64
	apiHits   atomic.Int64
}

func newRefreshEnv(t *testing.T, api http.HandlerFunc, checkedAt string) *refreshEnv {
	t.Helper()
	env := &refreshEnv{}

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		env.proxyHits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(proxy.Close)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.apiHits.Add(1)
		api(w, r)
	}))
	t.Cleanup(apiSrv.Close)

	t.Setenv("PLANWAT_PROXY", proxy.URL)
	t.Setenv("PLANWAT_PROXY_BACKUP", "")
	t.Setenv("PLANWAT_PROXY_HOST", "")
	t.Setenv("PLANWAT_SCRAPER_PROVIDER", "scrapingant")
	t.Setenv("PLANWAT_SCRAPER_KEY", "test-key")
	t.Setenv("PLANWAT_SCRAPER_API_URL", apiSrv.URL)
	t.Setenv("PLANWAT_SCRAPER_PROXY_TYPE", "datacenter")
	t.Setenv("PLANWAT_SCRAPER_COUNTRY", "PL")
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")

	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{
		FetchedAt: checkedAt,
		CheckedAt: checkedAt,
		Events:    []model.Event{{Date: "2026-11-23", Start: "08:00", End: "09:35", Subject: "Stary"}},
	}
	if err := st.Save(refreshTestPlan, plan); err != nil {
		t.Fatal(err)
	}
	fetcher, err := wat.NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{{ID: refreshTestPlan, URL: "http://example.test/plan", StartYear: 2026}})
	env.srv = NewServer(reg, st, updater.NewUpdater(st, fetcher), Options{OnDemand: true})
	env.handler, err = env.srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	env.store = st
	t.Cleanup(func() { waitFor(t, func() bool { return !env.srv.busy(refreshTestPlan) }) })
	return env
}

func (e *refreshEnv) visit(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/plan/"+refreshTestPlan, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET planu: status = %d", rec.Code)
	}
	return rec.Header().Get("X-Plan-Refresh")
}

func (e *refreshEnv) refresh(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/refresh?plan="+refreshTestPlan, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST odświeżania: status = %d", rec.Code)
	}
	return rec
}

func (s *Server) busy(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busyLocked(id)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("nie doczekałem się warunku")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func checkedAgo(d time.Duration) string {
	return time.Now().Add(-d).In(warsawLoc).Format(timeLayout)
}

func gone(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotFound)
}

func TestRefreshFreshPlanSkipsFetch(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(5*time.Minute))

	rec := env.refresh(t)
	if got := rec.Header().Get("X-Plan-Refresh"); got != "fresh" {
		t.Fatalf("X-Plan-Refresh = %q, chcę fresh", got)
	}
	checked, err := time.Parse(time.RFC3339, rec.Header().Get("X-Plan-Checked-At"))
	if err != nil {
		t.Fatalf("X-Plan-Checked-At: %v", err)
	}
	if ago := time.Since(checked); ago < 4*time.Minute || ago > 6*time.Minute {
		t.Fatalf("sprawdzony %v temu, chcę ok. 5 min", ago)
	}
	time.Sleep(50 * time.Millisecond)
	if env.srv.busy(refreshTestPlan) || env.proxyHits.Load() != 0 || env.apiHits.Load() != 0 {
		t.Fatalf("proxy = %d, API = %d, chcę 0 i 0", env.proxyHits.Load(), env.apiHits.Load())
	}
}

func TestRefreshDuringFetchIsPending(t *testing.T) {
	release := make(chan struct{})
	env := newRefreshEnv(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		gone(w, r)
	}, checkedAgo(time.Hour))

	env.visit(t)
	waitFor(t, func() bool { return env.apiHits.Load() == 1 })

	rec := env.refresh(t)
	if got := rec.Header().Get("X-Plan-Refresh"); got != "pending" {
		t.Fatalf("X-Plan-Refresh = %q, chcę pending", got)
	}
	close(release)
	waitFor(t, func() bool { return !env.srv.busy(refreshTestPlan) })
	if env.proxyHits.Load() != 0 {
		t.Fatalf("zapytań do proxy = %d, chcę 0", env.proxyHits.Load())
	}
}

func TestRefreshStalePlanUsesProxy(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Hour))

	rec := env.refresh(t)
	if got := rec.Header().Get("X-Plan-Refresh"); got != "background" {
		t.Fatalf("X-Plan-Refresh = %q, chcę background", got)
	}
	waitFor(t, func() bool { return !env.srv.busy(refreshTestPlan) })
	if env.proxyHits.Load() != 1 {
		t.Fatalf("zapytań do proxy = %d, chcę 1", env.proxyHits.Load())
	}
}

func TestVisitChecksAPIOncePerWindow(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Hour))

	for i := 0; i < 3; i++ {
		env.visit(t)
		waitFor(t, func() bool { return !env.srv.busy(refreshTestPlan) })
	}
	if env.apiHits.Load() != 1 {
		t.Fatalf("zapytań do API = %d, chcę 1", env.apiHits.Load())
	}

	env.srv.mu.Lock()
	env.srv.lastVisit[refreshTestPlan] = time.Now().Add(-visitCheckEvery - time.Second)
	env.srv.mu.Unlock()
	env.visit(t)
	waitFor(t, func() bool { return !env.srv.busy(refreshTestPlan) })
	if env.apiHits.Load() != 2 {
		t.Fatalf("zapytań do API po %v = %d, chcę 2", visitCheckEvery, env.apiHits.Load())
	}
	if env.proxyHits.Load() != 0 {
		t.Fatalf("zapytań do proxy = %d, chcę 0", env.proxyHits.Load())
	}
}

func TestVisitSkipsRecentlyCheckedPlan(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Minute))

	env.visit(t)
	time.Sleep(50 * time.Millisecond)
	if env.apiHits.Load() != 0 {
		t.Fatalf("zapytań do API = %d, chcę 0", env.apiHits.Load())
	}
}

func TestVisitRefreshHeader(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Hour))

	if got := env.visit(t); got != "background" {
		t.Fatalf("pierwsze wejście: X-Plan-Refresh = %q, chcę background", got)
	}
	waitFor(t, func() bool { return !env.srv.busy(refreshTestPlan) })
	if got := env.visit(t); got != "none" {
		t.Fatalf("drugie wejście: X-Plan-Refresh = %q, chcę none", got)
	}
}

func TestVisitFreshPlanHeader(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Minute))

	if got := env.visit(t); got != "none" {
		t.Fatalf("X-Plan-Refresh = %q, chcę none", got)
	}
}

func TestVisitDuringBackgroundFetchIsFast(t *testing.T) {
	release := make(chan struct{})
	env := newRefreshEnv(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		gone(w, r)
	}, checkedAgo(time.Hour))
	defer close(release)

	if got := env.visit(t); got != "background" {
		t.Fatalf("X-Plan-Refresh = %q, chcę background", got)
	}
	waitFor(t, func() bool { return env.apiHits.Load() == 1 })

	for i := 0; i < 5; i++ {
		started := time.Now()
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/plan/"+refreshTestPlan, nil))
		if took := time.Since(started); took > 200*time.Millisecond {
			t.Fatalf("GET w trakcie pobierania w tle trwał %v", took)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		timing := rec.Header().Get("Server-Timing")
		for _, part := range []string{"ready;dur=", "read;dur=", "visit;dur=", "etag;dur=", "total;dur="} {
			if !strings.Contains(timing, part) {
				t.Fatalf("Server-Timing = %q, brak %q", timing, part)
			}
		}
	}
}
