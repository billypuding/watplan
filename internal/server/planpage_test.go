package server

import (
	"html"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
)

func planPageServer(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save("WMT26AX3S1", model.Plan{FetchedAt: "2026-11-20 10:00:00", ModifiedAt: "2026-11-19 14:07:56"}); err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{
		{ID: "WMT26AX3S1", URL: "http://example.invalid/a.htm", Faculty: "WML"},
		{ID: "WCY26KY4S1", URL: "http://example.invalid/b", Faculty: "WCY"},
	})
	h, err := NewServer(reg, st, nil, Options{Offline: true, CanonicalHost: "watplan.me"}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestPlanPage(t *testing.T) {
	h := planPageServer(t)
	get := func(path string, header ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "watplan.me"
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get("/plan/WMT26AX3S1")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		"<title>Plan zajęć WMT26AX3S1 – WML WAT | WATplan</title>",
		`<h1 class="sr-only">Plan zajęć WMT26AX3S1</h1>`,
		`<meta name="description" content="Plan zajęć grupy WMT26AX3S1`,
		`<meta name="robots" content="index, follow">`,
		`<link rel="canonical" href="https://watplan.me/plan/WMT26AX3S1">`,
		`<meta property="og:title" content="Plan zajęć WMT26AX3S1`,
		`"@type":"BreadcrumbList"`,
		`<meta property="og:url" content="https://watplan.me/plan/WMT26AX3S1">`,
		`<meta property="og:image" content="https://watplan.me/plan/WMT26AX3S1/og.png">`,
		`<meta property="og:image:alt" content="Plan zajęć WMT26AX3S1 – Wydział Mechatroniki, Uzbrojenia i Lotnictwa">`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`<meta property="og:title" content="Plan zajęć WMT26AX3S1 – Wydział Mechatroniki, Uzbrojenia i Lotnictwa">`,
		`<p class="message">Plan zajęć grupy WMT26AX3S1 – Wydział Mechatroniki`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/plan/WMT26AX3S1 bez %q", want)
		}
	}
	if strings.Contains(body, "noindex") {
		t.Error("strona grupy ma noindex")
	}
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("Cache-Control") != cacheHTML {
		t.Fatalf("ETag %q, Cache-Control %q", etag, rec.Header().Get("Cache-Control"))
	}
	if again := get("/plan/WMT26AX3S1", "If-None-Match", etag); again.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match: %d", again.Code)
	}
	if wcy := get("/plan/WCY26KY4S1/").Body.String(); !strings.Contains(wcy, "Wydział Cybernetyki") {
		t.Fatal("brak nazwy wydziału WCY (ze slashem na końcu)")
	}

	qrRec := get("/plan/WMT26AX3S1/qr.svg")
	if qrRec.Code != http.StatusOK || qrRec.Header().Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(qrRec.Body.String(), "<svg") {
		t.Fatalf("qr.svg: %d %q", qrRec.Code, qrRec.Header().Get("Content-Type"))
	}
	if get("/plan/NIEMA/qr.svg").Code != http.StatusNotFound {
		t.Fatal("qr.svg dla nieznanej grupy powinien dawać 404")
	}

	missing := get("/plan/NIEMA")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "noindex") {
		t.Fatalf("nieznana grupa: %d", missing.Code)
	}
	if !strings.Contains(get("/app").Body.String(), `content="noindex, follow"`) {
		t.Fatal("/app nie powinno być indeksowane")
	}
}

func TestQRSVG(t *testing.T) {
	out, err := qrSVG("https://watplan.me/plan/WMT26AX3S1")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `viewBox="0 0 37 37"`) || strings.Count(s, "h1v1h-1z") < 200 {
		t.Fatalf("nieoczekiwany kod QR: %.120s", s)
	}
	again, _ := qrSVG("https://watplan.me/plan/WMT26AX3S1")
	if string(again) != s {
		t.Fatal("kod QR nie jest deterministyczny")
	}
}

