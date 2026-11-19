package updater

import (
	"context"
	"errors"
	"log"
	"sort"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/wat"
)

func (u *Updater) Discover(sources []config.Source, static, prev []config.PlanConfig) []config.PlanConfig {
	plans, _ := u.discover(sources, static, prev)
	return plans
}

func (u *Updater) discover(sources []config.Source, static, prev []config.PlanConfig) ([]config.PlanConfig, bool) {
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

	extras := make(map[string][]config.PlanSource)
	for _, list := range [][]config.PlanConfig{static, prev} {
		for _, p := range list {
			if len(p.Extra) > 0 {
				extras[p.ID] = p.Extra
			}
		}
	}

	blocked := false
	var later []config.PlanConfig
	for _, src := range sources {
		plans, err := u.fetcher.DiscoverPlans(src)
		if errors.Is(err, wat.ErrBlocked) {
			blocked = true
		}
		now := time.Now().In(warsawLoc).Format(timeLayout)
		if src.Semester != "" {
			if err == nil {
				err = u.fetcher.CheckCurrent(src, plans)
			}
			if err != nil {
				logDiscoveryError(src, err)
				u.recordSource(sourceKey(src), FacultyStatus{Checked: now, Error: describeDiscoveryError(src, err)})
				continue
			}
			log.Printf("discover %q: znaleziono %d planów", sourceKey(src), len(plans))
			u.recordSource(sourceKey(src), FacultyStatus{Groups: len(plans), Checked: now})
			later = append(later, plans...)
			continue
		}
		if err != nil || len(plans) == 0 {
			if err != nil {
				logDiscoveryError(src, err)
				u.recordSource(sourceKey(src), FacultyStatus{Checked: now, Error: describeDiscoveryError(src, err)})
			} else {
				log.Printf("discover %q: nie znaleziono żadnych planów (zostawiam poprzednie plany tego wydziału)", src.Faculty)
				u.recordSource(sourceKey(src), FacultyStatus{Checked: now, Error: "lista grup jest pusta"})
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
		u.recordSource(sourceKey(src), FacultyStatus{Groups: len(plans), Checked: now})
		for _, p := range plans {
			byID[p.ID] = p
		}
	}
	for id, extra := range extras {
		if p, ok := byID[id]; ok && len(p.Extra) == 0 {
			p.Extra = extra
			byID[id] = p
		}
	}

	merged := make([]config.PlanConfig, 0, len(byID))
	for _, p := range byID {
		merged = append(merged, p)
	}
	if len(later) > 0 {
		merged, _ = config.MergeExtra(merged, later)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Faculty != merged[j].Faculty {
			return merged[i].Faculty < merged[j].Faculty
		}
		return merged[i].ID < merged[j].ID
	})
	return merged, blocked
}

func (u *Updater) RunWithDiscovery(ctx context.Context, sources []config.Source,
	static []config.PlanConfig, reg *config.Registry, plansPath string, interval time.Duration) {

	cycle := func() {
		plans, _ := u.discover(sources, static, reg.List())
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
