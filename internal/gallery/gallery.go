// Package gallery persists generated images: metadata rows in PostgreSQL and
// image bytes on the filesystem, scoped per user.
package gallery

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weldnor/imagegen/internal/openrouter"
)

// ErrNotFound is returned when an image does not exist or is not owned by the
// requesting user.
var ErrNotFound = errors.New("image not found")

// Metadata is the descriptive information stored alongside an image.
type Metadata struct {
	Prompt         string
	Model          string
	ModelName      string
	ImageSize      string // stored as NULL when empty
	AspectRatio    string
	ReferenceCount int
	ContentType    string
}

// Image is a stored image's metadata row.
type Image struct {
	ID       string
	UserID   string
	FilePath string // relative to the store's base directory
	Created  time.Time
	Metadata
}

// Store persists image metadata in Postgres and bytes under BaseDir.
type Store struct {
	pool    *pgxpool.Pool
	baseDir string
}

// NewStore returns a Store writing bytes under baseDir.
func NewStore(pool *pgxpool.Pool, baseDir string) *Store {
	return &Store{pool: pool, baseDir: baseDir}
}

// AbsPath returns the absolute path to an image's bytes.
func (s *Store) AbsPath(img Image) string {
	return filepath.Join(s.baseDir, img.FilePath)
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

const selectColumns = `id, user_id, prompt, model, model_name,
	coalesce(image_size, ''), aspect_ratio, reference_count, content_type, file_path, created_at`

func scanImage(row pgx.Row) (Image, error) {
	var img Image
	err := row.Scan(&img.ID, &img.UserID, &img.Prompt, &img.Model, &img.ModelName,
		&img.ImageSize, &img.AspectRatio, &img.ReferenceCount, &img.ContentType,
		&img.FilePath, &img.Created)
	return img, err
}

// Save writes the image bytes to disk and then inserts the metadata row. If the
// insert fails, the freshly written file is removed so no orphan bytes remain.
func (s *Store) Save(ctx context.Context, userID string, data []byte, meta Metadata) (Image, error) {
	id, err := newUUID()
	if err != nil {
		return Image{}, err
	}
	ext := openrouter.ExtensionForContentType(meta.ContentType)
	relPath := filepath.Join(userID, id+"."+ext)
	absPath := filepath.Join(s.baseDir, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return Image{}, fmt.Errorf("creating user image directory: %w", err)
	}
	if err := writeFileAtomic(absPath, data); err != nil {
		return Image{}, fmt.Errorf("writing image bytes: %w", err)
	}

	var imageSize any
	if meta.ImageSize != "" {
		imageSize = meta.ImageSize
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO images
			(id, user_id, prompt, model, model_name, image_size, aspect_ratio,
			 reference_count, content_type, file_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING `+selectColumns,
		id, userID, meta.Prompt, meta.Model, meta.ModelName, imageSize, meta.AspectRatio,
		meta.ReferenceCount, meta.ContentType, relPath)

	img, err := scanImage(row)
	if err != nil {
		_ = os.Remove(absPath)
		removeDirIfEmpty(filepath.Dir(absPath))
		return Image{}, fmt.Errorf("inserting image row: %w", err)
	}
	return img, nil
}

// List returns the user's images, newest first.
func (s *Store) List(ctx context.Context, userID string) ([]Image, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+selectColumns+` FROM images WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Image{}
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, rows.Err()
}

// Get returns one image the user owns, or ErrNotFound.
func (s *Store) Get(ctx context.Context, userID, imageID string) (Image, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+selectColumns+` FROM images WHERE id = $1 AND user_id = $2`, imageID, userID)
	img, err := scanImage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	if err != nil {
		// An invalid uuid text also surfaces here; treat as not found.
		return Image{}, ErrNotFound
	}
	return img, nil
}

// Delete removes one image (row and bytes) the user owns, or returns ErrNotFound.
func (s *Store) Delete(ctx context.Context, userID, imageID string) error {
	row := s.pool.QueryRow(ctx,
		`DELETE FROM images WHERE id = $1 AND user_id = $2 RETURNING file_path`, imageID, userID)
	var relPath string
	if err := row.Scan(&relPath); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return ErrNotFound
	}
	abs := filepath.Join(s.baseDir, relPath)
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing image file: %w", err)
	}
	removeDirIfEmpty(filepath.Dir(abs))
	return nil
}

// ClearForUser removes every image the user owns (rows and bytes) and the
// user's image directory.
func (s *Store) ClearForUser(ctx context.Context, userID string) error {
	rows, err := s.pool.Query(ctx,
		`DELETE FROM images WHERE user_id = $1 RETURNING file_path`, userID)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		paths = append(paths, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range paths {
		if err := os.Remove(filepath.Join(s.baseDir, p)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing image file: %w", err)
		}
	}
	// Best effort: drop the now-empty user directory.
	_ = os.Remove(filepath.Join(s.baseDir, userID))
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func removeDirIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}
