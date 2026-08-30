package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

// hacks_nav.go — lets Push Manager's own UI know what else is installed, so
// a feature whose dependency isn't deployed can hide itself instead of
// erroring (e.g. the preset Browser tab needs browser-bridge). A hack's own
// web UI link, if it has one, lives in Push Hack Catalog instead (see
// catalog/schema.md's "Web UI navigation" section) — not duplicated here.

// hackInstalled reports whether a hack with the given id has been deployed
// (its hack.json exists), checked live rather than cached — used to hide UI
// for optional hacks that aren't installed, e.g. browser-bridge's Shadow UI
// tab and Push Manager's web Browser button, without needing a restart when
// the hack is installed/removed via the catalog.
func hackInstalled(id string) bool {
	_, err := os.Stat(filepath.Join("/data/push-hack/hacks", id, "hack.json"))
	return err == nil
}

// installedHackIDs lists every deployed hack's id — lets the web UI hide
// controls for a feature whose dependency isn't installed, the same
// live-checked way panelDefs gates the Shadow UI's own tabs.
func installedHackIDs() []string {
	matches, _ := filepath.Glob("/data/push-hack/hacks/*/hack.json")
	ids := make([]string, 0, len(matches))
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var hc struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(data, &hc) == nil && hc.ID != "" {
			ids = append(ids, hc.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func handleHacksInstalled(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, installedHackIDs())
}
