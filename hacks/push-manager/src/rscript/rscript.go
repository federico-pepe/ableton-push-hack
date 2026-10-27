// Package rscript reports on the PushHack Remote Script in Live's User Library
// and removes the old per-hack scripts it replaces. No Linux-only code, so it
// tests off the device.
package rscript

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Old are the scripts PushHack replaces. Only these can be removed through
// RemoveOld; a name never becomes a path unchecked.
var Old = []string{"PushHackBrowser", "PushHackArrangement"}

var (
	ErrUnknownName = errors.New("not an old Push Hack script")
	ErrNoPushHack  = errors.New("PushHack is not installed")
	ErrNotThere    = errors.New("not installed")
)

var versionRe = regexp.MustCompile(`(?m)^SCRIPT_VERSION = "([^"]+)"`)

// Hello asks the running script who it is. It returns an error when the script
// is not loaded in Live.
type Hello func() (version string, protocol int, err error)

// State is what the System page shows.
type State struct {
	Installed        bool     `json:"installed"`         // the folder is in the User Library
	InstalledVersion string   `json:"installed_version"` // read from the files
	Running          bool     `json:"running"`           // Live has loaded it and it answers
	RunningVersion   string   `json:"running_version"`
	Protocol         int      `json:"protocol"`
	NeedsRestart     bool     `json:"needs_restart"` // the files differ from what Live runs
	OldScripts       []string `json:"old_scripts"`   // still in the User Library
}

// Read looks at root, the Remote Scripts folder, and asks hello.
func Read(root string, hello Hello) State {
	st := State{OldScripts: []string{}}
	dir := filepath.Join(root, "PushHack")
	if _, err := os.Stat(filepath.Join(dir, "__init__.py")); err == nil {
		st.Installed = true
		if b, err := os.ReadFile(filepath.Join(dir, "protocol.py")); err == nil {
			if m := versionRe.FindSubmatch(b); m != nil {
				st.InstalledVersion = string(m[1])
			}
		}
	}
	if hello != nil {
		if v, p, err := hello(); err == nil {
			st.Running, st.RunningVersion, st.Protocol = true, v, p
		}
	}
	st.NeedsRestart = st.Installed && st.InstalledVersion != "" && st.InstalledVersion != st.RunningVersion
	for _, name := range Old {
		if fi, err := os.Lstat(filepath.Join(root, name)); err == nil && fi.IsDir() {
			st.OldScripts = append(st.OldScripts, name)
		}
	}
	sort.Strings(st.OldScripts)
	return st
}

// RemoveOld deletes one old script from root. It refuses until PushHack is
// there to replace it.
func RemoveOld(root, name string) error {
	known := false
	for _, n := range Old {
		known = known || n == name
	}
	if !known {
		return ErrUnknownName
	}
	if _, err := os.Stat(filepath.Join(root, "PushHack", "__init__.py")); err != nil {
		return ErrNoPushHack
	}
	dir := filepath.Join(root, name)
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return ErrNotThere
	}
	return os.RemoveAll(dir)
}
