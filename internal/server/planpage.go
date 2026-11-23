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
	"unicode/utf8"

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

const maxTitle = 60

func facultyCode(plan config.PlanConfig) string {
	if plan.Faculty != "" {
		return plan.Faculty
	}
	if len(plan.ID) >= 3 {
		return strings.ToUpper(plan.ID[:3])
	}
	return ""
}

func planTitle(plan config.PlanConfig) string {
	id := html.EscapeString(plan.ID)
	full := fmt.Sprintf("Plan zajęć %s – %s, WAT | WATplan", id, html.EscapeString(facultyName(plan)))
	if utf8.RuneCountInString(html.UnescapeString(full)) <= maxTitle {
		return full
	}
	return fmt.Sprintf("Plan zajęć %s – %s WAT | WATplan", id, html.EscapeString(facultyCode(plan)))
}

func renderPlanPage(tpl []byte, plan config.PlanConfig, site string, ssr *ssrPage) []byte {
	id := html.EscapeString(plan.ID)
	faculty := html.EscapeString(facultyName(plan))
	title := planTitle(plan)
	desc := fmt.Sprintf("Plan zajęć grupy %s (%s WAT): dziś, tydzień i miesiąc, sale, prowadzący, eksport do kalendarza. Bez logowania.", id, faculty)
	ogTitle := fmt.Sprintf("Plan zajęć %s – %s", id, faculty)
	canonical := site + "/plan/" + id
	crumbs, _ := json.Marshal(map[string]any{
		"@context": "https://schema.org",
		"@type":    "BreadcrumbList",
		"itemListElement": []map[string]any{
			{"@type": "ListItem", "position": 1, "name": "WATplan", "item": site + "/"},
			{"@type": "ListItem", "position": 2, "name": facultyName(plan), "item": site + facultyPath(facultyCode(plan))},
			{"@type": "ListItem", "position": 3, "name": plan.ID, "item": canonical},
		},
	})
	out := titleRe.ReplaceAll(tpl, []byte("<title>"+title+"</title>"))
	out = descriptionRe.ReplaceAll(out, []byte(`<meta name="description" content="`+desc+`">`))
	out = ogTitleRe.ReplaceAll(out, []byte(`<meta property="og:title" content="`+ogTitle+`">`))
	out = ogDescRe.ReplaceAll(out, []byte(`<meta property="og:description" content="`+desc+`">`+"\n"+`<meta property="og:type" content="website">`+"\n"+`<meta property="og:url" content="`+canonical+`">`+"\n"+shareImageTags(canonical+"/og.png", "Plan zajęć "+id+" – "+faculty)))
	out = robotsRe.ReplaceAll(out, []byte(`<meta name="robots" content="index, follow">`))
	out = canonicalRe.ReplaceAll(out, []byte(`<link rel="canonical" href="`+canonical+`">`))
	out = bytes.Replace(out, []byte(`<main id="view"`), []byte(`<h1 class="sr-only">Plan zajęć `+id+`</h1>`+"\n"+`<main id="view"`), 1)
	out = bytes.Replace(out, []byte(`<span id="groupLabel">Wybierz grupę</span>`), []byte(`<span id="groupLabel">`+id+`</span>`), 1)
	if ssr != nil {
		out = viewRe.ReplaceAllLiteral(out, []byte(`<main id="view" class="view">`+ssr.view+`</main>`+"\n"+ssr.info+"\n"+`<script type="application/json" id="ssrPlan" data-id="`+id+`">`+string(ssr.data)+`</script>`))
		out = bytes.Replace(out, []byte("</head>"), []byte(`<meta name="watplan-ssr" content="`+id+`">`+"\n</head>"), 1)
	} else {
		out = viewRe.ReplaceAll(out, []byte(`${1}<p class="message">Plan zajęć grupy `+id+` – `+faculty+`, Wojskowa Akademia Techniczna.</p>${2}`))
	}
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
		var ssr *ssrPage
		if raw, err := s.store.Raw(plan.ID); err == nil {
			now := time.Now
			if s.options.Now != nil {
				now = s.options.Now
			}
			ssr = renderSSR(plan.ID, raw, now())
		}
		page := renderPlanPage(tpl, plan, s.siteURL(r), ssr)
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

type facultyGroups struct {
	code string
	ids  []string
}

func groupByFaculty(plans []config.PlanConfig) []facultyGroups {
	byFaculty := map[string][]string{}
	for _, p := range plans {
		code := facultyCode(p)
		byFaculty[code] = append(byFaculty[code], p.ID)
	}
	out := make([]facultyGroups, 0, len(byFaculty))
	for code, ids := range byFaculty {
		sort.Strings(ids)
		out = append(out, facultyGroups{code, ids})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].code < out[j].code })
	return out
}

func facultyPath(code string) string {
	return allPlansAt + "/" + strings.ToLower(code)
}

func groupLinks(b *strings.Builder, ids []string) {
	b.WriteString(`<ul class="group-links">`)
	for _, id := range ids {
		esc := html.EscapeString(id)
		fmt.Fprintf(b, `<li><a href="/plan/%s">%s</a></li>`, esc, esc)
	}
	b.WriteString("</ul>\n")
}

type listPage struct {
	title, desc, path, body string
	crumbs                  []map[string]any
}

