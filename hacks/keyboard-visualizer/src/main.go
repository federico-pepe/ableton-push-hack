package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

//go:embed ui/index.html ui/vendor/tonal.min.js
var embeddedUI embed.FS

type HackConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Port    int    `json:"port"`
}

var config HackConfig

func loadConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&config); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if config.Port == 0 {
		config.Port = 7702
	}
	return nil
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := embeddedUI.ReadFile("ui/index.html")
	if err != nil {
		http.Error(w, "UI not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

func handleVendorTonal(w http.ResponseWriter, r *http.Request) {
	data, err := embeddedUI.ReadFile("ui/vendor/tonal.min.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(data)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	held := snapshotHeldNotes()
	n := 0
	for _, v := range held {
		if v {
			n++
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"name":       config.Name,
		"version":    config.Version,
		"port":       config.Port,
		"notes_held": n,
	})
}

func main() {
	configPath := flag.String("config", "hack.json", "path to hack.json config file")
	portOverride := flag.Int("port", 0, "override port from config")
	pushManager := flag.String("push-manager", "http://localhost:7701", "push-manager base URL for display API")
	flag.Parse()

	if err := loadConfig(*configPath); err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *portOverride != 0 {
		config.Port = *portOverride
	}

	// ALSA and display-render init deferred past boot-settle (USB-A safety) and
	// run independently of the HTTP server, which starts immediately.
	go func() {
		waitForBootSettle()
		go runMidiIn(*pushManager)
		runRenderLoop(*pushManager)
	}()

	// Push-manager/push-display are separate, optional hacks this one depends
	// on for anything to actually show on screen — no ALSA/USB involved, so
	// this check doesn't need to wait for boot-settle.
	go runDependencyWatcher(*pushManager)

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleUI)
	mux.HandleFunc("/vendor/tonal.min.js", handleVendorTonal)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/notes/stream", handleNotesStream)

	handler := withCORS(withLogging(mux))

	addr := fmt.Sprintf(":%d", config.Port)
	log.Printf("Keyboard Visualizer %s starting on %s (push-manager: %s)", config.Version, addr, *pushManager)

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
