package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

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
