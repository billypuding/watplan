package wat

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/billypuding/watplan/internal/config"
)

var (
	planFileRe   = regexp.MustCompile(`(?i)^([A-Z]{3})(\d{2})[A-Z0-9]{2,}$`)
	wcyGroupRe   = regexp.MustCompile(`grupa_id=([^"&#\s]+)`)
	wcyPlainID   = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	wcySubID     = regexp.MustCompile(`^([A-Za-z0-9]+)\*([A-Za-z0-9]+)$`)
	wrappedIDRe  = regexp.MustCompile(`^\(([A-Za-z0-9]+)\)`)
	courseFileRe = regexp.MustCompile(`^\d[A-Z]{2}(\d{2})[A-Z]{2}$`)
	namedFileRe  = regexp.MustCompile(`^([A-Z]{3})_([A-Z][a-z]+)$`)
	attrValueRe  = regexp.MustCompile(`(?i)(?:href|src|value|url|file|path)\s*=\s*["']([^"']+)["']`)
	rawHtmFileRe = regexp.MustCompile(`(?i)\b([a-z0-9_/-]+\.html?)\b`)
)

func (f *Fetcher) DiscoverPlans(src config.Source) ([]config.PlanConfig, error) {
	base, err := url.Parse(src.IndexURL)
	if err != nil {
		return nil, fmt.Errorf("zły index_url %q: %w", src.IndexURL, err)
	}

	plans, err := f.discover(base, src)
	for i := range plans {
		plans[i].Semester = src.Semester
	}
	return plans, err
}

func (f *Fetcher) discover(base *url.URL, src config.Source) ([]config.PlanConfig, error) {
	switch src.Format {
	case FormatWCY:
		return f.discoverWCY(base, src)
	case FormatWIGPDF:
		return f.discoverWIG(base, src)
	}

	found := map[string]config.PlanConfig{}
	visited := map[string]bool{}

	var err error
	if src.Depth == 0 {
		key := base.String()
		prev, cached := f.cachedListing(key)
		var body []byte
		var got Validators
		body, got, err = f.FetchPage(key, prev.validators)
		if errors.Is(err, ErrNotModified) && cached {
			return append([]config.PlanConfig(nil), prev.plans...), nil
		}
		if err == nil {
			visited[key] = true
			if err := f.collect(base, base, body, src, 0, visited, found); err != nil {
				return nil, err
			}
			plans := listingPlans(found)
			f.storeListing(key, got, plans)
			return plans, nil
		}
		err = fmt.Errorf("%s: %w", key, err)
	} else {
		err = f.crawl(base, base, src, src.Depth, visited, found)
	}
	if alt := siblingIndex(base); err != nil && errors.Is(err, ErrGone) && alt != nil {
		log.Printf("discover %s: %s zniknął, próbuję %s", src.Faculty, base, alt)
		if altErr := f.crawl(alt, alt, src, src.Depth, visited, found); altErr == nil {
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}
	return listingPlans(found), nil
}

type listing struct {
	validators Validators
	plans      []config.PlanConfig
}

func listingPlans(found map[string]config.PlanConfig) []config.PlanConfig {
	plans := make([]config.PlanConfig, 0, len(found))
	for _, p := range found {
		plans = append(plans, p)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })
	return plans
}

func (f *Fetcher) cachedListing(key string) (listing, bool) {
	f.listMu.Lock()
	defer f.listMu.Unlock()
	l, ok := f.listings[key]
	return l, ok
}

func (f *Fetcher) storeListing(key string, v Validators, plans []config.PlanConfig) {
	if v.empty() {
		return
	}
	f.listMu.Lock()
	defer f.listMu.Unlock()
	if f.listings == nil {
		f.listings = map[string]listing{}
	}
	f.listings[key] = listing{validators: v, plans: append([]config.PlanConfig(nil), plans...)}
}

func (f *Fetcher) crawl(root, page *url.URL, src config.Source, depth int,
	visited map[string]bool, found map[string]config.PlanConfig) error {

	key := page.String()
	if visited[key] {
		return nil
	}
	visited[key] = true

	body, err := f.FetchHTML(key)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return f.collect(root, page, body, src, depth, visited, found)
}

func (f *Fetcher) collect(root, page *url.URL, body []byte, src config.Source, depth int,
	visited map[string]bool, found map[string]config.PlanConfig) error {

	var rawLinks []string
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		rawLinks = extractLinksRegex(body)
	} else {
		rawLinks = extractLinks(doc)
		rawLinks = append(rawLinks, extractLinksRegex(body)...)
	}

	var subpages []*url.URL
	for _, raw := range rawLinks {
		link, err := page.Parse(raw)
		if err != nil {
			continue
		}
		link.Fragment = ""

		if plan, ok := planFromURL(link, src); ok {
			if _, exists := found[plan.ID]; !exists {
				found[plan.ID] = plan
			}
			continue
		}
		if depth > 0 && link.Host == root.Host && isPage(link) {
			subpages = append(subpages, link)
		}
	}

	for _, sub := range subpages {
		if err := f.crawl(root, sub, src, depth-1, visited, found); err != nil {
			if errors.Is(err, ErrBlocked) {
				return err
			}
			log.Printf("discover %s: %v", src.Faculty, err)
		}
	}
	return nil
}

