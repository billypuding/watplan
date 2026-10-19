package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
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

	sources, err := config.LoadSources(sourcesFile)
	if err != nil {
		log.Fatal(err)
	}

	reg := config.NewRegistry(static)

	st, err := store.NewStore(envOr("PLANWAT_DATA", defaultDataDir))
	if err != nil {
		log.Fatal(err)
	}

	fetcher, err := wat.NewFetcher()
	if err != nil {
		log.Fatal(err)
	}

	upd := updater.NewUpdater(st, fetcher)

	handler, err := server.NewServer(reg, st, upd).Handler()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go upd.RunWithDiscovery(ctx, sources, static, reg, plansFile, refreshInterval)

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

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
