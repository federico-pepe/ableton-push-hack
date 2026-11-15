package livedevice

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One rack template for push-hack: core's copy must be the scripts' one.
func TestTemplateIsScriptsTemplate(t *testing.T) {
	theirs, err := os.ReadFile("../../scripts/VST/templates/vst3-instrument.adg.xml")
	if err != nil {
		t.Skip("not in a push-hack checkout")
	}
	if string(theirs) != instrumentTemplate {
		t.Fatal("core/livedevice/templates differs from scripts/VST/templates: copy it over")
	}
}

func TestUIDFields(t *testing.T) {
	// python: struct.unpack(">4i", bytes.fromhex("0412705452DFA12812E1640E193F1E8E"))
	f, err := uidFields("0412705452DFA12812E1640E193F1E8E")
	if err != nil || f != [4]int32{68317268, 1390387496, 316761102, 423566990} {
		t.Fatal(f, err)
	}
	f, _ = uidFields("FFFFFFFF000000000000000000000001")
	if f[0] != -1 || f[3] != 1 {
		t.Fatal(f)
	}
}

func testClass(kind string) *Class {
	c := &Class{UID: ClassUID("test", kind, "x"), Name: "Test <X> & Co", Kind: kind,
		Params: []Param{{Name: "A", Type: "float", Max: 1}, {Name: "B", Type: "enum", Options: []string{"x", "y"}, Max: 1}},
		Banks:  [][]int{{1}, {0, 1}}}
	PadBanks(c)
	return c
}

func TestRenderInstrument(t *testing.T) {
	xml, err := RenderPreset(testClass(Instrument))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<InstrumentGroupDevice", `<Name Value="Test &lt;X> &amp; Co" />`,
		`<ParameterId Value="1" />`, `<Type Value="PluginEnumParameter" />`, `<DeviceType Value="1" />`} {
		if !strings.Contains(xml, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(xml, "@") {
		t.Error("a placeholder is left")
	}
	// B once, first (bank 1), then 7 placeholders, then A
	if strings.Count(xml, `<ParameterId Value="1" />`) != 1 || strings.Index(xml, `<ParameterId Value="1" />`) > strings.Index(xml, `<ParameterId Value="0" />`) {
		t.Error("parameter order")
	}
	if strings.Count(xml, "<PluginParameterSettings ") != 9 {
		t.Errorf("%d parameters mapped", strings.Count(xml, "<PluginParameterSettings "))
	}
}

func TestRenderEffect(t *testing.T) {
	xml, _ := RenderPreset(testClass(Effect))
	for _, gone := range []string{"Instrument", "ChainSelectorFilterMidiCtrl", "RangeTypeIndex", "ShowsZonesInsteadOfNoteNames", "<ZoneSettings>", `<DeviceType Value="1" />`} {
		if strings.Contains(xml, gone) {
			t.Errorf("still has %s", gone)
		}
	}
	for _, want := range []string{"<AudioEffectGroupDevice", "<AudioEffectBranchPreset", `<DeviceType Value="2" />`, `<MpeEnabled Value="0" />`} {
		if !strings.Contains(xml, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestWritePresets(t *testing.T) {
	dir := t.TempDir()
	inst, fx, midi := testClass(Instrument), testClass(Effect), testClass(MIDIEffect)
	fx.Name = "Verb"
	gone := testClass(Instrument)
	gone.Name = "Gone"
	if n, err := WritePresets(dir, "Vend", []*Class{gone}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	stale := filepath.Join(dir, "Instruments", "Vend", "Gone.adg")
	n, err := WritePresets(dir, "Vend", []*Class{inst, fx, midi})
	if err != nil || n != 3 { // 2 written, 1 removed
		t.Fatal(n, err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("stale entry kept")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "Audio Effects", "Vend", "Verb.adg"))
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := gzip.NewReader(bytes.NewReader(raw))
	xml, _ := io.ReadAll(zr)
	if !bytes.Contains(xml, []byte("AudioEffectGroupDevice")) {
		t.Fatal("not an effect rack")
	}
	if n, _ := WritePresets(dir, "Vend", []*Class{inst, fx, midi}); n != 0 {
		t.Fatalf("rewrote %d unchanged entries", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "Instruments", "Vend", "Test _X_ _ Co.adg")); err != nil {
		t.Fatal("instrument entry:", err)
	}
}

// A rack Live saved in a hack's folder (Push's Save puts a device preset
// beside the rack it came from) is the user's: WritePresets never removes it,
// with a manifest or without one (a folder an older core wrote).
func TestWritePresetsKeepsUserRacks(t *testing.T) {
	inst := testClass(Instrument)
	saved := func(path, state string) {
		x, err := RenderPreset(inst)
		if err != nil {
			t.Fatal(err)
		}
		x = strings.Replace(x, "<ProcessorState />", "<ProcessorState>"+state+"</ProcessorState>", 1)
		data, _ := gzipBytes([]byte(x))
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, data, 0o644)
	}
	bare := func(path string, c *Class) {
		x, _ := RenderPreset(c)
		data, _ := gzipBytes([]byte(x))
		os.WriteFile(path, data, 0o644)
	}
	for _, withManifest := range []bool{false, true} {
		dir := t.TempDir()
		folder := filepath.Join(dir, "Instruments", "Vend")
		mine := filepath.Join(folder, "My Sound.adg")
		saved(mine, "0A000000")
		junk := filepath.Join(folder, "Not a rack.adg")
		os.WriteFile(junk, []byte("x"), 0o644)
		old := filepath.Join(folder, "Old.adg") // a class's entry an older core wrote
		oc := testClass(Instrument)
		oc.Name = "Old"
		bare(old, oc)
		if withManifest {
			os.WriteFile(filepath.Join(folder, PresetManifest), []byte(`["Old.adg"]`), 0o644)
		}
		if _, err := WritePresets(dir, "Vend", []*Class{inst}); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{mine, junk} {
			if _, err := os.Stat(p); err != nil {
				t.Fatalf("manifest %v: removed the user's %s", withManifest, filepath.Base(p))
			}
		}
		if _, err := os.Stat(old); err == nil {
			t.Fatalf("manifest %v: kept the entry of a class that is gone", withManifest)
		}
		raw, _ := os.ReadFile(filepath.Join(folder, PresetManifest))
		if string(raw) != `["Test _X_ _ Co.adg"]` {
			t.Fatalf("manifest %s", raw)
		}
	}
	// With a manifest, a bare rack it does not list is not WritePresets' either.
	dir := t.TempDir()
	folder := filepath.Join(dir, "Instruments", "Vend")
	WritePresets(dir, "Vend", []*Class{inst})
	other := testClass(Instrument)
	other.Name = "Other"
	bare(filepath.Join(folder, "Other.adg"), other)
	WritePresets(dir, "Vend", []*Class{inst})
	if _, err := os.Stat(filepath.Join(folder, "Other.adg")); err != nil {
		t.Fatal("removed a rack the manifest does not list")
	}
}

func TestPresetFolders(t *testing.T) {
	inst, fx, midi := testClass(Instrument), testClass(Effect), testClass(MIDIEffect)
	trk := testClass(MIDI)
	trk.Folder = "Vend MIDI"
	got := PresetFolders("/p", "Vend", []*Class{inst, fx, midi, trk, inst})
	want := []string{"/p/Audio Effects/Vend", "/p/Instruments/Vend", "/p/Instruments/Vend MIDI"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatal(got)
	}
}
