package server

import (
	"crypto/sha1"
	"encoding/hex"
	"html"
	"net/http"
	"sync"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/ogimage"
)

const (
	shareFooter    = "Dzień, tydzień, miesiąc i kalendarz · za darmo, bez logowania"
	shareCacheSize = 256
	cacheShareCard = "public, max-age=86400, stale-while-revalidate=604800"
)

type shareCards struct {
	renderer *ogimage.Renderer
	site     []byte
	mu       sync.Mutex
	cache    map[string][]byte
}

func newShareCards(logo []byte, groups int) (*shareCards, error) {
	r, err := ogimage.New(logo)
	if err != nil {
		return nil, err
	}
	site, err := r.Render(ogimage.Card{Kicker: "Plan zajęć WAT", Title: "Wszystkie wydziały", Subtitle: groupsWord(groups) + " w jednym miejscu", Footer: shareFooter})
	if err != nil {
		return nil, err
	}
	return &shareCards{renderer: r, site: site, cache: map[string][]byte{}}, nil
}

func shareImageTags(url, alt string) string {
	return `<meta property="og:image" content="` + url + `">` + "\n" +
		`<meta property="og:image:width" content="1200">` + "\n" +
		`<meta property="og:image:height" content="630">` + "\n" +
		`<meta property="og:image:alt" content="` + html.EscapeString(alt) + `">` + "\n" +
		`<meta name="twitter:card" content="summary_large_image">`
}

func (c *shareCards) plan(plan config.PlanConfig) ([]byte, error) {
	c.mu.Lock()
	if png, ok := c.cache[plan.ID]; ok {
		c.mu.Unlock()
		return png, nil
	}
	c.mu.Unlock()
	png, err := c.renderer.Render(ogimage.Card{Kicker: "Plan zajęć", Title: plan.ID, Subtitle: facultyName(plan), Footer: shareFooter})
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if len(c.cache) >= shareCacheSize {
		clear(c.cache)
	}
	c.cache[plan.ID] = png
	c.mu.Unlock()
	return png, nil
}

func writePNG(w http.ResponseWriter, r *http.Request, png []byte) {
	sum := sha1.Sum(png)
	etag := `"` + hex.EncodeToString(sum[:10]) + `"`
	w.Header().Set("Cache-Control", cacheShareCard)
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Robots-Tag", "noindex")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

func (c *shareCards) serveSite(w http.ResponseWriter, r *http.Request) {
	writePNG(w, r, c.site)
}

func (c *shareCards) servePlan(w http.ResponseWriter, r *http.Request, plan config.PlanConfig) {
	png, err := c.plan(plan)
	if err != nil {
		http.Error(w, "Nie udało się utworzyć grafiki.", http.StatusInternalServerError)
		return
	}
	writePNG(w, r, png)
}
