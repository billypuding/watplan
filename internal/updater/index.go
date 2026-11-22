package updater

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/wat"
)

const (
	defaultIndexEvery = 30 * time.Minute
	indexSafetyNet    = 24 * time.Hour
	indexMargin       = 15 * time.Minute
	checkedWriteEvery = 10 * time.Minute
)

type IndexState struct {
	Faculty string `json:"faculty"`
	Updated string `json:"updated,omitempty"`
	Checked string `json:"checked,omitempty"`
	NoDate  bool   `json:"no_date,omitempty"`
	Failed  bool   `json:"failed,omitempty"`
}

type indexWatch struct {
	mu      sync.Mutex
	every   time.Duration
	byDir   map[string]string
	state   map[string]*IndexState
	fetchMu map[string]*sync.Mutex
	saved   int64
	persist func([]byte)
}

type PlanTimes struct {
	Fetched, Checked, Modified          time.Time
	HasFetched, HasChecked, HasModified bool
}

func TimesOf(p model.Plan) PlanTimes {
	var t PlanTimes
	parse := func(v string) (time.Time, bool) {
		at, err := time.ParseInLocation(timeLayout, v, warsawLoc)
		return at, err == nil
	}
	t.Fetched, t.HasFetched = parse(p.FetchedAt)
	t.Checked, t.HasChecked = parse(p.LastChecked())
	t.Modified, t.HasModified = parse(p.ModifiedAt)
	return t
}

func dirOf(raw string) string {
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		return raw[:i+1]
	}
	return raw
}

func datedSource(src config.Source) bool {
	return src.Format == "" && src.IndexURL != ""
}

func (u *Updater) SetIndexSources(sources []config.Source, every time.Duration) {
	if every <= 0 {
		every = defaultIndexEvery
	}
	w := &indexWatch{every: every, byDir: map[string]string{}, state: map[string]*IndexState{}, fetchMu: map[string]*sync.Mutex{}}
	for _, src := range sources {
		if !datedSource(src) {
			continue
		}
		w.byDir[dirOf(src.IndexURL)] = src.IndexURL
		w.state[src.IndexURL] = &IndexState{Faculty: src.Faculty}
		w.fetchMu[src.IndexURL] = &sync.Mutex{}
	}
	u.mu.Lock()
	u.index = w
	u.mu.Unlock()
}

func (u *Updater) indexes() *indexWatch {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.index
}

func (u *Updater) LoadIndexState(data []byte, persist func([]byte)) error {
	w := u.indexes()
	if w == nil {
		return nil
	}
	if len(data) > 0 {
		var stored struct {
			Indexes map[string]IndexState `json:"indexes"`
			Saved   int64                 `json:"saved"`
		}
		if err := json.Unmarshal(data, &stored); err != nil {
			return err
		}
		w.mu.Lock()
		for url, st := range stored.Indexes {
			if cur, ok := w.state[url]; ok {
				st.Faculty = cur.Faculty
				*cur = st
			}
		}
		w.saved = stored.Saved
		w.mu.Unlock()
	}
	w.mu.Lock()
	w.persist = persist
	w.mu.Unlock()
	return nil
}

func (w *indexWatch) save() {
	w.mu.Lock()
	persist := w.persist
	out := struct {
		Indexes map[string]IndexState `json:"indexes"`
		Saved   int64                 `json:"saved"`
	}{Indexes: map[string]IndexState{}, Saved: w.saved}
	for url, st := range w.state {
		out.Indexes[url] = *st
	}
	w.mu.Unlock()
	if persist == nil {
		return
	}
	if data, err := json.Marshal(out); err == nil {
		go persist(data)
	}
}

func (w *indexWatch) urlsFor(cfg config.PlanConfig) ([]string, bool) {
	urls := []string{cfg.URL}
	for _, e := range cfg.Extra {
		urls = append(urls, e.URL)
	}
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		idx, ok := w.byDir[dirOf(u)]
		if !ok {
			return nil, false
		}
		out = append(out, idx)
	}
	return out, true
}

type indexView struct {
	covered bool
	fresh   bool
	updated time.Time
}

