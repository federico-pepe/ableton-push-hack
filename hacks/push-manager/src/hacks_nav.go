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
// erroring (e.g. the preset Browser tab needs browser-bridge), and so an
// external hack can put its own link in Push Manager's menu bar just by
// declaring `web_ui` in its hack.json (see catalog/schema.md's "Web UI
// navigation"). Same field the catalog's own cards read — no second hook to
// implement, no push-manager code change per hack.

// hacksDir is a var only so the tests can point it at a temp dir.
var hacksDir = "/data/push-hack/hacks"

// hackInstalled reports whether a hack with the given id has been deployed
// (its hack.json exists), checked live rather than cached — used to hide UI
// for optional hacks that aren't installed, e.g. browser-bridge's Shadow UI
// tab and Push Manager's web Browser button, without needing a restart when
// the hack is installed/removed via the catalog.
func hackInstalled(id string) bool {
	_, err := os.Stat(filepath.Join(hacksDir, id, "hack.json"))
	return err == nil
}

// hackUI is the shape of both hack.json nav hooks — web_ui (a menu-bar
// link) and shadow_ui (a Shadow UI tab, rendered by remote_panel.go).
// Same two fields; the port comes from the hack's own `port`.
type hackUI struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// hackNav is one installed hack as the UI layer sees it: id for gating a
// feature button, plus port + whichever nav hooks it declares. Both are nil
// for a hack with no UI of its own (a Remote Script, push-display).
type hackNav struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Port     int     `json:"port"`
	WebUI    *hackUI `json:"web_ui,omitempty"`
	ShadowUI *hackUI `json:"shadow_ui,omitempty"`
}

// installedHacks reads every deployed hack.json, live — so a hack installed
// or removed through the catalog appears in / drops out of the menu bar on
// the UI's next 10s poll, with no push-manager restart.
func installedHacks() []hackNav {
	matches, _ := filepath.Glob(filepath.Join(hacksDir, "*", "hack.json"))
	hacks := make([]hackNav, 0, len(matches))
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var h hackNav
		if json.Unmarshal(data, &h) == nil && h.ID != "" {
			hacks = append(hacks, h)
		}
	}
	sort.Slice(hacks, func(i, j int) bool { return hacks[i].ID < hacks[j].ID })
	return hacks
}

func handleHacksInstalled(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, installedHacks())
}
