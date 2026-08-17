// Package httpx holds the HTTP boilerplate duplicated byte-for-byte across
// push-manager, automation and keyboard-visualizer: request logging, CORS,
// JSON responses, the server timeout triple, and embedded-UI serving.
package httpx

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

// WithLogging logs method, path and duration for every request.
func WithLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

// WithCORS wraps next with permissive CORS headers. allowMethods is the
// per-hack Access-Control-Allow-Methods value — the only thing that ever
// diverged between push-manager/automation/keyboard-visualizer's copies of
// this middleware, so it stays a parameter rather than being unified.
func WithCORS(allowMethods string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", allowMethods)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// JSON writes v as indented JSON with the appropriate content type.
func JSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

// Error maps a filesystem-flavored error to the appropriate HTTP status.
func Error(w http.ResponseWriter, err error) {
	switch {
	case os.IsNotExist(err):
		http.Error(w, "not found", http.StatusNotFound)
	case os.IsPermission(err):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// NewServer builds an *http.Server with the timeout triple every hack uses:
// 30s read (fast API calls), 5min write (large file downloads), 120s idle.
func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}
}
