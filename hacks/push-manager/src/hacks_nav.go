package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

// hacks_nav.go — lets Push Manager's header link to any other installed
// hack's own web UI, instead of the user having to know its port. A hack
// opts in by declaring "web_ui": {"label": "...", "path": "..."} in its own
// hack.json (see catalog/schema.md). Scanned the same way live_log.go's
// installedHacksSummary() reads deployed hack.json files, since nothing
// else already tracks "what's installed" in a form the browser can use.

type navEntry struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Port  int    `json:"port"`
	Path  string `json:"path"`
}

// hackInstalled reports whether a hack with the given id has been deployed
// (its hack.json exists), checked live rather than cached — used to hide UI
// for optional hacks that aren't installed, e.g. browser-bridge's Shadow UI
// tab and Push Manager web header link, without needing a restart when the
// hack is installed/removed via the catalog.
func hackInstalled(id string) bool {
	_, err := os.Stat(filepath.Join("/data/push-hack/hacks", id, "hack.json"))
	return err == nil
}

// hackNavEntries scans /data/push-hack/hacks/*/hack.json for hacks that
// declare a web_ui and have a port to reach it on. Push Manager itself is
// excluded — its header already has fixed nav for its own sections.
func hackNavEntries() []navEntry {
	matches, _ := filepath.Glob("/data/push-hack/hacks/*/hack.json")
	var entries []navEntry
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var hc struct {
			ID    string `json:"id"`
			Port  int    `json:"port"`
			WebUI *struct {
				Label string `json:"label"`
				Path  string `json:"path"`
			} `json:"web_ui"`
		}
		if json.Unmarshal(data, &hc) != nil || hc.ID == "" || hc.ID == config.ID {
			continue
		}
		if hc.WebUI == nil || hc.Port == 0 {
			continue
		}
		path := hc.WebUI.Path
		if path == "" {
			path = "/"
		}
		label := hc.WebUI.Label
		if label == "" {
			label = hc.ID
		}
		entries = append(entries, navEntry{ID: hc.ID, Label: label, Port: hc.Port, Path: path})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Label < entries[j].Label })
	return entries
}

func handleHacksNav(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, hackNavEntries())
}

// installedHackIDs lists every deployed hack's id — lets the web UI hide
// controls for a feature whose dependency isn't installed (e.g. the preset
// Browser tab needs browser-bridge), the same live-checked way panelDefs
// gates the Shadow UI's own tabs.
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
