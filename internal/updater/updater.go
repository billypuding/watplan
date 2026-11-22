package updater

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata"

	"github.com/billypuding/watplan/internal/alert"
	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/wat"
)

const (
	timeLayout = "2006-01-02 15:04:05"

	maxConcurrentUpdates = 2
	dropConfirmations    = 3
)

var warsawLoc *time.Location

func init() {
	var err error
	warsawLoc, err = time.LoadLocation("Europe/Warsaw")
	if err != nil {
		log.Printf("ostrzeżenie: brak strefy Europe/Warsaw, używam czasu lokalnego: %v", err)
		warsawLoc = time.Local
	}
}

type Updater struct {
	store   *store.Store
	fetcher *wat.Fetcher
	workers int
	mu      sync.Mutex
	locks   map[string]*sync.Mutex
	busy    map[string]int
	sources map[string]FacultyStatus
	missing atomic.Int64

	onChange  func(config.PlanConfig, model.Change)
	clock     func() time.Time
	started   time.Time
	lastOK    atomic.Int64
	lastFail  atomic.Int64
	cacheOnly atomic.Bool

	suspectMu sync.Mutex
	suspect   map[string]bool
	alert     *alert.Notifier
}

const (
	historyKeep = 40
	freshBase   = 36 * time.Hour
)

func (u *Updater) OnChange(fn func(config.PlanConfig, model.Change)) {
	u.onChange = fn
}

type updateResult struct {
	events    int
	oldEvents int
	streak    int
	skipped   bool
	pending   bool
	unchanged bool
}

func NewUpdater(store *store.Store, fetcher *wat.Fetcher) *Updater {
	workers := maxConcurrentUpdates
	if v := os.Getenv("PLANWAT_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			workers = n
		}
	}
	return &Updater{
		store:   store,
		fetcher: fetcher,
		workers: workers,
		locks:   make(map[string]*sync.Mutex),
		busy:    make(map[string]int),
		started: time.Now(),
	}
}

func (u *Updater) track(id string) func() {
	u.mu.Lock()
	u.busy[id]++
	u.mu.Unlock()
	return func() {
		u.mu.Lock()
		if u.busy[id]--; u.busy[id] <= 0 {
			delete(u.busy, id)
		}
		u.mu.Unlock()
	}
}

func (u *Updater) Busy(id string) bool {
	if u == nil {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.busy[id] > 0
}

func (u *Updater) planLock(id string) *sync.Mutex {
	u.mu.Lock()
	defer u.mu.Unlock()
	lock, ok := u.locks[id]
	if !ok {
		lock = &sync.Mutex{}
		u.locks[id] = lock
	}
	return lock
}

func (u *Updater) UpdateAll(ctx context.Context, plans []config.PlanConfig) error {
	total := len(plans)
	if total == 0 {
		return nil
	}

	workers := u.workers
	if workers < 1 {
		workers = maxConcurrentUpdates
	}
	if total < workers {
		workers = total
	}

	var (
		wg    sync.WaitGroup
		done  atomic.Int64
		okN   atomic.Int64
		skipN atomic.Int64
		sameN atomic.Int64
		pendN atomic.Int64
		failN atomic.Int64
	)

	log.Printf("odświeżanie: start, %d planów, %d równolegle", total, workers)

	var blockedN atomic.Int64

	jobs := make(chan config.PlanConfig)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				res, err := u.update(p)
				n := done.Add(1)
				switch {
				case errors.Is(err, wat.ErrBlocked):

					blockedN.Add(1)
				case err != nil:
					failN.Add(1)
					log.Printf("[%d/%d] %s: błąd: %v", n, total, p.ID, err)
				case res.skipped:
					skipN.Add(1)
					log.Printf("[%d/%d] %s: pominięto (plan bez zajęć)", n, total, p.ID)
				case res.unchanged:
					sameN.Add(1)
					log.Printf("[%d/%d] %s: bez zmian (304)", n, total, p.ID)
				case res.pending:
					pendN.Add(1)
					log.Printf("[%d/%d] %s: wstrzymano podejrzany spadek (%d -> %d), potwierdzenie %d/%d",
						n, total, p.ID, res.oldEvents, res.events, res.streak, dropConfirmations)
				default:
					okN.Add(1)
					log.Printf("[%d/%d] %s: %d zajęć", n, total, p.ID, res.events)
				}
			}
		}()
	}

