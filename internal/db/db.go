// Package db opens the pgx connection pool and runs embedded SQL migrations.
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgx connection pool for url and verifies it with a ping,
// bounded by a short timeout so an unreachable database fails fast with a clear
// error rather than hanging.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL is not a valid connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connecting to the database: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database is unreachable (check DATABASE_URL): %w", err)
	}
	return pool, nil
}

// migration is one embedded SQL file.
type migration struct {
	version string // the file name, e.g. "0001_init.sql"
	sql     string
}

// loadMigrations reads and lexically orders every *.sql file at the root of
// fsys.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: e.Name(), sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	if len(out) == 0 {
		return nil, errors.New("no migration files found")
	}
	return out, nil
}

// Migrate applies every not-yet-applied migration from fsys in order, each in
// its own transaction, and records it in schema_migrations. Running it again
// against the same database is a no-op.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) error {
	migs, err := loadMigrations(fsys)
	if err != nil {
		return err
	}

	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	applied := map[string]bool{}
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("reading schema_migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		if err := applyOne(ctx, pool, m); err != nil {
			return fmt.Errorf("applying migration %s: %w", m.version, err)
		}
	}
	return nil
}

func applyOne(ctx context.Context, pool *pgxpool.Pool, m migration) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful commit

	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, m.version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ErrNoRows is re-exported so callers do not need to import pgx directly just to
// check for a missing row.
var ErrNoRows = pgx.ErrNoRows
