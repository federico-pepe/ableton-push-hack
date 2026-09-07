package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A hack gets a menu-bar link by declaring web_ui in its own hack.json —
// this pins that decode, incl. "no web_ui = no link" and skipping junk.
func TestInstalledHacks(t *testing.T) {
	dir := t.TempDir()
	write := func(id, body string) {
		if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id, "hack.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("push-catalog", `{"id":"push-catalog","name":"Push Hack Catalog","port":7702,
		"web_ui":{"label":"Catalog","path":"/"}}`)
	write("browser-bridge", `{"id":"browser-bridge","name":"Browser Bridge","port":7704}`)
	write("broken", `not json`)

	hacksDir = dir
	defer func() { hacksDir = "/data/push-hack/hacks" }()

	got := installedHacks()
	if len(got) != 2 {
		t.Fatalf("want 2 hacks, got %d: %+v", len(got), got)
	}
	if got[0].ID != "browser-bridge" || got[0].WebUI != nil {
		t.Errorf("browser-bridge should sort first and have no web_ui: %+v", got[0])
	}
	if got[1].Port != 7702 || got[1].WebUI == nil || got[1].WebUI.Label != "Catalog" {
		t.Errorf("push-catalog web_ui not decoded: %+v", got[1])
	}
	if !hackInstalled("push-catalog") || hackInstalled("automation") {
		t.Error("hackInstalled disagrees with what's on disk")
	}
}
