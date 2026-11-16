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
	"strings"
	"syscall"
	"time"

	"github.com/billypuding/watplan/internal/config"
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
	discoverFlag := flag.String("discover", "", "wyszukaj grupy wydziału (np. WIG) według sources.json, zapisz je w plans.json i zakończ")
	flag.Parse()

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
	if !offline && !onDemand {
		sources, err = config.LoadSources(sourcesFile)
		if err != nil {
			log.Fatal(err)
		}
	}

	reg := config.NewRegistry(static)

	st, err := store.NewStore(envOr("PLANWAT_DATA", defaultDataDir))
	if err != nil {
		log.Fatal(err)
	}

	var upd *updater.Updater
	if !offline || len(imports) > 0 {
		fetcher, err := wat.NewFetcher()
		if err != nil {
			log.Fatal(err)
		}
		upd = updater.NewUpdater(st, fetcher)
	}

	for _, spec := range imports {
		if err := importFile(upd, reg, spec); err != nil {
			log.Fatal(err)
		}
	}

	handler, err := server.NewServer(reg, st, upd, server.Options{
		Offline:  offline,
		OnDemand: onDemand,
	}).Handler()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !offline && !onDemand {
		go upd.RunWithDiscovery(ctx, sources, static, reg, plansFile, refreshInterval)
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
	var src *config.Source
	for i := range sources {
		if strings.EqualFold(sources[i].Faculty, faculty) {
			src = &sources[i]
		}
	}
	if src == nil {
		return fmt.Errorf("brak wydziału %q w %s", faculty, sourcesFile)
	}

	fetcher, err := wat.NewFetcher()
	if err != nil {
		return err
	}
	log.Printf("szukam grup %s na %s", src.Faculty, src.IndexURL)
	found, err := fetcher.DiscoverPlans(*src)
	if err != nil {
		return fmt.Errorf("wyszukiwanie grup %s: %w", src.Faculty, err)
	}
	if len(found) == 0 {
		return fmt.Errorf("nie znaleziono żadnych grup %s, %s bez zmian", src.Faculty, plansFile)
	}

	plans := config.ReplaceFaculty(static, src.Faculty, found)
	if err := config.SavePlans(plansFile, plans); err != nil {
		return err
	}
	log.Printf("zapisano %d grup %s w %s (wszystkich planów: %d)", len(found), src.Faculty, plansFile, len(plans))
	return nil
}
