package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestServerSmoke boots the built binary against the test database and checks
// that it migrates, serves /healthz, and serves the frontend at / without a
// session. Covers task 7.9.
func TestServerSmoke(t *testing.T) {
	dbURL := os.Getenv("IMAGEN_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("IMAGEN_TEST_DATABASE_URL not set")
	}
	if testing.Short() {
		t.Skip("skips building the binary in -short mode")
	}

	// Reset the schema so migrations run from empty.
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	pool.Close()

	bin := t.TempDir() + "/server"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	port := freePort(t)
	storage := t.TempDir()

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"OPENROUTER_API_KEY=sk-or-smoke",
		"AUTH_USERS=alice:$2a$10$OC8R4yTGCzRvlH3ru9mkIeqKQm5tgfye83af3fzimrUQWBB3lwVgu",
		"DATABASE_URL="+dbURL,
		"IMAGE_STORAGE_DIR="+storage,
		"LISTEN_ADDR=127.0.0.1:"+port,
		"SESSION_COOKIE_SECURE=false",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()

	base := "http://127.0.0.1:" + port
	waitFor(t, base+"/healthz")

	// / serves the frontend without a session.
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("<title>Imagen</title>")) {
		t.Errorf("GET / did not return index.html")
	}

	// A protected endpoint is 401 without a session.
	resp, err = http.Get(base + "/api/images")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/images without session = %d, want 401", resp.StatusCode)
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

func waitFor(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("server did not become ready at %s", url)
}
