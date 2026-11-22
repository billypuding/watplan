package wat

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
)

var ErrBudget = errors.New("dzienny limit pobierań wyczerpany")

const (
	defaultScraperBudget = 400
	defaultProxyBudget   = 300
)

type BudgetStatus struct {
	Used  int `json:"used_24h"`
	Limit int `json:"limit_24h"`
}

func budgetFromEnv(name string, fallback int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("zły %s %q (liczba zapytań na dobę, 0 = bez limitu)", name, v)
	}
	return n, nil
}

func (f *Fetcher) spend(provider string, limit int) error {
	if limit <= 0 {
		return nil
	}
	used := f.usage.Requests24h(provider)
	f.budgetMu.Lock()
	wasOver := f.budgetOver[provider]
	over := used >= limit
	f.budgetOver[provider] = over
	f.budgetMu.Unlock()
	if over {
		if !wasOver {
			log.Printf("limit: %s %d/%d zapytań w 24 h, wstrzymuję pobieranie tą drogą", provider, used, limit)
		}
		f.alert.Raise("budget:"+provider, fmt.Sprintf("Dzienny limit zapytań do %s wyczerpany (%d/%d w 24 h). Serwer pokazuje zapisane plany, dopóki limit się nie zwolni.", provider, used, limit))
		return fmt.Errorf("%w (%s: %d/%d w 24 h)", ErrBudget, provider, used, limit)
	}
	if wasOver {
		f.alert.Resolved("budget:"+provider, fmt.Sprintf("Limit zapytań do %s znów pozwala pobierać (%d/%d w 24 h).", provider, used, limit))
	}
	return nil
}

func (f *Fetcher) withinBudget(provider string, limit int) bool {
	return limit <= 0 || f.usage.Requests24h(provider) < limit
}

func (f *Fetcher) BudgetLeft(mode FetchMode) bool {
	if f == nil {
		return true
	}
	scraperOK := f.scraper == nil || f.withinBudget(f.scraper.provider, f.scraperBudget)
	if mode == FetchViaProxy && f.tlsProxy != nil {
		return f.withinBudget(f.tlsProxy.provider(), f.proxyBudget) || (f.scraper != nil && scraperOK)
	}
	return scraperOK
}

func (f *Fetcher) Budget() map[string]BudgetStatus {
	if f == nil {
		return nil
	}
	out := map[string]BudgetStatus{}
	if f.scraper != nil {
		out[f.scraper.provider] = BudgetStatus{Used: f.usage.Requests24h(f.scraper.provider), Limit: f.scraperBudget}
	}
	if f.tlsProxy != nil {
		out[f.tlsProxy.provider()] = BudgetStatus{Used: f.usage.Requests24h(f.tlsProxy.provider()), Limit: f.proxyBudget}
	}
	return out
}
