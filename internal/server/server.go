package server

import (
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/subtle"
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
	refreshCooldown  = time.Minute
	manualFreshFor   = 15 * time.Minute
	ensureRetryEvery = 10 * time.Minute

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

	TrustProxy     bool
	RefreshPerHour int
	ChecksPerHour  int
	PushPerHour    int

	DBRequired        bool
	HealthFetchMaxAge time.Duration

	StatusToken string
}

const defaultHealthFetchMaxAge = 3 * time.Hour

const (
	defaultRefreshPerHour = 10
	defaultChecksPerHour  = 30
	defaultPushPerHour    = 20
)

var (
	errRateLimited = errors.New("za dużo zapytań")
	errNoBudget    = errors.New("limit pobierań wyczerpany")
)

type Server struct {
	registry    *config.Registry
	store       *store.Store
	updater     *updater.Updater
	options     Options
	mu          sync.Mutex
	lastRefresh map[string]time.Time
	lastVisit   map[string]time.Time
	refreshIPs  *rateLimiter
	checkIPs    *rateLimiter
	pushIPs     *rateLimiter
	cards       *shareCards
	blobs       planBlobs
	lastEnsure  map[string]time.Time
	ensureErr   map[string]error
	inflight    map[string]bool
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
		t := newTiming()
		if s.ready != nil {
			select {
			case <-s.ready:
			case <-time.After(readyWait):
			case <-r.Context().Done():
				return
			}
		}
		t.mark("ready")
		next(w, r.WithContext(context.WithValue(r.Context(), timingKey{}, t)))
	}
}

func NewServer(registry *config.Registry, store *store.Store, updater *updater.Updater, options Options) *Server {
	s := &Server{
		registry:    registry,
		store:       store,
		updater:     updater,
		options:     options,
		lastRefresh: make(map[string]time.Time),
		lastVisit:   make(map[string]time.Time),
		refreshIPs:  newRateLimiter(orDefault(options.RefreshPerHour, defaultRefreshPerHour), time.Hour),
		checkIPs:    newRateLimiter(orDefault(options.ChecksPerHour, defaultChecksPerHour), time.Hour),
		pushIPs:     newRateLimiter(orDefault(options.PushPerHour, defaultPushPerHour), time.Hour),
		lastEnsure:  make(map[string]time.Time),
		ensureErr:   make(map[string]error),
		inflight:    make(map[string]bool),
	}
	store.OnSave(s.rooms.update)
	return s
}

