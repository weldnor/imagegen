package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weldnor/imagegen/internal/auth"
	"github.com/weldnor/imagegen/internal/config"
	"github.com/weldnor/imagegen/internal/db"
	"github.com/weldnor/imagegen/internal/dbtest"
	"github.com/weldnor/imagegen/internal/gallery"
	"github.com/weldnor/imagegen/internal/openrouter"
	"github.com/weldnor/imagegen/migrations"
)

// alice/bob credentials (hashes match the passwords).
const (
	aliceHash = "$2a$10$OC8R4yTGCzRvlH3ru9mkIeqKQm5tgfye83af3fzimrUQWBB3lwVgu"
	alicePass = "alice-password"
	bobHash   = "$2a$10$JNBSnHo36Fu.rursUdwiGueD2PGsHFEm6SbPinrRgpxJPin24y.Rq"
	bobPass   = "bob-password"
	cookieNm  = "imagen_session"
)

func newAuthService(t *testing.T) *auth.Service {
	t.Helper()
	svc, _ := newAuthServiceAndPool(t)
	return svc
}

func newAuthServiceAndPool(t *testing.T) (*auth.Service, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	users, err := auth.NewUsers([]config.UserCred{
		{Username: "alice", Hash: aliceHash},
		{Username: "bob", Hash: bobHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := users.SyncToDB(ctx, pool); err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(users, auth.NewSessionStore(pool, time.Hour), auth.Options{
		CookieName:   cookieNm,
		CookieSecure: false,
		SessionTTL:   time.Hour,
	})
	return svc, pool
}

// testEnv is a fully wired router with a fake generator and a real gallery store.
type testEnv struct {
	router  http.Handler
	srv     *httptest.Server
	pool    *pgxpool.Pool
	gen     *fakeGen
	gallery *gallery.Store
}

func newTestEnv(t *testing.T, gen *fakeGen) *testEnv {
	return newTestEnvWith(t, gen, 4, 8<<20)
}

func newTestEnvWith(t *testing.T, gen *fakeGen, concurrency int, maxUpload int64) *testEnv {
	t.Helper()
	authSvc, pool := newAuthServiceAndPool(t)
	gstore := gallery.NewStore(pool, t.TempDir())
	api := &API{Gen: gen, Gallery: gstore, Concurrency: concurrency, MaxUpload: maxUpload}
	deps := Deps{Auth: authSvc, Static: NewStaticHandler("")}
	api.Bind(&deps)
	router := NewRouter(deps)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &testEnv{router: router, srv: srv, pool: pool, gen: gen, gallery: gstore}
}

func (e *testEnv) login(t *testing.T, user, pass string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	resp, err := http.Post(e.srv.URL+"/api/login", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login(%s) status = %d", user, resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == cookieNm {
			return c
		}
	}
	t.Fatalf("no session cookie for %s", user)
	return nil
}

func (e *testEnv) do(t *testing.T, method, path string, cookie *http.Cookie, body string) *http.Response {
	t.Helper()
	var r *strings.Reader
	if body != "" {
		r = strings.NewReader(body)
	} else {
		r = strings.NewReader("")
	}
	req, err := http.NewRequest(method, e.srv.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// fakeGen is an in-memory Generator. fn decides each call's result by call index.
type fakeGen struct {
	fn       func(call int) (*openrouter.Image, error)
	calls    atomic.Int64
	inFlight atomic.Int64
	maxInFlt atomic.Int64
	delay    time.Duration
}

func (f *fakeGen) Generate(ctx context.Context, p openrouter.GenerateParams) (*openrouter.Image, error) {
	n := int(f.calls.Add(1) - 1)
	cur := f.inFlight.Add(1)
	for {
		m := f.maxInFlt.Load()
		if cur <= m || f.maxInFlt.CompareAndSwap(m, cur) {
			break
		}
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.inFlight.Add(-1)
	return f.fn(n)
}

func okImage(_ int) (*openrouter.Image, error) {
	return &openrouter.Image{Data: []byte("PNGDATA"), ContentType: "image/png"}, nil
}

// login posts alice's credentials to srv and returns the session cookie. Kept
// for the middleware-wiring tests that build their own router.
func login(t *testing.T, srv *httptest.Server) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": "alice", "password": alicePass})
	resp, err := http.Post(srv.URL+"/api/login", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == cookieNm {
			return c
		}
	}
	t.Fatal("no session cookie in login response")
	return nil
}
