package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type healthBody struct {
	OK     bool            `json:"ok"`
	Checks map[string]bool `json:"checks"`
}

func getHealth(t *testing.T, h http.Handler) (int, healthBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var body healthBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("JSON: %v (%s)", err, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	for _, secret := range []string{"oxylabs", "scrapingant", "proxy", "error", "token"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), secret) {
			t.Fatalf("/healthz zdradza %q: %s", secret, rec.Body.String())
		}
	}
	return rec.Code, body
}

func TestHealthOK(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Minute))
	code, body := getHealth(t, env.handler)
	if code != http.StatusOK || !body.OK {
		t.Fatalf("status %d, %+v", code, body)
	}
	for _, k := range []string{"data", "db", "fetch", "budget"} {
		if !body.Checks[k] {
			t.Fatalf("%s = false: %+v", k, body.Checks)
		}
	}
}

func TestHealthFailures(t *testing.T) {
	t.Run("dane jeszcze się ładują", func(t *testing.T) {
		env := newRefreshEnv(t, gone, checkedAgo(time.Minute))
		env.srv.SetReady(make(chan struct{}))
		code, body := getHealth(t, env.handler)
		if code != http.StatusServiceUnavailable || body.Checks["data"] {
			t.Fatalf("status %d, %+v", code, body)
		}
	})
	t.Run("brak wymaganej bazy", func(t *testing.T) {
		env := newRefreshEnv(t, gone, checkedAgo(time.Minute))
		env.srv.options.DBRequired = true
		code, body := getHealth(t, env.handler)
		if code != http.StatusServiceUnavailable || body.Checks["db"] {
			t.Fatalf("status %d, %+v", code, body)
		}
	})
	t.Run("limit pobierań wyczerpany", func(t *testing.T) {
		t.Setenv("PLANWAT_BUDGET_SCRAPER_DAILY", "1")
		env := newRefreshEnv(t, gone, checkedAgo(time.Hour))
		env.visit(t)
		waitFor(t, func() bool { return env.apiHits.Load() == 1 && !env.srv.busy(refreshTestPlan) })
		code, body := getHealth(t, env.handler)
		if code != http.StatusServiceUnavailable || body.Checks["budget"] {
			t.Fatalf("status %d, %+v", code, body)
		}
	})
}
