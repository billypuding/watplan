package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name, remote, cf string
		trust            bool
		want             string
	}{
		{"zwykły klient bez nagłówka", "203.0.113.7:5000", "", false, "203.0.113.7"},
		{"podrobiony nagłówek spoza Cloudflare", "203.0.113.7:5000", "198.51.100.1", false, "203.0.113.7"},
		{"podrobiony nagłówek spoza Cloudflare przy zaufanym proxy", "203.0.113.7:5000", "198.51.100.1", true, "203.0.113.7"},
		{"bezpośrednio z Cloudflare IPv4", "172.68.10.5:443", "198.51.100.1", false, "198.51.100.1"},
		{"bezpośrednio z Cloudflare IPv6", "[2606:4700::1]:443", "2001:db8::5", false, "2001:db8::5"},
		{"proxy platformy bez zaufania", "10.0.3.7:1000", "198.51.100.1", false, "10.0.3.7"},
		{"proxy platformy z zaufaniem", "10.0.3.7:1000", "198.51.100.1", true, "198.51.100.1"},
		{"zaufane proxy, śmieci w nagłówku", "10.0.3.7:1000", "nie-ip", true, "10.0.3.7"},
		{"localhost z zaufaniem", "127.0.0.1:1000", "198.51.100.9", true, "198.51.100.9"},
	}
	for _, c := range cases {
		s := &Server{options: Options{TrustProxy: c.trust}}
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		if c.cf != "" {
			r.Header.Set("CF-Connecting-IP", c.cf)
		}
		if got := s.clientIP(r); got != c.want {
			t.Errorf("%s: %q, chcę %q", c.name, got, c.want)
		}
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(2, time.Hour)
	l.now = func() time.Time { return now }
	if !l.allow("a") || !l.allow("a") {
		t.Fatal("pierwsze dwa zapytania powinny przejść")
	}
	if l.allow("a") {
		t.Fatal("trzecie zapytanie w godzinie powinno być zablokowane")
	}
	if !l.allow("b") {
		t.Fatal("inny adres ma własny limit")
	}
	now = now.Add(61 * time.Minute)
	if !l.allow("a") {
		t.Fatal("po godzinie limit się zwalnia")
	}
	if !newRateLimiter(-1, time.Hour).allow("x") {
		t.Fatal("limit -1 = bez limitu")
	}
}

func TestRefreshLimitedPerIP(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Hour))
	env.srv.refreshIPs = newRateLimiter(1, time.Hour)
	env.srv.options.TrustProxy = true
	post := func(ip string) string {
		req := httptest.NewRequest(http.MethodPost, "/api/refresh?plan="+refreshTestPlan, nil)
		req.RemoteAddr = "10.0.0.5:1234"
		req.Header.Set("CF-Connecting-IP", ip)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)
		return rec.Header().Get("X-Plan-Refresh")
	}
	if got := post("198.51.100.1"); got != "background" {
		t.Fatalf("pierwsze odświeżenie: %q", got)
	}
	waitFor(t, func() bool { return !env.srv.busy(refreshTestPlan) })
	env.srv.mu.Lock()
	env.srv.lastRefresh[refreshTestPlan] = time.Time{}
	env.srv.mu.Unlock()
	if got := post("198.51.100.1"); got != "limited" {
		t.Fatalf("drugie z tego samego IP: %q, chcę limited", got)
	}
	if got := post("198.51.100.2"); got != "background" {
		t.Fatalf("inne IP: %q, chcę background", got)
	}
	waitFor(t, func() bool { return !env.srv.busy(refreshTestPlan) })
	if env.proxyHits.Load() != 2 {
		t.Fatalf("zapytań do proxy = %d, chcę 2", env.proxyHits.Load())
	}
}

func TestVisitChecksLimitedPerIP(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Hour))
	env.srv.checkIPs = newRateLimiter(1, time.Hour)
	if got := env.visit(t); got != "background" {
		t.Fatalf("pierwsze wejście: %q", got)
	}
	waitFor(t, func() bool { return !env.srv.busy(refreshTestPlan) })
	env.srv.mu.Lock()
	env.srv.lastVisit[refreshTestPlan] = time.Time{}
	env.srv.mu.Unlock()
	if got := env.visit(t); got != "none" {
		t.Fatalf("drugie sprawdzenie z tego samego IP w godzinie: %q, chcę none", got)
	}
	if env.apiHits.Load() != 1 {
		t.Fatalf("zapytań do API = %d, chcę 1", env.apiHits.Load())
	}
}

func TestRefreshWhenBudgetExhausted(t *testing.T) {
	t.Setenv("PLANWAT_BUDGET_PROXY_DAILY", "1")
	t.Setenv("PLANWAT_BUDGET_SCRAPER_DAILY", "1")
	env := newRefreshEnv(t, gone, checkedAgo(time.Hour))
	if got := env.visit(t); got != "background" {
		t.Fatalf("wejście: %q", got)
	}
	waitFor(t, func() bool { return env.apiHits.Load() == 1 && !env.srv.busy(refreshTestPlan) })
	rec := env.refresh(t)
	if got := rec.Header().Get("X-Plan-Refresh"); got != "budget" {
		t.Fatalf("X-Plan-Refresh = %q, chcę budget", got)
	}
	if env.proxyHits.Load() != 0 {
		t.Fatalf("zapytań do proxy = %d, chcę 0", env.proxyHits.Load())
	}
}
