package server

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/store"
)

func TestSecurityHeaders(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewServer(config.NewRegistry(nil), st, nil, Options{Offline: true, CanonicalHost: "watplan.me"}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	get := func(host, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	want := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Strict-Transport-Security": "max-age=86400",
	}
	for _, c := range []struct{ host, path string }{
		{"watplan.me", "/"},
		{"watplan.me", "/app"},
		{"watplan.me", "/o-watplan"},
		{"watplan.me", "/style.css?v=x"},
		{"watplan.me", "/sw.js"},
		{"watplan.me", "/api/plans"},
		{"www.watplan.me", "/app"},
	} {
		rec := get(c.host, c.path)
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s%s: %s = %q, chcę %q", c.host, c.path, k, got, v)
			}
		}
		pp := rec.Header().Get("Permissions-Policy")
		for _, feature := range []string{"camera=()", "microphone=()", "geolocation=()", "payment=()", "usb=()"} {
			if !strings.Contains(pp, feature) {
				t.Errorf("%s%s: Permissions-Policy bez %s", c.host, c.path, feature)
			}
		}
		csp := rec.Header().Get("Content-Security-Policy")
		for _, part := range []string{"default-src 'self'", "frame-ancestors 'none'", "object-src 'none'", "base-uri 'none'", "https://cloud.umami.is", "https://gateway.umami.is"} {
			if !strings.Contains(csp, part) {
				t.Errorf("%s%s: CSP bez %q", c.host, c.path, part)
			}
		}
		if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
			t.Errorf("%s%s: CSP z unsafe-*: %s", c.host, c.path, csp)
		}
		if strings.Contains(rec.Header().Get("Strict-Transport-Security"), "preload") || strings.Contains(rec.Header().Get("Strict-Transport-Security"), "includeSubDomains") {
			t.Errorf("%s%s: HSTS z preload/includeSubDomains", c.host, c.path)
		}
	}

	for _, path := range []string{"/", "/app", "/o-watplan"} {
		rec := get("watplan.me", path)
		csp := rec.Header().Get("Content-Security-Policy")
		scripts := inlineJS.FindAllStringSubmatch(rec.Body.String(), -1)
		for _, m := range scripts {
			sum := sha256.Sum256([]byte(m[1]))
			hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
			if !strings.Contains(csp, hash) {
				t.Errorf("%s: brak hasha %s w CSP dla skryptu %.60q", path, hash, m[1])
			}
		}
		if path != "/o-watplan" && len(scripts) == 0 {
			t.Errorf("%s: brak skryptów inline, a powinien być co najmniej jeden", path)
		}
	}
}
