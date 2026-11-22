package server

import (
	"compress/gzip"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/durable"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/push"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/updater"
	"github.com/billypuding/watplan/internal/wat"
)

//go:embed web
var webFS embed.FS

const (
	refreshCooldown = time.Minute

	staleAfter = time.Hour
	staleRetry = 30 * time.Minute
	staleWait  = 4 * time.Second
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
	Push *push.Service
	DB   *durable.Queue

	Offline bool

	OnDemand bool

	CanonicalHost string
}

type Server struct {
	registry    *config.Registry
	store       *store.Store
	updater     *updater.Updater
	options     Options
	mu          sync.Mutex
	lastRefresh map[string]time.Time
	lastStale   map[string]time.Time
	rooms       roomsCache
	semester    semesterCache
	dbMu        sync.RWMutex
	ready       <-chan struct{}
}

const readyWait = 25 * time.Second

func (s *Server) SetDB(q *durable.Queue) {
	s.dbMu.Lock()
	s.options.DB = q
	s.dbMu.Unlock()
}

func (s *Server) SetReady(ch <-chan struct{}) {
	s.ready = ch
}

func (s *Server) waitReady(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.ready != nil {
			select {
			case <-s.ready:
			case <-time.After(readyWait):
			case <-r.Context().Done():
				return
			}
		}
		next(w, r)
	}
}

func NewServer(registry *config.Registry, store *store.Store, updater *updater.Updater, options Options) *Server {
	s := &Server{
		registry:    registry,
		store:       store,
		updater:     updater,
		options:     options,
		lastRefresh: make(map[string]time.Time),
		lastStale:   make(map[string]time.Time),
	}
	store.OnSave(s.rooms.update)
	return s
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
	mux.HandleFunc("/api/plan/", s.waitReady(withGzip(s.handlePlan)))
	mux.HandleFunc("/api/refresh", s.waitReady(s.handleRefresh))
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/changes/", s.waitReady(withGzip(s.handleChanges)))
	mux.HandleFunc("/api/rooms", s.waitReady(withGzip(s.handleRooms)))
	mux.HandleFunc("/api/semester", s.waitReady(s.handleSemester))
	mux.HandleFunc("/api/makeup", s.waitReady(withGzip(s.handleMakeup)))
	mux.HandleFunc("/api/push/key", s.handlePushKey)
	mux.HandleFunc("/api/push/subscribe", s.handlePushSubscribe)
	mux.HandleFunc("/api/push/unsubscribe", s.handlePushUnsubscribe)
	mux.HandleFunc("/cal/", s.waitReady(withGzip(s.handleCalendar)))

	appPage, err := fs.ReadFile(static, "app.html")
	if err != nil {
		return nil, err
	}
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(appPage)
	})
	faqPage, err := fs.ReadFile(static, "o-watplan.html")
	if err != nil {
		return nil, err
	}
	mux.HandleFunc("/o-watplan", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(faqPage)
	})
	mux.HandleFunc("/app/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, withQuery("/app", r), http.StatusMovedPermanently)
	})

	files := withCache(http.FileServer(http.FS(static)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			q := r.URL.Query()
			if q.Has("plan") || q.Has("view") {
				http.Redirect(w, r, withQuery("/app", r), http.StatusFound)
				return
			}
		}
		files.ServeHTTP(w, r)
	})

	return withCanonicalHost(mux, s.options.CanonicalHost), nil
}

func withCanonicalHost(next http.Handler, canonical string) http.Handler {
	if canonical == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host == canonical || !strings.HasSuffix(host, ".onrender.com") ||
			strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/sw.js" {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "https://"+canonical+r.URL.RequestURI(), http.StatusMovedPermanently)
	})
}

func withQuery(path string, r *http.Request) string {
	if r.URL.RawQuery == "" {
		return path
	}
	return path + "?" + r.URL.RawQuery
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

	data, ok := s.planData(w, plan)
	if !ok {
		return
	}

	sum := sha1.Sum(data)
	etag := `"` + hex.EncodeToString(sum[:10]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if part == want || part == "*" {
			return true
		}
	}
	return false
}

