package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weldnor/imagegen/internal/db"
	"github.com/weldnor/imagegen/internal/dbtest"
	"github.com/weldnor/imagegen/migrations"
)

// Known passwords used across the auth tests.
const (
	alicePass = "alice-password"
	bobPass   = "bob-password"
)

// newTestUsers returns a migrated pool and a Users store over it.
func newTestUsers(t *testing.T) (*Users, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	return NewUsers(pool), pool
}

func TestVerifyPassword(t *testing.T) {
	u, _ := newTestUsers(t)
	ctx := context.Background()

	id, err := u.CreateUser(ctx, "alice", alicePass, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if got, ok, err := u.VerifyPassword(ctx, "alice", alicePass); err != nil || !ok || got != id {
		t.Errorf("correct password: got (%q, %v, %v), want (%q, true, nil)", got, ok, err, id)
	}
	if _, ok, err := u.VerifyPassword(ctx, "alice", "wrong"); err != nil || ok {
		t.Error("wrong password accepted")
	}
	if _, ok, err := u.VerifyPassword(ctx, "nobody", alicePass); err != nil || ok {
		t.Error("unknown user accepted")
	}
}

func TestLookupByTelegramID(t *testing.T) {
	u, _ := newTestUsers(t)
	ctx := context.Background()

	id, err := u.CreateUser(ctx, "alice", alicePass, []int64{111})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	gotID, gotName, ok, err := u.LookupByTelegramID(ctx, 111)
	if err != nil || !ok || gotID != id || gotName != "alice" {
		t.Errorf("LookupByTelegramID(111) = (%q, %q, %v, %v)", gotID, gotName, ok, err)
	}
	if _, _, ok, err := u.LookupByTelegramID(ctx, 999); err != nil || ok {
		t.Errorf("LookupByTelegramID(999) should not be found: ok=%v err=%v", ok, err)
	}
}

func TestCreateUserRejectsDuplicateUsername(t *testing.T) {
	u, _ := newTestUsers(t)
	ctx := context.Background()

	if _, err := u.CreateUser(ctx, "alice", alicePass, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := u.CreateUser(ctx, "alice", bobPass, nil); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("err = %v, want ErrUsernameTaken", err)
	}
}

func TestCreateUserRejectsLinkedTelegramID(t *testing.T) {
	u, _ := newTestUsers(t)
	ctx := context.Background()

	if _, err := u.CreateUser(ctx, "alice", alicePass, []int64{111}); err != nil {
		t.Fatal(err)
	}
	if _, err := u.CreateUser(ctx, "bob", bobPass, []int64{111}); !errors.Is(err, ErrTelegramIDLinked) {
		t.Fatalf("err = %v, want ErrTelegramIDLinked", err)
	}
	// Nothing was created for "bob".
	if _, _, ok, err := u.LookupByTelegramID(ctx, 111); err != nil || !ok {
		t.Fatal("expected the original link to still point at alice")
	}
	if _, ok, err := u.VerifyPassword(ctx, "bob", bobPass); err != nil || ok {
		t.Error("bob should not have been created")
	}
}

func TestAddTelegramID(t *testing.T) {
	u, _ := newTestUsers(t)
	ctx := context.Background()

	if _, err := u.CreateUser(ctx, "alice", alicePass, nil); err != nil {
		t.Fatal(err)
	}

	if err := u.AddTelegramID(ctx, "alice", 222); err != nil {
		t.Fatalf("AddTelegramID: %v", err)
	}
	if _, _, ok, _ := u.LookupByTelegramID(ctx, 222); !ok {
		t.Fatal("telegram id not linked")
	}

	if err := u.AddTelegramID(ctx, "nobody", 333); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
	if err := u.AddTelegramID(ctx, "alice", 222); !errors.Is(err, ErrTelegramIDLinked) {
		t.Fatalf("err = %v, want ErrTelegramIDLinked", err)
	}
}

func TestListUsers(t *testing.T) {
	u, _ := newTestUsers(t)
	ctx := context.Background()

	if _, err := u.CreateUser(ctx, "alice", alicePass, []int64{1, 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := u.CreateUser(ctx, "bob", bobPass, nil); err != nil {
		t.Fatal(err)
	}

	got, err := u.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ListUsers returned %d users, want 2", len(got))
	}
	if got[0].Username != "alice" || len(got[0].TelegramIDs) != 2 {
		t.Errorf("alice summary = %+v", got[0])
	}
	if got[1].Username != "bob" || len(got[1].TelegramIDs) != 0 {
		t.Errorf("bob summary = %+v", got[1])
	}
}
