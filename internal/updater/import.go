package updater

import (
	"fmt"
	"io"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/wat"
)

// Import zapisuje plan przeczytany z lokalnego pliku HTML (np. zapisanego
// w przeglądarce, gdy WAT zablokował IP). Nie wykonuje żadnych zapytań do WAT.
func (u *Updater) Import(cfg config.PlanConfig, r io.Reader) (int, error) {
	lock := u.planLock(cfg.ID)
	lock.Lock()
	defer lock.Unlock()

	events, err := wat.ParsePlan(r, cfg.StartYear)
	if err != nil {
		return 0, fmt.Errorf("parsowanie %s: %w", cfg.ID, err)
	}
	if len(events) == 0 {
		return 0, fmt.Errorf("plan %s: plik nie zawiera żadnych zajęć", cfg.ID)
	}

	now := time.Now().In(warsawLoc).Format(timeLayout)
	plan := model.Plan{Events: events, FetchedAt: now, ModifiedAt: now}
	if err := u.store.Save(cfg.ID, plan); err != nil {
		return 0, fmt.Errorf("zapis %s: %w", cfg.ID, err)
	}
	return len(events), nil
}
