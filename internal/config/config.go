package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

var planIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type PlanConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	StartYear int    `json:"start_year"`
	Faculty   string `json:"faculty,omitempty"`
}

func LoadConfig(path string) ([]PlanConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var plans []PlanConfig
	if err := json.Unmarshal(data, &plans); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if len(plans) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	for i, p := range plans {
		switch {
		case !planIDPattern.MatchString(p.ID):
			return nil, fmt.Errorf("config %s: plan %d ma nieprawidłowe id %q", path, i+1, p.ID)
		case seen[p.ID]:
			return nil, fmt.Errorf("config %s: powtórzone id %q", path, p.ID)
		case p.URL == "":
			return nil, fmt.Errorf("config %s: plan %q nie ma adresu url", path, p.ID)
		case p.StartYear == 0:
			return nil, fmt.Errorf("config %s: plan %q nie ma pola start_year", path, p.ID)
		}
		seen[p.ID] = true
		if p.Name == "" {
			plans[i].Name = p.ID
		}
	}
	return plans, nil
}