func TestSitemapAndRobots(t *testing.T) {
	h := planPageServer(t)
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "watplan.me"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := get("/sitemap.xml")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/xml") {
		t.Fatalf("status %d, %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		"<loc>https://watplan.me/</loc>",
		"<loc>https://watplan.me/o-watplan</loc>",
		"<loc>https://watplan.me/plany</loc>",
		"<loc>https://watplan.me/plan/WMT26AX3S1</loc>\n    <lastmod>2026-11-19</lastmod>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap bez %q", want)
		}
	}
	if strings.Contains(body, "WCY26KY4S1") {
		t.Error("sitemap zawiera grupę bez pobranego planu")
	}
	rec = get("/robots.txt")
	for _, want := range []string{"Allow: /api/plan/", "Disallow: /api/", "Sitemap: https://watplan.me/sitemap.xml"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("robots.txt bez %q", want)
		}
	}
}

func TestBotsDoNotTriggerFetches(t *testing.T) {
	env := newRefreshEnv(t, gone, checkedAgo(time.Hour))
	for _, ua := range []string{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "UptimeRobot/2.0", "Mozilla/5.0 (compatible; bingbot/2.0)"} {
		req := httptest.NewRequest(http.MethodGet, "/api/plan/"+refreshTestPlan, nil)
		req.Header.Set("User-Agent", ua)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("X-Plan-Refresh") != "none" {
			t.Fatalf("%s: status %d, X-Plan-Refresh %q", ua, rec.Code, rec.Header().Get("X-Plan-Refresh"))
		}
	}
	if env.apiHits.Load() != 0 {
		t.Fatalf("zapytań do API po wejściach botów = %d, chcę 0", env.apiHits.Load())
	}
	if got := env.visit(t); got != "background" {
		t.Fatalf("zwykły użytkownik: %q, chcę background", got)
	}
}

