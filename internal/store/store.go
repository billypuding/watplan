package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/billypuding/watplan/internal/durable"
	"github.com/billypuding/watplan/internal/model"
)

type Store struct {
	dir    string
	mu     sync.RWMutex
	mirror durable.Mirror
}

const (
	planPrefix    = "plan"
	historyPrefix = "history"
)

func (s *Store) SetMirror(m durable.Mirror) {
	s.mu.Lock()
	s.mirror = m
	s.mu.Unlock()
}

func (s *Store) Restore(m durable.Mirror) (restored, uploaded int, err error) {
	remote, err := m.All(planPrefix + "/")
	if err != nil {
		return 0, 0, err
	}
	seen := map[string]bool{}
	for key, data := range remote {
		id := durable.TrimKey(planPrefix, key)
		seen[id] = true
		var incoming model.Plan
		if json.Unmarshal(data, &incoming) != nil {
			continue
		}
		local, lerr := s.Load(id)
		if lerr == nil && local.FetchedAt >= incoming.FetchedAt {
			continue
		}
		if err := s.writeFile(s.path(id), data); err != nil {
			return restored, uploaded, err
		}
		restored++
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return restored, uploaded, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".history.json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if seen[id] {
			continue
		}
		data, err := s.Raw(id)
		if err != nil {
			continue
		}
		if err := m.Put(durable.Keyed(planPrefix, id), data); err != nil {
			return restored, uploaded, err
		}
		uploaded++
	}

	histories, err := m.All(historyPrefix + "/")
	if err != nil {
		return restored, uploaded, err
	}
	for key, data := range histories {
		id := durable.TrimKey(historyPrefix, key)
		if err := s.writeFile(s.historyPath(id), data); err != nil {
			return restored, uploaded, err
		}
	}
	return restored, uploaded, nil
}

func (s *Store) writeFile(path string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) mirrorPut(key string, data []byte) {
	if s.mirror != nil {
		_ = s.mirror.Put(key, data)
	}
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Raw(id string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return os.ReadFile(s.path(id))
}

func (s *Store) Load(id string) (model.Plan, error) {
	data, err := s.Raw(id)
	if err != nil {
		return model.Plan{}, err
	}

	var plan model.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return model.Plan{}, err
	}
	return plan, nil
}

func (s *Store) Save(id string, plan model.Plan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tmp := s.path(id) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path(id)); err != nil {
		return err
	}
	s.mirrorPut(durable.Keyed(planPrefix, id), data)
	return nil
}

func (s *Store) History(id string) ([]model.Change, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readHistory(id)
}

func (s *Store) readHistory(id string) ([]model.Change, error) {
	data, err := os.ReadFile(s.historyPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []model.Change
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Store) AddHistory(id string, change model.Change, keep int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.readHistory(id)
	if err != nil {
		list = nil
	}
	list = append([]model.Change{change}, list...)
	if keep > 0 && len(list) > keep {
		list = list[:keep]
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	tmp := s.historyPath(id) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.historyPath(id)); err != nil {
		return err
	}
	s.mirrorPut(durable.Keyed(historyPrefix, id), data)
	return nil
}

func (s *Store) historyPath(id string) string {
	return filepath.Join(s.dir, id+".history.json")
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
