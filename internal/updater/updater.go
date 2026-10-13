package updater

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"sync"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/wat"
)

const timeLayout = "2006-01-02 15:04:05"

type Updater struct {
	store *store.Store
	mu    sync.Mutex
}

func NewUpdater(store *store.Store) *Updater {
	return &Updater{store: store}
}

func (u *Updater) Run(ctx context.Context, plans []config.PlanConfig, interval time.Duration) {
	if err := u.UpdateAll(plans); err != nil {
		log.Printf("odświeżanie przy starcie: %v", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := u.UpdateAll(plans); err != nil {
				log.Printf("odświeżanie w tle: %v", err)
			}
		}
	}
}

func (u *Updater) UpdateAll(plans []config.PlanConfig) error {
	var errs []error
	for _, p := range plans {
		if err := u.Update(p); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (u *Updater) Update(cfg config.PlanConfig) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	body, err := wat.FetchHTML(cfg.URL)
	if err != nil {
		return fmt.Errorf("pobieranie: %w", err)
	}

	events, err := wat.ParsePlan(bytes.NewReader(body), cfg.StartYear)
	if err != nil {
		return fmt.Errorf("parsowanie: %w", err)
	}
	if len(events) == 0 {
		return errors.New("pobrano 0 zajęć, poprzednie dane zostają bez zmian")
	}

	old, err := u.store.Load(cfg.ID)
	if err != nil {
		old = model.Plan{}
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("plan %s: nie można odczytać poprzednich danych: %v", cfg.ID, err)
		}
	}

	now := time.Now().Format(timeLayout)
	diff := model.Diff(old.Events, events)

	modifiedAt := old.ModifiedAt
	if modifiedAt == "" || !diff.Empty() {
		modifiedAt = now
		log.Printf("plan %s: zmiany (dodane %d, usunięte %d, zmienione %d)",
			cfg.ID, len(diff.Added), len(diff.Removed), len(diff.Modified))
	} else {
		log.Printf("plan %s: bez zmian", cfg.ID)
	}

	plan := model.Plan{Events: events, FetchedAt: now, ModifiedAt: modifiedAt}
	if err := u.store.Save(cfg.ID, plan); err != nil {
		return fmt.Errorf("zapis: %w", err)
	}
	return nil
}
