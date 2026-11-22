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
	out = ogTitleRe.ReplaceAll(out, []byte(`<meta property="og:title" content="`+title+`">`))
	out = ogDescRe.ReplaceAll(out, []byte(`<meta property="og:description" content="`+desc+`">`))
	out = robotsRe.ReplaceAll(out, []byte(`<meta name="robots" content="index, follow">`))
	out = canonicalRe.ReplaceAll(out, []byte(`<link rel="canonical" href="`+canonical+`">`))
	out = viewRe.ReplaceAll(out, []byte(`${1}<p class="message">Plan zajęć grupy `+id+` – `+faculty+`, Wojskowa Akademia Techniczna.</p>${2}`))
	return bytes.Replace(out, []byte("</head>"), []byte(`<script type="application/ld+json">`+string(crumbs)+"</script>\n</head>"), 1)
}

func (s *Server) handlePlanPage(tpl []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/plan/"), "/")
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
