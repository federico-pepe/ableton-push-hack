package httpx

import (
	"io/fs"
	"net/http"
)

// ServeEmbedded serves a single embedded HTML file at "/" — the pattern
// automation and keyboard-visualizer's handleUI shared byte-for-byte
// (push-manager's handleUI serves three files — index.html/app.css/app.js —
// so it keeps its own handler rather than forcing that shape in here).
func ServeEmbedded(fsys fs.FS, indexPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(fsys, indexPath)
		if err != nil {
			http.Error(w, "UI not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	}
}
