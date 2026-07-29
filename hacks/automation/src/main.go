package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

//go:embed ui/index.html
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
		config.Port = 7703
	}
	return nil
}

// ── Middleware ─────────────────────────────────────────────────────────────

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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Helpers ────────────────────────────────────────────────────────────────

func jsonResponse(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

// ── Handlers ───────────────────────────────────────────────────────────────

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

func handleStatus(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"status":  "ok",
		"name":    config.Name,
		"version": config.Version,
		"port":    config.Port,
	})
}

// ── Main ───────────────────────────────────────────────────────────────────

func main() {
	configPath := flag.String("config", "hack.json", "path to hack.json config file")
	portOverride := flag.Int("port", 0, "override port from config")
	pushManager := flag.String("push-manager", "http://localhost:7701", "push-manager base URL for BPM sync")
	flag.Parse()

	if err := loadConfig(*configPath); err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *portOverride != 0 {
		config.Port = *portOverride
	}

	// Set persistence path alongside hack.json
	autoConfigPath = filepath.Join(filepath.Dir(*configPath), "automation.json")
	// Default TransportSync on; loadAutoConfig overwrites if saved state differs.
	autoState.TransportSync = true
	loadAutoConfig()

	// Defer ALSA init past USB-A boot-settle window (same as push-manager)
	go func() {
		waitForBootSettle()
		detectPush3Port() // needed for MIDI clock subscription
		initMidiOut()
		initMidiIn() // subscribe to Push 3 for MIDI clock + transport
		// Re-detect ports every 30s (client numbers can shift)
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				detectPush3Port()
				detectLivePort()
			}
		}()
	}()

	// Start playback engine and BPM sync poller. Transport (play/stop) is driven
	// solely by the Push Play button (CC85) — see onPlayButtonPress in midi.go.
	startAutoEngine()
	startBPMPoller(*pushManager)

	// Routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleUI)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/auto/state", handleAutoState)
	mux.HandleFunc("/api/auto/play", handleAutoPlay)
	mux.HandleFunc("/api/auto/stop", handleAutoStop)
	mux.HandleFunc("/api/auto/stream", handleAutoStream)
	mux.HandleFunc("/api/auto/lane", handleAutoLaneCreate)
	mux.HandleFunc("/api/auto/lane/", handleAutoLaneByID)
	mux.HandleFunc("/api/auto/transport_sync", handleAutoTransportSync)

	handler := withCORS(withLogging(mux))

	addr := fmt.Sprintf(":%d", config.Port)
	log.Printf("Automation %s starting on %s", config.Version, addr)
	log.Printf("BPM sync from: %s/api/live/tempo", *pushManager)

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
