package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/billypuding/watplan/internal/alert"
	"github.com/billypuding/watplan/internal/backup"
	"github.com/billypuding/watplan/internal/cdn"
	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/durable"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/push"
	"github.com/billypuding/watplan/internal/server"
	"github.com/billypuding/watplan/internal/store"
	"github.com/billypuding/watplan/internal/updater"
	"github.com/billypuding/watplan/internal/wat"
)

const (
	defaultAddr     = "127.0.0.1:8080"
	defaultConfig   = "plans.json"
	defaultSources  = "sources.json"
	defaultDataDir  = "data"
	refreshInterval = time.Hour
)

func main() {
	offlineFlag := flag.Bool("offline", false, "nie kontaktuj się z WAT, serwuj tylko zapisane plany")
	onDemandFlag := flag.Bool("on-demand", false, "pobieraj plan z WAT dopiero przy pierwszym żądaniu, bez odświeżania wszystkich w tle")
	var imports []string
	flag.Func("import", "zaimportuj plan z pliku HTML w formacie ID=ścieżka (można powtórzyć)", func(value string) error {
		imports = append(imports, value)
		return nil
	})
	vapidFlag := flag.Bool("vapid-keys", false, "wygeneruj klucze VAPID do powiadomień push, wypisz je i zakończ")
	discoverFlag := flag.String("discover", "", "wyszukaj grupy wydziału (np. WIG) według sources.json, zapisz je w plans.json i zakończ")
	flag.Parse()

	if *vapidFlag {
		public, private, err := push.GenerateKeys()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("PLANWAT_VAPID_PUBLIC=%s\nPLANWAT_VAPID_PRIVATE=%s\n", public, private)
		return
	}

	offline := *offlineFlag || envBool("PLANWAT_OFFLINE")
	onDemand := !offline && (*onDemandFlag || envBool("PLANWAT_ON_DEMAND"))
	if offline {
		log.Printf("tryb offline: nie będę łączyć się z WAT, używam planów z katalogu danych")
	} else if onDemand {
		log.Printf("tryb on-demand: plany pobieram tylko przy pierwszym żądaniu, bez masowego odświeżania")
	}

	plansFile := envOr("PLANWAT_CONFIG", defaultConfig)
	sourcesFile := envOr("PLANWAT_SOURCES", defaultSources)

	static, err := config.LoadConfig(plansFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Fatal(err)
		}
		log.Printf("brak %s, startuję z pustą listą planów", plansFile)
		static = nil
	}

	if *discoverFlag != "" {
		if err := discoverFaculty(*discoverFlag, sourcesFile, plansFile, static); err != nil {
			log.Fatal(err)
		}
		return
	}

	var sources []config.Source
	if !offline {
		sources, err = config.LoadSources(sourcesFile)
		if err != nil {
			if !onDemand {
				log.Fatal(err)
			}
			log.Printf("brak listy wydziałów (%v), w tle pobiorę tylko brakujące plany", err)
		}
	}

	reg := config.NewRegistry(static)

	dataDir := envOr("PLANWAT_DATA", defaultDataDir)
	st, err := store.NewStore(dataDir)
	if err != nil {
		log.Fatal(err)
	}

	var alerts *alert.Notifier
	if hook := os.Getenv("PLANWAT_ALERT_WEBHOOK"); hook != "" {
		alerts = alert.New(hook)
		log.Printf("powiadomienia o awariach włączone")
	}

	var pushSvc *push.Service
	if public, private := os.Getenv("PLANWAT_VAPID_PUBLIC"), os.Getenv("PLANWAT_VAPID_PRIVATE"); public != "" && private != "" {
		pushFile := envOr("PLANWAT_PUSH_FILE", filepath.Join(dataDir, "push.json"))
		if err := push.ValidateKeys(public, private); err != nil {
			log.Printf("powiadomienia push WYŁĄCZONE: %v", err)
			alerts.Raise("push-keys", fmt.Sprintf("Powiadomienia push są wyłączone: %v. Sprawdź PLANWAT_VAPID_PUBLIC i PLANWAT_VAPID_PRIVATE (nowe klucze: watplan -vapid-keys).", err))
		} else {
			pushSvc, err = push.New(public, private, envOr("PLANWAT_VAPID_SUBJECT", "mailto:awalczak@duck.com"), pushFile)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("powiadomienia push włączone (%d subskrypcji, %s)", pushSvc.Count(), pushFile)
		}
	}

	publisher, cdnURL, bucket := setupCDN(alerts)
	if publisher != nil {
		st.SetPublisher(publisher.Publish)
	}

	var upd *updater.Updater
	if !offline || len(imports) > 0 {
		fetcher, err := wat.NewFetcher()
		if err != nil {
			log.Fatal(err)
		}
		fetcher.SetAlert(alerts)
		upd = updater.NewUpdater(st, fetcher)
		upd.SetAlert(alerts)
		upd.SetIndexSources(sources, envDuration("PLANWAT_INDEX_CHECK_EVERY"))
	}

	for _, spec := range imports {
		if err := importFile(upd, reg, spec); err != nil {
			log.Fatal(err)
		}
	}

	if upd != nil && pushSvc != nil {
		upd.OnChange(func(p config.PlanConfig, c model.Change) {
			pushSvc.Notify(p.ID, push.Describe(p.ID, p.Name, c))
		})
	}

	var backupRun func() (string, int, int, error)
	if name := os.Getenv("PLANWAT_BACKUP_BUCKET"); name != "" {
		if bucket == nil || os.Getenv("PLANWAT_DB_URL") == "" {
			log.Printf("PLANWAT_BACKUP_BUCKET bez R2 albo bez bazy: kopia wyłączona")
		} else {
			backupRun = backupNow(bucket, name, func() durable.Mirror {
				if q := dbMirror.Load(); q != nil {
					return q
				}
				return nil
			}, alerts)
		}
	}

	srvApp := server.NewServer(reg, st, upd, server.Options{
		Push:           pushSvc,
		Offline:        offline,
		OnDemand:       onDemand,
		CanonicalHost:  os.Getenv("PLANWAT_CANONICAL_HOST"),
		TrustProxy:     envBool("PLANWAT_TRUST_PROXY"),
		RefreshPerHour: envInt("PLANWAT_LIMIT_REFRESH_PER_HOUR"),
		ChecksPerHour:  envInt("PLANWAT_LIMIT_CHECKS_PER_HOUR"),
		PushPerHour:    envInt("PLANWAT_LIMIT_PUSH_PER_HOUR"),

		DBRequired:        os.Getenv("PLANWAT_DB_URL") != "",
		HealthFetchMaxAge: envDuration("PLANWAT_HEALTH_FETCH_MAX_AGE"),

		StatusToken: os.Getenv("PLANWAT_STATUS_TOKEN"),

		CDNURL:    cdnURL,
		CDNStatus: cdnStatus(publisher),

		Now:    testClock(),
		Backup: backupRun,
	})
	handler, err := srvApp.Handler()
	if err != nil {
		log.Fatal(err)
	}
	dbURL := os.Getenv("PLANWAT_DB_URL")
	if dbURL != "" {
		upd.SetCacheOnly(true)
	}
	connectDB := func() error {
		turso, err := durable.OpenTurso(dbURL, os.Getenv("PLANWAT_DB_TOKEN"))
		if err != nil {
			return err
		}
		started := time.Now()
		restored, uploaded, err := st.Restore(turso)
		if err != nil {
			return fmt.Errorf("odtwarzanie planów: %w", err)
		}
		db := durable.NewQueue(turso)
		db.SetAlert(alerts)
		dbMirror.Store(db)
		st.SetMirror(db)
		srvApp.SetDB(db)
		log.Printf("baza: odtworzono %d planów, do wysłania %d, %v", restored, uploaded, time.Since(started).Round(time.Millisecond))
		if pushSvc != nil {
			if added, err := pushSvc.Restore(db); err != nil {
				log.Printf("odtwarzanie subskrypcji z bazy: %v", err)
			} else if added > 0 {
				log.Printf("baza: odtworzono %d subskrypcji", added)
			}
		}
		return nil
	}
	restore := func() {
		if dbURL == "" {
			return
		}
		attempt := 0
		err := durable.Retry(context.Background(), dbStartDelays, func() error {
			attempt++
			err := connectDB()
			if err != nil {
				log.Printf("baza: próba %d/%d nieudana: %v", attempt, len(dbStartDelays)+1, err)
			}
			return err
		})
		if err == nil {
			upd.SetCacheOnly(false)
			return
		}
		log.Printf("baza niedostępna (%v): działam tylko na zapisanych planach, ponawiam co %v", err, dbRetryEvery)
		alerts.Raise("db-start", fmt.Sprintf("Serwer wystartował bez bazy Turso (%v). Działa tylko na zapisanych planach i nie pobiera nowych. Ponawiam połączenie co %v.", err, dbRetryEvery))
		go func() {
			for {
				time.Sleep(dbRetryEvery)
				if err := connectDB(); err != nil {
					log.Printf("baza: ponowna próba nieudana: %v", err)
					continue
				}
				upd.SetCacheOnly(false)
				log.Printf("baza wróciła, koniec trybu tylko z zapisanymi planami")
				alerts.Resolved("db-start", "Baza Turso wróciła, serwer znów pobiera nowe plany.")
				return
			}
		}()
	}
	ready := make(chan struct{})
	srvApp.SetReady(ready)
	go func() {
		defer close(ready)
		restore()
		if usage := upd.Usage(); usage != nil {
			if data, err := st.LoadMeta("usage"); err == nil {
				if err := usage.Load(data); err != nil {
					log.Printf("licznik zużycia: %v", err)
				}
			}
			usage.SetPersist(func(data []byte) {
				if err := st.SaveMeta("usage", data); err != nil {
					log.Printf("zapis licznika zużycia: %v", err)
				}
			})
		}
		if upd != nil {
			data, _ := st.LoadMeta("indexes")
			if err := upd.LoadIndexState(data, func(data []byte) {
				if err := st.SaveMeta("indexes", data); err != nil {
					log.Printf("zapis dat wydziałów: %v", err)
				}
			}); err != nil {
				log.Printf("daty wydziałów: %v", err)
			}
		}
	}()

	go func() {
		<-ready
		srvApp.Warm()
	}()

	if publisher != nil {
		go func() {
			<-ready
			queued, current, err := publisher.Backfill(st.PlanIDs(), st.Raw)
			if err != nil {
				log.Printf("CDN: uzupełnianie przerwane: %v", err)
				alerts.Raise("cdn-backfill", fmt.Sprintf("Nie udało się sprawdzić planów w R2 (%v). Sprawdź PLANWAT_R2_*.", err))
				return
			}
			log.Printf("CDN: do wysłania %d planów, aktualnych %d", queued, current)
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if backupRun != nil {
		go runBackups(ctx, os.Getenv("PLANWAT_BACKUP_BUCKET"), backupRun)
	}

	if !offline {
		go upd.WatchCredits(ctx)
		go func() {
			<-ready
			upd.RepairMisparsed(ctx, reg, 0)
		}()
	}

	if !offline && !onDemand {
		go func() {
			<-ready
			upd.RunWithDiscovery(ctx, sources, static, reg, plansFile, refreshInterval)
		}()
	}
	if onDemand && !envBool("PLANWAT_NO_BACKGROUND") {
		opts := updater.MaintainOptions{}
		if pushSvc != nil {
			opts.Watched = pushSvc.Watched
		}
		go func() {
			<-ready
			upd.Maintain(ctx, sources, reg, opts)
		}()
	}

	srv := &http.Server{
		Addr:              envOr("PLANWAT_ADDR", defaultAddr),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("serwer działa pod adresem http://%s", srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func importFile(upd *updater.Updater, reg *config.Registry, spec string) error {
	id, path, ok := strings.Cut(spec, "=")
	if !ok || strings.TrimSpace(id) == "" || strings.TrimSpace(path) == "" {
		return fmt.Errorf("zły format -import %q, oczekuję ID=ścieżka", spec)
	}

	plan, ok := reg.Get(strings.TrimSpace(id))
	if !ok {
		return fmt.Errorf("plan %q nie występuje w %s", id, envOr("PLANWAT_CONFIG", defaultConfig))
	}

	f, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		return fmt.Errorf("otwarcie %s: %w", path, err)
	}
	defer f.Close()

	n, err := upd.Import(plan, f)
	if err != nil {
		return err
	}
	log.Printf("zaimportowano plan %s: %d zajęć", plan.ID, n)
	return nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

var dbMirror atomic.Pointer[durable.Queue]

var dbStartDelays = []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second}

const dbRetryEvery = 5 * time.Minute

func envDuration(key string) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		log.Fatalf("zły %s %q (np. 3h, 90m)", key, v)
	}
	return d
}

func envInt(key string) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("zły %s %q (liczba całkowita, -1 = bez limitu)", key, v)
	}
	return n
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "tak", "on":
		return true
	}
	return false
}