dispatch:
	for _, p := range plans {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- p:
		}
	}
	close(jobs)
	wg.Wait()

	fail := failN.Load()
	blocked := blockedN.Load()
	log.Printf("odświeżanie: gotowe - %d ok, %d bez zmian, %d pominiętych, %d wstrzymanych, %d zablokowanych przez WAF, %d błędów, %d/%d planów",
		okN.Load(), sameN.Load(), skipN.Load(), pendN.Load(), blocked, fail, done.Load(), total)
	if blocked > 0 {
		return fmt.Errorf("%d z %d planów niedostępnych: %w", blocked, total, wat.ErrBlocked)
	}
	if fail > 0 {
		return fmt.Errorf("%d z %d planów nie udało się odświeżyć", fail, total)
	}
	return nil
}

func (u *Updater) Update(cfg config.PlanConfig) error {
	_, err := u.updateMode(cfg, wat.FetchAuto)
	return err
}

func (u *Updater) UpdateManual(cfg config.PlanConfig) error {
	defer u.track(cfg.ID)()
	_, err := u.updateMode(cfg, wat.FetchViaProxy)
	if err == nil {
		return nil
	}
	if !manualFallback(err) {
		return err
	}
	log.Printf("plan %s: pobieranie przez proxy nie udało się (%v), próbuję przez API", cfg.ID, err)
	return u.Update(cfg)
}

func manualFallback(err error) bool {
	if errors.Is(err, wat.ErrBlocked) || errors.Is(err, wat.ErrProxy) || errors.Is(err, wat.ErrBudget) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

var ErrCacheOnly = errors.New("serwer działa bez bazy i nie pobiera nowych planów, dopóki baza nie wróci")

func (u *Updater) SetCacheOnly(on bool) {
	if u != nil {
		u.cacheOnly.Store(on)
	}
}

func (u *Updater) CacheOnly() bool {
	return u != nil && u.cacheOnly.Load()
}

func (u *Updater) Ensure(cfg config.PlanConfig) error {
	if u.CacheOnly() {
		return ErrCacheOnly
	}
	defer u.track(cfg.ID)()
	lock := u.planLock(cfg.ID)
	lock.Lock()
	defer lock.Unlock()

	if _, err := u.store.Load(cfg.ID); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	_, err := u.updateLocked(cfg)
	return err
}

func (u *Updater) update(cfg config.PlanConfig) (updateResult, error) {
	return u.updateMode(cfg, wat.FetchAuto)
}

func (u *Updater) updateMode(cfg config.PlanConfig, mode wat.FetchMode) (updateResult, error) {
	defer u.track(cfg.ID)()
	lock := u.planLock(cfg.ID)
	lock.Lock()
	defer lock.Unlock()
	return u.updateLockedMode(cfg, mode)
}

func (u *Updater) updateLocked(cfg config.PlanConfig) (updateResult, error) {
	return u.updateLockedMode(cfg, wat.FetchAuto)
}

func (u *Updater) updateLockedMode(cfg config.PlanConfig, mode wat.FetchMode) (res updateResult, err error) {
	defer func() { u.recordFetch(err) }()
	old, err := u.store.Load(cfg.ID)
	if err != nil {
		old = model.Plan{}
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("plan %s: nie można odczytać poprzednich danych: %v", cfg.ID, err)
		}
	}

	var prev wat.Validators
	if len(old.Events) > 0 && old.DropStreak == 0 && len(cfg.Extra) == 0 {
		prev = wat.Validators{ETag: old.ETag, LastModified: old.LastMod}
	}

	events, got, err := u.fetchEvents(cfg, prev, mode)
	checked := time.Now().In(warsawLoc).Format(timeLayout)
	if errors.Is(err, wat.ErrNotModified) {
		old.FetchedAt = checked
		old.CheckedAt = checked
		if err := u.store.Save(cfg.ID, old); err != nil {
			return updateResult{}, fmt.Errorf("zapis: %w", err)
		}
		u.clearSuspect(cfg.ID)
		return updateResult{events: len(old.Events), unchanged: true}, nil
	}
	if err != nil {
		return updateResult{}, err
	}

	if len(events) == 0 {
		if len(old.Events) > 0 {
			u.markSuspect(cfg.ID, fmt.Sprintf("plan %s w nowym odczycie nie ma żadnych zajęć (wcześniej %d)", cfg.ID, len(old.Events)))
			return updateResult{}, fmt.Errorf("nowy odczyt bez zajęć (poprzednio %d), zostawiam poprzednie dane", len(old.Events))
		}
		return updateResult{skipped: true}, nil
	}

	if suspiciousDrop(len(old.Events), len(events)) {
		streak := 1
		if old.DropCount == len(events) {
			streak = old.DropStreak + 1
		}
		if streak < dropConfirmations {
			u.markSuspect(cfg.ID, fmt.Sprintf("w planie %s liczba zajęć spadła z %d do %d (potwierdzenie %d/%d)", cfg.ID, len(old.Events), len(events), streak, dropConfirmations))
			pending := old
			pending.DropCount = len(events)
			pending.DropStreak = streak
			pending.CheckedAt = checked
			if err := u.store.Save(cfg.ID, pending); err != nil {
				return updateResult{}, fmt.Errorf("zapis: %w", err)
			}
			return updateResult{
				events:    len(events),
				oldEvents: len(old.Events),
				streak:    streak,
				pending:   true,
			}, nil
		}
		log.Printf("plan %s: potwierdzony spadek liczby zajęć (z %d do %d)", cfg.ID, len(old.Events), len(events))
	}

	now := time.Now().In(warsawLoc).Format(timeLayout)
	diff := model.Diff(old.Events, events)

	modifiedAt := old.ModifiedAt
	if modifiedAt == "" || !diff.Empty() {
		modifiedAt = now
		log.Printf("plan %s: zmiany (dodane %d, usunięte %d, zmienione %d)",
			cfg.ID, len(diff.Added), len(diff.Removed), len(diff.Modified))
	}
	var change model.Change
	if len(old.Events) > 0 && !diff.Empty() {
		clock := u.clock
		if clock == nil {
			clock = time.Now
		}
		today := clock().In(warsawLoc).Format("2006-01-02")
		change = model.Changes(old.Events, events, today, now)
		change.Since = old.FetchedAt
	}

	plan := model.Plan{Events: events, FetchedAt: now, CheckedAt: checked, ModifiedAt: modifiedAt, ETag: got.ETag, LastMod: got.LastModified}
	if err := u.store.Save(cfg.ID, plan); err != nil {
		return updateResult{}, fmt.Errorf("zapis: %w", err)
	}
	u.clearSuspect(cfg.ID)
	if !change.Empty() {
		if err := u.store.AddHistory(cfg.ID, change, historyKeep); err != nil {
			log.Printf("plan %s: zapis historii zmian: %v", cfg.ID, err)
		}
		if u.onChange != nil && u.freshBaseline(old.FetchedAt) {
			go u.onChange(cfg, change)
		} else if u.onChange != nil {
			log.Printf("plan %s: zmiany względem starej wersji z %s, bez powiadomienia", cfg.ID, old.FetchedAt)
		}
	}
	return updateResult{events: len(events)}, nil
}

