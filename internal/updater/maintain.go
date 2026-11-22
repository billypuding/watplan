package updater

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"strings"
	"time"

	"github.com/billypuding/watplan/internal/alert"
	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/wat"
)

type FacultyStatus struct {
	Groups  int    `json:"groups"`
	New     int    `json:"new,omitempty"`
	Checked string `json:"checked"`
	Error   string `json:"error,omitempty"`
}

type MaintainOptions struct {
	StartDelay    time.Duration
	Round         time.Duration
	DiscoverEvery time.Duration
	FetchGap      time.Duration
	RetryAfter    time.Duration
	Watched       func() []string
}

func (o MaintainOptions) withDefaults() MaintainOptions {
	if o.StartDelay <= 0 {
		o.StartDelay = 2 * time.Minute
	}
	if o.Round <= 0 {
		o.Round = 30 * time.Minute
	}
	if o.DiscoverEvery <= 0 {
		o.DiscoverEvery = 24 * time.Hour
	}
	if o.FetchGap <= 0 {
		o.FetchGap = 90 * time.Second
	}
	if o.RetryAfter <= 0 {
		o.RetryAfter = 24 * time.Hour
	}
	return o
}

func (u *Updater) SourceStatus() map[string]FacultyStatus {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make(map[string]FacultyStatus, len(u.sources))
	for k, v := range u.sources {
		out[k] = v
	}
	return out
}

func (u *Updater) MissingPlans() int {
	if u == nil {
		return 0
	}
	return int(u.missing.Load())
}

func (u *Updater) recordSource(faculty string, st FacultyStatus) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.sources == nil {
		u.sources = make(map[string]FacultyStatus)
	}
	if prev, ok := u.sources[faculty]; ok && st.Error != "" && prev.Error == "" && prev.Checked == st.Checked {
		return
	}
	if prev, ok := u.sources[faculty]; ok && st.Error == "" && prev.Error == "" && prev.Checked == st.Checked {
		st.Groups += prev.Groups
		st.New += prev.New
	}
	u.sources[faculty] = st
	u.alertSource(faculty, st)
}

func (u *Updater) SetAlert(n *alert.Notifier) {
	u.alert = n
}

func (u *Updater) alertSource(key string, st FacultyStatus) {
	if u.alert == nil {
		return
	}
	switch {
	case st.Error == "":
		u.alert.Resolved("faculty:"+key, fmt.Sprintf("%s: lista grup znów się pobiera (%d grup).", key, st.Groups))
	case strings.HasPrefix(st.Error, "jeszcze nie opublikowano") || strings.HasPrefix(st.Error, "na stronie są jeszcze plany"):
	default:
		u.alert.Problem("faculty:"+key, fmt.Sprintf("%s: %s", key, st.Error))
	}
}

func sourceKey(src config.Source) string {
	if src.Semester == "" {
		return src.Faculty
	}
	return src.Faculty + " " + src.Semester
}

func describeDiscoveryError(src config.Source, err error) string {
	switch {
	case src.Optional && errors.Is(err, wat.ErrGone):
		return "jeszcze nie opublikowano (404)"
	case errors.Is(err, wat.ErrStaleSource):
		return "na stronie są jeszcze plany z poprzedniego roku"
	case errors.Is(err, wat.ErrGone):
		return fmt.Sprintf("lista grup zniknęła (404) - wydział przeniósł plany, popraw index_url w sources.json (%s)", src.IndexURL)
	case errors.Is(err, wat.ErrBlocked):
		return "WAT (Incapsula) blokuje pobieranie listy grup"
	default:
		return err.Error()
	}
}

func logDiscoveryError(src config.Source, err error) {
	if (src.Optional && errors.Is(err, wat.ErrGone)) || errors.Is(err, wat.ErrStaleSource) {
		log.Printf("discover %q: %s", sourceKey(src), describeDiscoveryError(src, err))
		return
	}
	if errors.Is(err, wat.ErrGone) {
		log.Printf("UWAGA discover %q: %s", src.Faculty, describeDiscoveryError(src, err))
		return
	}
	log.Printf("discover %q: błąd: %v (zostawiam poprzednie plany tego wydziału)", src.Faculty, err)
}