func (s *Server) planData(w http.ResponseWriter, plan config.PlanConfig) ([]byte, bool) {
	id := plan.ID
	data, err := s.store.Raw(id)
	if errors.Is(err, fs.ErrNotExist) && s.options.OnDemand && s.updater != nil {
		log.Printf("plan %s: brak w cache, pobieram na żądanie", id)
		if updateErr := s.updater.Ensure(plan); updateErr != nil {
			log.Printf("pobieranie planu %s na żądanie: %v", id, updateErr)
			s.writeUpdateError(w, updateErr)
			return nil, false
		}
		data, err = s.store.Raw(id)
	}
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusServiceUnavailable, "plan nie został jeszcze pobrany")
		return nil, false
	}
	if err == nil && s.options.OnDemand && s.updater != nil {
		if done := s.refreshIfStale(plan); done != nil {
			select {
			case <-done:
				if fresh, freshErr := s.store.Raw(id); freshErr == nil {
					data = fresh
				}
			case <-time.After(staleWait):
			}
		}
	}
	if err != nil {
		log.Printf("odczyt planu %s: %v", id, err)
		writeError(w, http.StatusInternalServerError, "błąd odczytu planu")
		return nil, false
	}
	return data, true
}

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.options.Push == nil {
		writeError(w, http.StatusNotFound, "powiadomienia są wyłączone na tym serwerze")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": s.options.Push.PublicKey()})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda POST")
		return
	}
	if s.options.Push == nil {
		writeError(w, http.StatusNotFound, "powiadomienia są wyłączone na tym serwerze")
		return
	}
	var req struct {
		Subscription push.Subscription `json:"subscription"`
		Plans        []string          `json:"plans"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "nieprawidłowe dane")
		return
	}
	sub := req.Subscription
	sub.Plans = nil
	for _, id := range req.Plans {
		if _, ok := s.registry.Get(id); ok {
			sub.Plans = append(sub.Plans, id)
		}
	}
	if err := s.options.Push.Subscribe(sub); err != nil {
		if errors.Is(err, push.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Printf("push: zapis subskrypcji: %v", err)
		writeError(w, http.StatusInternalServerError, "nie udało się zapisać subskrypcji")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": sub.Plans})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda POST")
		return
	}
	if s.options.Push == nil {
		writeError(w, http.StatusNotFound, "powiadomienia są wyłączone na tym serwerze")
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil || req.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "nieprawidłowe dane")
		return
	}
	if err := s.options.Push.Unsubscribe(req.Endpoint); err != nil {
		log.Printf("push: usuwanie subskrypcji: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda GET")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/changes/")
	plan, ok := s.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "nie znaleziono planu")
		return
	}
	list, err := s.store.History(plan.ID)
	if err != nil {
		log.Printf("historia zmian %s: %v", plan.ID, err)
		writeError(w, http.StatusInternalServerError, "błąd odczytu historii zmian")
		return
	}
	if list == nil {
		list = []model.Change{}
	}
	resp := map[string]any{"plan": plan.ID, "changes": list}
	if stored, err := s.store.Load(plan.ID); err == nil {
		resp["fetched_at"] = stored.FetchedAt
		resp["modified_at"] = stored.ModifiedAt
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "wymagana metoda GET")
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/cal/")
	id, ok := strings.CutSuffix(name, ".ics")
	if !ok {
		writeError(w, http.StatusNotFound, "nie znaleziono kalendarza")
		return
	}
	plan, ok := s.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "nie znaleziono planu")
		return
	}

	alarm := 0
	if v := r.URL.Query().Get("alarm"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 1440 {
			writeError(w, http.StatusBadRequest, "parametr 'alarm' to liczba minut od 0 do 1440")
			return
		}
		alarm = n
	}

	data, ok := s.planData(w, plan)
	if !ok {
		return
	}
	var stored model.Plan
	if err := json.Unmarshal(data, &stored); err != nil {
		log.Printf("odczyt planu %s: %v", id, err)
		writeError(w, http.StatusInternalServerError, "błąd odczytu planu")
		return
	}

	stamp, err := time.ParseInLocation(timeLayout, stored.ModifiedAt, warsawLoc)
	if err != nil {
		stamp = time.Now()
	}
	hide := map[string]bool{}
	for _, name := range r.URL.Query()["hide"] {
		if len(hide) >= 100 {
			break
		}
		hide[name] = true
	}
	body := buildICS(plan, stored.Events, icsOptions{alarm: alarm, hide: hide, baseURL: s.baseURL(r), stamp: stamp})

	sum := sha1.Sum(body)
	etag := `"` + hex.EncodeToString(sum[:10]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=900")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	if r.URL.Query().Has("download") {
		w.Header().Set("Content-Disposition", `attachment; filename="plan-`+plan.ID+`.ics"`)
	}
	if r.Method == http.MethodHead {
		return
	}
	w.Write(body)
}