func (u *Updater) fetchPage(target string, prev wat.Validators, mode wat.FetchMode) ([]byte, wat.Validators, error) {
	if mode == wat.FetchViaProxy {
		return u.fetcher.FetchPageViaProxy(target, prev)
	}
	return u.fetcher.FetchPage(target, prev)
}

func (u *Updater) fetchEvents(cfg config.PlanConfig, prev wat.Validators, mode wat.FetchMode) ([]model.Event, wat.Validators, error) {
	body, got, err := u.fetchPage(cfg.URL, prev, mode)
	if errors.Is(err, wat.ErrNotModified) {
		return nil, got, err
	}
	var events []model.Event
	switch {
	case err == nil:
		events, err = wat.Parse(cfg.Format, bytes.NewReader(body), cfg.StartYear)
		if err != nil {
			return nil, got, fmt.Errorf("parsowanie: %w", err)
		}
	case errors.Is(err, wat.ErrGone) && len(cfg.Extra) > 0:
	default:
		return nil, got, fmt.Errorf("pobieranie: %w", err)
	}
	if len(cfg.Extra) == 0 {
		return events, got, nil
	}

	for _, src := range cfg.Extra {
		body, _, err := u.fetchPage(src.URL, wat.Validators{}, mode)
		if errors.Is(err, wat.ErrGone) {
			continue
		}
		if err != nil {
			return nil, wat.Validators{}, fmt.Errorf("pobieranie %s: %w", src.URL, err)
		}
		format, year := src.Format, src.StartYear
		if format == "" {
			format = cfg.Format
		}
		if year == 0 {
			year = cfg.StartYear
		}
		more, err := wat.Parse(format, bytes.NewReader(body), year)
		if err != nil {
			return nil, wat.Validators{}, fmt.Errorf("parsowanie %s: %w", src.URL, err)
		}
		events = append(events, more...)
	}

	seen := make(map[model.Event]bool, len(events))
	unique := events[:0]
	for _, e := range events {
		if !seen[e] {
			seen[e] = true
			unique = append(unique, e)
		}
	}
	model.SortEvents(unique)
	return unique, wat.Validators{}, nil
}

