// ui_tabs.go — one ordered list of UI entries, two switches each.
//
// An "entry" is anything that can show up in one of push-manager's two
// navigations:
//   - the Shadow UI's top-strip tabs (Push 3's screen), and
//   - the web UI's menu bar.
// Built-in panels (panelDefs in ui_shadow.go) are Shadow-only. An installed
// hack contributes whichever it declares in its own hack.json:
//   "web_ui":    {"label", "path"}  -> a menu-bar link
//   "shadow_ui": {"label", "path"}  -> a Shadow UI tab, rendered by
//                                      remote_panel.go over localhost HTTP
// Both sit next to the hack's own `port`. See catalog/schema.md.
//
// The user reorders the list and flips either switch from the web UI
// (Display page); it persists next to hack.json as ui_tabs.json. One order
// drives both navigations. Push 3 has 8 top buttons, so at most 8 Shadow
// tabs are ever drawn — the rest of the list stays config.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
)

const maxShadowTabs = 8

// uiEntry is one row of that list: what it is, and where it may appear.
// The has* flags tell the settings UI which switches to even show.
type uiEntry struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Source string `json:"source"` // "builtin" | "hack"

	HasShadow bool   `json:"has_shadow"`
	Shadow    bool   `json:"shadow"`           // switch: show as a Shadow UI tab
	Available bool   `json:"shadow_available"` // its required hack is installed
	Requires  string `json:"requires,omitempty"`

	HasApp bool   `json:"has_app"`         // takes the whole surface from Home (hack.json "surface")
	Color  string `json:"color,omitempty"` // Home's stripe and pads, a push3 palette name
	Star   bool   `json:"star"`            // Home: among the favorites, first
	Super  bool   `json:"super"`           // Home: the superstar, Shift + Help, Help

	HasWeb   bool   `json:"has_web"`
	Web      bool   `json:"web"`       // switch: show in the web menu bar
	WebLabel string `json:"web_label,omitempty"`
	WebPath  string `json:"web_path,omitempty"`
	Port     int    `json:"port,omitempty"`

	shadowPath string       // remote tabs only
	new        func() Panel // built-in tabs only
}

// uiPref is the persisted half: id + both switches, ordered.
type uiPref struct {
	ID     string `json:"id"`
	Shadow bool   `json:"shadow"`
	Web    bool   `json:"web"`
	Star   bool   `json:"star,omitempty"`
	Super  bool   `json:"super,omitempty"`
}

var (
	uiTabsPath string // set by main() to <hackdir>/ui_tabs.json
	uiTabsMu   sync.Mutex
	uiPrefs    []uiPref
)

func loadUITabs() {
	data, err := os.ReadFile(uiTabsPath)
	if err != nil {
		return // no config yet: everything on, in built-in-then-hack order
	}
	var prefs []uiPref
	if err := json.Unmarshal(data, &prefs); err != nil {
		return
	}
	uiTabsMu.Lock()
	uiPrefs = prefs
	uiTabsMu.Unlock()
}

func saveUITabs(prefs []uiPref) error {
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(uiTabsPath, data, 0o644)
}

// candidateEntries is everything that could appear right now: built-in
// panels plus one per installed hack declaring web_ui or shadow_ui. Read
// live off disk, so a hack installed through the catalog shows up without a
// push-manager restart.
func candidateEntries() []uiEntry {
	out := make([]uiEntry, 0, len(panelDefs)+4)
	for _, d := range panelDefs {
		e := uiEntry{
			ID: d.id, Label: d.label, Source: "builtin",
			HasShadow: true, Available: d.requires == "" || hackInstalled(d.requires),
			Requires: d.requires, new: d.new,
		}
		// The CATALOG entry is the one built-in with a web side too: Push
		// Hack Catalog is a core hack on a fixed port, so the menu-bar link
		// does not depend on the installed copy declaring web_ui (an older
		// one, or one installed under a different id, does not).
		if d.id == "catalog" {
			e.HasWeb, e.Port, e.WebLabel, e.WebPath = true, catalogPort, "Catalog", "/"
		}
		out = append(out, e)
	}
	for _, h := range installedHacks() {
		if h.Port == 0 || (h.WebUI == nil && h.ShadowUI == nil && h.Surface == nil) || !h.Enabled {
			continue
		}
		e := uiEntry{ID: h.ID, Label: h.Name, Source: "hack", Port: h.Port, Available: true}
		if h.Surface != nil {
			e.HasApp, e.Color = true, h.Surface.Color
			if h.Surface.Label != "" {
				e.Label = h.Surface.Label
			}
		}
		if h.WebUI != nil {
			e.HasWeb = true
			e.WebLabel = h.WebUI.Label
			if e.WebLabel == "" {
				e.WebLabel = h.Name
			}
			e.WebPath = h.WebUI.Path
			if e.WebPath == "" {
				e.WebPath = "/"
			}
		}
		if h.ShadowUI != nil {
			e.HasShadow = true
			e.shadowPath = h.ShadowUI.Path
			if h.ShadowUI.Label != "" {
				e.Label = h.ShadowUI.Label
			}
		}
		out = append(out, e)
	}
	return out
}

