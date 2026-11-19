package updater

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata"

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
	sources map[string]FacultyStatus
	missing atomic.Int64

	onChange func(config.PlanConfig, model.Change)
	clock    func() time.Time
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
	}
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
	_, err := u.update(cfg)
	return err
}

func (u *Updater) Ensure(cfg config.PlanConfig) error {
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
	lock := u.planLock(cfg.ID)
	lock.Lock()
	defer lock.Unlock()
	return u.updateLocked(cfg)
}

func (u *Updater) updateLocked(cfg config.PlanConfig) (updateResult, error) {
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

	events, got, err := u.fetchEvents(cfg, prev)
	if errors.Is(err, wat.ErrNotModified) {
		old.FetchedAt = time.Now().In(warsawLoc).Format(timeLayout)
		if err := u.store.Save(cfg.ID, old); err != nil {
			return updateResult{}, fmt.Errorf("zapis: %w", err)
		}
		return updateResult{events: len(old.Events), unchanged: true}, nil
	}
	if err != nil {
		return updateResult{}, err
	}

	if len(events) == 0 {
		if len(old.Events) > 0 {
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
			pending := old
			pending.DropCount = len(events)
			pending.DropStreak = streak
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

	plan := model.Plan{Events: events, FetchedAt: now, ModifiedAt: modifiedAt, ETag: got.ETag, LastMod: got.LastModified}
	if err := u.store.Save(cfg.ID, plan); err != nil {
		return updateResult{}, fmt.Errorf("zapis: %w", err)
	}
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

func (u *Updater) fetchEvents(cfg config.PlanConfig, prev wat.Validators) ([]model.Event, wat.Validators, error) {
	body, got, err := u.fetcher.FetchPage(cfg.URL, prev)
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
		body, _, err := u.fetcher.FetchPage(src.URL, wat.Validators{})
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
