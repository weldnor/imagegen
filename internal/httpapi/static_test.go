package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStaticServesFrontendWithoutSession covers task 7.8: the login form (and
// its assets) must load with no session cookie.
func TestStaticServesFrontendWithoutSession(t *testing.T) {
	env := newTestEnv(t, &fakeGen{fn: okImage})

	cases := []struct {
		path        string
		wantSnippet string
	}{
		{"/", "<title>Imagen</title>"},
		{"/index.html", "promptInput"},
		{"/src/app.js", "MODEL_CONFIGS"},
		{"/src/styles.css", ":root"},
		{"/favicon.svg", "<svg"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(env.srv.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s (no cookie) = %d, want 200", tc.path, resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), tc.wantSnippet) {
				t.Errorf("GET %s body missing %q", tc.path, tc.wantSnippet)
			}
		})
	}
}

// TestStaticFromDiskWhenStaticDirSet covers the STATIC_DIR escape hatch.
func TestStaticFromDiskWhenStaticDirSet(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("DISK INDEX"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewStaticHandler(dir)

	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK || !strings.Contains(rw.Body.String(), "DISK INDEX") {
		t.Fatalf("disk static = %d %q", rw.Code, rw.Body.String())
	}
}
