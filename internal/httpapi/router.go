package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weldnor/imagegen/internal/auth"
)

// Deps are the collaborators the router needs. Handler fields left nil respond
// 501 Not Implemented; they are filled in as sections 5-7 land.
type Deps struct {
	Auth *auth.Service

	// Protected API handlers (require a valid session).
	Generate    http.HandlerFunc // POST   /api/generate
	ListImages  http.HandlerFunc // GET    /api/images
	GetImage    http.HandlerFunc // GET    /api/images/{id}
	DeleteImage http.HandlerFunc // DELETE /api/images/{id}
	ClearImages http.HandlerFunc // DELETE /api/images
	Models      http.HandlerFunc // GET    /api/models

	// Static serves the frontend for any non-/api, non-/healthz path.
	Static http.Handler
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_, _ = w.Write([]byte(`{"error":"not implemented"}`))
}

func orNotImplemented(h http.HandlerFunc) http.HandlerFunc {
	if h == nil {
		return notImplemented
	}
	return h
}

// NewRouter assembles the chi router with middleware order
// recovery -> logging -> (per protected route) session middleware.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(recoverer)
	r.Use(requestLogger)

	// Public liveness check: 200, no body, no session.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Route("/api", func(api chi.Router) {
		// Public auth endpoints.
		api.Post("/login", d.Auth.LoginHandler)
		api.Get("/session", d.Auth.SessionHandler)
		api.Post("/logout", d.Auth.LogoutHandler)

		// Everything else under /api requires a valid session.
		api.Group(func(protected chi.Router) {
			protected.Use(d.Auth.Middleware)
			protected.Post("/generate", orNotImplemented(d.Generate))
			protected.Get("/images", orNotImplemented(d.ListImages))
			protected.Get("/images/{id}", orNotImplemented(d.GetImage))
			protected.Delete("/images/{id}", orNotImplemented(d.DeleteImage))
			protected.Delete("/images", orNotImplemented(d.ClearImages))
			protected.Get("/models", orNotImplemented(d.Models))
		})
	})

	// Static frontend (no session required) for all other paths.
	if d.Static != nil {
		r.NotFound(d.Static.ServeHTTP)
	} else {
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
	}

	return r
}
