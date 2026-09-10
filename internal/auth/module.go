package auth

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/weldnor/imagegen/internal/config"
)

// Module provides the DB-backed user store, session store, temporary Telegram
// binding store, and the web auth Service, and starts the session-purge loop.
var Module = fx.Module("auth",
	fx.Provide(
		NewUsers,
		provideSessions,
		provideTelegramBindings,
		provideService,
	),
	fx.Invoke(startPurgeLoop),
)

func provideSessions(pool *pgxpool.Pool, cfg *config.Config) *SessionStore {
	return NewSessionStore(pool, cfg.SessionTTL)
}

func provideTelegramBindings(pool *pgxpool.Pool, cfg *config.Config) *TelegramBindings {
	return NewTelegramBindings(pool, cfg.SessionTTL)
}

func provideService(users *Users, sessions *SessionStore, cfg *config.Config) *Service {
	return NewService(users, sessions, Options{
		CookieName:   cfg.SessionCookieName,
		CookieSecure: cfg.SessionCookieSecure,
		SessionTTL:   cfg.SessionTTL,
	})
}

func startPurgeLoop(lc fx.Lifecycle, sessions *SessionStore) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go sessions.StartPurgeLoop(ctx, 10*time.Minute)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}
