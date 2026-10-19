package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Source struct {
	Faculty   string `json:"faculty"`
	IndexURL  string `json:"index_url"`
	Depth     int    `json:"depth,omitempty"`
	StartYear int    `json:"start_year,omitempty"` // początek roku akademickiego; gdy 0, rok z nazwy pliku
}

func LoadSources(path string) ([]Source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var sources []Source
	if err := json.Unmarshal(data, &sources); err != nil {
		return nil, fmt.Errorf("sources %s: %w", path, err)
	}
	for i, s := range sources {
		if s.Faculty == "" || s.IndexURL == "" {
			return nil, fmt.Errorf("sources %s: wpis %d musi mieć faculty i index_url", path, i+1)
		}
	}
	return sources, nil
}

func SavePlans(path string, plans []PlanConfig) error {
	data, err := json.MarshalIndent(plans, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "plans-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

type Registry struct {
	mu    sync.RWMutex
	plans []PlanConfig
}

func NewRegistry(initial []PlanConfig) *Registry {
	return &Registry{plans: append([]PlanConfig(nil), initial...)}
}

func (r *Registry) Set(plans []PlanConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plans = append([]PlanConfig(nil), plans...)
}

func (r *Registry) List() []PlanConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]PlanConfig(nil), r.plans...)
}

func (r *Registry) Get(id string) (PlanConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.plans {
		if p.ID == id {
			return p, true
		}
	}
	return PlanConfig{}, false
}
