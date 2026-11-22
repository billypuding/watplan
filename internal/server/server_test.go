package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestOfflineRefreshDoesNotFetch(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{{
		ID:  "TST26XX1S0",
		URL: "http://example.invalid/plan.htm",
	}})
	srv := NewServer(reg, st, nil, Options{Offline: true})
	handler, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/refresh?plan=TST26XX1S0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, chcę %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestRefreshCooldownHeader(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save("TST26XX1S0", model.Plan{FetchedAt: "2020-01-01 10:00:00"}); err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{{
		ID:  "TST26XX1S0",
		URL: "http://example.invalid/plan.htm",
	}})
	srv := NewServer(reg, st, nil, Options{})
	srv.lastRefresh["TST26XX1S0"] = time.Now()
	handler, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/refresh?plan=TST26XX1S0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("X-Plan-Refresh"); got != "cooldown" {
		t.Fatalf("X-Plan-Refresh = %q, chcę cooldown", got)
	}
}

func TestOnDemandFetchesMissingPlanOnce(t *testing.T) {
	page, err := os.ReadFile("../wat/testdata/plan_wat.html")
	if err != nil {
		t.Fatal(err)
	}

	var hits atomic.Int64
	watSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	}))
	t.Cleanup(watSrv.Close)

	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{{
		ID:        "WMT26AX3S1",
		URL:       watSrv.URL,
		StartYear: 2026,
	}})

	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	fetcher, err := wat.NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	upd := updater.NewUpdater(st, fetcher)
	srv := NewServer(reg, st, upd, Options{OnDemand: true})
	handler, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}

	var body []byte
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/plan/WMT26AX3S1", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("żądanie %d: status = %d, chcę 200; body=%s", i+1, rec.Code, rec.Body.String())
		}
		body = rec.Body.Bytes()
	}

	if got := hits.Load(); got != 1 {
		t.Fatalf("pobrań z WAT = %d, chcę 1 (drugie żądanie z cache)", got)
	}

	var plan model.Plan
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Events) == 0 {
		t.Fatal("plan bez zajęć")
	}
}

func TestOnDemandVisitServesCacheAndRefreshesInBackground(t *testing.T) {
	page, err := os.ReadFile("../wat/testdata/plan_wat.html")
	if err != nil {
		t.Fatal(err)
	}
	watSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	}))
	t.Cleanup(watSrv.Close)

	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const old = "2026-11-11 19:28:02"
	if err := st.Save("WMT26AX3S1", model.Plan{FetchedAt: old, ModifiedAt: old, Events: []model.Event{{Date: "2026-11-12", Start: "08:00", End: "09:35", Subject: "Stary"}}}); err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{{ID: "WMT26AX3S1", URL: watSrv.URL, StartYear: 2026}})

	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	fetcher, err := wat.NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(reg, st, updater.NewUpdater(st, fetcher), Options{OnDemand: true})
	handler, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/plan/WMT26AX3S1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, chcę 200", rec.Code)
	}
	var plan model.Plan
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.FetchedAt != old {
		t.Fatalf("serwer czekał na pobranie zamiast od razu oddać cache (%s)", plan.FetchedAt)
	}
	waitFor(t, func() bool {
		saved, err := st.Load("WMT26AX3S1")
		return err == nil && saved.CheckedAt != "" && !srv.busy("WMT26AX3S1")
	})
}