func (s *Server) Handler() (http.Handler, error) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, err
	}
	static, err := newAssetFS(sub)
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
	mux.HandleFunc("/api/plan/", s.waitReady(s.handlePlan))
	mux.HandleFunc("/api/refresh", s.waitReady(s.handleRefresh))
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/healthz", s.handleHealth)
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
	mux.HandleFunc("/app", htmlPage(appPage))
	mux.HandleFunc("/plan/", s.handlePlanPage(appPage))
	mux.HandleFunc("/sitemap.xml", s.handleSitemap)
	faqPage, err := fs.ReadFile(static, "o-watplan.html")
	if err != nil {
		return nil, err
	}
	mux.HandleFunc("/o-watplan", htmlPage(faqPage))
	allPlans := s.registry.List()
	site := "https://watplan.me"
	if s.options.CanonicalHost != "" {
		site = "https://" + s.options.CanonicalHost
	}
	mux.HandleFunc(allPlansAt, htmlPage(renderAllPlans(faqPage, allPlans, site)))
	logo, err := fs.ReadFile(sub, "favicon-192.png")
	if err != nil {
		return nil, err
	}
	cards, err := newShareCards(logo, len(allPlans))
	if err != nil {
		return nil, err
	}
	s.cards = cards
	mux.HandleFunc("/og.png", cards.serveSite)
	mux.HandleFunc("/app/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, withQuery("/app", r), http.StatusMovedPermanently)
	})

	indexPage, err := fs.ReadFile(static, "index.html")
	if err != nil {
		return nil, err
	}
	landing := htmlPage(indexPage)
	files := withCache(http.FileServer(http.FS(static)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			q := r.URL.Query()
			if q.Has("plan") || q.Has("view") {
				http.Redirect(w, r, withQuery("/app", r), http.StatusFound)
				return
			}
			landing(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})

	return withSecurityHeaders(withCanonicalHost(withAPIHeaders(mux), s.options.CanonicalHost), contentSecurityPolicy(static.scriptHashes())), nil
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
		if host == canonical || host == "" || host == "localhost" || net.ParseIP(host) != nil ||
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
	data, err := json.Marshal(items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "błąd listy planów")
		return
	}
	sum := sha1.Sum(data)
	etag := `"` + hex.EncodeToString(sum[:10]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", cacheAPIWithET)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
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

	t := timingFrom(r.Context())
	defer func() {
		if t.total() > slowResponse {
			log.Printf("wolna odpowiedź /api/plan/%s: %s", plan.ID, t)
		}
	}()
	data, ok := s.planData(w, r, plan, t, false)
	if !ok {
		return
	}

	blob := s.blobs.get(plan.ID, data)
	w.Header().Set("ETag", blob.etag)
	w.Header().Set("Cache-Control", cacheAPIWithET)
	w.Header().Set("Vary", "Accept-Encoding")
	t.mark("etag")
	w.Header().Set("Server-Timing", t.header())
	if etagMatches(r.Header.Get("If-None-Match"), blob.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, encoding := blob.encoded(r.Header.Get("Accept-Encoding"))
	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Write(body)
	t.mark("write")
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

func (s *Server) planData(w http.ResponseWriter, r *http.Request, plan config.PlanConfig, t *timing, wait bool) ([]byte, bool) {
	id := plan.ID
	data, err := s.store.Raw(id)
	t.mark("read")
	if errors.Is(err, fs.ErrNotExist) && s.options.OnDemand && s.updater != nil && !wait {
		ensureErr := error(errNoPlanData)
		if !isBot(r) {
			ensureErr = s.startEnsure(plan, s.clientIP(r))
		}
		if ensureErr != nil {
			s.writeUpdateError(w, ensureErr)
			return nil, false
		}
		t.mark("visit")
		w.Header().Set("X-Plan-Refresh", "background")
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "fetching"})
		return nil, false
	}
	if errors.Is(err, fs.ErrNotExist) && s.options.OnDemand && s.updater != nil {
		log.Printf("plan %s: brak w cache, pobieram na żądanie", id)
		if updateErr := s.updater.Ensure(plan); updateErr != nil {
			log.Printf("pobieranie planu %s na żądanie: %v", id, updateErr)
			s.writeUpdateError(w, updateErr)
			return nil, false
		}
		data, err = s.store.Raw(id)
		t.mark("fetch")
	}
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusServiceUnavailable, "plan nie został jeszcze pobrany")
		return nil, false
	}
	if err == nil && s.options.OnDemand && s.updater != nil {
		refresh := "none"
		if s.checkOnVisit(plan, data, s.clientIP(r), isBot(r)) {
			refresh = "background"
		}
		w.Header().Set("X-Plan-Refresh", refresh)
		t.mark("visit")
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
	if len(req.Plans) > 0 && !s.options.Push.Has(sub.Endpoint) && !s.pushIPs.allow(s.clientIP(r)) {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "za dużo nowych subskrypcji z tej sieci, spróbuj za godzinę")
		return
	}
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

	data, ok := s.planData(w, r, plan, nil, true)
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

	data, err := s.store.Raw(plan.ID)
	if err == nil {
		if checked, ok := checkedAt(data); ok && time.Since(checked) < manualFreshFor {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Plan-Refresh", "fresh")
			w.Header().Set("X-Plan-Checked-At", checked.Format(time.RFC3339))
			w.Write(data)
			return
		}
	}

	refresh := s.startManual(plan, s.clientIP(r))
	w.Header().Set("X-Plan-Refresh", refresh)
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}
	status := "refreshing"
	if refresh != "background" {
		status = refresh
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": status})
}

func readPlanTimes(data []byte) updater.PlanTimes {
	var stamps struct {
		FetchedAt  string `json:"fetched_at"`
		CheckedAt  string `json:"checked_at"`
		ModifiedAt string `json:"modified_at"`
	}
	if err := json.Unmarshal(data, &stamps); err != nil {
		return updater.PlanTimes{}
	}
	return updater.TimesOf(model.Plan{FetchedAt: stamps.FetchedAt, CheckedAt: stamps.CheckedAt, ModifiedAt: stamps.ModifiedAt})
}

func checkedAt(data []byte) (time.Time, bool) {
	t := readPlanTimes(data)
	return t.Checked, t.HasChecked
}

func (s *Server) busyLocked(id string) bool {
	return s.inflight[id] || s.updater.Busy(id)
}

func (s *Server) startLocked(plan config.PlanConfig, label string, run func(config.PlanConfig) error) {
	s.inflight[plan.ID] = true
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.inflight, plan.ID)
			s.mu.Unlock()
		}()
		if err := run(plan); err != nil {
			log.Printf("%s planu %s: %v", label, plan.ID, err)
		}
	}()
}

