package wat

import (
	"bytes"
	"fmt"
	"log"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/billypuding/watplan/internal/config"
)

var (
	planFileRe   = regexp.MustCompile(`(?i)^([A-Z]{3})(\d{2})[A-Z0-9]{2,}$`)
	attrValueRe  = regexp.MustCompile(`(?i)(?:href|src|value|url|file|path)\s*=\s*["']([^"']+)["']`)
	rawHtmFileRe = regexp.MustCompile(`(?i)\b([a-z0-9_/-]+\.html?)\b`)
)

func (f *Fetcher) DiscoverPlans(src config.Source) ([]config.PlanConfig, error) {
	base, err := url.Parse(src.IndexURL)
	if err != nil {
		return nil, fmt.Errorf("zły index_url %q: %w", src.IndexURL, err)
	}

	found := map[string]config.PlanConfig{}
	visited := map[string]bool{}

	if err := f.crawl(base, base, src, src.Depth, visited, found); err != nil {
		return nil, err
	}

	plans := make([]config.PlanConfig, 0, len(found))
	for _, p := range found {
		plans = append(plans, p)
	}
	return plans, nil
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

	m := planFileRe.FindStringSubmatch(name)
	if m == nil {
		return config.PlanConfig{}, false
	}
	yy, err := strconv.Atoi(m[2])
	if err != nil {
		return config.PlanConfig{}, false
	}

	startYear := 2000 + yy
	if src.StartYear != 0 {
		startYear = src.StartYear
	}

	planURL := *u
	planURL.Path = planPath
	planURL.RawQuery = ""

	id := strings.ToUpper(name)
	return config.PlanConfig{
		ID:        id,
		Name:      id,
		URL:       planURL.String(),
		StartYear: startYear,
		Faculty:   src.Faculty,
	}, true
}

func isPage(u *url.URL) bool {
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	return ext == "" || ext == ".htm" || ext == ".html" || ext == ".php" || ext == ".xml"
}
