package httpapi

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMiddlewareWiring covers task 4.9: public paths work without a session,
// protected paths are 401 without a cookie and succeed with one.
func TestMiddlewareWiring(t *testing.T) {
	svc := newAuthService(t)
	okBody := []byte("protected-ok")
	router := NewRouter(Deps{
		Auth: svc,
		ListImages: func(w http.ResponseWriter, _ *http.Request) {
			w.Write(okBody)
		},
	})
	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	// Public: no session needed.
	for _, path := range []string{"/healthz", "/api/session"} {
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if path == "/healthz" && resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, resp.StatusCode)
		}
		if path == "/api/session" && resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s (no cookie) = %d, want 401", path, resp.StatusCode)
		}
	}

	// Protected without a cookie -> 401.
	resp, err := client.Get(srv.URL + "/api/images")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/images without cookie = %d, want 401", resp.StatusCode)
	}

	// Protected with a valid cookie -> handler runs.
	cookie := login(t, srv)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/images", nil)
	req.AddCookie(cookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(got) != "protected-ok" {
		t.Fatalf("GET /api/images with cookie = %d %q, want 200 protected-ok", resp.StatusCode, got)
	}
}

// TestLoggingOmitsSecrets covers task 4.8: the request log never contains a
// password or a full session-cookie value.
func TestLoggingOmitsSecrets(t *testing.T) {
	var logBuf bytes.Buffer
	oldOut := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
	}()

	svc := newAuthService(t)
	router := NewRouter(Deps{
		Auth:       svc,
		ListImages: func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) },
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	const secret = alicePass
	body := `{"username":"alice","password":"` + secret + `"}`
	resp, err := http.Post(srv.URL+"/api/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == cookieNm {
			cookie = c
		}
	}
	resp.Body.Close()
	if cookie == nil {
		t.Fatal("no cookie from login")
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/images", nil)
	req.AddCookie(cookie)
	resp, _ = srv.Client().Do(req)
	resp.Body.Close()

	logged := logBuf.String()
	if logged == "" {
		t.Fatal("nothing was logged; cannot verify redaction")
	}
	if strings.Contains(logged, secret) {
		t.Errorf("log contains the password:\n%s", logged)
	}
	if strings.Contains(logged, cookie.Value) {
		t.Errorf("log contains the session cookie value:\n%s", logged)
	}
	if !strings.Contains(logged, "/api/login") || !strings.Contains(logged, "/api/images") {
		t.Errorf("expected request lines missing from log:\n%s", logged)
	}
}
