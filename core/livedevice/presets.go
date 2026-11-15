package livedevice

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

// UserPresets is the Presets folder of Live's User Library on Push. Push's
// browser lists racks saved there; it does not list plugins.
const UserPresets = "/data/Music/Ableton/User Library/Presets"

// The rack template push-hack's make-vst3-preset.py uses (scripts/VST/templates),
// kept the same by presets_test.go.
//
//go:embed templates/vst3-instrument.adg.xml
var instrumentTemplate string

// WritePresets writes one browser entry (a rack holding the plugin, with its
// parameter map in bank order) per class into presets (normally
// UserPresets):
//
//	instrument, midi: Instruments/<folder>/<name>.adg   (an Instrument Rack)
//	effect:           Audio Effects/<folder>/<name>.adg (an Audio Effect Rack)
//
// <folder> is the class's Folder, else vendor. A MIDIEffect class gets no
// entry (Live has no slot for one). An entry for a class that no longer
// exists is removed, and only that: each folder lists the entries written
// there in PresetManifest, and a rack WritePresets did not write is left
// alone. Push's Save puts a device preset beside the rack it came from, so
// the folders hold the user's racks too. New files and folders get the owner
// of presets (Live runs as ableton, a hack as root). Returns how many files
// changed.
func WritePresets(presets, vendor string, classes []*Class) (int, error) {
	if vendor == "" {
		return 0, errors.New("livedevice: WritePresets needs a vendor")
	}
	uid, gid := -1, -1
	if st, err := os.Stat(presets); err == nil {
		if s, ok := st.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(s.Uid), int(s.Gid)
		}
	}
	want := map[string]map[string]bool{} // folder -> files we write
	changed := 0
	for _, c := range classes {
		dir := presetFolder(presets, vendor, c)
		if dir == "" {
			continue
		}
		file := safeName(c.Name) + ".adg"
		if want[dir] == nil {
			want[dir] = map[string]bool{}
		}
		want[dir][file] = true
		xml, err := RenderPreset(c)
		if err != nil {
			return changed, err
		}
		data, err := gzipBytes([]byte(xml))
		if err != nil {
			return changed, err
		}
		path := filepath.Join(dir, file)
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return changed, err
		}
		if err := writeAtomic(path, data, 0o644); err != nil {
			return changed, err
		}
		if uid >= 0 {
			os.Chown(dir, uid, gid)
			os.Chown(path, uid, gid)
		}
		changed++
	}
	for dir, files := range want {
		wrote, known := readPresetManifest(dir)
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".adg") || files[name] {
				continue
			}
			path := filepath.Join(dir, name)
			// A folder from before the manifest: an entry WritePresets wrote
			// is a rack with no plugin state in it; a rack Live saved has
			// the plugin's state.
			if wrote[name] || (!known && isBareRack(path)) {
				os.Remove(path)
				changed++
			}
		}
		if err := writePresetManifest(dir, files, uid, gid); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// PresetManifest is the file, in a folder of browser entries, that lists the
// entries a hack wrote there: a JSON array of file names. WritePresets keeps
// one in each folder it writes, and push-catalog remove deletes the files it
// lists (in that folder and in any folder under it), never anything else. A
// hack that writes racks of its own in a folder under its preset folders
// lists them in a PresetManifest there.
const PresetManifest = ".livedevice-presets"