// resolveUIEntries applies the saved order and switches to the live
// candidate set. A candidate the config has never seen (a hack just
// installed, or a built-in added by an update) is appended at the end with
// both switches on — new things show up rather than silently going missing.
func resolveUIEntries() []uiEntry {
	cand := candidateEntries()
	byID := make(map[string]uiEntry, len(cand))
	for _, e := range cand {
		e.Shadow, e.Web = true, true
		byID[e.ID] = e
	}

	uiTabsMu.Lock()
	prefs := append([]uiPref(nil), uiPrefs...)
	uiTabsMu.Unlock()

	out := make([]uiEntry, 0, len(cand))
	seen := make(map[string]bool, len(cand))
	super := false
	for _, p := range prefs {
		e, ok := byID[p.ID]
		if !ok || seen[p.ID] {
			continue // stale config entry: hack removed, or duplicate
		}
		e.Shadow, e.Web, e.Star, e.Super = p.Shadow, p.Web, p.Star || p.Super, p.Super && !super
		super = super || e.Super // one superstar: the first one saved
		seen[p.ID] = true
		out = append(out, e)
	}
	for _, e := range cand {
		if !seen[e.ID] {
			out = append(out, byID[e.ID])
		}
	}
	return out
}

// activeShadowTabs is the subset the hardware actually shows: switched on,
// its dependency installed, and within the 8 top buttons.
func activeShadowTabs() []uiEntry {
	var out []uiEntry
	for _, e := range resolveUIEntries() {
		if !e.HasShadow || !e.Shadow || !e.Available {
			continue
		}
		out = append(out, e)
		if len(out) == maxShadowTabs {
			break
		}
	}
	return out
}

// newPanel builds the Panel for one resolved entry: a built-in constructor,
// or a RemotePanel talking to the hack that declared the tab.
func (e uiEntry) newPanel() Panel {
	if e.new != nil {
		return e.new()
	}
	return newRemotePanel(e.Label, e.Port, e.shadowPath)
}

// ── HTTP ──────────────────────────────────────────────────────────────────────

// uiTabsResponse is what both verbs return.
func uiTabsResponse() map[string]any {
	return map[string]any{
		"entries": resolveUIEntries(),
		"max":     maxShadowTabs,
	}
}

func handleUITabs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, uiTabsResponse())
	case http.MethodPost:
		var prefs []uiPref
		if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
			http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
			return
		}
		uiTabsMu.Lock()
		uiPrefs = prefs
		uiTabsMu.Unlock()
		if err := saveUITabs(prefs); err != nil {
			http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		shadowUIReload()
		jsonResponse(w, uiTabsResponse())
	default:
		http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
	}
}

// ── Push Hack Home ────────────────────────────────────────────────────────────

// Home's colours and lines for the built-in tools; a hack's Shadow UI tab
// gets gray_blue and no lines.
var toolLooks = map[string]struct {
	color string
	about [2]string
}{
	"files":   {"steel", [2]string{"SETS, SAMPLES", "USB"}},
	"stats":   {"steel", [2]string{"CPU, MEMORY", "TEMPERATURE"}},
	"midi":    {"steel", [2]string{"MONITOR", "INTERCEPT"}},
	"browser": {"lime", [2]string{"PRESETS AND", "SAMPLES"}},
	"plugins": {"teal", [2]string{"VST3 IN LIVE", ""}},
	"catalog": {"violet", [2]string{"INSTALL AND", "UPDATE HACKS"}},
}

// homeEntries is what Home lists: every app (an installed, enabled hack
// declaring "surface") and every tool the Shadow UI shows, the superstar
// first, then the starred, then the rest in the user's order.
func homeEntries() []homeEntry {
	tools := map[string]bool{}
	for _, t := range activeShadowTabs() {
		tools[t.ID] = true
	}
	var out []homeEntry
	for _, e := range resolveUIEntries() {
		he := homeEntry{ID: e.ID, Label: e.Label, Star: e.Star, Super: e.Super}
		switch {
		case e.HasApp && e.Available:
			he.App, he.Color = true, e.Color
		case tools[e.ID]:
			look, ok := toolLooks[e.ID]
			if !ok {
				look.color = "gray_blue"
			}
			he.Color, he.About = look.color, look.about
		default:
			continue
		}
		out = append(out, he)
	}
	rank := func(e homeEntry) int {
		switch {
		case e.Super:
			return 0
		case e.Star:
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// setStarPref stars or unstars an entry, and makes it the superstar or
// not; a superstar is always starred, and there is only one. Saved with the
// order in ui_tabs.json.
func setStarPref(id string, star, super bool) {
	entries := resolveUIEntries()
	prefs := make([]uiPref, 0, len(entries))
	for _, e := range entries {
		p := uiPref{ID: e.ID, Shadow: e.Shadow, Web: e.Web, Star: e.Star, Super: e.Super}
		if super {
			p.Super = false
		}
		if e.ID == id {
			p.Star, p.Super = star || super, super
		}
		prefs = append(prefs, p)
	}
	uiTabsMu.Lock()
	uiPrefs = prefs
	uiTabsMu.Unlock()
	if err := saveUITabs(prefs); err != nil {
		log.Printf("ui_tabs: save stars: %v", err)
	}
}