func (u *Updater) Maintain(ctx context.Context, sources []config.Source, reg *config.Registry, opts MaintainOptions) {
	opts = opts.withDefaults()
	if !sleepCtx(ctx, opts.StartDelay) {
		return
	}

	failed := map[string]time.Time{}
	var lastDiscover time.Time
	for {
		if len(sources) > 0 && time.Since(lastDiscover) >= opts.DiscoverEvery {
			u.checkSources(sources, reg)
			lastDiscover = time.Now()
		}
		if !u.fillMissing(ctx, reg, failed, opts) {
			return
		}
		if !u.refreshWatched(ctx, reg, opts) {
			return
		}
		if !sleepCtx(ctx, opts.Round) {
			return
		}
	}
}

func (u *Updater) checkSources(sources []config.Source, reg *config.Registry) {
	known := map[string]bool{}
	current := reg.List()
	for _, p := range current {
		known[p.ID] = true
	}

	var added, later []config.PlanConfig
	for _, src := range sources {
		now := time.Now().In(warsawLoc).Format(timeLayout)
		plans, err := u.fetcher.DiscoverPlans(src)
		if err == nil && len(plans) == 0 {
			err = errors.New("lista grup jest pusta")
		}
		if err == nil && src.Semester != "" {
			err = u.fetcher.CheckCurrent(src, plans)
		}
		if err != nil {
			logDiscoveryError(src, err)
			u.recordSource(sourceKey(src), FacultyStatus{Checked: now, Error: describeDiscoveryError(src, err)})
			continue
		}
		if src.Semester != "" {
			later = append(later, plans...)
			log.Printf("discover %q: %d grup na stronie wydziału", sourceKey(src), len(plans))
			u.recordSource(sourceKey(src), FacultyStatus{Groups: len(plans), Checked: now})
			continue
		}
		fresh := 0
		for _, p := range plans {
			if known[p.ID] {
				continue
			}
			known[p.ID] = true
			added = append(added, p)
			fresh++
		}
		log.Printf("discover %q: %d grup na stronie wydziału, %d nowych", src.Faculty, len(plans), fresh)
		u.recordSource(sourceKey(src), FacultyStatus{Groups: len(plans), New: fresh, Checked: now})
	}

	if len(added) == 0 && len(later) == 0 {
		return
	}
	merged, n := config.MergeExtra(current, append(added, later...))
	if n == 0 {
		return
	}
	reg.Set(merged)
	log.Printf("discover: %d nowych grup lub źródeł planów", n)
}

func (u *Updater) fillMissing(ctx context.Context, reg *config.Registry, failed map[string]time.Time, opts MaintainOptions) bool {
	if u.CacheOnly() {
		return true
	}
	var todo []config.PlanConfig
	for _, p := range reg.List() {
		if _, err := u.store.Raw(p.ID); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		todo = append(todo, p)
	}
	u.missing.Store(int64(len(todo)))

	fetched := 0
	for _, p := range todo {
		if at, ok := failed[p.ID]; ok && time.Since(at) < opts.RetryAfter {
			continue
		}
		if u.fetcher.Blocked(p.URL) {
			continue
		}
		res, err := u.update(p)
		switch {
		case err != nil:
			failed[p.ID] = time.Now()
			log.Printf("w tle: %s: %v", p.ID, err)
		case res.skipped:
			failed[p.ID] = time.Now()
			log.Printf("w tle: %s: plan bez zajęć", p.ID)
		default:
			delete(failed, p.ID)
			fetched++
			u.missing.Add(-1)
			log.Printf("w tle: %s: pobrano %d zajęć", p.ID, res.events)
		}
		if !sleepCtx(ctx, opts.FetchGap) {
			return false
		}
	}
	if len(todo) > 0 {
		log.Printf("w tle: pobrano %d z %d brakujących planów", fetched, len(todo))
	}
	return true
}

func (u *Updater) refreshWatched(ctx context.Context, reg *config.Registry, opts MaintainOptions) bool {
	if opts.Watched == nil {
		return true
	}
	checked := 0
	for _, id := range opts.Watched() {
		p, ok := reg.Get(id)
		if !ok || u.fetcher.Blocked(p.URL) {
			continue
		}
		if !u.FetchAllowed(false) {
			break
		}
		if stored, err := u.store.Load(id); err == nil {
			d := u.Decide(p, TimesOf(stored), time.Now())
			if !d.Due && !d.NeedIndex {
				u.Skip(id, d)
				continue
			}
		}
		if err := u.CheckPlan(p); err != nil {
			log.Printf("obserwowany %s: %v", id, err)
		}
		checked++
		if !sleepCtx(ctx, opts.FetchGap) {
			return false
		}
	}
	if checked > 0 {
		log.Printf("w tle: sprawdzono %d obserwowanych planów", checked)
	}
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
