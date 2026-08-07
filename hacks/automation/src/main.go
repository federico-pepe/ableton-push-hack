package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/hackcfg"
	"github.com/federico-pepe/ableton-push-hack/core/httpx"
)

//go:embed ui/index.html
var embeddedUI embed.FS

var config hackcfg.Config

// ── Handlers ───────────────────────────────────────────────────────────────

var handleUI = httpx.ServeEmbedded(embeddedUI, "ui/index.html")

func jsonResponse(w http.ResponseWriter, v interface{}) { httpx.JSON(w, v) }

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

	var err error
	config, err = hackcfg.Load(*configPath, 7703)
	if err != nil {
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

	handler := httpx.WithCORS("GET, POST, PUT, DELETE, OPTIONS", httpx.WithLogging(mux))

	addr := fmt.Sprintf(":%d", config.Port)
	log.Printf("Automation %s starting on %s", config.Version, addr)
	log.Printf("BPM sync from: %s/api/live/tempo", *pushManager)

	srv := httpx.NewServer(addr, handler)

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
