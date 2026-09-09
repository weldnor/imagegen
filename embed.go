// Package imagegen embeds the static frontend so the server binary is
// self-contained. Set STATIC_DIR to serve the frontend from disk instead.
package imagegen

import (
	"embed"
	"io/fs"
)

//go:embed index.html favicon.svg src
var frontendFS embed.FS

// FrontendFS is the embedded frontend rooted so that "index.html", "src/app.js",
// and "favicon.svg" resolve directly.
var FrontendFS fs.FS = frontendFS
