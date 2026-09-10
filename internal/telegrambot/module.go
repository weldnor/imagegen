package telegrambot

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/weldnor/imagegen/internal/auth"
	"github.com/weldnor/imagegen/internal/config"
	"github.com/weldnor/imagegen/internal/gallery"
	"github.com/weldnor/imagegen/internal/openrouter"
)

// Module provides the Bot and starts/stops its long-polling loop alongside
// the HTTP server.
var Module = fx.Module("telegrambot",
	fx.Provide(provideBot),
	fx.Invoke(registerLifecycle),
)

func provideBot(cfg *config.Config, gen *openrouter.Client, gal *gallery.Store, users *auth.Users, bindings *auth.TelegramBindings) (*Bot, error) {
	return NewBot(Config{
		Token:         cfg.TelegramBotToken,
		AdminPassword: cfg.AdminPassword,
		AdminTTL:      cfg.SessionTTL,
		Concurrency:   cfg.OpenRouterConcurrency,
	}, gen, gal, users, bindings)
}

// pool is depended on only to guarantee ordering: the bot must not start
// polling before migrations have run.
func registerLifecycle(lc fx.Lifecycle, b *Bot, _ *pgxpool.Pool) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go b.Start(ctx)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}
