// Package dbtest provides a throwaway PostgreSQL pool for integration tests.
//
// Tests that need a database call Pool(t); it skips the test when
// IMAGEN_TEST_DATABASE_URL is not set, so `go test ./...` still passes on a
// machine without Postgres. When the variable is set (CI, local Docker), it
// drops and recreates the public schema so every test starts from empty.
package dbtest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// URLEnv is the environment variable holding the test database connection string.
const URLEnv = "IMAGEN_TEST_DATABASE_URL"

// Pool connects to the test database, resets its schema, and returns a pool that
// is closed automatically when the test ends. It calls t.Skip if URLEnv is unset.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv(URLEnv)
	if url == "" {
		t.Skipf("%s not set; skipping database integration test", URLEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connecting to %s: %v", URLEnv, err)
	}
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		pool.Close()
		t.Fatalf("resetting schema: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
