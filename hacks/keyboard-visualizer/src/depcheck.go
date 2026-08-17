package main

// depcheck.go — this hack never writes to Push's shared-memory framebuffer
// itself; it's an HTTP client of push-manager's /api/display/* API, which in
// turn requires push-display (the LD_PRELOAD hook) to actually show anything
// on screen. Both are optional hacks the user may not have installed, so we
// actively check for them and log a clear, actionable warning rather than
// silently failing every display call (see runRenderLoop/toggleTakeover in
// render.go, which already log per-call errors but easy to miss in a wall of
// log lines — this gives one unambiguous diagnosis on startup and on change).

import (
	"log"
	"net/http"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/pmclient"
)

type depState int

const (
	depUnknown depState = iota
	depPushManagerUnreachable
	depPushDisplayNotAttached
	depOK
)

// runDependencyWatcher checks push-manager's /api/display/status on startup
// and every 30s, logging once per state transition (not spamming every
// check) so the cause of "takeover does nothing" is immediately obvious in
// the log.
func runDependencyWatcher(pushManagerURL string) {
	client := &pmclient.Client{Base: pushManagerURL, HTTP: &http.Client{Timeout: 2 * time.Second}}
	last := depUnknown

	check := func() {
		state := checkDependency(client)
		if state == last {
			return
		}
		last = state
		switch state {
		case depPushManagerUnreachable:
			log.Printf("WARNING: push-manager not reachable at %s — display takeover will not work "+
				"until push-manager is installed and running. See hacks/keyboard-visualizer/README.md.",
				pushManagerURL)
		case depPushDisplayNotAttached:
			log.Printf("WARNING: push-manager reachable but push-display's shared-memory framebuffer " +
				"is not connected — display takeover will not work until push-display is installed. " +
				"See hacks/keyboard-visualizer/README.md.")
		case depOK:
			log.Printf("push-manager + push-display OK — display takeover available (Shift+Note to toggle).")
		}
	}

	check()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		check()
	}
}

func checkDependency(client *pmclient.Client) depState {
	status, err := client.DisplayStatus()
	if err != nil {
		return depPushManagerUnreachable
	}
	if !status.Connected {
		return depPushDisplayNotAttached
	}
	return depOK
}
