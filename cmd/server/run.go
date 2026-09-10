package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/fx"

	"github.com/weldnor/imagegen/internal/auth"
	"github.com/weldnor/imagegen/internal/config"
	"github.com/weldnor/imagegen/internal/db"
	"github.com/weldnor/imagegen/internal/gallery"
	"github.com/weldnor/imagegen/internal/httpapi"
	"github.com/weldnor/imagegen/internal/openrouter"
	"github.com/weldnor/imagegen/internal/telegrambot"
)

// run assembles the app from every internal/* module via uber-fx and serves
// (HTTP + the Telegram bot's polling loop) until interrupted.
func run() error {
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app := fx.New(
		config.Module,
		db.Module,
		auth.Module,
		gallery.Module,
		openrouter.Module,
		httpapi.Module,
		telegrambot.Module,
		fx.Invoke(registerHTTPServer),
	)

	if err := app.Start(rootCtx); err != nil {
		return err
	}

	<-rootCtx.Done()
	log.Printf("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return app.Stop(shutdownCtx)
}

// registerHTTPServer starts the HTTP server via fx.Lifecycle: it binds the
// listener synchronously in OnStart (so a bad LISTEN_ADDR fails startup fast)
// and serves in a goroutine; OnStop gracefully shuts it down.
func registerHTTPServer(lc fx.Lifecycle, cfg *config.Config, handler http.Handler) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return err
	}

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				log.Printf("listening on %s", cfg.ListenAddr)
				if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Printf("http server: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	})
	return nil
}