func discoverFaculty(faculty, sourcesFile, plansFile string, static []config.PlanConfig) error {
	sources, err := config.LoadSources(sourcesFile)
	if err != nil {
		return err
	}
	var matched []config.Source
	for _, src := range sources {
		if strings.EqualFold(src.Faculty, faculty) {
			matched = append(matched, src)
		}
	}
	if len(matched) == 0 {
		return fmt.Errorf("brak wydziału %q w %s", faculty, sourcesFile)
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Semester == "" && matched[j].Semester != "" })

	fetcher, err := wat.NewFetcher()
	if err != nil {
		return err
	}
	plans := static
	for _, src := range matched {
		log.Printf("szukam grup %s %s na %s", src.Faculty, src.Semester, src.IndexURL)
		found, err := fetcher.DiscoverPlans(src)
		if src.Optional && errors.Is(err, wat.ErrGone) {
			log.Printf("%s %s: jeszcze nie opublikowano", src.Faculty, src.Semester)
			continue
		}
		if err == nil && src.Semester != "" && len(found) > 0 {
			if cerr := fetcher.CheckCurrent(src, found); errors.Is(cerr, wat.ErrStaleSource) {
				log.Printf("%s %s: na stronie są jeszcze plany z poprzedniego roku, pomijam", src.Faculty, src.Semester)
				continue
			} else if cerr != nil {
				return fmt.Errorf("sprawdzanie planów %s %s: %w", src.Faculty, src.Semester, cerr)
			}
		}
		if err != nil {
			return fmt.Errorf("wyszukiwanie grup %s: %w", src.Faculty, err)
		}
		if len(found) == 0 {
			return fmt.Errorf("nie znaleziono żadnych grup %s, %s bez zmian", src.Faculty, plansFile)
		}
		if src.Semester == "" {
			extras := map[string][]config.PlanSource{}
			for _, p := range plans {
				if len(p.Extra) > 0 {
					extras[p.ID] = p.Extra
				}
			}
			for i := range found {
				found[i].Extra = extras[found[i].ID]
			}
			plans = config.ReplaceFaculty(plans, src.Faculty, found)
			log.Printf("%d grup %s", len(found), src.Faculty)
			continue
		}
		var n int
		plans, n = config.MergeExtra(plans, found)
		log.Printf("%s %s: %d grup, %d nowych grup lub źródeł", src.Faculty, src.Semester, len(found), n)
	}

	if err := config.SavePlans(plansFile, plans); err != nil {
		return err
	}
	log.Printf("zapisano %s (wszystkich planów: %d)", plansFile, len(plans))
	return nil
}

