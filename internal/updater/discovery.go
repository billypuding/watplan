package updater

import (
	"context"
	"log"
	"sort"
	"time"

	"github.com/billypuding/watplan/internal/config"
)

func (u *Updater) Discover(sources []config.Source, static, prev []config.PlanConfig) []config.PlanConfig {
	configured := make(map[string]bool, len(sources))
	for _, s := range sources {
		configured[s.Faculty] = true
	}

	byID := make(map[string]config.PlanConfig)
	for _, p := range static {
		if configured[p.Faculty] {
			byID[p.ID] = p
		}
	}

	for _, src := range sources {
		plans, err := u.fetcher.DiscoverPlans(src)
		if err != nil || len(plans) == 0 {
			if err != nil {
				log.Printf("discover %q: błąd: %v (zostawiam poprzednie plany tego wydziału)", src.Faculty, err)
			} else {
				log.Printf("discover %q: nie znaleziono żadnych planów (zostawiam poprzednie plany tego wydziału)", src.Faculty)
			}
			for _, p := range prev {
				if p.Faculty == src.Faculty {
					if _, exists := byID[p.ID]; !exists {
						byID[p.ID] = p
					}
				}
			}
			continue
		}

		log.Printf("discover %q: znaleziono %d planów", src.Faculty, len(plans))
		for _, p := range plans {
			byID[p.ID] = p
		}
	}

	merged := make([]config.PlanConfig, 0, len(byID))
	for _, p := range byID {
		merged = append(merged, p)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Faculty != merged[j].Faculty {
			return merged[i].Faculty < merged[j].Faculty
		}
		return merged[i].ID < merged[j].ID
	})
	return merged
}

func (u *Updater) RunWithDiscovery(ctx context.Context, sources []config.Source,
	static []config.PlanConfig, reg *config.Registry, plansPath string, interval time.Duration) {

	cycle := func() {
		plans := u.Discover(sources, static, reg.List())
		reg.Set(plans)
		if plansPath != "" {
			if err := config.SavePlans(plansPath, plans); err != nil {
				log.Printf("zapis %s: %v", plansPath, err)
			}
		}
		if err := u.UpdateAll(ctx, plans); err != nil {
			log.Printf("odświeżanie planów: %v", err)
		}
	}

	cycle()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cycle()
		}
	}
}
