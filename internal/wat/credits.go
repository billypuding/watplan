package wat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultCreditsEvery = 4 * time.Hour

type Credits struct {
	Remaining int    `json:"remaining"`
	Total     int    `json:"total"`
	Checked   string `json:"checked"`
}

type creditWatch struct {
	mu      sync.Mutex
	url     string
	every   time.Duration
	levels  []int
	alerted map[int]bool
	last    Credits
	client  *http.Client
}

func newCreditWatch(scraper *scraperConfig) (*creditWatch, error) {
	if scraper == nil || scraper.provider != "scrapingant" {
		return nil, nil
	}
	every := defaultCreditsEvery
	if v := os.Getenv("PLANWAT_CREDITS_CHECK_EVERY"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("zły PLANWAT_CREDITS_CHECK_EVERY %q (np. 4h)", v)
		}
		every = d
	}
	levels := []int{30, 10}
	if v := os.Getenv("PLANWAT_CREDITS_ALERT"); v != "" {
		levels = nil
		for _, part := range strings.Split(v, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || n <= 0 || n >= 100 {
				return nil, fmt.Errorf("zły PLANWAT_CREDITS_ALERT %q (procenty, np. 30,10)", v)
			}
			levels = append(levels, n)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(levels)))
	return &creditWatch{
		url:     envOrDefault("PLANWAT_SCRAPER_USAGE_URL", "https://api.scrapingant.com/v2/usage"),
		every:   every,
		levels:  levels,
		alerted: map[int]bool{},
		client:  &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func (f *Fetcher) WatchCredits(ctx context.Context) {
	if f == nil || f.credits == nil {
		return
	}
	for {
		f.CheckCredits()
		t := time.NewTimer(f.credits.every)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

func (f *Fetcher) CheckCredits() {
	w := f.credits
	if w == nil {
		return
	}
	remaining, total, err := f.fetchCredits()
	if err != nil {
		log.Printf("kredyty %s: %v", f.scraper.provider, err)
		return
	}
	pct := 0
	if total > 0 {
		pct = remaining * 100 / total
	}
	w.mu.Lock()
	w.last = Credits{Remaining: remaining, Total: total, Checked: time.Now().Format("2006-01-02 15:04:05")}
	var raise, resolve []int
	for _, level := range w.levels {
		switch {
		case pct < level && !w.alerted[level]:
			w.alerted[level] = true
			raise = append(raise, level)
		case pct >= level && w.alerted[level]:
			delete(w.alerted, level)
			resolve = append(resolve, level)
		}
	}
	w.mu.Unlock()
	log.Printf("kredyty %s: zostało %d z %d (%d%%)", f.scraper.provider, remaining, total, pct)
	for _, level := range raise {
		f.alert.Raise(fmt.Sprintf("credits:%d", level), fmt.Sprintf("Kredyty %s poniżej %d%%: zostało %d z %d.", f.scraper.provider, level, remaining, total))
	}
	for _, level := range resolve {
		f.alert.Resolved(fmt.Sprintf("credits:%d", level), fmt.Sprintf("Kredyty %s znów powyżej %d%% (%d z %d).", f.scraper.provider, level, remaining, total))
	}
}

func (f *Fetcher) fetchCredits() (int, int, error) {
	u, err := url.Parse(f.credits.url)
	if err != nil {
		return 0, 0, err
	}
	q := u.Query()
	q.Set("x-api-key", f.scraper.apiKey)
	u.RawQuery = q.Encode()
	resp, err := f.credits.client.Get(u.String())
	if err != nil {
		return 0, 0, f.scraper.redact(withoutRequestURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	var body struct {
		Total     int `json:"plan_total_credits"`
		Remaining int `json:"remained_credits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, 0, err
	}
	return body.Remaining, body.Total, nil
}

func (f *Fetcher) Credits() (Credits, bool) {
	if f == nil || f.credits == nil {
		return Credits{}, false
	}
	f.credits.mu.Lock()
	defer f.credits.mu.Unlock()
	return f.credits.last, f.credits.last.Checked != ""
}
