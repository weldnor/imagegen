// Package httpapi wires the chi router, middleware, JSON handlers, and static
// file serving for the frontend.
package httpapi

import (
	"log"
	"net/http"
	"runtime/debug"
	"time"
)

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// requestLogger logs one line per request: method, path, status, duration,
// client address. It deliberately logs neither request bodies nor any header
// value, so credentials (login body) and session cookies never reach the log.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %dB %s",
			r.Method, r.URL.Path, rec.status, rec.bytes, time.Since(start).Round(time.Millisecond))
	})
}

// recoverer turns a panic in a handler into a 500 instead of crashing the
// server. The stack trace goes to the server log, never to the client.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic serving %s %s: %v\n%s", r.Method, r.URL.Path, v, debug.Stack())
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"internal server error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
