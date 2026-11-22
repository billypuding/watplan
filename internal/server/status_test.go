package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatusRequiresToken(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Minute))
	get := func(token, auth string) map[string]any {
		env.srv.options.StatusToken = token
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	basic := func(name string, body map[string]any) {
		if len(body) != 2 || body["mode"] != "on-demand" || body["now"] == nil {
			t.Fatalf("%s: chcę tylko mode i now, jest %v", name, body)
		}
	}
	basic("bez tokenu w konfiguracji", get("", "Bearer cokolwiek"))
	basic("bez nagłówka", get("sekret", ""))
	basic("zły token", get("sekret", "Bearer zly"))
	basic("inny schemat", get("sekret", "Basic sekret"))

	full := get("sekret", "Bearer sekret")
	for _, k := range []string{"usage", "budget", "db", "proxy", "blocked_hosts", "missing_plans"} {
		if _, ok := full[k]; !ok {
			t.Fatalf("z tokenem brak %q: %v", k, full)
		}
	}
}
