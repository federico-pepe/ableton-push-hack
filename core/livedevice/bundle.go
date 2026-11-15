package livedevice

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// VST3Dir is where Live on Push looks for VST3 plugins (once push-manager's
// VST3 scan switch is on).
const VST3Dir = "/data/.vst3"

// BundleConfig is the plugin's livedevice.json: how one hack's copy of the
// plugin finds its hack.
type BundleConfig struct {
	Vendor string `json:"vendor"` // shown in Live's plugin list
	URL    string `json:"url,omitempty"`
	Email  string `json:"email,omitempty"`
	Socket string `json:"socket"` // the Server's control socket
	// Classes is the class file (WriteClasses). Empty: ClassesPath, inside
	// the bundle, where it outlives the hack (see InstallBundle).
	Classes string `json:"classes"`
	// Presets: the browser folders the hack writes (PresetFolders).
	// push-catalog removes them with the hack; the plugin ignores them.
	Presets []string `json:"presets,omitempty"`
}

var bundleName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]*$`)

// BundleDir is a hack's plugin bundle, "<dir>/<name>.vst3".
func BundleDir(dir, name string) string { return filepath.Join(dir, name+".vst3") }

// ClassesPath is where a hack's class file goes by default: inside its
// bundle. When the hack is removed, its folder goes, the bundle stays, and
// the devices in the user's Sets still load (silent) instead of being
// missing plugins (Live 13.0b25 crashed recovering a Set with a missing one).
func ClassesPath(dir, name string) string {
	return filepath.Join(BundleDir(dir, name), "Contents", "Resources", "classes.json")
}

// InstallBundle puts the plugin binary soSrc into dir as "<name>.vst3", with
// cfg next to it (an empty cfg.Classes becomes ClassesPath). Every hack
// installs the same livedevice.so under its own name. A binary that changed
// is written beside the old one and renamed over it, so a Live that has the
// old one loaded keeps running. Returns the installed binary's path (for
// WriteClasses), the config as written, and whether anything changed.
func InstallBundle(dir, name, soSrc string, cfg BundleConfig) (string, BundleConfig, bool, error) {
	if !bundleName.MatchString(name) {
		return "", cfg, false, errors.New("livedevice: bad bundle name " + name)
	}
	if cfg.Socket == "" {
		return "", cfg, false, errors.New("livedevice: bundle config needs a socket")
	}
	if cfg.Classes == "" {
		cfg.Classes = ClassesPath(dir, name)
	}
	so, err := os.ReadFile(soSrc)
	if err != nil {
		return "", cfg, false, err
	}
	root := filepath.Join(BundleDir(dir, name), "Contents")
	soPath := filepath.Join(root, "x86_64-linux", name+".so")
	changed := false
	if old, err := os.ReadFile(soPath); err != nil || !bytes.Equal(old, so) {
		if err := writeAtomic(soPath, so, 0o755); err != nil {
			return "", cfg, false, err
		}
		changed = true
	}
	doc, _ := json.MarshalIndent(cfg, "", " ")
	cfgPath := filepath.Join(root, "Resources", "livedevice.json")
	// browser folders the previous config owned and this one does not (the
	// hack renamed them): their entries go, and the folder if it is empty
	if raw, err := os.ReadFile(cfgPath); err == nil {
		var old BundleConfig
		if json.Unmarshal(raw, &old) == nil {
			keep := map[string]bool{}
			for _, d := range cfg.Presets {
				keep[d] = true
			}
			for _, d := range old.Presets {
				if !keep[d] {
					retirePresetFolder(d)
				}
			}
		}
	}
	if old, err := os.ReadFile(cfgPath); err != nil || !bytes.Equal(old, doc) {
		if err := writeAtomic(cfgPath, doc, 0o644); err != nil {
			return "", cfg, false, err
		}
		changed = true
	}
	// A moduleinfo.json made at build time would list no classes (they come
	// from the class file at run time), and Live would believe it.
	os.Remove(filepath.Join(root, "Resources", "moduleinfo.json"))
	return soPath, cfg, changed, nil
}

// retirePresetFolder removes the .adg entries a hack wrote into a browser
// folder it no longer uses, and the folder once nothing else is in it.
func retirePresetFolder(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".adg") {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	os.Remove(dir) // only if empty
}
