package httpapi

import (
	"io/fs"
	"net/http"
	"os"

	imagegen "github.com/weldnor/imagegen"
)

// NewStaticHandler serves the frontend. When staticDir is non-empty the files
// are read from that directory; otherwise the binary's embedded copy is used.
// No session is required so the login form can load.
func NewStaticHandler(staticDir string) http.Handler {
	var fsys fs.FS
	if staticDir != "" {
		fsys = os.DirFS(staticDir)
	} else {
		fsys = imagegen.FrontendFS
	}
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only GET/HEAD make sense for static assets.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