func (u *Updater) freshBaseline(fetchedAt string) bool {
	at, err := time.ParseInLocation(timeLayout, fetchedAt, warsawLoc)
	if err != nil {
		return false
	}
	clock := u.clock
	if clock == nil {
		clock = time.Now
	}
	return clock().Sub(at) < freshBase
}

func suspiciousDrop(oldCount, newCount int) bool {
	const minGuardEvents = 8
	return oldCount >= minGuardEvents && newCount*2 < oldCount
}

func (u *Updater) BlockedHosts() map[string]time.Time {
	if u == nil || u.fetcher == nil {
		return nil
	}
	return u.fetcher.BlockedHosts()
}

func (u *Updater) ProxyError() (string, time.Time) {
	if u == nil || u.fetcher == nil {
		return "", time.Time{}
	}
	return u.fetcher.ProxyError()
}

func (u *Updater) ProxyHost() string {
	if u == nil || u.fetcher == nil {
		return ""
	}
	return u.fetcher.ProxyHost()
}

func (u *Updater) Usage() *wat.Usage {
	if u == nil || u.fetcher == nil {
		return nil
	}
	return u.fetcher.Usage()
}

func (u *Updater) FetchAllowed(manual bool) bool {
	if u == nil || u.fetcher == nil {
		return true
	}
	if manual {
		return u.fetcher.BudgetLeft(wat.FetchViaProxy)
	}
	return u.fetcher.BudgetLeft(wat.FetchAuto)
}

func (u *Updater) Budget() map[string]wat.BudgetStatus {
	if u == nil || u.fetcher == nil {
		return nil
	}
	return u.fetcher.Budget()
}

func (u *Updater) recordFetch(err error) {
	now := time.Now().UnixNano()
	switch {
	case err == nil:
		u.lastOK.Store(now)
	case errors.Is(err, wat.ErrGone), errors.Is(err, wat.ErrBudget):
	default:
		u.lastFail.Store(now)
	}
}

func (u *Updater) FetchHealthy(maxAge time.Duration) bool {
	if u == nil {
		return true
	}
	fail := u.lastFail.Load()
	ok := u.lastOK.Load()
	if fail == 0 || fail <= ok {
		return true
	}
	since := u.started
	if ok > 0 {
		since = time.Unix(0, ok)
	}
	return time.Since(since) <= maxAge
}

func (u *Updater) WatchCredits(ctx context.Context) {
	if u == nil || u.fetcher == nil {
		return
	}
	u.fetcher.WatchCredits(ctx)
}

func (u *Updater) Credits() (wat.Credits, bool) {
	if u == nil || u.fetcher == nil {
		return wat.Credits{}, false
	}
	return u.fetcher.Credits()
}

func (u *Updater) markSuspect(id, text string) {
	u.suspectMu.Lock()
	if u.suspect == nil {
		u.suspect = map[string]bool{}
	}
	u.suspect[id] = true
	n := len(u.suspect)
	u.suspectMu.Unlock()
	log.Printf("podejrzany odczyt: %s", text)
	u.alert.Raise("parser", fmt.Sprintf("Podejrzany odczyt planu: %s. Zostawiam poprzednie dane. Planów z problemem: %d. Możliwa zmiana formatu na stronie WAT.", text, n))
}

func (u *Updater) clearSuspect(id string) {
	u.suspectMu.Lock()
	had := u.suspect[id]
	delete(u.suspect, id)
	empty := len(u.suspect) == 0
	u.suspectMu.Unlock()
	if had && empty {
		u.alert.Resolved("parser", "Odczyty planów znów wyglądają poprawnie.")
	}
}
