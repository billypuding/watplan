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
	timeLayout           = "2006-01-02 15:04:05"
	maxConcurrentUpdates = 16
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
}

type updateResult struct {
	events    int
	oldEvents int
	streak    int
	skipped   bool
	pending   bool
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
		pendN atomic.Int64
		failN atomic.Int64
	)

	log.Printf("odświeżanie: start, %d planów, %d równolegle", total, workers)

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
				case err != nil:
					failN.Add(1)
					log.Printf("[%d/%d] %s: błąd: %v", n, total, p.ID, err)
				case res.skipped:
					skipN.Add(1)
					log.Printf("[%d/%d] %s: pominięto (plan bez zajęć)", n, total, p.ID)
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
	log.Printf("odświeżanie: gotowe - %d ok, %d pominiętych, %d wstrzymanych, %d błędów, %d/%d planów",
		okN.Load(), skipN.Load(), pendN.Load(), fail, done.Load(), total)
	if fail > 0 {
		return fmt.Errorf("%d z %d planów nie udało się odświeżyć", fail, total)
	}
	return nil
}

func (u *Updater) Update(cfg config.PlanConfig) error {
	_, err := u.update(cfg)
	return err
}

func (u *Updater) update(cfg config.PlanConfig) (updateResult, error) {
	lock := u.planLock(cfg.ID)
	lock.Lock()
	defer lock.Unlock()

	body, err := u.fetcher.FetchHTML(cfg.URL)
	if err != nil {
		return updateResult{}, fmt.Errorf("pobieranie: %w", err)
	}

	events, err := wat.ParsePlan(bytes.NewReader(body), cfg.StartYear)
	if err != nil {
		return updateResult{}, fmt.Errorf("parsowanie: %w", err)
	}

	old, err := u.store.Load(cfg.ID)
	if err != nil {
		old = model.Plan{}
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("plan %s: nie można odczytać poprzednich danych: %v", cfg.ID, err)
		}
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

	plan := model.Plan{Events: events, FetchedAt: now, ModifiedAt: modifiedAt}
	if err := u.store.Save(cfg.ID, plan); err != nil {
		return updateResult{}, fmt.Errorf("zapis: %w", err)
	}
	return updateResult{events: len(events)}, nil
}

func suspiciousDrop(oldCount, newCount int) bool {
	const minGuardEvents = 8
	return oldCount >= minGuardEvents && newCount*2 < oldCount
}
