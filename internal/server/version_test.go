package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestVersionInPagesAndStatus(t *testing.T) {
	h := planPageServer(t)
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "watplan.me"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	meta := regexp.MustCompile(`<meta name="watplan-version" content="([^"]+)">`)
	app := meta.FindStringSubmatch(get("/app").Body.String())
	plan := meta.FindStringSubmatch(get("/plan/WMT26AX3S1").Body.String())
	if app == nil || plan == nil || app[1] != plan[1] || len(app[1]) < 7 {
		t.Fatalf("wersja w /app %v, w /plan %v", app, plan)
	}
	var status map[string]any
	if err := json.Unmarshal(get("/api/status").Body.Bytes(), &status); err != nil || status["version"] != app[1] {
		t.Fatalf("/api/status version = %v, strona = %q (%v)", status["version"], app[1], err)
	}
}

func TestBuildVersionFallsBackToAssets(t *testing.T) {
	v := buildVersion("abcdef0123456789")
	if !strings.HasPrefix(v, "abcdef0") && !regexp.MustCompile(`^[0-9a-f]{7}\+? \(\d{2}\.\d{2}\.\d{4} \d{2}:\d{2}\)$`).MatchString(v) {
		t.Fatalf("buildVersion = %q", v)
	}
}
