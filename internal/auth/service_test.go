package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weldnor/imagegen/internal/config"
	"github.com/weldnor/imagegen/internal/db"
	"github.com/weldnor/imagegen/internal/dbtest"
	"github.com/weldnor/imagegen/migrations"
)

const testCookieName = "imagen_session"

// setup returns a ready Service plus the alice/bob user ids.
func setup(t *testing.T, ttl time.Duration) (*Service, *pgxpool.Pool, map[string]string) {
	t.Helper()
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	users, err := NewUsers([]config.UserCred{
		{Username: "alice", Hash: aliceHash},
		{Username: "bob", Hash: bobHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := users.SyncToDB(ctx, pool); err != nil {
		t.Fatal(err)
	}
	svc := NewService(users, NewSessionStore(pool, ttl), Options{
		CookieName:   testCookieName,
		CookieSecure: true,
		SessionTTL:   ttl,
	})
	ids := map[string]string{
		"alice": users.byName["alice"].UserID,
		"bob":   users.byName["bob"].UserID,
	}
	return svc, pool, ids
}

func TestSessionStoreLifecycle(t *testing.T) {
	svc, pool, ids := setup(t, time.Hour)
	ctx := context.Background()
	store := svc.Sessions

	sess, err := store.Create(ctx, ids["alice"])
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(sess.ID) < 40 {
		t.Errorf("session id %q looks too short for 32 random bytes", sess.ID)
	}

	got, ok, err := store.Lookup(ctx, sess.ID)
	if err != nil || !ok {
		t.Fatalf("Lookup live session: ok=%v err=%v", ok, err)
	}
	if got.Username != "alice" || got.UserID != ids["alice"] {
		t.Errorf("Lookup returned %+v", got)
	}

	if _, ok, _ := store.Lookup(ctx, "does-not-exist"); ok {
		t.Error("Lookup of unknown id returned ok")
	}

	// Force expiry and confirm rejection + purge.
	if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at = now() - interval '1 hour' WHERE id = $1`, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Lookup(ctx, sess.ID); ok {
		t.Error("expired session accepted")
	}
	var n int
	pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id = $1`, sess.ID).Scan(&n)
	if n != 0 {
		t.Error("expired session row was not purged on lookup")
	}

	// Delete is idempotent.
	sess2, _ := store.Create(ctx, ids["alice"])
	if err := store.Delete(ctx, sess2.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Delete(ctx, sess2.ID); err != nil {
		t.Fatalf("second Delete errored: %v", err)
	}

	// PurgeExpired counts rows removed.
	s3, _ := store.Create(ctx, ids["bob"])
	pool.Exec(ctx, `UPDATE sessions SET expires_at = now() - interval '1 hour' WHERE id = $1`, s3.ID)
	removed, err := store.PurgeExpired(ctx)
	if err != nil || removed < 1 {
		t.Fatalf("PurgeExpired removed=%d err=%v", removed, err)
	}
}

func doLogin(t *testing.T, svc *Service, username, password string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(loginRequest{Username: username, Password: password})
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(body)))
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	svc.LoginHandler(rec, req)
	return rec.Result()
}

func TestLoginHandler(t *testing.T) {
	svc, _, _ := setup(t, 2*time.Hour)

	t.Run("success sets cookie", func(t *testing.T) {
		resp := doLogin(t, svc, "alice", alicePass)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var got map[string]string
		json.NewDecoder(resp.Body).Decode(&got)
		if got["username"] != "alice" {
			t.Errorf("body username = %q", got["username"])
		}
		cookies := resp.Cookies()
		if len(cookies) != 1 {
			t.Fatalf("got %d cookies, want 1", len(cookies))
		}
		c := cookies[0]
		if c.Name != testCookieName || c.Value == "" {
			t.Errorf("cookie = %+v", c)
		}
		if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
			t.Errorf("cookie attributes wrong: HttpOnly=%v Secure=%v SameSite=%v Path=%q", c.HttpOnly, c.Secure, c.SameSite, c.Path)
		}
		if c.MaxAge != int((2 * time.Hour).Seconds()) {
			t.Errorf("cookie MaxAge = %d, want %d", c.MaxAge, int((2 * time.Hour).Seconds()))
		}
	})

	t.Run("wrong password is generic 401 with no cookie", func(t *testing.T) {
		resp := doLogin(t, svc, "alice", "nope")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		if len(resp.Cookies()) != 0 {
			t.Error("a cookie was set on failed login")
		}
		var got map[string]string
		json.NewDecoder(resp.Body).Decode(&got)
		if got["error"] != genericLoginError {
			t.Errorf("error = %q, want generic %q", got["error"], genericLoginError)
		}
	})

	t.Run("unknown user gives the same generic 401", func(t *testing.T) {
		resp := doLogin(t, svc, "charlie", "whatever")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		var got map[string]string
		json.NewDecoder(resp.Body).Decode(&got)
		if got["error"] != genericLoginError {
			t.Errorf("error = %q, want %q", got["error"], genericLoginError)
		}
	})

	t.Run("malformed body is 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("{not json"))
		rec := httptest.NewRecorder()
		svc.LoginHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing password is 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"alice"}`))
		rec := httptest.NewRecorder()
		svc.LoginHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

func TestLoginHandlerRateLimited(t *testing.T) {
	svc, _, _ := setup(t, time.Hour)
	var last *http.Response
	for i := 0; i < 12; i++ {
		last = doLogin(t, svc, "alice", "wrong")
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after 12 failures status = %d, want 429", last.StatusCode)
	}
	// A correct password from the same IP+user is still blocked while tripped.
	if resp := doLogin(t, svc, "alice", alicePass); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("correct login while rate-limited status = %d, want 429", resp.StatusCode)
	}
}

// loginAndCookie logs in and returns the session cookie.
func loginAndCookie(t *testing.T, svc *Service, username, password string) *http.Cookie {
	t.Helper()
	resp := doLogin(t, svc, username, password)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %d", resp.StatusCode)
	}
	return resp.Cookies()[0]
}

func TestMiddleware(t *testing.T) {
	svc, pool, _ := setup(t, time.Hour)
	ctx := context.Background()

	protected := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := SessionFromContext(r.Context())
		if !ok {
			t.Error("handler reached without a session in context")
		}
		w.Write([]byte(sess.Username))
	}))

	good := loginAndCookie(t, svc, "alice", alicePass)

	expiredCookie := loginAndCookie(t, svc, "bob", bobPass)
	pool.Exec(ctx, `UPDATE sessions SET expires_at = now() - interval '1 hour' WHERE id = $1`, expiredCookie.Value)

	cases := []struct {
		name       string
		cookie     *http.Cookie
		wantStatus int
	}{
		{"no cookie", nil, http.StatusUnauthorized},
		{"unknown cookie", &http.Cookie{Name: testCookieName, Value: "bogus"}, http.StatusUnauthorized},
		{"expired session", expiredCookie, http.StatusUnauthorized},
		{"valid session", good, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusOK && rec.Body.String() != "alice" {
				t.Errorf("body = %q, want alice", rec.Body.String())
			}
		})
	}

	// last_seen_at is refreshed by a valid request.
	var before, after time.Time
	pool.QueryRow(ctx, `SELECT last_seen_at FROM sessions WHERE id = $1`, good.Value).Scan(&before)
	time.Sleep(10 * time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	req.AddCookie(good)
	protected.ServeHTTP(httptest.NewRecorder(), req)
	pool.QueryRow(ctx, `SELECT last_seen_at FROM sessions WHERE id = $1`, good.Value).Scan(&after)
	if !after.After(before) {
		t.Errorf("last_seen_at not refreshed: before=%v after=%v", before, after)
	}
}

func TestSessionAndLogoutHandlers(t *testing.T) {
	svc, _, _ := setup(t, time.Hour)

	// GET /api/session without a cookie -> 401.
	rec := httptest.NewRecorder()
	svc.SessionHandler(rec, httptest.NewRequest(http.MethodGet, "/api/session", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-cookie session status = %d, want 401", rec.Code)
	}

	cookie := loginAndCookie(t, svc, "alice", alicePass)

	// GET /api/session with a valid cookie -> 200 {username}.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(cookie)
	svc.SessionHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid session status = %d, want 200", rec.Code)
	}
	var got map[string]string
	json.NewDecoder(rec.Body).Decode(&got)
	if got["username"] != "alice" {
		t.Errorf("username = %q", got["username"])
	}

	// POST /api/logout clears the cookie and deletes the row.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(cookie)
	svc.LogoutHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", rec.Code)
	}
	cleared := rec.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 || cleared[0].Value != "" {
		t.Errorf("logout did not clear the cookie: %+v", cleared)
	}

	// Reusing the old cookie now fails.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(cookie)
	svc.SessionHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused post-logout cookie status = %d, want 401", rec.Code)
	}

	// Logout with no session still succeeds.
	rec = httptest.NewRecorder()
	svc.LogoutHandler(rec, httptest.NewRequest(http.MethodPost, "/api/logout", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("logout without a session status = %d, want 200", rec.Code)
	}
}