func TestAllPlansPage(t *testing.T) {
	h := planPageServer(t)
	req := httptest.NewRequest(http.MethodGet, "/plany", nil)
	req.Host = "watplan.me"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		"<title>Plany zajęć wszystkich grup WAT | WATplan</title>",
		`<link rel="canonical" href="https://watplan.me/plany">`,
		`<meta property="og:url" content="https://watplan.me/plany">`,
		`<meta property="og:image" content="https://watplan.me/og.png">`,
		`<a href="#WCY">WCY</a><a href="#WML">WML</a>`,
		`<h2 id="h-WCY"><a href="/plany/wcy">Wydział Cybernetyki</a> <span>WCY, 1 grupa</span></h2>`,
		`<li><a href="/plan/WMT26AX3S1">WMT26AX3S1</a></li>`,
		`<li><a href="/plan/WCY26KY4S1">WCY26KY4S1</a></li>`,
		`<footer class="site-foot">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/plany bez %q", want)
		}
	}
	for _, unwanted := range []string{`"FAQPage"`, "noindex", "<details"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("/plany zawiera %q", unwanted)
		}
	}
}

func TestGroupsWord(t *testing.T) {
	for n, want := range map[int]string{1: "1 grupa", 2: "2 grupy", 4: "4 grupy", 5: "5 grup", 12: "12 grup", 14: "14 grup", 22: "22 grupy", 112: "112 grup", 182: "182 grupy", 831: "831 grup"} {
		if got := groupsWord(n); got != want {
			t.Errorf("groupsWord(%d) = %q, chcę %q", n, got, want)
		}
	}
}

func TestShareCards(t *testing.T) {
	h := planPageServer(t)
	get := func(path string, header ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "watplan.me"
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, path := range []string{"/og.png", "/plan/WMT26AX3S1/og.png", "/plan/WCY26KY4S1/og.png"} {
		rec := get(path)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("%s: %d %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		cfg, err := png.DecodeConfig(rec.Body)
		if err != nil || cfg.Width != 1200 || cfg.Height != 630 {
			t.Fatalf("%s: %v %dx%d", path, err, cfg.Width, cfg.Height)
		}
		if !strings.HasPrefix(rec.Header().Get("Cache-Control"), "public") {
			t.Fatalf("%s: Cache-Control %q", path, rec.Header().Get("Cache-Control"))
		}
		if again := get(path, "If-None-Match", rec.Header().Get("ETag")); again.Code != http.StatusNotModified {
			t.Fatalf("%s: If-None-Match %d", path, again.Code)
		}
	}
	if a, b := get("/plan/WMT26AX3S1/og.png").Body.String(), get("/plan/WCY26KY4S1/og.png").Body.String(); a == b {
		t.Fatal("grafiki różnych grup są takie same")
	}
	if get("/plan/NIEMA/og.png").Code != http.StatusNotFound {
		t.Fatal("grafika nieznanej grupy powinna dawać 404")
	}
	for _, page := range []string{"/", "/o-watplan"} {
		if body := get(page).Body.String(); !strings.Contains(body, `<meta property="og:image" content="https://watplan.me/og.png">`) {
			t.Errorf("%s bez og:image", page)
		}
	}
}

func TestPlanTitleAndDescriptionLength(t *testing.T) {
	for code := range facultyNames {
		plan := config.PlanConfig{ID: code + "26XX9S1", Faculty: code}
		title := planTitle(plan)
		if n := utf8.RuneCountInString(title); n > maxTitle {
			t.Errorf("%s: tytuł ma %d znaków: %q", code, n, title)
		}
		page := string(renderPlanPage([]byte(`<title>x</title><meta name="description" content="x"><main id="view" class="view"></main></head>`), plan, "https://watplan.me"))
		desc := descriptionRe.FindString(page)
		if n := utf8.RuneCountInString(desc) - len(`<meta name="description" content="">`); n > 160 {
			t.Errorf("%s: opis ma %d znaków", code, n)
		}
	}
	if got := planTitle(config.PlanConfig{ID: "WCY26KY4S1", Faculty: "WCY"}); got != "Plan zajęć WCY26KY4S1 – Wydział Cybernetyki, WAT | WATplan" {
		t.Errorf("krótka nazwa wydziału powinna zostać w tytule: %q", got)
	}
}

func TestFacultyPages(t *testing.T) {
	h := planPageServer(t)
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "watplan.me"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := get("/plany/wml")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		"<title>Plan zajęć WML WAT – Wydział Mechatroniki, Uzbrojenia i Lotnictwa</title>",
		`<link rel="canonical" href="https://watplan.me/plany/wml">`,
		`<meta property="og:url" content="https://watplan.me/plany/wml">`,
		`<h1 class="faq-h1">Plan zajęć – Wydział Mechatroniki, Uzbrojenia i Lotnictwa (WML)</h1>`,
		`<li><a href="/plan/WMT26AX3S1">WMT26AX3S1</a></li>`,
		`"@type":"BreadcrumbList"`,
		`"item":"https://watplan.me/plany/wml"`,
		"plan lekcji",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/plany/wml bez %q", want)
		}
	}
	if strings.Contains(body, "WCY26KY4S1") {
		t.Error("strona WML zawiera grupę WCY")
	}
	if get("/plany/WCY/").Code != http.StatusOK {
		t.Error("/plany/WCY/ (wielkie litery, ukośnik) powinno działać")
	}
	if get("/plany/xyz").Code != http.StatusNotFound {
		t.Error("nieznany wydział powinien dawać 404")
	}
	if !strings.Contains(get("/plany").Body.String(), `<a href="/plany/wml">Wydział Mechatroniki`) {
		t.Error("/plany nie linkuje do strony wydziału")
	}
	if !strings.Contains(get("/plan/WMT26AX3S1").Body.String(), `"item":"https://watplan.me/plany/wml"`) {
		t.Error("okruszki strony grupy nie linkują do wydziału")
	}
	sitemap := get("/sitemap.xml").Body.String()
	for _, want := range []string{"<loc>https://watplan.me/plany/wml</loc>", "<loc>https://watplan.me/plany/wcy</loc>"} {
		if !strings.Contains(sitemap, want) {
			t.Errorf("sitemap bez %q", want)
		}
	}
}

func TestFacultyTitlesFit(t *testing.T) {
	for code, name := range facultyNames {
		title := html.UnescapeString(facultyTitle(code, name))
		if n := utf8.RuneCountInString(title); n > maxFacultyTitle {
			t.Errorf("%s: %d znaków: %q", code, n, title)
		}
		if !strings.Contains(title, code) || !strings.Contains(title, name) {
			t.Errorf("%s: tytuł bez skrótu albo pełnej nazwy: %q", code, title)
		}
	}
}
