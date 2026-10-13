package server

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/updater"
)

//go:embed web
var webFS embed.FS

const refreshCooldown = 2 * time.Minute

type Server struct {
	plans       []config.PlanConfig
	store       *store.Store
	updater     *updater.Updater
	mu          sync.Mutex
	lastRefresh time.Time
}

func NewServer(plans []config.PlanConfig, store *store.Store, updater *updater.Updater) *Server {
	return &Server{plans: plans, store: store, updater: updater}
}

func (s *Server) Handler() (http.Handler, error) {
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/plans", s.handlePlans)
	mux.HandleFunc("/api/plan/", s.handlePlan)
	mux.HandleFunc("/api/refresh", s.handleRefresh)
	mux.Handle("/", http.FileServer(http.FS(static)))
	return mux, nil
}

func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda GET")
		return
	}

	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	items := make([]item, 0, len(s.plans))
	for _, p := range s.plans {
		items = append(items, item{ID: p.ID, Name: p.Name})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda GET")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/plan/")
	if !s.hasPlan(id) {
		writeError(w, http.StatusNotFound, "nie znaleziono planu")
		return
	}

	data, err := s.store.Raw(id)
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusServiceUnavailable, "plan nie został jeszcze pobrany")
		return
	}
	if err != nil {
		log.Printf("odczyt planu %s: %v", id, err)
		writeError(w, http.StatusInternalServerError, "błąd odczytu planu")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda POST")
		return
	}

	if wait := s.reserveRefresh(); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "plan był odświeżany przed chwilą, spróbuj za moment")
		return
	}

	if err := s.updater.UpdateAll(s.plans); err != nil {
		log.Printf("ręczne odświeżanie: %v", err)
		writeError(w, http.StatusBadGateway, "nie udało się pobrać planu ze strony WAT")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) reserveRefresh() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if wait := refreshCooldown - time.Since(s.lastRefresh); wait > 0 {
		return wait
	}
	s.lastRefresh = time.Now()
	return 0
}

func (s *Server) hasPlan(id string) bool {
	for _, p := range s.plans {
		if p.ID == id {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
