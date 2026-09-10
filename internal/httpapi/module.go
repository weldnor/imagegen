package httpapi

import (
	"go.uber.org/fx"

	"github.com/weldnor/imagegen/internal/auth"
	"github.com/weldnor/imagegen/internal/config"
	"github.com/weldnor/imagegen/internal/gallery"
	"github.com/weldnor/imagegen/internal/openrouter"
)

// Module provides the API handlers, router Deps, and the assembled
// http.Handler.
var Module = fx.Module("httpapi",
	fx.Provide(
		provideAPI,
		provideDeps,
		NewRouter,
	),
)

func provideAPI(gen *openrouter.Client, gal *gallery.Store, cfg *config.Config) *API {
	return &API{
		Gen:         gen,
		Gallery:     gal,
		Concurrency: cfg.OpenRouterConcurrency,
		MaxUpload:   cfg.MaxUploadBytes,
	}
}

func provideDeps(authSvc *auth.Service, api *API, cfg *config.Config) Deps {
	d := Deps{
		Auth:   authSvc,
		Static: NewStaticHandler(cfg.StaticDir),
	}
	api.Bind(&d)
	return d
}
