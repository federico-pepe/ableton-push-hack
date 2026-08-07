package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/federico-pepe/ableton-push-hack/core/hackcfg"
	"github.com/federico-pepe/ableton-push-hack/core/httpx"
)

//go:embed ui/index.html ui/vendor/tonal.min.js
var embeddedUI embed.FS

var config hackcfg.Config

var handleUI = httpx.ServeEmbedded(embeddedUI, "ui/index.html")

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

	var err error
	config, err = hackcfg.Load(*configPath, 7702)
	if err != nil {
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

	handler := httpx.WithCORS("GET, POST, OPTIONS", httpx.WithLogging(mux))

	addr := fmt.Sprintf(":%d", config.Port)
	log.Printf("Keyboard Visualizer %s starting on %s (push-manager: %s)", config.Version, addr, *pushManager)

	srv := httpx.NewServer(addr, handler)

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