func setupCDN(alerts *alert.Notifier) (*cdn.Publisher, string, *cdn.Bucket) {
	endpoint := os.Getenv("PLANWAT_R2_ENDPOINT")
	cdnURL := strings.TrimRight(os.Getenv("PLANWAT_CDN_URL"), "/")
	if endpoint == "" {
		if cdnURL != "" {
			log.Printf("PLANWAT_CDN_URL bez PLANWAT_R2_*: CDN wyłączony")
		}
		return nil, "", nil
	}
	bucket, err := cdn.New(endpoint, os.Getenv("PLANWAT_R2_BUCKET"), os.Getenv("PLANWAT_R2_ACCESS_KEY_ID"), os.Getenv("PLANWAT_R2_SECRET_ACCESS_KEY"), os.Getenv("PLANWAT_R2_REGION"))
	if err == nil && !strings.HasPrefix(cdnURL, "https://") && !strings.HasPrefix(cdnURL, "http://127.0.0.1:") && !strings.HasPrefix(cdnURL, "http://localhost:") {
		err = fmt.Errorf("PLANWAT_CDN_URL musi być adresem https://, jest %q", cdnURL)
	}
	if err != nil {
		log.Printf("CDN WYŁĄCZONY: %v", err)
		alerts.Raise("cdn-config", fmt.Sprintf("CDN jest wyłączony: %v", err))
		return nil, "", nil
	}
	log.Printf("CDN włączony: plany idą do R2 i są czytane z %s", cdnURL)
	return cdn.NewPublisher(bucket, alerts), cdnURL, bucket
}