var errNoPlanData = errors.New("plan bez zajęć")

func (s *Server) startEnsure(plan config.PlanConfig, ip string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busyLocked(plan.ID) {
		return nil
	}
	if err := s.ensureErr[plan.ID]; err != nil && time.Since(s.lastEnsure[plan.ID]) < ensureRetryEvery {
		return err
	}
	if s.updater.CacheOnly() {
		return updater.ErrCacheOnly
	}
	if !s.updater.FetchAllowed(false) {
		return errNoBudget
	}
	if !s.checkIPs.allow(ip) {
		return errRateLimited
	}
	s.lastEnsure[plan.ID] = time.Now()
	delete(s.ensureErr, plan.ID)
	s.inflight[plan.ID] = true
	go func() {
		log.Printf("plan %s: brak w cache, pobieram w tle", plan.ID)
		err := s.updater.Ensure(plan)
		if err == nil {
			if _, statErr := s.store.Raw(plan.ID); errors.Is(statErr, fs.ErrNotExist) {
				err = errNoPlanData
			}
		}
		if err != nil {
			log.Printf("pobieranie planu %s na żądanie: %v", plan.ID, err)
		}
		s.mu.Lock()
		delete(s.inflight, plan.ID)
		if err != nil {
			s.ensureErr[plan.ID] = err
		}
		s.mu.Unlock()
	}()
	return nil
}

func orDefault(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

func (s *Server) startManual(plan config.PlanConfig, ip string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busyLocked(plan.ID) {
		return "pending"
	}
	if time.Since(s.lastRefresh[plan.ID]) < refreshCooldown {
		return "cooldown"
	}
	if !s.updater.FetchAllowed(true) {
		return "budget"
	}
	if !s.refreshIPs.allow(ip) {
		return "limited"
	}
	s.reserveRefreshLocked(plan.ID)
	s.startLocked(plan, "ręczne odświeżanie", s.updater.UpdateManual)
	return "background"
}

func (s *Server) checkOnVisit(plan config.PlanConfig, data []byte, ip string, bot bool) bool {
	if bot {
		return false
	}
	d := s.updater.Decide(plan, readPlanTimes(data), time.Now())
	if !d.Due && !d.NeedIndex {
		if d.ViaIndex {
			go s.updater.Skip(plan.ID, d)
		}
		return false
	}
	minGap := model.CheckIntervals[len(model.CheckIntervals)-1].Every
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastVisit[plan.ID]) < minGap || s.busyLocked(plan.ID) {
		return false
	}
	if !s.updater.FetchAllowed(false) || !s.checkIPs.allow(ip) {
		return false
	}
	s.lastVisit[plan.ID] = time.Now()
	s.startLocked(plan, "sprawdzanie przy wejściu", s.updater.CheckPlan)
	return true
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	mode := "background"
	switch {
	case s.options.Offline:
		mode = "offline"
	case s.options.OnDemand:
		mode = "on-demand"
	}
	now := time.Now().In(warsawLoc).Format(timeLayout)
	if !s.statusAuthorized(r) {
		writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "now": now})
		return
	}
	blocked := map[string]string{}
	for host, until := range s.updater.BlockedHosts() {
		blocked[host] = until.In(warsawLoc).Format(timeLayout)
	}
	detail := map[string]any{
		"mode":          mode,
		"blocked_hosts": blocked,
		"proxy":         s.updater.ProxyHost(),
		"faculties":     s.updater.SourceStatus(),
		"push":          s.options.Push != nil,
		"proxy_error":   proxyErrorText(s.updater),
		"db":            s.dbStatus(),
		"missing_plans": s.updater.MissingPlans(),
		"usage":         s.updater.Usage().Snapshot(),
		"budget":        s.updater.Budget(),
		"now":           now,
	}
	if credits, ok := s.updater.Credits(); ok {
		detail["credits"] = credits
	}
	if indexes, saved := s.updater.IndexStatus(); indexes != nil {
		detail["indexes"] = indexes
		detail["index_saved_checks"] = saved
	}
	if s.options.Push != nil {
		detail["push_subscriptions"] = s.options.Push.Count()
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) statusAuthorized(r *http.Request) bool {
	if s.options.StatusToken == "" {
		return false
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(s.options.StatusToken)) == 1
}

func (s *Server) dataReady() bool {
	if s.ready != nil {
		select {
		case <-s.ready:
		default:
			return false
		}
	}
	return s.store.Count() > 0
}

