package main

import (
	"context"
	"log"
	"os"

	"github.com/weldnor/imagegen/internal/config"
	"github.com/weldnor/imagegen/internal/db"
	"github.com/weldnor/imagegen/migrations"
)

// runMigrateOnly loads configuration, connects to the database, applies
// migrations, and exits. It does not build the fx app (no HTTP server or bot),
// so it works even when the bot/admin credentials are otherwise unreachable.
func runMigrateOnly() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}
	log.Printf("migrations applied")
	log.Printf("-migrate-only: done")
	return nil
}
