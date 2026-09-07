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
	"strconv"
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

	HasWeb   bool   `json:"has_web"`
	Web      bool   `json:"web"`       // switch: show in the web menu bar
	WebLabel string `json:"web_label,omitempty"`
	WebPath  string `json:"web_path,omitempty"`
	Port     int    `json:"port,omitempty"`

	// PortConflict names the other installed hacks claiming this entry's
	// port. Non-empty means at most one of them is actually reachable —
	// see portConflicts.
	PortConflict []string `json:"port_conflict,omitempty"`

	shadowPath string       // remote tabs only
	new        func() Panel // built-in tabs only
}

// uiPref is the persisted half: id + both switches, ordered.
type uiPref struct {
	ID     string `json:"id"`
	Shadow bool   `json:"shadow"`
	Web    bool   `json:"web"`
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

// warnPortConflicts logs any port claimed by more than one installed hack.
// Cheap, once, at startup — a hack that silently never answers because a
// sibling took its port is otherwise a miserable thing to diagnose.
func warnPortConflicts() {
	for port, ids := range portConflicts() {
		log.Printf("ui_tabs: port %d claimed by %v - only one of them can be running", port, ids)
	}
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
		if h.Port == 0 || (h.WebUI == nil && h.ShadowUI == nil) || !h.Enabled {
			continue
		}
		e := uiEntry{ID: h.ID, Label: h.Name, Source: "hack", Port: h.Port, Available: true}
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
	for _, p := range prefs {
		e, ok := byID[p.ID]
		if !ok || seen[p.ID] {
			continue // stale config entry: hack removed, or duplicate
		}
		e.Shadow, e.Web = p.Shadow, p.Web
		seen[p.ID] = true
		out = append(out, e)
	}
	for _, e := range cand {
		if !seen[e.ID] {
			out = append(out, byID[e.ID])
		}
	}
	annotatePortConflicts(out)
	return out
}

// portConflicts groups installed, enabled hacks by port, keeping only the
// ports more than one hack claims. Only one process can bind a port, so a
// conflict means at least one of those hacks is not running — a link or a
// remote tab pointing at it reaches the wrong hack, or nothing. A disabled
// hack isn't a party to this: its service isn't autostarted, so it isn't
// bound to anything and doesn't turn its port-mate into a false positive.
// Beyond that, up-ness isn't checked: that would be a poller, and the
// hack.json + boot-autostart state on disk is enough to tell the user what
// to fix.
func portConflicts() map[int][]string {
	byPort := map[int][]string{}
	for _, h := range installedHacks() {
		if h.Port != 0 && h.Enabled {
			byPort[h.Port] = append(byPort[h.Port], h.ID)
		}
	}
	for port, ids := range byPort {
		if len(ids) < 2 {
			delete(byPort, port)
		}
	}
	return byPort
}

// annotatePortConflicts tags each hack-backed entry with the other hacks on
// its port. Built-ins are skipped: they bind nothing, they only point at a
// port, so the CATALOG entry sharing 7702 with the catalog hack itself is
// the normal case rather than a clash.
func annotatePortConflicts(entries []uiEntry) {
	conf := portConflicts()
	if len(conf) == 0 {
		return
	}
	for i, e := range entries {
		if e.Source != "hack" || e.Port == 0 {
			continue
		}
		for _, id := range conf[e.Port] {
			if id != e.ID {
				entries[i].PortConflict = append(entries[i].PortConflict, id)
			}
		}
	}
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

// uiTabsResponse is what both verbs return. port_conflicts is keyed by port
// (as a string, since JSON object keys are) and covers *every* installed
// hack, including ones with no nav hooks — a clash there is still worth
// showing on the one page that lists what is installed.
func uiTabsResponse() map[string]any {
	conf := map[string][]string{}
	for port, ids := range portConflicts() {
		conf[strconv.Itoa(port)] = ids
	}
	return map[string]any{
		"entries":        resolveUIEntries(),
		"max":            maxShadowTabs,
		"port_conflicts": conf,
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
