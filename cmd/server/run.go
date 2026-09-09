package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/weldnor/imagegen/internal/auth"
	"github.com/weldnor/imagegen/internal/config"
	"github.com/weldnor/imagegen/internal/db"
	"github.com/weldnor/imagegen/internal/gallery"
	"github.com/weldnor/imagegen/internal/httpapi"
	"github.com/weldnor/imagegen/internal/openrouter"
	"github.com/weldnor/imagegen/migrations"
)

// run loads configuration, connects to the database, applies migrations, wires
// the stores and HTTP handlers, and serves until interrupted. When migrateOnly
// is true it returns right after migrating.
func run(migrateOnly bool) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(rootCtx, pool, migrations.FS); err != nil {
		return err
	}
	log.Printf("migrations applied")
	if migrateOnly {
		log.Printf("-migrate-only: done")
		return nil
	}

	// Users: build the in-memory map and upsert a row per configured username.
	users, err := auth.NewUsers(cfg.Users)
	if err != nil {
		return err
	}
	if err := users.SyncToDB(rootCtx, pool); err != nil {
		return err
	}

	sessions := auth.NewSessionStore(pool, cfg.SessionTTL)
	go sessions.StartPurgeLoop(rootCtx, 10*time.Minute)

	authSvc := auth.NewService(users, sessions, auth.Options{
		CookieName:   cfg.SessionCookieName,
		CookieSecure: cfg.SessionCookieSecure,
		SessionTTL:   cfg.SessionTTL,
	})

	galleryStore := gallery.NewStore(pool, cfg.ImageStorageDir)

	orClient := openrouter.New(openrouter.Config{
		APIKey:  cfg.OpenRouterAPIKey,
		BaseURL: cfg.OpenRouterBaseURL,
		Timeout: cfg.OpenRouterTimeout,
	})

	apiHandlers := &httpapi.API{
		Gen:         orClient,
		Gallery:     galleryStore,
		Concurrency: cfg.OpenRouterConcurrency,
		MaxUpload:   cfg.MaxUploadBytes,
	}
	deps := httpapi.Deps{
		Auth:   authSvc,
		Static: httpapi.NewStaticHandler(cfg.StaticDir),
	}
	apiHandlers.Bind(&deps)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httpapi.NewRouter(deps),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-rootCtx.Done():
		log.Printf("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