const (
	backupFirstAfter = 15 * time.Minute
	backupEvery      = 24 * time.Hour
)

func backupNow(bucket *cdn.Bucket, name string, db func() durable.Mirror, alerts *alert.Notifier) func() (string, int, int, error) {
	dst := bucket.WithBucket(name)
	return func() (string, int, int, error) {
		src := db()
		if src == nil {
			return "", 0, 0, errors.New("baza niepodłączona")
		}
		key, keys, size, err := backup.Run(src, dst, time.Now())
		if err != nil {
			log.Printf("kopia bazy: %v", err)
			alerts.Raise("backup", fmt.Sprintf("Kopia bazy do R2 (%s) nie powiodła się: %v", name, err))
			return "", 0, 0, err
		}
		log.Printf("kopia bazy: %s, %d kluczy, %d KB", key, keys, size/1024)
		alerts.Resolved("backup", "Kopia bazy do R2 znów działa.")
		return key, keys, size, nil
	}
}

func runBackups(ctx context.Context, name string, run func() (string, int, int, error)) {
	log.Printf("kopia bazy: codziennie do R2 %s", name)
	timer := time.NewTimer(backupFirstAfter)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(backupEvery)
		run()
	}
}

func cdnStatus(p *cdn.Publisher) func() (string, int) {
	if p == nil {
		return nil
	}
	return p.Status
}

func testClock() func() time.Time {
	v := os.Getenv("PLANWAT_TEST_NOW")
	if v == "" {
		return nil
	}
	at, err := time.Parse(time.RFC3339, v)
	if err != nil {
		log.Fatalf("zły PLANWAT_TEST_NOW %q (RFC3339, np. 2026-11-19T10:15:00+01:00)", v)
	}
	log.Printf("PLANWAT_TEST_NOW: strony grup renderuję na %s", at.Format(time.RFC3339))
	start := time.Now()
	return func() time.Time { return at.Add(time.Since(start)) }
}
