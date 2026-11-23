package updater

import (
	"context"
	"log"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/wat"
)

const repairGap = 90 * time.Second

func (u *Updater) misparsedPlans(reg *config.Registry) []config.PlanConfig {
	var out []config.PlanConfig
	for _, cfg := range reg.List() {
		plan, err := u.store.Load(cfg.ID)
		if err != nil || plan.Parser >= wat.ParserVersion || !wat.Misparsed(plan.Events) {
			continue
		}
		out = append(out, cfg)
	}
	return out
}

func (u *Updater) RepairMisparsed(ctx context.Context, reg *config.Registry, gap time.Duration) {
	if gap <= 0 {
		gap = repairGap
	}
	todo := u.misparsedPlans(reg)
	if len(todo) == 0 {
		return
	}
	log.Printf("naprawa odczytu: %d planów do ponownego pobrania (co %v)", len(todo), gap)
	fixed := 0
	for _, cfg := range todo {
		if !sleepCtx(ctx, gap) {
			return
		}
		if u.CacheOnly() {
			continue
		}
		if err := u.Update(cfg); err != nil {
			log.Printf("naprawa odczytu %s: %v", cfg.ID, err)
			continue
		}
		fixed++
	}
	log.Printf("naprawa odczytu: poprawiono %d z %d planów", fixed, len(todo))
}