// readPresetManifest: the files a folder's manifest lists, and whether it has
// one. A damaged manifest lists nothing.
func readPresetManifest(dir string) (map[string]bool, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, PresetManifest))
	if err != nil {
		return nil, false
	}
	out := map[string]bool{}
	var files []string
	if json.Unmarshal(raw, &files) == nil {
		for _, f := range files {
			if f != "" && f != "." && f != ".." && !strings.ContainsAny(f, `/\`) {
				out[f] = true
			}
		}
	}
	return out, true
}

func writePresetManifest(dir string, files map[string]bool, uid, gid int) error {
	list := make([]string, 0, len(files))
	for f := range files {
		list = append(list, f)
	}
	sort.Strings(list)
	raw, _ := json.Marshal(list)
	path := filepath.Join(dir, PresetManifest)
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, raw) {
		return nil
	}
	if err := writeAtomic(path, raw, 0o644); err != nil {
		return err
	}
	if uid >= 0 {
		os.Chown(path, uid, gid)
	}
	return nil
}

// isBareRack: a gzipped rack whose plugins carry no state, as WritePresets
// writes them. A rack Live saves keeps each plugin's state in
// <ProcessorState>; anything that does not read as a rack is not one.
func isBareRack(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return false
	}
	xml, err := io.ReadAll(io.LimitReader(z, 16<<20))
	if err != nil {
		return false
	}
	return bytes.Contains(xml, []byte("<ProcessorState />")) && !bytes.Contains(xml, []byte("<ProcessorState>"))
}

// presetFolder: where a class's browser entry goes; "" for none.
func presetFolder(presets, vendor string, c *Class) string {
	var top string
	switch c.Kind {
	case Instrument, MIDI:
		top = "Instruments"
	case Effect:
		top = "Audio Effects"
	default:
		return ""
	}
	folder := c.Folder
	if folder == "" {
		folder = vendor
	}
	return filepath.Join(presets, top, safeName(folder))
}

// PresetFolders lists the folders WritePresets writes for these classes,
// for BundleConfig.Presets: push-catalog removes them with the hack.
func PresetFolders(presets, vendor string, classes []*Class) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range classes {
		if dir := presetFolder(presets, vendor, c); dir != "" && !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	sort.Strings(out)
	return out
}

// RenderPreset is a class's browser entry, as uncompressed XML.
func RenderPreset(c *Class) (string, error) {
	f, err := uidFields(c.UID)
	if err != nil {
		return "", err
	}
	xml := strings.Replace(instrumentTemplate, "@PARAMS@", paramsXML(c), 1)
	for i, v := range f {
		xml = strings.Replace(xml, fmt.Sprintf("@F%d@", i), fmt.Sprint(v), 1)
	}
	name := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;").Replace(c.Name)
	xml = strings.Replace(xml, "@NAME@", name, 1)
	if c.Kind == Effect {
		xml = asEffect(xml)
	}
	return xml, nil
}

// paramOrder: the rack's parameter map. Banks flattened, each parameter once
// (Live keeps a separate value per listed entry), at most 128.
func paramOrder(c *Class) []int {
	var order []int
	seen := map[int]bool{}
	for _, bank := range c.Banks {
		for _, i := range bank {
			if i >= 0 && i < len(c.Params) && !seen[i] {
				seen[i] = true
				order = append(order, i)
			}
		}
	}
	if len(order) > 128 {
		order = order[:128]
	}
	return order
}

// paramsXML: the encoder map, the same XML make-vst3-preset.py writes.
func paramsXML(c *Class) string {
	order := paramOrder(c)
	if len(order) == 0 {
		return "<ParameterSettings />"
	}
	pad := strings.Repeat("\t", 6)
	p := pad + "\t"
	var rows []string
	for i, id := range order {
		kind := "Float"
		if c.Params[id].Type == "enum" {
			kind = "Enum"
		}
		rows = append(rows, fmt.Sprintf("%[1]s<PluginParameterSettings Id=\"%[2]d\">\n"+
			"%[1]s\t<Index Value=\"%[2]d\" />\n"+
			"%[1]s\t<VisualIndex Value=\"%[2]d\" />\n"+
			"%[1]s\t<ParameterId Value=\"%[3]d\" />\n"+
			"%[1]s\t<Type Value=\"Plugin%[4]sParameter\" />\n"+
			"%[1]s\t<MacroControlIndex Value=\"-1\" />\n"+
			"%[1]s\t<MidiControllerRange />\n"+
			"%[1]s\t<LomId Value=\"0\" />\n"+
			"%[1]s</PluginParameterSettings>", p, i, id, kind))
	}
	return "<ParameterSettings>\n" + strings.Join(rows, "\n") + "\n" + pad + "</ParameterSettings>"
}

// uidFields: 32 hex digits as four signed 32-bit ints, big-endian, as Live
// stores a VST3 class ID.
func uidFields(uid string) ([4]int32, error) {
	var f [4]int32
	b, err := hex.DecodeString(uid)
	if err != nil || len(b) != 16 {
		return f, fmt.Errorf("livedevice: uid %q is not 32 hex digits", uid)
	}
	for i := range f {
		f[i] = int32(binary.BigEndian.Uint32(b[i*4:]))
	}
	return f, nil
}

var (
	reDropLine = regexp.MustCompile(`[ \t]*<(ChainSelectorFilterMidiCtrl|RangeTypeIndex|ShowsZonesInsteadOfNoteNames) Value="[^"]*" />\n`)
	reZones    = regexp.MustCompile(`(?s)[ \t]*<ZoneSettings>.*?</ZoneSettings>\n`)
)

// asEffect turns the Instrument Rack into an Audio Effect Rack: exactly the
// differences Live's own Audio Effect Racks show (compared with a Core
// Library rack, 2026-11-14). The group and branch types, no MIDI chain filter,
// range type or zone display on the rack, no key or velocity zones on the
// branch, DeviceType 2 for the plugin.
func asEffect(xml string) string {
	xml = strings.ReplaceAll(xml, "InstrumentGroupDevice", "AudioEffectGroupDevice")
	xml = strings.ReplaceAll(xml, "InstrumentBranchPreset", "AudioEffectBranchPreset")
	xml = reDropLine.ReplaceAllString(xml, "")
	xml = reZones.ReplaceAllString(xml, "")
	xml = strings.Replace(xml, `<DeviceType Value="1" />`, `<DeviceType Value="2" />`, 1)
	xml = strings.Replace(xml, `<MpeEnabled Value="1" />`, `<MpeEnabled Value="0" />`, 1)
	return xml
}

var unsafeChars = regexp.MustCompile(`[^\w .()-]`)

// safeName: the file name make-vst3-preset.py would use.
func safeName(s string) string { return unsafeChars.ReplaceAllString(s, "_") }

// gzipBytes compresses with no name and no time in the header, so the same
// entry gives the same bytes and an unchanged file is not rewritten.
func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
