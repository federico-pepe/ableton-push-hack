// random-preset — hold Shift+Add on Push to load a random preset onto the
// selected track. A background daemon: no HTTP server, no display. It watches
// Push 3's hardware MIDI for the Shift+Add chord (mirroring keyboard-visualizer's
// independent chord watch) and, on fire, asks push-manager to load a random
// preset (push-manager forwards to browser-bridge, which drives Live's browser).
//
// Requires push-manager (:7701, preset index + /api/live/load) and browser-bridge
// (the PushHackBrowser remote script, activated in Live).
package main

import (
	"flag"
	"log"
	"os"
)

func main() {
	// Accepted for init.d compatibility (the generated service passes -config);
	// this hack has no config beyond the push-manager URL (env-overridable).
	cfg := flag.String("config", "hack.json", "unused; accepted for init.d compatibility")
	flag.Parse()
	_ = cfg

	pmURL := os.Getenv("PUSH_MANAGER_URL")
	if pmURL == "" {
		pmURL = "http://localhost:7701"
	}

	log.Printf("random-preset: Shift+Add (CC%d+CC%d)->random preset, Shift+Swap (CC%d+CC%d)->random drum rack, via %s",
		ccShift, ccAdd, ccShift, ccSwap, pmURL)
	waitForBootSettle()
	runMidiIn(pmURL) // blocks forever
}
