package server

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"rsc.io/qr"
)

var facultyNames = map[string]string{
	"WCY": "Wydział Cybernetyki",
	"WEL": "Wydział Elektroniki",
	"WIG": "Wydział Inżynierii Lądowej i Geodezji",
	"WIM": "Wydział Inżynierii Mechanicznej",
	"WLO": "Wydział Logistyki",
	"WML": "Wydział Mechatroniki, Uzbrojenia i Lotnictwa",
	"WTC": "Wydział Nowych Technologii i Chemii",
	"IOE": "Instytut Optoelektroniki",
}

var (
	titleRe       = regexp.MustCompile(`<title>[^<]*</title>`)
	descriptionRe = regexp.MustCompile(`<meta name="description" content="[^"]*">`)
	ogTitleRe     = regexp.MustCompile(`<meta property="og:title" content="[^"]*">`)
	ogDescRe      = regexp.MustCompile(`<meta property="og:description" content="[^"]*">`)
	robotsRe      = regexp.MustCompile(`<meta name="robots" content="[^"]*">`)
	canonicalRe   = regexp.MustCompile(`<link rel="canonical" href="[^"]*">`)
	ogURLRe       = regexp.MustCompile(`<meta property="og:url" content="[^"]*">`)
	viewRe        = regexp.MustCompile(`(<main id="view"[^>]*>)(</main>)`)
	botRe         = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|facebookexternalhit|embedly|preview`)
)

func isBot(r *http.Request) bool {
	return botRe.MatchString(r.Header.Get("User-Agent"))
}

func facultyName(plan config.PlanConfig) string {
	code := plan.Faculty
	if code == "" && len(plan.ID) >= 3 {
		code = strings.ToUpper(plan.ID[:3])
	}
	if name, ok := facultyNames[code]; ok {
		return name
	}
	return "Wojskowa Akademia Techniczna"
}

func (s *Server) siteURL(r *http.Request) string {
	if s.options.CanonicalHost != "" {
		return "https://" + s.options.CanonicalHost
	}
	return s.baseURL(r)
}

func renderPlanPage(tpl []byte, plan config.PlanConfig, site string) []byte {
	id := html.EscapeString(plan.ID)
	faculty := html.EscapeString(facultyName(plan))
	title := fmt.Sprintf("Plan zajęć %s – %s, WAT | WATplan", id, faculty)
	desc := fmt.Sprintf("Plan zajęć grupy %s (%s WAT): zajęcia na dziś, tydzień i miesiąc, sale, prowadzący i eksport do kalendarza. Za darmo i bez logowania.", id, faculty)
	ogTitle := fmt.Sprintf("Plan zajęć %s – %s", id, faculty)
	canonical := site + "/plan/" + id
	crumbs, _ := json.Marshal(map[string]any{
		"@context": "https://schema.org",
		"@type":    "BreadcrumbList",
		"itemListElement": []map[string]any{
			{"@type": "ListItem", "position": 1, "name": "WATplan", "item": site + "/"},
			{"@type": "ListItem", "position": 2, "name": facultyName(plan)},
			{"@type": "ListItem", "position": 3, "name": plan.ID, "item": canonical},
		},
	})
	out := titleRe.ReplaceAll(tpl, []byte("<title>"+title+"</title>"))
	out = descriptionRe.ReplaceAll(out, []byte(`<meta name="description" content="`+desc+`">`))
	out = ogTitleRe.ReplaceAll(out, []byte(`<meta property="og:title" content="`+ogTitle+`">`))
	out = ogDescRe.ReplaceAll(out, []byte(`<meta property="og:description" content="`+desc+`">`+"\n"+`<meta property="og:type" content="website">`+"\n"+`<meta property="og:url" content="`+canonical+`">`+"\n"+shareImageTags(canonical+"/og.png", "Plan zajęć "+id+" – "+faculty)))
	out = robotsRe.ReplaceAll(out, []byte(`<meta name="robots" content="index, follow">`))
	out = canonicalRe.ReplaceAll(out, []byte(`<link rel="canonical" href="`+canonical+`">`))
	out = viewRe.ReplaceAll(out, []byte(`${1}<p class="message">Plan zajęć grupy `+id+` – `+faculty+`, Wojskowa Akademia Techniczna.</p>${2}`))
	return bytes.Replace(out, []byte("</head>"), []byte(`<script type="application/ld+json">`+string(crumbs)+"</script>\n</head>"), 1)
}

func qrSVG(text string) ([]byte, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	const quiet = 4
	size := code.Size + 2*quiet
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, size, size, size, size)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.Bytes(), nil
}

func (s *Server) handlePlanQR(w http.ResponseWriter, r *http.Request, plan config.PlanConfig) {
	svg, err := qrSVG(s.siteURL(r) + "/plan/" + plan.ID)
	if err != nil {
		http.Error(w, "Nie udało się utworzyć kodu QR.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Write(svg)
}

func (s *Server) handlePlanPage(tpl []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/plan/"), "/")
		if base, ok := strings.CutSuffix(id, "/og.png"); ok {
			if plan, found := s.registry.Get(base); found && s.cards != nil {
				s.cards.servePlan(w, r, plan)
				return
			}
			http.NotFound(w, r)
			return
		}
		if base, ok := strings.CutSuffix(id, "/qr.svg"); ok {
			if plan, found := s.registry.Get(base); found {
				s.handlePlanQR(w, r, plan)
				return
			}
			http.NotFound(w, r)
			return
		}
		plan, ok := s.registry.Get(id)
		if !ok {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", cacheHTML)
			w.WriteHeader(http.StatusNotFound)
			w.Write(tpl)
			return
		}
		page := renderPlanPage(tpl, plan, s.siteURL(r))
		sum := sha1.Sum(page)
		etag := `"` + hex.EncodeToString(sum[:10]) + `"`
		w.Header().Set("Cache-Control", cacheHTML)
		w.Header().Set("ETag", etag)
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	}
}

var (
	faqLDRe    = regexp.MustCompile(`(?s)<script type="application/ld\+json">\s*\{"@context":"https://schema.org","@type":"FAQPage".*?</script>\n`)
	faqBodyRe  = regexp.MustCompile(`(?s)<h1 class="faq-h1">.*</section>\n`)
	allPlansAt = "/plany"
)

func groupsWord(n int) string {
	switch {
	case n == 1:
		return "1 grupa"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return fmt.Sprintf("%d grupy", n)
	default:
		return fmt.Sprintf("%d grup", n)
	}
}

func renderAllPlans(tpl []byte, plans []config.PlanConfig, site string) []byte {
	byFaculty := map[string][]string{}
	for _, p := range plans {
		code := p.Faculty
		if code == "" && len(p.ID) >= 3 {
			code = strings.ToUpper(p.ID[:3])
		}
		byFaculty[code] = append(byFaculty[code], p.ID)
	}
	codes := make([]string, 0, len(byFaculty))
	for code := range byFaculty {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	var b strings.Builder
	fmt.Fprintf(&b, "<h1 class=\"faq-h1\">Plany zajęć wszystkich grup WAT</h1>\n  <p class=\"faq-lead\">%s ze wszystkich wydziałów Wojskowej Akademii Technicznej. Wybierz swoją, a zobaczysz plan na dziś, tydzień i miesiąc.</p>\n", groupsWord(len(plans)))
	b.WriteString("  <nav class=\"faculty-jump\" aria-label=\"Wydziały\">")
	for _, code := range codes {
		fmt.Fprintf(&b, `<a href="#%s">%s</a>`, code, code)
	}
	b.WriteString("</nav>\n")
	for _, code := range codes {
		ids := byFaculty[code]
		sort.Strings(ids)
		name := facultyName(config.PlanConfig{Faculty: code})
		fmt.Fprintf(&b, "  <section class=\"faculty-groups\" id=\"%s\" aria-labelledby=\"h-%s\">\n    <h2 id=\"h-%s\">%s <span>%s, %s</span></h2>\n    <ul class=\"group-links\">", code, code, code, html.EscapeString(name), code, groupsWord(len(ids)))
		for _, id := range ids {
			esc := html.EscapeString(id)
			fmt.Fprintf(&b, `<li><a href="/plan/%s">%s</a></li>`, esc, esc)
		}
		b.WriteString("</ul>\n  </section>\n")
	}
	title := "Plany zajęć wszystkich grup WAT | WATplan"
	desc := fmt.Sprintf("Lista %d grup WAT ze wszystkich wydziałów z linkami do planów zajęć: dzień, tydzień, miesiąc i eksport do kalendarza. Za darmo i bez logowania.", len(plans))
	out := faqLDRe.ReplaceAll(tpl, nil)
	out = faqBodyRe.ReplaceAllLiteral(out, []byte(b.String()))
	out = titleRe.ReplaceAll(out, []byte("<title>"+title+"</title>"))
	out = descriptionRe.ReplaceAll(out, []byte(`<meta name="description" content="`+desc+`">`))
	out = ogTitleRe.ReplaceAll(out, []byte(`<meta property="og:title" content="`+title+`">`))
	out = ogDescRe.ReplaceAll(out, []byte(`<meta property="og:description" content="`+desc+`">`))
	out = canonicalRe.ReplaceAll(out, []byte(`<link rel="canonical" href="`+site+allPlansAt+`">`))
	return ogURLRe.ReplaceAll(out, []byte(`<meta property="og:url" content="`+site+allPlansAt+`">`))
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	site := s.siteURL(r)
	today := time.Now().In(warsawLoc).Format("2006-01-02")
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	entry := func(loc, lastmod string) {
		fmt.Fprintf(&b, "  <url>\n    <loc>%s</loc>\n    <lastmod>%s</lastmod>\n  </url>\n", html.EscapeString(loc), lastmod)
	}
	entry(site+"/", today)
	entry(site+"/o-watplan", today)
	entry(site+allPlansAt, today)
	plans := s.registry.List()
	sort.Slice(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })
	for _, p := range plans {
		data, err := s.store.Raw(p.ID)
		if err != nil {
			continue
		}
		lastmod := today
		if t := readPlanTimes(data); t.HasModified {
			lastmod = t.Modified.Format("2006-01-02")
		}
		entry(site+"/plan/"+p.ID, lastmod)
	}
	b.WriteString("</urlset>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(b.String()))
}