func renderListPage(tpl []byte, site string, page listPage) []byte {
	out := faqLDRe.ReplaceAll(tpl, nil)
	out = faqBodyRe.ReplaceAllLiteral(out, []byte(page.body))
	out = titleRe.ReplaceAll(out, []byte("<title>"+page.title+"</title>"))
	out = descriptionRe.ReplaceAll(out, []byte(`<meta name="description" content="`+page.desc+`">`))
	out = ogTitleRe.ReplaceAll(out, []byte(`<meta property="og:title" content="`+page.title+`">`))
	out = ogDescRe.ReplaceAll(out, []byte(`<meta property="og:description" content="`+page.desc+`">`))
	out = canonicalRe.ReplaceAll(out, []byte(`<link rel="canonical" href="`+site+page.path+`">`))
	out = ogURLRe.ReplaceAll(out, []byte(`<meta property="og:url" content="`+site+page.path+`">`))
	if len(page.crumbs) > 0 {
		for i, c := range page.crumbs {
			c["@type"] = "ListItem"
			c["position"] = i + 1
		}
		ld, _ := json.Marshal(map[string]any{"@context": "https://schema.org", "@type": "BreadcrumbList", "itemListElement": page.crumbs})
		out = bytes.Replace(out, []byte("</head>"), []byte(`<script type="application/ld+json">`+string(ld)+"</script>\n</head>"), 1)
	}
	return out
}

func renderAllPlans(tpl []byte, plans []config.PlanConfig, site string) []byte {
	groups := groupByFaculty(plans)
	var b strings.Builder
	fmt.Fprintf(&b, "<h1 class=\"faq-h1\">Plany zajęć wszystkich grup WAT</h1>\n  <p class=\"faq-lead\">%s ze wszystkich wydziałów Wojskowej Akademii Technicznej. Wybierz swoją, a zobaczysz plan na dziś, tydzień i miesiąc.</p>\n", groupsWord(len(plans)))
	b.WriteString("  <nav class=\"faculty-jump\" aria-label=\"Wydziały\">")
	for _, g := range groups {
		fmt.Fprintf(&b, `<a href="#%s">%s</a>`, g.code, g.code)
	}
	b.WriteString("</nav>\n")
	for _, g := range groups {
		name := html.EscapeString(facultyName(config.PlanConfig{Faculty: g.code}))
		fmt.Fprintf(&b, "  <section class=\"faculty-groups\" id=\"%s\" aria-labelledby=\"h-%s\">\n    <h2 id=\"h-%s\"><a href=\"%s\">%s</a> <span>%s, %s</span></h2>\n    ", g.code, g.code, g.code, facultyPath(g.code), name, g.code, groupsWord(len(g.ids)))
		groupLinks(&b, g.ids)
		b.WriteString("  </section>\n")
	}
	return renderListPage(tpl, site, listPage{
		title: "Plany zajęć wszystkich grup WAT | WATplan",
		desc:  fmt.Sprintf("Lista %d grup WAT ze wszystkich wydziałów z linkami do planów zajęć: dzień, tydzień, miesiąc i eksport do kalendarza. Za darmo i bez logowania.", len(plans)),
		path:  allPlansAt,
		body:  b.String(),
	})
}

const maxFacultyTitle = 65

func facultyTitle(code, name string) string {
	for _, t := range []string{
		fmt.Sprintf("Plan zajęć %s WAT – %s | WATplan", code, name),
		fmt.Sprintf("Plan zajęć %s WAT – %s", code, name),
	} {
		if utf8.RuneCountInString(t) <= maxFacultyTitle {
			return html.EscapeString(t)
		}
	}
	return html.EscapeString(fmt.Sprintf("Plan zajęć %s WAT – wszystkie grupy | WATplan", code))
}

func renderFacultyPages(tpl []byte, plans []config.PlanConfig, site string) map[string][]byte {
	pages := map[string][]byte{}
	for _, g := range groupByFaculty(plans) {
		full := facultyName(config.PlanConfig{Faculty: g.code})
		name := html.EscapeString(full)
		var b strings.Builder
		fmt.Fprintf(&b, "<h1 class=\"faq-h1\">Plan zajęć – %s (%s)</h1>\n  <p class=\"faq-lead\">%s Wojskowej Akademii Technicznej. Wybierz swoją grupę, a zobaczysz plan zajęć (plan lekcji) na dziś, tydzień i miesiąc, z salami i prowadzącymi.</p>\n  <p class=\"faq-lead\"><a href=\"%s\">Wszystkie wydziały</a></p>\n  <section class=\"faculty-groups\" aria-label=\"Grupy\">\n    ", name, g.code, groupsWord(len(g.ids))+" – "+name, allPlansAt)
		groupLinks(&b, g.ids)
		b.WriteString("  </section>\n")
		path := facultyPath(g.code)
		pages[strings.ToLower(g.code)] = renderListPage(tpl, site, listPage{
			title: facultyTitle(g.code, full),
			desc:  html.EscapeString(fmt.Sprintf("Plan zajęć %s WAT (%s): %s z planem na dziś, tydzień i miesiąc, sale, prowadzący i eksport do kalendarza. Bez logowania.", g.code, full, groupsWord(len(g.ids)))),
			path:  path,
			body:  b.String(),
			crumbs: []map[string]any{
				{"name": "WATplan", "item": site + "/"},
				{"name": "Wszystkie grupy", "item": site + allPlansAt},
				{"name": full, "item": site + path},
			},
		})
	}
	return pages
}

func (s *Server) handleFacultyPage(pages map[string][]byte) http.HandlerFunc {
	handlers := map[string]http.HandlerFunc{}
	for code, page := range pages {
		handlers[code] = htmlPage(page)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.ToLower(strings.Trim(strings.TrimPrefix(r.URL.Path, allPlansAt+"/"), "/"))
		if h, ok := handlers[code]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}
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
	for _, g := range groupByFaculty(s.registry.List()) {
		entry(site+facultyPath(g.code), today)
	}
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
