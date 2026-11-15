// Package livedevice lets a hack show its engines in Live as ordinary
// devices. The device in Live is a thin VST3 plugin (vst3/, built into
// livedevice.so); the engine runs in the hack's own process, one block at a
// time over shared memory. See ../../plans/2026-11-14-live-devices.md and
// ../../docs/live-devices.md.
//
// A hack does four things:
//
//   - describe its devices: one Class per kind of device, with Params and
//     Banks (PadBanks lays the banks out on Push's pages);
//   - InstallBundle: put the plugin in /data/.vst3 as "<Name>.vst3";
//   - WriteClasses: the class file the plugin builds its factory from;
//   - run a Server with a Provider that makes a Device for each instance
//     Live creates. Device.Process runs once per audio block, realtime.
//
// WritePresets makes the browser entries Push lists.
package livedevice

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Kinds of device. The plugin builds its buses from the kind.
const (
	Instrument = "instrument" // MIDI in, audio out
	Effect     = "effect"     // audio in and out, MIDI in
	MIDI       = "midi"       // MIDI in and out, silent audio out: an instrument whose notes other tracks take ("MIDI From")
	MIDIEffect = "midifx"     // MIDI in and out, no audio. Live 13.0b25 scans it as an audio effect: not useful yet
)

// PadType is the type of a placeholder parameter (PadBanks): blank on Push,
// never handed to a Device.
const PadType = "pad"

// MaxParams: the most parameters a class may have (the block holds this many
// changes; Live's parameter map in a rack holds 128 of them).
const MaxParams = 256

// Param is one parameter of a device, as Live shows it. Values travel
// normalized (0..1). Key and Role are the hack's own: the plugin ignores them.
type Param struct {
	Name    string   `json:"name"`
	Key     string   `json:"key,omitempty"`
	Type    string   `json:"type"` // float | int | enum | pad
	Min     float64  `json:"min"`
	Max     float64  `json:"max"`
	Unit    string   `json:"unit,omitempty"`   // shown after the value ("%" scales 0..1 to 0..100)
	Format  string   `json:"format,omitempty"` // printf format of the value, optional
	Options []string `json:"options,omitempty"`
	// Default is where a new instance starts, normalized. Live pushes it into
	// the device when it loads one, so it must be the engine's real starting
	// value: a guessed 0 loads a synth at volume 0.
	Default float64 `json:"default"`
	Role    string  `json:"role,omitempty"`
}

// Class is one kind of device: a plugin class in Live.
type Class struct {
	UID      string  `json:"uid"`  // 32 hex digits, stable forever: Live saves it in every Set
	Name     string  `json:"name"` // shown in Live and on Push
	Kind     string  `json:"kind"`
	Category string  `json:"category,omitempty"` // VST3 sub categories; empty: from Kind
	Ref      string  `json:"ref,omitempty"`      // the hack's own name for it
	Folder   string  `json:"folder,omitempty"`   // browser folder under the kind's (WritePresets); empty: the vendor
	Params   []Param `json:"params"`
	// Banks: parameter indexes in the order Push pages them, 8 to a page.
	Banks [][]int `json:"banks"`
}

// ClassUID makes a stable UID from a namespace (the hack's), a kind and the
// hack's own name for the class.
func ClassUID(namespace, kind, ref string) string {
	sum := md5.Sum([]byte(namespace + ":" + kind + ":" + ref))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// PadBanks lays the banks out on Push's pages. Push pages a plugin's
// parameters 8 at a time in the order the rack lists them, so a bank of 5
// would share its page with the next bank's first 3. Every bank but the last
// is filled to whole pages with placeholder parameters (PadType), each one
// its own parameter: a parameter listed twice gets two values in Live. A
// parameter in two banks stays in the first; an emptied bank goes. A
// parameter in no bank is added at the end, 8 to a bank.
func PadBanks(c *Class) {
	seen := map[int]bool{}
	var banks [][]int
	for _, bank := range c.Banks {
		var keep []int
		for _, i := range bank {
			if i >= 0 && i < len(c.Params) && !seen[i] {
				seen[i] = true
				keep = append(keep, i)
			}
		}
		if len(keep) > 0 {
			banks = append(banks, keep)
		}
	}
	var rest []int
	for i := range c.Params {
		if !seen[i] && c.Params[i].Type != PadType {
			rest = append(rest, i)
		}
	}
	for i := 0; i < len(rest); i += 8 {
		banks = append(banks, rest[i:min(i+8, len(rest))])
	}
	c.Banks = banks
	for b := 0; b < len(c.Banks)-1; b++ {
		for len(c.Banks[b])%8 != 0 {
			c.Banks[b] = append(c.Banks[b], len(c.Params))
			c.Params = append(c.Params, Param{Name: "", Type: PadType})
		}
	}
}

// Check reports what the plugin would refuse in a class.
func (c *Class) Check() error {
	switch {
	case len(strings.Trim(c.UID, "0123456789ABCDEFabcdef")) != 0 || len(c.UID) != 32:
		return errors.New(c.Name + ": uid must be 32 hex digits")
	case c.Name == "":
		return errors.New("a class has no name")
	case c.Kind != Instrument && c.Kind != Effect && c.Kind != MIDI && c.Kind != MIDIEffect:
		return errors.New(c.Name + ": unknown kind " + c.Kind)
	case len(c.Params) > MaxParams:
		return errors.New(c.Name + ": too many parameters")
	}
	return nil
}

// ClassFile is what the plugin reads (WriteClasses).
type ClassFile struct {
	Version int      `json:"version"`
	Classes []*Class `json:"classes"`
}

// WriteClasses writes the class file. When it changed, the plugin binary's
// time is touched, so Live scans the bundle again at its next start (Live
// reads a plugin's classes only when it scans). soPath may be empty.
// Reports whether the file changed.
func WriteClasses(path, soPath string, classes []*Class) (bool, error) {
	for _, c := range classes {
		if err := c.Check(); err != nil {
			return false, err
		}
	}
	doc, err := json.MarshalIndent(ClassFile{Version: 1, Classes: classes}, "", " ")
	if err != nil {
		return false, err
	}
	if old, err := os.ReadFile(path); err == nil && string(old) == string(doc) {
		return false, nil
	}
	if err := writeAtomic(path, doc, 0o644); err != nil {
		return false, err
	}
	if soPath != "" {
		now := time.Now()
		os.Chtimes(soPath, now, now)
	}
	return true, nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
