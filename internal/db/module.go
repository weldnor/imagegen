package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/weldnor/imagegen/internal/config"
	"github.com/weldnor/imagegen/migrations"
)

// Module provides a connected, migrated *pgxpool.Pool, closed automatically on
// shutdown.
var Module = fx.Module("db",
	fx.Provide(providePool),
)

func providePool(lc fx.Lifecycle, cfg *config.Config) (*pgxpool.Pool, error) {
	pool, err := Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := Migrate(context.Background(), pool, migrations.FS); err != nil {
		pool.Close()
		return nil, err
	}
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			pool.Close()
			return nil
		},
	})
	return pool, nil
}
