package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/weldnor/imagegen/internal/config"
	"github.com/weldnor/imagegen/internal/db"
	"github.com/weldnor/imagegen/internal/dbtest"
	"github.com/weldnor/imagegen/migrations"
)

// Known password/hash pairs used across the auth tests.
const (
	aliceHash = "$2a$10$OC8R4yTGCzRvlH3ru9mkIeqKQm5tgfye83af3fzimrUQWBB3lwVgu" // "alice-password"
	alicePass = "alice-password"
	bobHash   = "$2a$10$JNBSnHo36Fu.rursUdwiGueD2PGsHFEm6SbPinrRgpxJPin24y.Rq" // "bob-password"
	bobPass   = "bob-password"
)

func TestNewUsers(t *testing.T) {
	cases := []struct {
		name    string
		creds   []config.UserCred
		wantErr string
	}{
		{"valid single", []config.UserCred{{Username: "alice", Hash: aliceHash}}, ""},
		{"valid multiple", []config.UserCred{{Username: "alice", Hash: aliceHash}, {Username: "bob", Hash: bobHash}}, ""},
		{"empty list", nil, "no users"},
		{"empty username", []config.UserCred{{Username: "  ", Hash: aliceHash}}, "empty username"},
		{"empty hash", []config.UserCred{{Username: "alice", Hash: ""}}, "empty password hash"},
		{"non-bcrypt hash", []config.UserCred{{Username: "alice", Hash: "not-a-hash"}}, "not a bcrypt hash"},
		{"duplicate username", []config.UserCred{{Username: "alice", Hash: aliceHash}, {Username: "alice", Hash: bobHash}}, "more than once"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewUsers(tc.creds)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	u, err := NewUsers([]config.UserCred{{Username: "alice", Hash: aliceHash}})
	if err != nil {
		t.Fatal(err)
	}
	u.byName["alice"].UserID = "uid-1"

	if id, ok := u.Verify("alice", alicePass); !ok || id != "uid-1" {
		t.Errorf("correct password: got (%q, %v), want (uid-1, true)", id, ok)
	}
	if _, ok := u.Verify("alice", "wrong"); ok {
		t.Error("wrong password accepted")
	}
	if _, ok := u.Verify("nobody", alicePass); ok {
		t.Error("unknown user accepted")
	}
}

func TestSyncToDBUpsertsWithoutDuplicates(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}

	u, err := NewUsers([]config.UserCred{{Username: "alice", Hash: aliceHash}, {Username: "bob", Hash: bobHash}})
	if err != nil {
		t.Fatal(err)
	}

	if err := u.SyncToDB(ctx, pool); err != nil {
		t.Fatalf("first SyncToDB: %v", err)
	}
	firstAlice := u.byName["alice"].UserID
	if firstAlice == "" || u.byName["bob"].UserID == "" {
		t.Fatal("SyncToDB did not populate UserID")
	}

	if err := u.SyncToDB(ctx, pool); err != nil {
		t.Fatalf("second SyncToDB: %v", err)
	}
	if u.byName["alice"].UserID != firstAlice {
		t.Errorf("alice's id changed on re-sync: %q -> %q", firstAlice, u.byName["alice"].UserID)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("users row count = %d, want 2", n)
	}
}