func (s *Server) dbHealthy() bool {
	s.dbMu.RLock()
	db := s.options.DB
	s.dbMu.RUnlock()
	if db == nil {
		return !s.options.DBRequired
	}
	failed, _ := db.Status()
	return failed == ""
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	maxAge := s.options.HealthFetchMaxAge
	if maxAge <= 0 {
		maxAge = defaultHealthFetchMaxAge
	}
	checks := map[string]bool{
		"data":   s.dataReady(),
		"db":     s.dbHealthy(),
		"fetch":  s.options.Offline || s.updater.FetchHealthy(maxAge),
		"budget": s.options.Offline || s.updater.FetchAllowed(false),
	}
	ok := true
	for _, v := range checks {
		ok = ok && v
	}
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]any{"ok": ok, "checks": checks})
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
	if errors.Is(err, updater.ErrCacheOnly) {
		writeError(w, http.StatusServiceUnavailable, "serwer działa w trybie awaryjnym i chwilowo nie pobiera nowych planów; zapisane plany działają normalnie")
		return
	}
	if errors.Is(err, errRateLimited) {
		writeError(w, http.StatusTooManyRequests, "za dużo zapytań z tej sieci, spróbuj za kilka minut")
		return
	}
	if errors.Is(err, errNoBudget) || errors.Is(err, wat.ErrBudget) {
		writeError(w, http.StatusServiceUnavailable, "dzienny limit pobierań z WAT został wyczerpany, spróbuj później")
		return
	}
	if errors.Is(err, errNoPlanData) {
		writeError(w, http.StatusServiceUnavailable, "plan nie został jeszcze pobrany")
		return
	}
	if errors.Is(err, wat.ErrProxy) {
		msg := "serwer WATplan nie może połączyć się przez skonfigurowane proxy"
		if status, _ := s.updater.ProxyError(); status != "" {
			msg += " (" + status + ")"
		}
		writeError(w, http.StatusBadGateway, msg+". Pokazuję ostatni zapisany plan")
		return
	}
	if errors.Is(err, wat.ErrBlocked) {
		writeError(w, http.StatusServiceUnavailable,
			"WAT (Incapsula) blokuje pobieranie z serwera WATplan. Pokazuję ostatni zapisany plan")
		return
	}
	writeError(w, http.StatusBadGateway, "nie udało się pobrać planu ze strony WAT")
}

func (s *Server) reserveRefreshLocked(planID string) time.Duration {
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
	http.ResponseWriter
	gz       *gzip.Writer
	started  bool
	compress bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if !w.started {
		w.started = true
		if code != http.StatusNotModified && code != http.StatusNoContent {
			w.compress = true
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Del("Content-Length")
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	if !w.compress {
		return w.ResponseWriter.Write(b)
	}
	return w.gz.Write(b)
}

var gzipPool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

func withGzip(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(w)
		gzw := &gzipResponseWriter{ResponseWriter: w, gz: gz}
		defer func() {
			if gzw.compress {
				gz.Close()
			}
			gz.Reset(io.Discard)
			gzipPool.Put(gz)
		}()
		next(gzw, r)
	}
}

const (
	cacheHTML      = "public, max-age=0, must-revalidate"
	cacheAPI       = "private, no-store"
	cacheAPIWithET = "private, no-cache"
)

func htmlPage(page []byte) http.HandlerFunc {
	sum := sha1.Sum(page)
	etag := `"` + hex.EncodeToString(sum[:10]) + `"`
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheHTML)
		w.Header().Set("ETag", etag)
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	}
}

const permissionsPolicy = "accelerometer=(), autoplay=(), browsing-topics=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), magnetometer=(), microphone=(), midi=(), payment=(), publickey-credentials-get=(), serial=(), usb=(), xr-spatial-tracking=()"

func contentSecurityPolicy(scriptHashes []string) string {
	scripts := append([]string{"'self'"}, scriptHashes...)
	scripts = append(scripts, "https://cloud.umami.is")
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + strings.Join(scripts, " "),
		"style-src 'self'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self' https://cloud.umami.is https://gateway.umami.is",
		"manifest-src 'self'",
		"worker-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

func withSecurityHeaders(next http.Handler, csp string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", permissionsPolicy)
		h.Set("Content-Security-Policy", csp)
		h.Set("Strict-Transport-Security", "max-age=86400")
		next.ServeHTTP(w, r)
	})
}

func withAPIHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", cacheAPI)
		}
		next.ServeHTTP(w, r)
	})
}

func withCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sw.js" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", cacheHTML)
		} else if r.URL.Query().Has("v") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		next.ServeHTTP(w, r)
	})
}
