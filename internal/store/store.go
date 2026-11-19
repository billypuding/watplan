package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/billypuding/watplan/internal/model"
)

type Store struct {
	dir string
	mu  sync.RWMutex
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
	return os.Rename(tmp, s.path(id))
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
	return os.Rename(tmp, s.historyPath(id))
}

func (s *Store) historyPath(id string) string {
	return filepath.Join(s.dir, id+".history.json")
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
