package config

import (
	"os"

	"go.uber.org/fx"
)

// Module provides the parsed *Config, loaded once from the process
// environment (os.Getenv) for the whole app graph.
var Module = fx.Module("config",
	fx.Provide(func() (*Config, error) {
		return Load(os.Getenv)
	}),
)
