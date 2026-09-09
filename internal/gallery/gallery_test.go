package gallery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weldnor/imagegen/internal/db"
	"github.com/weldnor/imagegen/internal/dbtest"
	"github.com/weldnor/imagegen/migrations"
)

// newStore migrates a fresh DB, creates two users, and returns a Store rooted
// at a temp dir plus the two user ids.
func newStore(t *testing.T) (*Store, *pgxpool.Pool, string, string) {
	t.Helper()
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var a, b string
	if err := pool.QueryRow(ctx, `INSERT INTO users (username) VALUES ('a') RETURNING id`).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (username) VALUES ('b') RETURNING id`).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return NewStore(pool, t.TempDir()), pool, a, b
}

func sampleMeta() Metadata {
	return Metadata{
		Prompt: "a cat", Model: "google/gemini-2.5-flash-image", ModelName: "Gemini 2.5 Flash Image",
		ImageSize: "1K", AspectRatio: "1:1", ReferenceCount: 2, ContentType: "image/png",
	}
}

func TestSaveWritesFileAndRow(t *testing.T) {
	s, _, userA, _ := newStore(t)
	ctx := context.Background()

	img, err := s.Save(ctx, userA, []byte("PNGBYTES"), sampleMeta())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if img.ID == "" || img.UserID != userA {
		t.Fatalf("bad image row: %+v", img)
	}
	if img.FilePath != filepath.Join(userA, img.ID+".png") {
		t.Errorf("FilePath = %q", img.FilePath)
	}
	got, err := os.ReadFile(s.AbsPath(img))
	if err != nil || string(got) != "PNGBYTES" {
		t.Fatalf("file round-trip failed: %q err=%v", got, err)
	}
	if img.ImageSize != "1K" || img.ReferenceCount != 2 {
		t.Errorf("metadata not persisted: %+v", img)
	}
}

func TestSaveNullImageSize(t *testing.T) {
	s, pool, userA, _ := newStore(t)
	ctx := context.Background()
	m := sampleMeta()
	m.ImageSize = ""
	img, err := s.Save(ctx, userA, []byte("x"), m)
	if err != nil {
		t.Fatal(err)
	}
	var isNull bool
	pool.QueryRow(ctx, `SELECT image_size IS NULL FROM images WHERE id = $1`, img.ID).Scan(&isNull)
	if !isNull {
		t.Error("empty ImageSize should be stored as NULL")
	}
	if img.ImageSize != "" {
		t.Errorf("ImageSize = %q, want empty", img.ImageSize)
	}
}

func TestSaveRollsBackFileOnInsertError(t *testing.T) {
	s, _, _, _ := newStore(t)
	ctx := context.Background()

	// A user id with no matching users row violates the FK, so the insert fails.
	_, err := s.Save(ctx, "00000000-0000-4000-8000-000000000000", []byte("orphan?"), sampleMeta())
	if err == nil {
		t.Fatal("Save succeeded despite an FK violation")
	}

	// No file should remain anywhere under the base dir.
	var found []string
	filepath.WalkDir(s.baseDir, func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			found = append(found, p)
		}
		return nil
	})
	if len(found) != 0 {
		t.Fatalf("orphan file(s) left behind: %v", found)
	}
}

func TestListNewestFirstAndScoped(t *testing.T) {
	s, _, userA, userB := newStore(t)
	ctx := context.Background()

	a1, _ := s.Save(ctx, userA, []byte("1"), sampleMeta())
	a2, _ := s.Save(ctx, userA, []byte("2"), sampleMeta())
	_, _ = s.Save(ctx, userB, []byte("3"), sampleMeta())

	list, err := s.List(ctx, userA)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("user A sees %d images, want 2 (leak from user B?)", len(list))
	}
	if list[0].ID != a2.ID || list[1].ID != a1.ID {
		t.Errorf("not newest-first: got %s then %s", list[0].ID, list[1].ID)
	}

	empty, err := s.List(ctx, "00000000-0000-4000-8000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("expected empty list, got %d", len(empty))
	}
}

func TestGetCrossUserIsolation(t *testing.T) {
	s, _, userA, userB := newStore(t)
	ctx := context.Background()
	img, _ := s.Save(ctx, userA, []byte("secret"), sampleMeta())

	if _, err := s.Get(ctx, userA, img.ID); err != nil {
		t.Fatalf("owner Get failed: %v", err)
	}
	if _, err := s.Get(ctx, userB, img.ID); err != ErrNotFound {
		t.Fatalf("cross-user Get err = %v, want ErrNotFound", err)
	}
	if _, err := s.Get(ctx, userA, "not-a-real-id"); err != ErrNotFound {
		t.Fatalf("bogus id Get err = %v, want ErrNotFound", err)
	}
}

func TestDeleteScopedRemovesRowAndFile(t *testing.T) {
	s, _, userA, userB := newStore(t)
	ctx := context.Background()
	img, _ := s.Save(ctx, userA, []byte("bytes"), sampleMeta())
	abs := s.AbsPath(img)

	// User B cannot delete user A's image.
	if err := s.Delete(ctx, userB, img.ID); err != ErrNotFound {
		t.Fatalf("cross-user Delete err = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatal("file removed by a non-owner delete")
	}

	// Owner delete removes both row and file.
	if err := s.Delete(ctx, userA, img.ID); err != nil {
		t.Fatalf("owner Delete: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Error("file still present after delete")
	}
	if _, err := s.Get(ctx, userA, img.ID); err != ErrNotFound {
		t.Error("row still present after delete")
	}

	// Deleting again is ErrNotFound, not a crash.
	if err := s.Delete(ctx, userA, img.ID); err != ErrNotFound {
		t.Errorf("second Delete err = %v, want ErrNotFound", err)
	}
}

func TestDeleteToleratesMissingFile(t *testing.T) {
	s, _, userA, _ := newStore(t)
	ctx := context.Background()
	img, _ := s.Save(ctx, userA, []byte("bytes"), sampleMeta())
	if err := os.Remove(s.AbsPath(img)); err != nil {
		t.Fatal(err)
	}
	// Row still there; Delete should succeed and clean up the row anyway.
	if err := s.Delete(ctx, userA, img.ID); err != nil {
		t.Fatalf("Delete with missing file: %v", err)
	}
}

func TestClearForUser(t *testing.T) {
	s, _, userA, userB := newStore(t)
	ctx := context.Background()
	ia1, _ := s.Save(ctx, userA, []byte("1"), sampleMeta())
	ia2, _ := s.Save(ctx, userA, []byte("2"), sampleMeta())
	ib, _ := s.Save(ctx, userB, []byte("3"), sampleMeta())

	if err := s.ClearForUser(ctx, userA); err != nil {
		t.Fatalf("ClearForUser: %v", err)
	}

	for _, img := range []Image{ia1, ia2} {
		if _, err := os.Stat(s.AbsPath(img)); !os.IsNotExist(err) {
			t.Errorf("file %s survived clear", img.FilePath)
		}
	}
	if _, err := os.Stat(filepath.Join(s.baseDir, userA)); !os.IsNotExist(err) {
		t.Error("user A's image directory was not removed")
	}
	list, _ := s.List(ctx, userA)
	if len(list) != 0 {
		t.Errorf("user A still has %d rows after clear", len(list))
	}

	// User B is untouched.
	if bl, _ := s.List(ctx, userB); len(bl) != 1 {
		t.Errorf("user B has %d rows, want 1", len(bl))
	}
	if _, err := os.Stat(s.AbsPath(ib)); err != nil {
		t.Error("user B's file was removed by user A's clear")
	}
}
