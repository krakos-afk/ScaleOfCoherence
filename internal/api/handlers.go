package api

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// apiPrefixes are the path prefixes owned by the API. Every other path is
// served from the SPA directory when one is configured.
var apiPrefixes = []string{"/auth", "/teams", "/items", "/workflows"}

func isAPIPath(p string) bool {
	for _, prefix := range apiPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// withSPA serves the built frontend from dir for non-API paths, falling back
// to index.html so client-side routes survive reloads and deep links. An empty
// dir disables static serving (the Vite dev server handles the frontend then).
func withSPA(api http.Handler, dir string) http.Handler {
	if dir == "" {
		return api
	}

	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) || publicPaths[r.URL.Path] {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Vite fingerprints files under /assets/, so they can be cached
		// forever; everything else (index.html) must be revalidated.
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		// Unknown paths are client-side routes: serve index.html.
		local := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(local))); err != nil || info.IsDir() {
			clone := r.Clone(r.Context())
			u := *r.URL
			u.Path = "/"
			clone.URL = &u
			r = clone
		}
		files.ServeHTTP(w, r)
	})
}
