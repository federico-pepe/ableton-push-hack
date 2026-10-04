// Covers what breaks silently: the list_plugins JSON contract with Browser
// Bridge, and the two-level navigation (open a plugin, back restores the list
// position, row 0 of the preset level is the default patch).
package main

import (
	"encoding/json"
	"testing"
)

func TestPluginsReplyParse(t *testing.T) {
	const j = `{"plugins":[{"vendor":"Surge Synth Team","name":"Surge XT","presets":["Surge-Bass1"]}]}`
	var r struct {
		Plugins []bridgePlugin `json:"plugins"`
	}
	if err := json.Unmarshal([]byte(j), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Plugins) != 1 || r.Plugins[0].Name != "Surge XT" || r.Plugins[0].Vendor != "Surge Synth Team" ||
		len(r.Plugins[0].Presets) != 1 || r.Plugins[0].Presets[0] != "Surge-Bass1" {
		t.Fatalf("json tags out of sync with Browser Bridge list_plugins: %+v", r.Plugins)
	}
}

func TestPluginsPanelNavigation(t *testing.T) {
	p := &PluginsPanel{plugins: make([]bridgePlugin, 10)}
	for i := range p.plugins {
		p.plugins[i].Name = "P"
	}
	p.plugins[7].Presets = []string{"a", "b"}

	for i := 0; i < 7; i++ {
		p.moveCursor(1)
	}
	if want := 7 - pluginsVisibleRows + 1; p.cursor != 7 || p.scroll != want {
		t.Fatalf("cursor=%d scroll=%d, want 7 and %d", p.cursor, p.scroll, want)
	}

	p.enter(false) // plugin 7 has presets: opens, does not load
	if !p.inPresets || p.pluginIdx != 7 || p.cursor != 0 {
		t.Fatalf("open failed: inPresets=%v idx=%d cursor=%d", p.inPresets, p.pluginIdx, p.cursor)
	}
	if n := p.rowCount(); n != 3 {
		t.Fatalf("rowCount=%d want 3 (default patch + 2 presets)", n)
	}
	for i := 0; i < 9; i++ {
		p.moveCursor(1)
	}
	if p.cursor != 2 {
		t.Fatalf("preset level bottom clamp: cursor=%d want 2", p.cursor)
	}

	p.back()
	if p.inPresets || p.cursor != 7 || p.scroll != 7-pluginsVisibleRows+1 {
		t.Fatalf("back did not restore the list: inPresets=%v cursor=%d scroll=%d", p.inPresets, p.cursor, p.scroll)
	}
}

func TestPluginLoadRejectsColon(t *testing.T) {
	if err := livePluginLoad("a:b", ""); err == nil {
		t.Fatal("a ':' in a plugin name must be refused, it would split the command")
	}
	if err := livePluginLoad("a", "b:c"); err == nil {
		t.Fatal("a ':' in a preset name must be refused")
	}
}
