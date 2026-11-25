package updater

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/wat"
)

const (
	repairGap   = 90 * time.Second
	repairRetry = time.Hour
)

func (u *Updater) misparsedPlans(reg *config.Registry) []config.PlanConfig {
	var out []config.PlanConfig
	for _, cfg := range reg.List() {
		plan, err := u.store.Load(cfg.ID)
		if err != nil || !wat.NeedsReparse(plan) {
			continue
		}
		out = append(out, cfg)
	}
	return out
}

func (u *Updater) repairRound(ctx context.Context, todo []config.PlanConfig, gap time.Duration) (fixed int, ok bool) {
	for _, cfg := range todo {
		if !sleepCtx(ctx, gap) {
			return fixed, false
		}
		if u.CacheOnly() {
			continue
		}
		err := u.Update(cfg)
		switch {
		case err == nil:
			fixed++
		case errors.Is(err, wat.ErrBudget) || errors.Is(err, wat.ErrBlocked):
			log.Printf("naprawa odczytu: przerwa (%v), wracam za %v", err, repairRetry)
			return fixed, true
		default:
			log.Printf("naprawa odczytu %s: %v", cfg.ID, err)
		}
	}
	return fixed, true
}

func (u *Updater) RepairMisparsed(ctx context.Context, reg *config.Registry, gap, retry time.Duration) {
	if gap <= 0 {
		gap = repairGap
	}
	if retry <= 0 {
		retry = repairRetry
	}
	for {
		todo := u.misparsedPlans(reg)
		if len(todo) == 0 {
			return
		}
		log.Printf("naprawa odczytu: %d planów do ponownego pobrania (co %v)", len(todo), gap)
		fixed, ok := u.repairRound(ctx, todo, gap)
		if !ok {
			return
		}
		log.Printf("naprawa odczytu: poprawiono %d z %d planów", fixed, len(todo))
		if !sleepCtx(ctx, retry) {
			return
		}
	}
}
