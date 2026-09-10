package openrouter

import (
	"go.uber.org/fx"

	"github.com/weldnor/imagegen/internal/config"
)

// Module provides the OpenRouter *Client.
var Module = fx.Module("openrouter",
	fx.Provide(provideClient),
)

func provideClient(cfg *config.Config) *Client {
	return New(Config{
		APIKey:  cfg.OpenRouterAPIKey,
		BaseURL: cfg.OpenRouterBaseURL,
		Timeout: cfg.OpenRouterTimeout,
	})
}
