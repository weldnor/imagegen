package gallery

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/weldnor/imagegen/internal/config"
)

// Module provides the gallery *Store.
var Module = fx.Module("gallery",
	fx.Provide(provideStore),
)

func provideStore(pool *pgxpool.Pool, cfg *config.Config) *Store {
	return NewStore(pool, cfg.ImageStorageDir)
}