func extractLinks(n *html.Node) []string {
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			var attr string
			switch n.Data {
			case "a":
				attr = "href"
			case "frame", "iframe":
				attr = "src"
			case "option":
				attr = "value"
			}
			if attr != "" {
				for _, a := range n.Attr {
					if a.Key == attr && a.Val != "" {
						out = append(out, a.Val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

func extractLinksRegex(body []byte) []string {
	var out []string
	seen := make(map[string]bool)

	for _, m := range attrValueRe.FindAllSubmatch(body, -1) {
		val := string(m[1])
		if !seen[val] {
			seen[val] = true
			out = append(out, val)
		}
	}

	for _, m := range rawHtmFileRe.FindAllSubmatch(body, -1) {
		val := string(m[1])
		if !seen[val] {
			seen[val] = true
			out = append(out, val)
		}
	}

	return out
}

func planFromURL(u *url.URL, src config.Source) (config.PlanConfig, bool) {
	file := path.Base(u.Path)
	ext := strings.ToLower(path.Ext(file))

	var planPath string
	switch {
	case ext == ".htm" || ext == ".html":
		planPath = u.Path
	case src.PDFLinks && ext == ".pdf":
		planPath = strings.TrimSuffix(u.Path, path.Ext(u.Path)) + ".htm"
	default:
		return config.PlanConfig{}, false
	}

	name := strings.TrimSuffix(file, path.Ext(file))
	if w := wrappedIDRe.FindStringSubmatch(name); w != nil {
		name = w[1]
	}

	id, display := strings.ToUpper(name), ""
	yy := 0
	switch {
	case planFileRe.MatchString(name):
		yy, _ = strconv.Atoi(planFileRe.FindStringSubmatch(name)[2])
	case courseFileRe.MatchString(name):
		yy, _ = strconv.Atoi(courseFileRe.FindStringSubmatch(name)[1])
	case namedFileRe.MatchString(name) && namedFileRe.FindStringSubmatch(name)[1] == src.Faculty:
		id = strings.ReplaceAll(id, "_", "-")
		display = strings.ReplaceAll(name, "_", " ")
	default:
		return config.PlanConfig{}, false
	}
	if display == "" {
		display = id
	}

	startYear := 2000 + yy
	if src.StartYear != 0 {
		startYear = src.StartYear
	}

	planURL := *u
	planURL.Path = planPath
	planURL.RawQuery = ""

	return config.PlanConfig{
		ID:        id,
		Name:      display,
		URL:       planURL.String(),
		StartYear: startYear,
		Faculty:   src.Faculty,
	}, true
}

func siblingIndex(u *url.URL) *url.URL {
	dir, file := path.Split(u.Path)
	var other string
	switch strings.ToLower(file) {
	case "index.xml":
		other = "index.html"
	case "index.html", "index.htm":
		other = "index.xml"
	default:
		return nil
	}
	alt := *u
	alt.Path = dir + other
	return &alt
}

func isPage(u *url.URL) bool {
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	return ext == "" || ext == ".htm" || ext == ".html" || ext == ".php" || ext == ".xml"
}

func (f *Fetcher) discoverWCY(base *url.URL, src config.Source) ([]config.PlanConfig, error) {
	body, err := f.FetchHTML(base.String())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	prefix := strings.ToUpper(src.Prefix)
	seen := map[string]bool{}
	seenRaw := map[string]bool{}
	var plans []config.PlanConfig
	for _, m := range wcyGroupRe.FindAllSubmatch(body, -1) {
		raw, err := url.QueryUnescape(string(m[1]))
		if err != nil {
			continue
		}
		var id string
		switch {
		case wcyPlainID.MatchString(raw):
			id = strings.ToUpper(raw)
			raw = id
		case wcySubID.MatchString(raw):
			sm := wcySubID.FindStringSubmatch(raw)
			id = strings.ToUpper(sm[1] + "-" + sm[2])
		default:
			continue
		}
		if seenRaw[raw] || (prefix != "" && !strings.HasPrefix(id, prefix)) {
			continue
		}
		seenRaw[raw] = true
		stem := id
		for n := 2; seen[id]; n++ {
			id = fmt.Sprintf("%s-%d", stem, n)
		}
		seen[id] = true
		name := id
		if raw != id {
			name = raw
		}
		planURL := *base
		planURL.RawQuery = "grupa_id=" + url.QueryEscape(raw)
		plans = append(plans, config.PlanConfig{
			ID:        id,
			Name:      name,
			URL:       planURL.String(),
			StartYear: src.StartYear,
			Faculty:   src.Faculty,
			Format:    FormatWCY,
		})
	}
	return plans, nil
}

var ErrStaleSource = errors.New("plany z poprzedniego roku akademickiego")

const staleRecheck = 24 * time.Hour

func (f *Fetcher) CheckCurrent(src config.Source, plans []config.PlanConfig) error {
	f.listMu.Lock()
	until, known := f.staleUntil[src.IndexURL]
	f.listMu.Unlock()
	if known && time.Now().Before(until) {
		return ErrStaleSource
	}
	err := f.checkCurrent(plans)
	f.listMu.Lock()
	defer f.listMu.Unlock()
	if f.staleUntil == nil {
		f.staleUntil = map[string]time.Time{}
	}
	if errors.Is(err, ErrStaleSource) {
		f.staleUntil[src.IndexURL] = time.Now().Add(staleRecheck)
	} else {
		delete(f.staleUntil, src.IndexURL)
	}
	return err
}

func (f *Fetcher) checkCurrent(plans []config.PlanConfig) error {
	const samples = 2
	tried := 0
	var lastErr error
	for _, p := range plans {
		if tried >= samples {
			break
		}
		tried++
		body, err := f.FetchHTML(p.URL)
		if err != nil {
			lastErr = err
			continue
		}
		events, err := Parse(p.Format, bytes.NewReader(body), p.StartYear)
		if err == nil && len(events) > 0 {
			return nil
		}
	}
	if lastErr != nil && tried == 1 {
		return lastErr
	}
	return ErrStaleSource
}
