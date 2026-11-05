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
	"github.com/billypuding/watplan/internal/wat"
)

//go:embed web
var webFS embed.FS

const (
	refreshCooldown = 2 * time.Minute
	// W trybie on-demand plan starszy niż staleAfter jest odświeżany w tle
	// przy otwarciu, ale nie częściej niż co staleRetry (także po błędzie).
	staleAfter = time.Hour
	staleRetry = 30 * time.Minute
	timeLayout = "2006-01-02 15:04:05"
)

var warsawLoc = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		return time.Local
	}
	return loc
}()

type Options struct {
	// Offline wyłącza wszystkie zapytania do WAT.
	Offline bool
	// OnDemand pobiera plan z WAT dopiero przy pierwszym żądaniu, zamiast
	// odświeżać wszystkie plany w tle.
	OnDemand bool
}

type Server struct {
	registry    *config.Registry
	store       *store.Store
	updater     *updater.Updater
	options     Options
	mu          sync.Mutex
	lastRefresh map[string]time.Time
	lastStale   map[string]time.Time
}

func NewServer(registry *config.Registry, store *store.Store, updater *updater.Updater, options Options) *Server {
	return &Server{
		registry:    registry,
		store:       store,
		updater:     updater,
		options:     options,
		lastRefresh: make(map[string]time.Time),
		lastStale:   make(map[string]time.Time),
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
	mux.HandleFunc("/api/status", s.handleStatus)

	mux.Handle("/", withCache(http.FileServer(http.FS(static))))

	return mux, nil
}

func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda GET")
		return
	}

	type item struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Faculty string `json:"faculty,omitempty"`
	}

	plans := s.registry.List()
	items := make([]item, 0, len(plans))
	for _, p := range plans {
		items = append(items, item{ID: p.ID, Name: p.Name, Faculty: p.Faculty})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda GET")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/plan/")
	plan, ok := s.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "nie znaleziono planu")
		return
	}

	data, err := s.store.Raw(id)
	if errors.Is(err, fs.ErrNotExist) && s.options.OnDemand && s.updater != nil {
		log.Printf("plan %s: brak w cache, pobieram na żądanie", id)
		if updateErr := s.updater.Ensure(plan); updateErr != nil {
			log.Printf("pobieranie planu %s na żądanie: %v", id, updateErr)
			s.writeUpdateError(w, updateErr)
			return
		}
		data, err = s.store.Raw(id)
	}
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusServiceUnavailable, "plan nie został jeszcze pobrany")
		return
	}
	if err == nil && s.options.OnDemand && s.updater != nil {
		s.refreshIfStale(plan)
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

	if s.options.Offline {
		writeError(w, http.StatusServiceUnavailable, "tryb offline: pobieranie z WAT jest wyłączone, wyświetlany jest zapisany plan")
		return
	}

	planID := r.URL.Query().Get("plan")
	if planID == "" {
		writeError(w, http.StatusBadRequest, "brak parametru 'plan'")
		return
	}

	plan, ok := s.registry.Get(planID)
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
		s.writeUpdateError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// refreshIfStale odświeża w tle plan, który był pobrany dawno temu. Bieżące
// żądanie dostaje dane z cache, kolejne - już świeże.
func (s *Server) refreshIfStale(plan config.PlanConfig) {
	cached, err := s.store.Load(plan.ID)
	if err != nil {
		return
	}
	fetched, err := time.ParseInLocation(timeLayout, cached.FetchedAt, warsawLoc)
	if err == nil && time.Since(fetched) < staleAfter {
		return
	}

	s.mu.Lock()
	if time.Since(s.lastStale[plan.ID]) < staleRetry {
		s.mu.Unlock()
		return
	}
	s.lastStale[plan.ID] = time.Now()
	s.mu.Unlock()

	go func() {
		if err := s.updater.Update(plan); err != nil {
			log.Printf("odświeżanie nieaktualnego planu %s: %v", plan.ID, err)
			return
		}
		log.Printf("plan %s: odświeżony na żądanie (poprzednio %s)", plan.ID, cached.FetchedAt)
	}()
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	mode := "background"
	switch {
	case s.options.Offline:
		mode = "offline"
	case s.options.OnDemand:
		mode = "on-demand"
	}
	blocked := map[string]string{}
	for host, until := range s.updater.BlockedHosts() {
		blocked[host] = until.In(warsawLoc).Format(timeLayout)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":          mode,
		"blocked_hosts": blocked,
		"now":           time.Now().In(warsawLoc).Format(timeLayout),
	})
}

func (s *Server) writeUpdateError(w http.ResponseWriter, err error) {
	if errors.Is(err, wat.ErrBlocked) {
		writeError(w, http.StatusServiceUnavailable,
			"WAT (Incapsula) blokuje pobieranie z serwera PlanWat. Pokazuję ostatni zapisany plan")
		return
	}
	writeError(w, http.StatusBadGateway, "nie udało się pobrać planu ze strony WAT")
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
		// HTML zawsze świeży (wskazuje wersjonowane app.js/style.css), reszta na dobę.
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		next.ServeHTTP(w, r)
	})
}
