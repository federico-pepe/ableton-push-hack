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
