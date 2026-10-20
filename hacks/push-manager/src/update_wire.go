package main

// update_wire.go -- start the Push Hack update checker and its web routes.
// The logic lives in the update package; plan: plans/2026-10-20-push-hack-self-update.md.

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"path/filepath"
	"time"

	"push-manager/update"
)

// startUpdateChecker serves /api/update/* and starts the daily check. State
// lives in <push-hack dir>/update/, two levels above this hack's own dir.
func startUpdateChecker(configPath string, mux *http.ServeMux) {
	dir := filepath.Join(filepath.Dir(configPath), "..", "..", "update")
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	store := update.OpenStore(filepath.Join(dir, "settings.json"))
	checker := update.New(update.Config{
		Installed:         buildVersion,
		URL:               store.Get().URL,
		StatePath:         filepath.Join(dir, "state.json"),
		AutoCheck:         store.AutoCheck,
		IncludePrerelease: store.IncludePrerelease,
		Logf:              log.Printf,
	})
	updateChecker = checker // the Shadow UI bar reads it (ui_update.go)
	(&update.API{C: checker, S: store}).Register(mux)

	go func() {
		waitForBootSettle() // no network use while USB-A settles; no-op on a restart
		// 1 min, plus up to 10 min of jitter, so a fleet does not call together.
		checker.Run(context.Background(), time.Minute+time.Duration(rand.Int63n(int64(10*time.Minute))))
	}()
}