func (w *indexWatch) view(cfg config.PlanConfig, now time.Time) indexView {
	if w == nil {
		return indexView{}
	}
	urls, ok := w.urlsFor(cfg)
	if !ok {
		return indexView{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	v := indexView{covered: true, fresh: true}
	for _, url := range urls {
		st := w.state[url]
		if st == nil || st.NoDate {
			return indexView{}
		}
		checked, err := time.ParseInLocation(timeLayout, st.Checked, warsawLoc)
		if st.Failed && err == nil && now.Sub(checked) < w.every {
			return indexView{}
		}
		updated, uerr := time.ParseInLocation(timeLayout, st.Updated, warsawLoc)
		if st.Failed || err != nil || uerr != nil || now.Sub(checked) >= w.every {
			v.fresh = false
			continue
		}
		if updated.After(v.updated) {
			v.updated = updated
		}
	}
	return v
}

type Decision struct {
	Due       bool
	ViaIndex  bool
	NeedIndex bool
	Saved     bool
}

func adaptiveDue(t PlanTimes, now time.Time) bool {
	if !t.HasChecked {
		return true
	}
	return now.Sub(t.Checked) >= model.CheckInterval(t.Modified, t.HasModified, now)
}

func (u *Updater) Decide(cfg config.PlanConfig, t PlanTimes, now time.Time) Decision {
	if !t.HasFetched || now.Sub(t.Fetched) >= indexSafetyNet {
		return Decision{Due: true}
	}
	adaptive := adaptiveDue(t, now)
	v := u.indexes().view(cfg, now)
	if !v.covered {
		return Decision{Due: adaptive}
	}
	if !v.fresh {
		return Decision{NeedIndex: adaptive}
	}
	if !t.Fetched.Before(v.updated.Add(indexMargin)) {
		return Decision{ViaIndex: true, Saved: adaptive}
	}
	return Decision{Due: true}
}

func (u *Updater) refreshIndexes(cfg config.PlanConfig) {
	w := u.indexes()
	if w == nil {
		return
	}
	urls, ok := w.urlsFor(cfg)
	if !ok {
		return
	}
	for _, url := range urls {
		u.refreshIndex(w, url)
	}
}

func (u *Updater) refreshIndex(w *indexWatch, url string) {
	lock := w.fetchMu[url]
	lock.Lock()
	defer lock.Unlock()
	w.mu.Lock()
	st := w.state[url]
	checked, err := time.ParseInLocation(timeLayout, st.Checked, warsawLoc)
	recent := err == nil && time.Since(checked) < w.every && (st.Updated != "" || st.Failed || st.NoDate)
	w.mu.Unlock()
	if recent {
		return
	}
	body, _, err := u.fetcher.FetchPage(url, wat.Validators{})
	now := time.Now().In(warsawLoc).Format(timeLayout)
	if err != nil {
		log.Printf("indeks %s: %v (do %v sprawdzam plany po staremu)", url, err, w.every)
		w.mu.Lock()
		st.Checked = now
		st.Failed = true
		w.mu.Unlock()
		w.save()
		return
	}
	updated, ok := wat.IndexUpdated(body)
	w.mu.Lock()
	prev := st.Updated
	st.Checked = now
	st.Failed = false
	st.NoDate = !ok
	if ok {
		st.Updated = updated.In(warsawLoc).Format(timeLayout)
	}
	cur := st.Updated
	faculty := st.Faculty
	w.mu.Unlock()
	switch {
	case !ok:
		log.Printf("indeks %s (%s): brak daty aktualizacji, sprawdzam plany po staremu", url, faculty)
	case prev != cur:
		log.Printf("indeks %s: aktualizacja wydziału %s (poprzednio %q)", faculty, cur, prev)
	}
	w.save()
}

func (u *Updater) CheckPlan(cfg config.PlanConfig) error {
	stored, err := u.store.Load(cfg.ID)
	if err != nil {
		return u.Update(cfg)
	}
	d := u.Decide(cfg, TimesOf(stored), time.Now())
	if d.NeedIndex {
		u.refreshIndexes(cfg)
		d = u.Decide(cfg, TimesOf(stored), time.Now())
		if d.NeedIndex {
			d = Decision{Due: true}
		}
	}
	if !d.Due {
		u.Skip(cfg.ID, d)
		return nil
	}
	return u.Update(cfg)
}

func (u *Updater) Skip(id string, d Decision) {
	if !d.ViaIndex {
		return
	}
	if d.Saved {
		if w := u.indexes(); w != nil {
			w.mu.Lock()
			w.saved++
			w.mu.Unlock()
			w.save()
		}
	}
	u.MarkChecked(id)
}

func (u *Updater) MarkChecked(id string) {
	lock := u.planLock(id)
	lock.Lock()
	defer lock.Unlock()
	plan, err := u.store.Load(id)
	if err != nil {
		return
	}
	if at, err := time.ParseInLocation(timeLayout, plan.LastChecked(), warsawLoc); err == nil && time.Since(at) < checkedWriteEvery {
		return
	}
	plan.CheckedAt = time.Now().In(warsawLoc).Format(timeLayout)
	if err := u.store.Save(id, plan); err != nil {
		log.Printf("plan %s: zapis sprawdzenia: %v", id, err)
	}
}

func (u *Updater) checkIndexAssumption(cfg config.PlanConfig, previousFetch string) {
	w := u.indexes()
	if w == nil {
		return
	}
	prev, err := time.ParseInLocation(timeLayout, previousFetch, warsawLoc)
	if err != nil {
		return
	}
	v := w.view(cfg, time.Now())
	if !v.covered || !v.fresh || !prev.After(v.updated.Add(indexMargin)) {
		return
	}
	text := fmt.Sprintf("plan %s zmienił się, choć data wydziału (%s) jest sprzed poprzedniego pobrania (%s)", cfg.ID, v.updated.Format(timeLayout), previousFetch)
	log.Printf("założenie o dacie wydziału nie działa: %s", text)
	u.alert.Raise("index-assumption", "Założenie o dacie aktualizacji wydziału nie działa: "+text+". Sprawdzanie przez indeks może pomijać zmiany.")
}

func (u *Updater) IndexStatus() (map[string]IndexState, int64) {
	w := u.indexes()
	if w == nil {
		return nil, 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]IndexState{}
	for url, st := range w.state {
		out[url] = *st
	}
	return out, w.saved
}
