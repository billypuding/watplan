package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestCacheHeaders(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(5*time.Minute))
	do := func(method, path string, header ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)
		return rec
	}

	page := do(http.MethodGet, "/app").Body.String()
	asset := regexp.MustCompile(`/app\.js\?v=[0-9a-f]+`).FindString(page)
	font := regexp.MustCompile(`/fonts/geist-latin\.woff2\?v=[0-9a-f]+`).FindString(page)
	icon := regexp.MustCompile(`/favicon\.svg\?v=[0-9a-f]+`).FindString(page)
	if asset == "" || font == "" || icon == "" {
		t.Fatalf("brak wersjonowanych adresów w /app: %q %q %q", asset, font, icon)
	}

	cases := []struct {
		name, method, path, want string
		etag, vary               bool
	}{
		{"js z ?v=", http.MethodGet, asset, "public, max-age=31536000, immutable", false, false},
		{"font z ?v=", http.MethodGet, font, "public, max-age=31536000, immutable", false, false},
		{"ikona z ?v=", http.MethodGet, icon, "public, max-age=31536000, immutable", false, false},
		{"plik bez wersji", http.MethodGet, "/robots.txt", "public, max-age=86400", false, false},
		{"HTML /", http.MethodGet, "/", cacheHTML, true, false},
		{"HTML /app", http.MethodGet, "/app?plan=" + refreshTestPlan, cacheHTML, true, false},
		{"HTML /o-watplan", http.MethodGet, "/o-watplan", cacheHTML, true, false},
		{"sw.js", http.MethodGet, "/sw.js", "no-cache", false, false},
		{"/api/plan", http.MethodGet, "/api/plan/" + refreshTestPlan, cacheAPIWithET, true, true},
		{"/api/plans", http.MethodGet, "/api/plans", cacheAPIWithET, true, true},
		{"/api/status", http.MethodGet, "/api/status", cacheAPI, false, false},
		{"/api/changes", http.MethodGet, "/api/changes/" + refreshTestPlan, cacheAPI, false, true},
		{"/api/refresh fresh", http.MethodPost, "/api/refresh?plan=" + refreshTestPlan, cacheAPI, false, false},
		{"/api/plan nieznany", http.MethodGet, "/api/plan/NIEMA", cacheAPI, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(c.method, c.path, "Accept-Encoding", "br, gzip")
			if got := rec.Header().Get("Cache-Control"); got != c.want {
				t.Fatalf("Cache-Control = %q, chcę %q (status %d)", got, c.want, rec.Code)
			}
			if strings.HasPrefix(c.path, "/api/") && !strings.HasPrefix(rec.Header().Get("Cache-Control"), "private") {
				t.Fatal("odpowiedź API bez private")
			}
			etag := rec.Header().Get("ETag")
			if c.etag != (etag != "") {
				t.Fatalf("ETag = %q, chcę ETag: %v", etag, c.etag)
			}
			if c.vary && !strings.Contains(strings.Join(rec.Header().Values("Vary"), ","), "Accept-Encoding") {
				t.Fatalf("brak Vary: Accept-Encoding (Content-Encoding %q)", rec.Header().Get("Content-Encoding"))
			}
			if c.etag {
				again := do(c.method, c.path, "Accept-Encoding", "br, gzip", "If-None-Match", etag)
				if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
					t.Fatalf("If-None-Match: status %d, treść %d B", again.Code, again.Body.Len())
				}
				if got := again.Header().Get("Cache-Control"); got != c.want {
					t.Fatalf("304: Cache-Control = %q, chcę %q", got, c.want)
				}
			}
		})
	}
	if got := do(http.MethodPost, "/api/refresh?plan="+refreshTestPlan).Header().Get("X-Plan-Refresh"); got != "fresh" {
		t.Fatalf("X-Plan-Refresh = %q, chcę fresh", got)
	}
}

func TestCacheHeaderBackgroundRefresh(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Hour))
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/refresh?plan="+refreshTestPlan, nil))
	if rec.Header().Get("X-Plan-Refresh") != "background" || rec.Header().Get("Cache-Control") != cacheAPI {
		t.Fatalf("X-Plan-Refresh = %q, Cache-Control = %q", rec.Header().Get("X-Plan-Refresh"), rec.Header().Get("Cache-Control"))
	}
	waitFor(t, func() bool { return !env.srv.busy(refreshTestPlan) })
}
