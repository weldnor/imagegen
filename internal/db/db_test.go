package db_test

import (
	"context"
	"testing"

	"github.com/weldnor/imagegen/internal/db"
	"github.com/weldnor/imagegen/internal/dbtest"
	"github.com/weldnor/imagegen/migrations"
)

func TestConnectUnreachable(t *testing.T) {
	// Port 1 is never a Postgres; Connect must fail fast with a clear message.
	_, err := db.Connect(context.Background(),
		"postgres://nobody:nobody@127.0.0.1:1/nodb?sslmode=disable&connect_timeout=2")
	if err == nil {
		t.Fatal("Connect succeeded against an unreachable database")
	}
	t.Logf("got expected error: %v", err)
}

func TestConnectBadURL(t *testing.T) {
	_, err := db.Connect(context.Background(), "://not a url")
	if err == nil {
		t.Fatal("Connect succeeded on a malformed DATABASE_URL")
	}
}

func TestMigrateAppliesAndIsIdempotent(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}

	// Schema objects from 0001_init exist.
	for _, q := range []string{
		`SELECT 1 FROM users LIMIT 0`,
		`SELECT 1 FROM sessions LIMIT 0`,
		`SELECT 1 FROM images LIMIT 0`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("expected table to exist for %q: %v", q, err)
		}
	}
	for _, idx := range []string{"sessions_expires_idx", "images_user_created_idx"} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_indexes WHERE indexname = $1`, idx).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("index %s: found %d, want 1", idx, n)
		}
	}

	// Second run is a no-op and does not error.
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("schema_migrations has %d rows after two runs, want 1", count)
	}
}
