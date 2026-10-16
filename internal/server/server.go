package server

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
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
	lastRefresh map[string]time.Time
}

func NewServer(plans []config.PlanConfig, store *store.Store, updater *updater.Updater) *Server {
	return &Server{
		plans:       plans,
		store:       store,
		updater:     updater,
		lastRefresh: make(map[string]time.Time),
	}
}

func (s *Server) Handler() (http.Handler, error) {
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	if verificationFile := os.Getenv("GOOGLE_VERIFICATION_FILE"); verificationFile != "" {
		mux.HandleFunc("/"+verificationFile, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("google-site-verification: " + verificationFile))
		})
	}

	mux.HandleFunc("/api/plans", withGzip(s.handlePlans))
	mux.HandleFunc("/api/plan/", withGzip(s.handlePlan))
	mux.HandleFunc("/api/refresh", s.handleRefresh)

	mux.Handle("/", withCache(http.FileServer(http.FS(static))))

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

	planID := r.URL.Query().Get("plan")
	if planID == "" {
		writeError(w, http.StatusBadRequest, "brak parametru 'plan'")
		return
	}

	plan, ok := s.getPlan(planID)
	if !ok {
		writeError(w, http.StatusNotFound, "nie znaleziono planu")
		return
	}

	if wait := s.reserveRefresh(plan.ID); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "ten plan był odświeżany przed chwilą, spróbuj za moment")
		return
	}

	if err := s.updater.Update(plan); err != nil {
		log.Printf("ręczne odświeżanie planu %s: %v", plan.ID, err)
		writeError(w, http.StatusBadGateway, "nie udało się pobrać planu ze strony WAT")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) reserveRefresh(planID string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastRefresh == nil {
		s.lastRefresh = make(map[string]time.Time)
	}

	last := s.lastRefresh[planID]
	if wait := refreshCooldown - time.Since(last); wait > 0 {
		return wait
	}
	s.lastRefresh[planID] = time.Now()
	return 0
}

func (s *Server) hasPlan(id string) bool {
	_, ok := s.getPlan(id)
	return ok
}

func (s *Server) getPlan(id string) (config.PlanConfig, bool) {
	for _, p := range s.plans {
		if p.ID == id {
			return p, true
		}
	}
	return config.PlanConfig{}, false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func withGzip(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()

		gzw := gzipResponseWriter{Writer: gz, ResponseWriter: w}
		next(gzw, r)
	}
}

func withCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}
