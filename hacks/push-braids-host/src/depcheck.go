package main

// depcheck.go — this hack never writes to Push's shared-memory framebuffer
// itself; it's an HTTP client of push-manager's /api/display/* API, which
// in turn requires push-display (the LD_PRELOAD hook) to actually show
// anything on screen. Both are optional hacks the user may not have
// installed, so this actively checks for them and logs a clear, actionable
// warning rather than silently failing every display call. Adapted from
// hacks/keyboard-visualizer/src/depcheck.go — same dependency, same shape.

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
// and every 30s, logging once per state transition so the cause of "no
// param screen" is immediately obvious in the log.
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
			log.Printf("WARNING: push-manager not reachable at %s — the on-screen param "+
				"controls will not work until push-manager is installed and running.", pushManagerURL)
		case depPushDisplayNotAttached:
			log.Printf("WARNING: push-manager reachable but push-display's shared-memory " +
				"framebuffer is not connected — the on-screen param controls will not work " +
				"until push-display is installed.")
		case depOK:
			log.Printf("push-manager + push-display OK — on-screen param controls available.")
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
