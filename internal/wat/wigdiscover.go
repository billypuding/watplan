package wat

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/billypuding/watplan/internal/config"
)

var wigDownloadRe = regexp.MustCompile(`^\d+:([A-Za-z0-9]+)$`)

func (f *Fetcher) discoverWIG(base *url.URL, src config.Source) ([]config.PlanConfig, error) {
	body, err := f.FetchHTML(base.String())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}

	found := map[string]config.PlanConfig{}
	for _, leaf := range wigLeafPages(base, body) {
		page, err := f.FetchHTML(leaf.String())
		if err != nil {
			if errors.Is(err, ErrBlocked) {
				return nil, err
			}
			log.Printf("discover %s: %s: %v", src.Faculty, leaf, err)
			continue
		}
		plans, skipped := wigPlansOnPage(leaf, page, src)
		if len(skipped) > 0 {
			log.Printf("discover %s: %s: grup %d, pominięte pliki: %v", src.Faculty, path.Base(leaf.Path), len(plans), skipped)
		}
		for _, plan := range plans {
			if old, ok := found[plan.ID]; !ok || (path.Ext(plan.URL) == ".pdf" && path.Ext(old.URL) != ".pdf") {
				found[plan.ID] = plan
			}
		}
	}

	plans := make([]config.PlanConfig, 0, len(found))
	for _, p := range found {
		plans = append(plans, p)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })
	return plans, nil
}

func wigLeafPages(base *url.URL, body []byte) []*url.URL {
	prefix := strings.TrimSuffix(base.Path, "/") + "/"
	children := map[string][]string{}
	var categories []string
	seen := map[string]bool{}

	for _, raw := range extractLinksRegex(body) {
		link, err := base.Parse(raw)
		if err != nil || link.Host != base.Host || !strings.HasPrefix(link.Path, prefix) || link.RawQuery != "" {
			continue
		}
		rest := strings.Trim(strings.TrimPrefix(link.Path, prefix), "/")
		if rest == "" || seen[rest] {
			continue
		}
		seen[rest] = true
		parts := strings.Split(rest, "/")
		switch len(parts) {
		case 1:
			categories = append(categories, parts[0])
		case 2:
			children[parts[0]] = append(children[parts[0]], rest)
		}
	}

	var leaves []*url.URL
	add := func(rest string) {
		u := *base
		u.Path = prefix + rest
		u.RawQuery, u.Fragment = "", ""
		leaves = append(leaves, &u)
	}
	for _, cat := range categories {
		if len(children[cat]) == 0 {
			add(cat)
		}
		for _, sub := range children[cat] {
			add(sub)
		}
	}
	return leaves
}

func wigPlansOnPage(page *url.URL, body []byte, src config.Source) ([]config.PlanConfig, []string) {
	var plans []config.PlanConfig
	var skipped []string
	for _, raw := range extractLinksRegex(body) {
		link, err := page.Parse(strings.ReplaceAll(raw, "&amp;", "&"))
		if err != nil {
			continue
		}
		link.Fragment = ""

		var name string
		if strings.EqualFold(path.Ext(link.Path), ".pdf") {
			name = strings.TrimSuffix(path.Base(link.Path), path.Ext(link.Path))
			link.RawQuery = ""
		} else if m := wigDownloadRe.FindStringSubmatch(link.Query().Get("download")); m != nil {
			name = m[1]
		} else {
			continue
		}

		id := strings.ToUpper(name)
		if !planFileRe.MatchString(id) || (src.Prefix != "" && !strings.HasPrefix(id, strings.ToUpper(src.Prefix))) {
			if !slices.Contains(skipped, name) {
				skipped = append(skipped, name)
			}
			continue
		}
		plans = append(plans, config.PlanConfig{
			ID:        id,
			Name:      id,
			URL:       link.String(),
			StartYear: src.StartYear,
			Faculty:   src.Faculty,
			Format:    FormatWIGPDF,
		})
	}
	return plans, skipped
}
