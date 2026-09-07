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
	initdDir = filepath.Join(dir, "init.d") // doesn't exist: everything reads as enabled
	rcdGlob = filepath.Join(dir, "rc*.d")
	defer func() {
		hacksDir = "/data/push-hack/hacks"
		initdDir, rcdGlob = "/etc/init.d", "/etc/rc*.d"
	}()

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
	for _, h := range got {
		if !h.Enabled {
			t.Errorf("%s should read as enabled: no /etc/init.d service to check in this test env", h.ID)
		}
	}
}

// hackEnabled mirrors push-catalog.sh's own semantics: a hack with no
// init.d service at all is always enabled; one with a service is enabled
// only if it has a boot-autostart rc<N>.d symlink.
func TestHackEnabled(t *testing.T) {
	dir := t.TempDir()
	initdDir = filepath.Join(dir, "init.d")
	rcdGlob = filepath.Join(dir, "rc*.d")
	defer func() { initdDir, rcdGlob = "/etc/init.d", "/etc/rc*.d" }()

	if !hackEnabled("no-service") {
		t.Error("a hack with no init.d service should read as enabled")
	}

	if err := os.MkdirAll(initdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(initdDir, "push-hack-disabled-one"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if hackEnabled("disabled-one") {
		t.Error("a service with no rc.d symlink should read as disabled")
	}

	if err := os.MkdirAll(filepath.Join(dir, "rc2.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(initdDir, "push-hack-enabled-one"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rc2.d", "S20push-hack-enabled-one"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hackEnabled("enabled-one") {
		t.Error("a service with an rc.d symlink should read as enabled")
	}
}
