package auth

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
)

// Service bundles the credential map, session store, and login rate limiter and
// exposes the auth HTTP handlers and the session middleware.
type Service struct {
	Users    *Users
	Sessions *SessionStore

	cookieName   string
	cookieSecure bool
	ttl          time.Duration
	limiter      *loginLimiter
}

// Options configure a Service.
type Options struct {
	CookieName   string
	CookieSecure bool
	SessionTTL   time.Duration

	// LoginMaxFailures / LoginWindow bound failed logins per IP+username.
	// Zero values fall back to 10 failures per 5 minutes.
	LoginMaxFailures int
	LoginWindow      time.Duration
}

// NewService builds a Service.
func NewService(users *Users, sessions *SessionStore, opt Options) *Service {
	maxFail := opt.LoginMaxFailures
	if maxFail <= 0 {
		maxFail = 10
	}
	win := opt.LoginWindow
	if win <= 0 {
		win = 5 * time.Minute
	}
	return &Service{
		Users:        users,
		Sessions:     sessions,
		cookieName:   opt.CookieName,
		cookieSecure: opt.CookieSecure,
		ttl:          opt.SessionTTL,
		limiter:      newLoginLimiter(maxFail, win),
	}
}

// ---- context ----

type ctxKey int

const sessionKey ctxKey = 0

// WithSession stores sess on ctx.
func WithSession(ctx context.Context, sess Session) context.Context {
	return context.WithValue(ctx, sessionKey, sess)
}

// SessionFromContext returns the session put on ctx by Middleware.
func SessionFromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey).(Session)
	return s, ok
}

// ---- handlers ----

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

const genericLoginError = "invalid username or password"

// LoginHandler implements POST /api/login.
func (svc *Service) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	key := clientIP(r) + "\x00" + req.Username
	if !svc.limiter.allowed(key) {
		writeError(w, http.StatusTooManyRequests, "too many failed login attempts; try again later")
		return
	}

	userID, ok := svc.Users.Verify(req.Username, req.Password)
	if !ok {
		svc.limiter.recordFailure(key)
		writeError(w, http.StatusUnauthorized, genericLoginError)
		return
	}
	svc.limiter.reset(key)

	sess, err := svc.Sessions.Create(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start a session")
		return
	}
	svc.setCookie(w, sess.ID, svc.ttl)
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

// SessionHandler implements GET /api/session.
func (svc *Service) SessionHandler(w http.ResponseWriter, r *http.Request) {
	sess, ok := svc.lookupFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": sess.Username})
}

// LogoutHandler implements POST /api/logout. It always succeeds.
func (svc *Service) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(svc.cookieName); err == nil && c.Value != "" {
		_ = svc.Sessions.Delete(r.Context(), c.Value)
	}
	svc.clearCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Middleware rejects any request without a valid, unexpired session and
// otherwise puts the session on the request context and refreshes last_seen_at.
func (svc *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := svc.lookupFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		_ = svc.Sessions.Touch(r.Context(), sess.ID)
		next.ServeHTTP(w, r.WithContext(WithSession(r.Context(), sess)))
	})
}

func (svc *Service) lookupFromRequest(r *http.Request) (Session, bool) {
	c, err := r.Cookie(svc.cookieName)
	if err != nil || c.Value == "" {
		return Session{}, false
	}
	sess, ok, err := svc.Sessions.Lookup(r.Context(), c.Value)
	if err != nil || !ok {
		return Session{}, false
	}
	return sess, true
}

// ---- cookies ----

func (svc *Service) setCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     svc.cookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   svc.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

func (svc *Service) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     svc.cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   svc.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// ---- helpers ----

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			xff = xff[:i]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
