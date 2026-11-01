// Covers what breaks silently: the list_plugins JSON contract with Browser
// Bridge, folder navigation (open a folder, back restores the position, back
// at the top does nothing), and the preset level (open a plugin, row 0 is the
// default patch).
package main

import (
	"encoding/json"
	"os"
	"testing"

	"push-manager/plugintree"
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
		t.Fatalf("json tags out of sync with PushHack list_plugins: %+v", r.Plugins)
	}
}

func node(name string, folders []*plugintree.Node, plugins ...plugintree.Plugin) *plugintree.Node {
	if folders == nil {
		folders = []*plugintree.Node{}
	}
	return &plugintree.Node{Name: name, Folders: folders, Plugins: plugins}
}

func TestPluginsPanelNavigation(t *testing.T) {
	var ps []plugintree.Plugin
	for i := 0; i < 10; i++ {
		ps = append(ps, plugintree.Plugin{Name: "P"})
	}
	ps[7].Presets = []string{"a", "b"}
	p := &PluginsPanel{root: node("", nil, ps...)}

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

func folderPanel() *PluginsPanel {
	inner := node("Reverb", nil,
		plugintree.Plugin{Name: "R1"}, plugintree.Plugin{Name: "R2", Presets: []string{"x"}})
	aw := node("Airwindows", []*plugintree.Node{inner}, plugintree.Plugin{Name: "Loose"})
	return &PluginsPanel{root: node("", []*plugintree.Node{aw}, plugintree.Plugin{Name: "Top"})}
}

func TestPluginsPanelFoldersListedBeforePlugins(t *testing.T) {
	p := folderPanel()
	if n := p.rowCount(); n != 2 {
		t.Fatalf("rowCount=%d want 2 (one folder, one plugin)", n)
	}
	p.enter(false) // row 0 is the folder
	if len(p.trail) != 1 || p.trail[0] != "Airwindows" || p.cursor != 0 {
		t.Fatalf("folder not opened: trail=%v cursor=%d", p.trail, p.cursor)
	}
	if n := p.rowCount(); n != 2 { // Reverb folder + Loose
		t.Fatalf("rowCount in Airwindows=%d want 2", n)
	}
}

func TestPluginsPanelOpenFolderAndBack(t *testing.T) {
	p := folderPanel()
	p.cursor, p.scroll = 0, 0
	p.enter(false) // Airwindows
	p.enter(false) // Reverb
	if got := crumb(p.trail); got != "Plugins / Airwindows / Reverb" {
		t.Fatalf("crumb %q", got)
	}
	p.moveCursor(1)
	p.back()
	if len(p.trail) != 1 || p.cursor != 0 {
		t.Fatalf("back one level: trail=%v cursor=%d", p.trail, p.cursor)
	}
	p.back()
	if len(p.trail) != 0 || p.cursor != 0 {
		t.Fatalf("back to top: trail=%v cursor=%d", p.trail, p.cursor)
	}
}

func TestPluginsPanelBackAtRootDoesNothing(t *testing.T) {
	p := folderPanel()
	p.moveCursor(1)
	p.back()
	if len(p.trail) != 0 || p.cursor != 1 || p.inPresets {
		t.Fatalf("trail=%v cursor=%d inPresets=%v", p.trail, p.cursor, p.inPresets)
	}
}

func TestPluginsPanelLoadButtonOnFolderOpens(t *testing.T) {
	p := folderPanel()
	p.enter(true) // soft button Load on a folder row
	if len(p.trail) != 1 || p.busy {
		t.Fatalf("Load on a folder must open it: trail=%v busy=%v", p.trail, p.busy)
	}
}

func TestPluginsPanelPresetsInsideFolder(t *testing.T) {
	p := folderPanel()
	p.enter(false) // Airwindows
	p.enter(false) // Reverb
	p.moveCursor(1)
	p.enter(false) // R2 has a preset: opens
	if !p.inPresets || p.pluginIdx != 1 || p.rowCount() != 2 {
		t.Fatalf("inPresets=%v idx=%d rows=%d", p.inPresets, p.pluginIdx, p.rowCount())
	}
	p.back()
	if p.inPresets || p.cursor != 1 || len(p.trail) != 2 {
		t.Fatalf("back from presets: inPresets=%v cursor=%d trail=%v", p.inPresets, p.cursor, p.trail)
	}
}

func TestPluginsPanelBreadcrumbASCII(t *testing.T) {
	long := []string{"Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "Bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "Cc"}
	for _, trail := range [][]string{nil, {"A"}, long} {
		s := crumb(trail)
		if len(s) > 60 {
			t.Errorf("crumb too long: %d", len(s))
		}
		for i := 0; i < len(s); i++ {
			if s[i] < 0x20 || s[i] > 0x7e {
				t.Fatalf("non-ASCII byte in %q", s)
			}
		}
	}
	if s := crumb(long); s[len(s)-2:] != "Cc" {
		t.Fatalf("the end of the path must stay visible: %q", s)
	}
}

func TestPluginsPanelCrumbNeverBreaksUTF8(t *testing.T) {
	// A vendor folder name comes from Live and may hold any character. A cut
	// inside a UTF-8 sequence would draw a box on the panel.
	trail := []string{"\u00dcnder Audio \u00dcnder Audio \u00dcnder Audio", "\u00dcnder Audio \u00dcnder Audio \u00dcnder"}
	s := crumb(trail)
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			t.Fatalf("non-ASCII byte at %d in %q", i, s)
		}
	}
}

func TestLoadPluginTreeNoteIsASCII(t *testing.T) {
	path := useTempFolders(t)
	os.WriteFile(path, []byte(`{"folders":[{"name":"caf\u00e9","plugins":["A"]}]}`), 0o644)
	_, note, err := loadPluginTree()
	if err != nil || note == "" {
		t.Fatalf("note %q err %v", note, err)
	}
	for i := 0; i < len(note); i++ {
		if note[i] < 0x20 || note[i] > 0x7e {
			t.Fatalf("non-ASCII byte in note %q", note)
		}
	}
}

// refresh() must repair every index the old tree left behind.
func TestPluginsPanelRefreshResetsStaleState(t *testing.T) {
	useTempFolders(t) // plugins A and B, no folders file: vendor folder "Acme"
	p := &PluginsPanel{
		root:      node("", nil),
		trail:     []string{"Gone"},
		saved:     []navPos{{3, 1}},
		inPresets: true,
		pluginIdx: 5,
		cursor:    9,
		scroll:    4,
	}
	p.refresh()
	if len(p.trail) != 0 || len(p.saved) != 0 || p.inPresets || p.cursor != 0 || p.scroll != 0 {
		t.Fatalf("stale trail not reset: trail=%v saved=%v inPresets=%v cursor=%d scroll=%d",
			p.trail, p.saved, p.inPresets, p.cursor, p.scroll)
	}
	if p.rowCount() != 1 { // the Acme vendor folder
		t.Fatalf("rowCount=%d want 1", p.rowCount())
	}
}

func TestPluginsPanelRefreshClampsPluginIdxAndCursor(t *testing.T) {
	useTempFolders(t)
	p := &PluginsPanel{inPresets: true, pluginIdx: 5, cursor: 7, root: node("", nil)}
	p.refresh() // root has Acme only; trail is empty so the trail is fine
	if p.inPresets {
		t.Fatal("pluginIdx past the end must leave the preset level")
	}
	if p.cursor >= p.rowCount() {
		t.Fatalf("cursor %d past %d rows", p.cursor, p.rowCount())
	}
}
