package livedevice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func params(n int) []Param {
	p := make([]Param, n)
	for i := range p {
		p[i] = Param{Name: string(rune('a' + i)), Type: "float", Max: 1}
	}
	return p
}

func TestPadBanksFillsPages(t *testing.T) {
	c := &Class{Params: params(13), Banks: [][]int{{0, 1, 2}, {3, 4, 5, 6, 7, 8, 9, 10, 11}, {0, 3}, {12}}}
	PadBanks(c)
	// {0,3} empties (both already listed) and goes; the last bank is not padded
	if len(c.Banks) != 3 || len(c.Banks[0]) != 8 || len(c.Banks[1]) != 16 || len(c.Banks[2]) != 1 {
		t.Fatalf("banks %v", c.Banks)
	}
	seen := map[int]bool{}
	for _, b := range c.Banks {
		for _, i := range b {
			if seen[i] {
				t.Fatalf("param %d listed twice", i)
			}
			seen[i] = true
		}
	}
	if len(c.Params) != 13+5+7 || c.Params[13].Type != PadType || c.Params[24].Type != PadType {
		t.Fatalf("%d params", len(c.Params))
	}
}

func TestPadBanksAddsUnbanked(t *testing.T) {
	c := &Class{Params: params(11), Banks: [][]int{{0, 1}}}
	PadBanks(c)
	// {0,1}+6 pads, then 2..9, then 10
	if len(c.Banks) != 3 || len(c.Banks[0]) != 8 || len(c.Banks[1]) != 8 || c.Banks[2][0] != 10 {
		t.Fatalf("banks %v", c.Banks)
	}
}

func TestClassUID(t *testing.T) {
	// a UID from before core had this, pinned: Sets save UIDs, so the formula
	// never changes (the input is the namespace the first hack used then)
	if got := ClassUID("schwung-vst3", "instrument", "braids"); got != "0412705452DFA12812E1640E193F1E8E" {
		t.Fatalf("uid %s", got)
	}
}

func TestCheck(t *testing.T) {
	ok := &Class{UID: ClassUID("x", Instrument, "a"), Name: "A", Kind: Instrument}
	if err := ok.Check(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*Class{
		{UID: "abc", Name: "A", Kind: Instrument},
		{UID: ok.UID, Kind: Instrument},
		{UID: ok.UID, Name: "A", Kind: "synth"},
		{UID: ok.UID, Name: "A", Kind: Effect, Params: params(MaxParams + 1)},
	} {
		if c.Check() == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}

func TestWriteClassesTouchesOnlyOnChange(t *testing.T) {
	dir := t.TempDir()
	so := filepath.Join(dir, "x.so")
	os.WriteFile(so, nil, 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(so, old, old)
	path := filepath.Join(dir, "classes.json")
	cls := []*Class{{UID: ClassUID("x", Instrument, "a"), Name: "A", Kind: Instrument, Params: params(2)}}
	changed, err := WriteClasses(path, so, cls)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	st, _ := os.Stat(so)
	if !st.ModTime().After(old) {
		t.Fatal("binary not touched")
	}
	os.Chtimes(so, old, old)
	if changed, _ := WriteClasses(path, so, cls); changed {
		t.Fatal("rewrote the same classes")
	}
	if st, _ := os.Stat(so); !st.ModTime().Equal(old) {
		t.Fatal("touched with no change")
	}
	var doc ClassFile
	raw, _ := os.ReadFile(path)
	if json.Unmarshal(raw, &doc) != nil || doc.Version != 1 || len(doc.Classes) != 1 {
		t.Fatalf("file %s", raw)
	}
}

func TestInstallBundle(t *testing.T) {
	dir := t.TempDir()
	so := filepath.Join(dir, "livedevice.so")
	os.WriteFile(so, []byte("ELF"), 0o755)
	vst3 := filepath.Join(dir, "vst3")
	path, cfg, changed, err := InstallBundle(vst3, "My Hack", so, BundleConfig{Vendor: "Me", Socket: "/s"})
	if err != nil || !changed || path != filepath.Join(vst3, "My Hack.vst3", "Contents", "x86_64-linux", "My Hack.so") {
		t.Fatal(path, changed, err)
	}
	if cfg.Classes != ClassesPath(vst3, "My Hack") {
		t.Fatal("class file not in the bundle:", cfg.Classes)
	}
	raw, _ := os.ReadFile(filepath.Join(vst3, "My Hack.vst3", "Contents", "Resources", "livedevice.json"))
	var got BundleConfig
	if json.Unmarshal(raw, &got) != nil || got.Classes != cfg.Classes || got.Socket != "/s" {
		t.Fatalf("config %s", raw)
	}
	if _, _, changed, _ := InstallBundle(vst3, "My Hack", so, BundleConfig{Vendor: "Me", Socket: "/s"}); changed {
		t.Fatal("reinstalled an unchanged bundle")
	}
	if _, _, _, err := InstallBundle(vst3, "../evil", so, BundleConfig{Socket: "/s"}); err == nil {
		t.Fatal("accepted a bad name")
	}
}

// A hack that renames its browser folders: the old ones it listed go (their
// entries, and the folder once empty); a file it did not write stays.
func TestInstallBundleRetiresRenamedPresetFolders(t *testing.T) {
	dir := t.TempDir()
	so := filepath.Join(dir, "livedevice.so")
	os.WriteFile(so, []byte("ELF"), 0o755)
	vst3 := filepath.Join(dir, "vst3")
	oldDir, keepDir, newDir := filepath.Join(dir, "p", "Old"), filepath.Join(dir, "p", "Shared"), filepath.Join(dir, "p", "New")
	for _, d := range []string{oldDir, keepDir} {
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "A.adg"), []byte("x"), 0o644)
	}
	if _, _, _, err := InstallBundle(vst3, "H", so, BundleConfig{Socket: "/s", Presets: []string{oldDir, keepDir}}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := InstallBundle(vst3, "H", so, BundleConfig{Socket: "/s", Presets: []string{keepDir, newDir}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("the renamed folder is still there")
	}
	if _, err := os.Stat(filepath.Join(keepDir, "A.adg")); err != nil {
		t.Fatal("a folder still listed lost its entry")
	}
	// a folder holding something the hack did not write keeps it
	os.MkdirAll(newDir, 0o755)
	os.WriteFile(filepath.Join(newDir, "mine.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(newDir, "B.adg"), []byte("x"), 0o644)
	InstallBundle(vst3, "H", so, BundleConfig{Socket: "/s", Presets: []string{keepDir}})
	if _, err := os.Stat(filepath.Join(newDir, "mine.txt")); err != nil {
		t.Fatal("removed a file it did not write")
	}
	if _, err := os.Stat(filepath.Join(newDir, "B.adg")); !os.IsNotExist(err) {
		t.Fatal("left its own entry")
	}
}
