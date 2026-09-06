package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeHack(t *testing.T, dir, id, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id, "hack.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The whole point of ui_tabs.go: saved order/switches win, an unknown entry
// still appears (switched on, at the end), a stale one is dropped, and the
// hardware only ever gets 8 tabs.
func TestResolveUIEntries(t *testing.T) {
	dir := t.TempDir()
	writeHack(t, dir, "automation", `{"id":"automation","name":"Automation","port":7703,
		"web_ui":{"label":"Automation","path":"/"},
		"shadow_ui":{"label":"AUTO","path":"/api/shadow"}}`)
	writeHack(t, dir, "push-display", `{"id":"push-display","name":"Push Display","port":0}`)

	hacksDir = dir
	uiTabsPath = filepath.Join(dir, "ui_tabs.json")
	defer func() { hacksDir = "/data/push-hack/hacks" }()

	uiPrefs = []uiPref{
		{ID: "catalog", Shadow: true, Web: true},
		{ID: "files", Shadow: false, Web: true},
		{ID: "long-gone", Shadow: true, Web: true},
	}
	defer func() { uiPrefs = nil }()

	got := resolveUIEntries()
	if got[0].ID != "catalog" || got[1].ID != "files" {
		t.Fatalf("saved order not applied: %s, %s", got[0].ID, got[1].ID)
	}
	if got[1].Shadow {
		t.Error("files should be switched off for shadow")
	}
	for _, e := range got {
		if e.ID == "long-gone" {
			t.Error("stale config entry survived")
		}
	}

	byID := map[string]uiEntry{}
	for _, e := range got {
		byID[e.ID] = e
	}
	if _, ok := byID["push-display"]; ok {
		t.Error("a hack with neither web_ui nor shadow_ui should not be an entry")
	}
	auto, ok := byID["automation"]
	if !ok {
		t.Fatal("automation missing")
	}
	if !auto.HasShadow || !auto.HasWeb || !auto.Shadow || !auto.Web {
		t.Errorf("a hack unseen by the config should appear with both switches on: %+v", auto)
	}
	if auto.Label != "AUTO" || auto.WebLabel != "Automation" {
		t.Errorf("shadow_ui label should win on the tab strip: %+v", auto)
	}
	if br := byID["browser"]; br.Available {
		t.Error("browser tab should be unavailable without browser-bridge installed")
	}

	// The built-in CATALOG entry carries the menu-bar link itself, so the
	// link survives an installed catalog that never declared web_ui.
	cat := byID["catalog"]
	if !cat.HasWeb || cat.Port != catalogPort || cat.WebLabel != "Catalog" {
		t.Errorf("catalog entry should carry its own web link: %+v", cat)
	}

	// Only enabled + available entries reach the hardware, capped at 8.
	for _, e := range activeShadowTabs() {
		if e.ID == "files" || e.ID == "browser" {
			t.Errorf("%s should not be an active tab", e.ID)
		}
	}
	if n := len(activeShadowTabs()); n > maxShadowTabs {
		t.Errorf("got %d active tabs, max is %d", n, maxShadowTabs)
	}
}

// Two hacks on one port: only one can bind it, so both get flagged and the
// full map is reported. A built-in pointing at the same port is not a clash
// — it binds nothing.
func TestPortConflicts(t *testing.T) {
	dir := t.TempDir()
	writeHack(t, dir, "keyboard-visualizer", `{"id":"keyboard-visualizer","name":"KV","port":7705,
		"web_ui":{"label":"KV","path":"/"}}`)
	writeHack(t, dir, "push-store", `{"id":"push-store","name":"Store","port":7705,
		"web_ui":{"label":"Store","path":"/"}}`)
	writeHack(t, dir, "screensaver", `{"id":"screensaver","name":"Saver","port":7706,
		"web_ui":{"label":"Saver","path":"/"}}`)
	// Same port as the built-in CATALOG entry points at — not a conflict.
	writeHack(t, dir, "push-catalog", `{"id":"push-catalog","name":"Catalog","port":7702,
		"web_ui":{"label":"Catalog","path":"/"}}`)

	hacksDir = dir
	uiTabsPath = filepath.Join(dir, "ui_tabs.json")
	defer func() { hacksDir = "/data/push-hack/hacks" }()

	conf := portConflicts()
	if len(conf) != 1 || len(conf[7705]) != 2 {
		t.Fatalf("want exactly one conflict on 7705, got %v", conf)
	}

	byID := map[string]uiEntry{}
	for _, e := range resolveUIEntries() {
		byID[e.ID] = e
	}
	if got := byID["keyboard-visualizer"].PortConflict; len(got) != 1 || got[0] != "push-store" {
		t.Errorf("keyboard-visualizer should name its rival, got %v", got)
	}
	if got := byID["push-store"].PortConflict; len(got) != 1 || got[0] != "keyboard-visualizer" {
		t.Errorf("conflict should be reported on both sides, got %v", got)
	}
	if got := byID["screensaver"].PortConflict; len(got) != 0 {
		t.Errorf("screensaver is alone on 7706, got %v", got)
	}
	if got := byID["push-catalog"].PortConflict; len(got) != 0 {
		t.Errorf("a built-in pointing at 7702 binds nothing, got %v", got)
	}
	if !byID["catalog"].HasWeb {
		t.Error("built-in catalog entry lost its web link")
	}
}