func (s *Server) baseURL(r *http.Request) string {
	if s.options.CanonicalHost != "" {
		return "https://" + s.options.CanonicalHost
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
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

	if wait := s.reserveRefresh(plan.ID); wait <= 0 {
		go func() {
			if err := s.updater.UpdateManual(plan); err != nil {
				log.Printf("ręczne odświeżanie planu %s: %v", plan.ID, err)
			}
		}()
	}

	data, err := s.store.Raw(plan.ID)
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Plan-Refresh", "background")
		w.Write(data)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "refreshing"})
}

func (s *Server) refreshIfStale(plan config.PlanConfig) <-chan struct{} {
	cached, err := s.store.Load(plan.ID)
	if err != nil {
		return nil
	}
	fetched, err := time.ParseInLocation(timeLayout, cached.FetchedAt, warsawLoc)
	if err == nil && time.Since(fetched) < staleAfter {
		return nil
	}

	s.mu.Lock()
	if time.Since(s.lastStale[plan.ID]) < staleRetry {
		s.mu.Unlock()
		return nil
	}
	s.lastStale[plan.ID] = time.Now()
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := s.updater.Update(plan); err != nil {
			log.Printf("odświeżanie nieaktualnego planu %s: %v", plan.ID, err)
			return
		}
		log.Printf("plan %s: odświeżony na żądanie (poprzednio %s)", plan.ID, cached.FetchedAt)
	}()
	return done
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
		"proxy":         s.updater.ProxyHost(),
		"faculties":     s.updater.SourceStatus(),
		"push":          s.options.Push != nil,
		"proxy_error":   proxyErrorText(s.updater),
		"db":            s.dbStatus(),
		"missing_plans": s.updater.MissingPlans(),
		"now":           time.Now().In(warsawLoc).Format(timeLayout),
	})
}

func (s *Server) dbStatus() map[string]any {
	s.dbMu.RLock()
	db := s.options.DB
	s.dbMu.RUnlock()
	if db == nil {
		return map[string]any{"enabled": false}
	}
	failed, saved := db.Status()
	return map[string]any{"enabled": true, "saved": saved, "error": failed, "plans_at_start": s.store.RemotePlans()}
}

func proxyErrorText(u *updater.Updater) string {
	status, at := u.ProxyError()
	if status == "" {
		return ""
	}
	return status + " (" + at.In(warsawLoc).Format(timeLayout) + ")"
}

func (s *Server) writeUpdateError(w http.ResponseWriter, err error) {
	if errors.Is(err, wat.ErrProxy) {
		msg := "serwer PlanWat nie może połączyć się przez skonfigurowane proxy"
		if status, _ := s.updater.ProxyError(); status != "" {
			msg += " (" + status + ")"
		}
		writeError(w, http.StatusBadGateway, msg+". Pokazuję ostatni zapisany plan")
		return
	}
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
		if r.URL.Path == "/" || r.URL.Path == "/sw.js" || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-cache")
		} else if r.URL.Query().Has("v") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		next.ServeHTTP(w, r)
	})
}