func TestRouting(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewServer(config.NewRegistry(nil), st, nil, Options{Offline: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path     string
		status   int
		location string
		contains string
	}{
		{"/?plan=WEL26EY4S0", http.StatusFound, "/app?plan=WEL26EY4S0", ""},
		{"/?plan=WEL26EY4S0&view=week", http.StatusFound, "/app?plan=WEL26EY4S0&view=week", ""},
		{"/?view=month", http.StatusFound, "/app?view=month", ""},
		{"/app/?plan=X", http.StatusMovedPermanently, "/app?plan=X", ""},
		{"/app", http.StatusOK, "", `src="/app.js`},
		{"/app?plan=WEL26EY4S0", http.StatusOK, "", `src="/app.js`},
		{"/", http.StatusOK, "", "selectedPlan"},
		{"/?landing", http.StatusOK, "", "selectedPlan"},
		{"/sw.js", http.StatusOK, "", "caches"},
		{"/o-watplan", http.StatusOK, "", "FAQPage"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.status {
			t.Errorf("%s: status %d, chcę %d", c.path, rec.Code, c.status)
			continue
		}
		if c.location != "" && rec.Header().Get("Location") != c.location {
			t.Errorf("%s: Location %q, chcę %q", c.path, rec.Header().Get("Location"), c.location)
		}
		if c.contains != "" && !strings.Contains(rec.Body.String(), c.contains) {
			t.Errorf("%s: odpowiedź nie zawiera %q", c.path, c.contains)
		}
		if c.status == http.StatusOK && rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: Cache-Control %q, chcę no-cache", c.path, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestCanonicalHostRedirect(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewServer(config.NewRegistry(nil), st, nil, Options{Offline: true, CanonicalHost: "watplan.me"}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		host, path string
		status     int
		location   string
	}{
		{"watplan.onrender.com", "/", http.StatusMovedPermanently, "https://watplan.me/"},
		{"watplan.onrender.com", "/app?plan=WEL26EY4S0&view=week", http.StatusMovedPermanently, "https://watplan.me/app?plan=WEL26EY4S0&view=week"},
		{"watplan.onrender.com:443", "/o-watplan", http.StatusMovedPermanently, "https://watplan.me/o-watplan"},
		{"watplan.onrender.com", "/api/plans", http.StatusOK, ""},
		{"watplan.onrender.com", "/sw.js", http.StatusOK, ""},
		{"watplan.me", "/", http.StatusOK, ""},
		{"127.0.0.1:8080", "/", http.StatusOK, ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		req.Host = c.host
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != c.status || rec.Header().Get("Location") != c.location {
			t.Errorf("%s%s: status %d, Location %q; chcę %d, %q", c.host, c.path, rec.Code, rec.Header().Get("Location"), c.status, c.location)
		}
	}
}

func TestPlanETagGives304(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Save("TST26XX1S0", model.Plan{FetchedAt: "2026-11-19 10:00:00", Events: []model.Event{{Date: "2026-11-20", Start: "08:00", End: "09:35", Subject: "X"}}})
	h, err := NewServer(config.NewRegistry([]config.PlanConfig{{ID: "TST26XX1S0"}}), st, nil, Options{Offline: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/plan/TST26XX1S0", nil))
	etag := rec.Header().Get("ETag")
	if rec.Code != http.StatusOK || etag == "" || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("status %d etag %q cache %q", rec.Code, etag, rec.Header().Get("Cache-Control"))
	}
	for _, header := range []string{etag, "W/" + etag, `"inny", ` + etag} {
		req := httptest.NewRequest(http.MethodGet, "/api/plan/TST26XX1S0", nil)
		req.Header.Set("If-None-Match", header)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
			t.Fatalf("If-None-Match %q: status %d, %d B", header, rec.Code, rec.Body.Len())
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/plan/TST26XX1S0", nil)
	req.Header.Set("If-None-Match", `"stary"`)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("inny ETag: status %d", rec.Code)
	}
}

func TestPlanRequestsWaitForRestoreButPagesDoNot(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.NewRegistry([]config.PlanConfig{{ID: "TST26XX1S0"}}), st, nil, Options{Offline: true})
	ready := make(chan struct{})
	srv.SetReady(ready)
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}

	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/app", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("strona aplikacji w czasie odtwarzania: %d", page.Code)
	}

	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/plan/TST26XX1S0", nil))
		done <- rec.Code
	}()
	select {
	case code := <-done:
		t.Fatalf("plan odpowiedział (%d) przed końcem odtwarzania", code)
	case <-time.After(100 * time.Millisecond):
	}
	_ = st.Save("TST26XX1S0", model.Plan{FetchedAt: "2026-11-20 10:00:00", Events: []model.Event{{Date: "2026-11-20", Start: "08:00", End: "09:35", Subject: "X"}}})
	close(ready)
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("po odtworzeniu: %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("plan nie odpowiedział po odtworzeniu")
	}
}
