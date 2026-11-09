package vst3scan

// Package vst3scan -- VST3 scan switch on the System page. Off-device logic;
// the HTTP handler is vst3.go in package main.
//
//	GET  /api/vst3                 state: on | off | unknown, plugin count
//	POST /api/vst3 {"enabled":b}   change the flag, restart the Push stack
//
// Flag = AreSystemPathsEnabled in Live's Preferences.cfg. Live reads it only at
// start (tested on device 2026-11-09: file patched while Live ran, no scan).
// Live API has no rescan call. So change = stop push3, patch one byte, start.
// push-manager keeps running while push3 stops (tested), so goroutine is enough.
// Plan: plans/2026-11-09-vst3-scan-toggle.md

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"
)

var marker = []byte("\x0fVst3Preferences")

// Layout after marker: 04 00 00 00, then flag byte (0 or 1).
var countField = []byte{4, 0, 0, 0}

var (
	ErrLayout = errors.New("unexpected Preferences.cfg layout")
	ErrNoFile = errors.New("no Live Preferences.cfg found")
	ErrBusy   = errors.New("a restart is already running")
)

// Flag returns the flag offset and value. Last marker wins, as in docs script.
func Flag(d []byte) (off int, on bool, err error) {
	i := lastIndex(d, marker)
	if i < 0 {
		return 0, false, ErrLayout
	}
	off = i + len(marker) + len(countField)
	if off >= len(d) || string(d[off-len(countField):off]) != string(countField) || d[off] > 1 {
		return 0, false, ErrLayout
	}
	return off, d[off] == 1, nil
}

func lastIndex(d, sub []byte) int {
	for i := len(d) - len(sub); i >= 0; i-- {
		if string(d[i:i+len(sub)]) == string(sub) {
			return i
		}
	}
	return -1
}

// WithFlag returns a copy of d with the flag set. Nothing else changes.
func WithFlag(d []byte, on bool) ([]byte, error) {
	off, _, err := Flag(d)
	if err != nil {
		return nil, err
	}
	out := append([]byte(nil), d...)
	if on {
		out[off] = 1
	} else {
		out[off] = 0
	}
	return out, nil
}

// ── Pick the Preferences.cfg of the highest Live version ───────────────────

var liveDirRe = regexp.MustCompile(`^Live (\d+)\.(\d+)(?:\.(\d+))?(?:b(\d+))?$`)

// liveVersion: "Live 12.4.25b1" -> {12,4,25,1}. "12.4b17" -> {12,4,0,17}.
// A release with no "b" sorts above its betas.
func liveVersion(name string) ([4]int, bool) {
	m := liveDirRe.FindStringSubmatch(name)
	if m == nil {
		return [4]int{}, false
	}
	var v [4]int
	for i := 0; i < 4; i++ {
		if m[i+1] != "" {
			v[i], _ = strconv.Atoi(m[i+1])
		}
	}
	if m[4] == "" {
		v[3] = 1 << 30
	}
	return v, true
}

func liveVersionLess(a, b [4]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// PrefsPath finds <root>/Live <highest>/Preferences.cfg.
func PrefsPath(root string) (string, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return "", ErrNoFile
	}
	best, bestV := "", [4]int{}
	for _, e := range ents {
		v, ok := liveVersion(e.Name())
		if !ok || !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name(), "Preferences.cfg")
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if best == "" || liveVersionLess(bestV, v) {
			best, bestV = p, v
		}
	}
	if best == "" {
		return "", ErrNoFile
	}
	return best, nil
}

// ── Change ─────────────────────────────────────────────────────────────────

type Ctl struct {
	configRoot string // /data/.config/Ableton
	backupPath string // /data/push-hack/backup/Preferences.cfg.bak
	pluginDir  string // /data/.vst3
	stack      func(action string) error
	pause      time.Duration
	// BeforeStop runs once the change is accepted, before Live stops. Optional.
	BeforeStop func()
	mu         sync.Mutex
	busy       bool
}

// New returns the Ctl for the real Push paths.
func New() *Ctl {
	return &Ctl{
		configRoot: "/data/.config/Ableton",
		backupPath: "/data/push-hack/backup/Preferences.cfg.bak",
		pluginDir:  "/data/.vst3",
		stack: func(action string) error {
			out, err := exec.Command("/etc/init.d/push3", action).CombinedOutput()
			if err != nil {
				return fmt.Errorf("push3 %s: %v: %s", action, err, out)
			}
			return nil
		},
		pause: 5 * time.Second,
	}
}

type State struct {
	State   string `json:"state"` // on | off | unknown
	File    string `json:"file,omitempty"`
	Plugins int    `json:"plugins"`
	Busy    bool   `json:"busy"`
	Error   string `json:"error,omitempty"`
}

func (c *Ctl) State() State {
	c.mu.Lock()
	st := State{State: "unknown", Busy: c.busy}
	c.mu.Unlock()
	if ents, err := os.ReadDir(c.pluginDir); err == nil {
		for _, e := range ents {
			if filepath.Ext(e.Name()) == ".vst3" {
				st.Plugins++
			}
		}
	}
	p, err := PrefsPath(c.configRoot)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.File = p
	d, err := os.ReadFile(p)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	if _, on, err := Flag(d); err != nil {
		st.Error = err.Error()
	} else if on {
		st.State = "on"
	} else {
		st.State = "off"
	}
	return st
}

// prepare runs every check that must pass before Live stops. changed=false
// means the flag already has the wanted value.
func (c *Ctl) prepare(want bool) (path string, data []byte, changed bool, err error) {
	path, err = PrefsPath(c.configRoot)
	if err != nil {
		return
	}
	if data, err = os.ReadFile(path); err != nil {
		return
	}
	var on bool
	if _, on, err = Flag(data); err != nil {
		return
	}
	if on == want {
		return path, data, false, nil
	}
	if _, err = WithFlag(data, want); err != nil {
		return
	}
	if err = os.MkdirAll(filepath.Dir(c.backupPath), 0o755); err != nil {
		return
	}
	if err = os.WriteFile(c.backupPath, data, 0o644); err != nil {
		return
	}
	return path, data, true, nil
}

// apply: stop, patch, check, start. Start always runs, also after a failure,
// so Push never stays without Live. A bad write is rolled back from data.
func (c *Ctl) apply(path string, data []byte, want bool) error {
	if c.BeforeStop != nil {
		c.BeforeStop()
	}
	if err := c.stack("stop"); err != nil {
		_ = c.stack("start")
		return err
	}
	time.Sleep(c.pause)
	err := writeFlag(path, data, want)
	if err != nil {
		log.Printf("vst3: %v; restoring backup", err)
		_ = replaceFile(path, data)
	}
	if serr := c.stack("start"); serr != nil && err == nil {
		err = serr
	}
	return err
}

func writeFlag(path string, data []byte, want bool) error {
	nd, err := WithFlag(data, want)
	if err != nil {
		return err
	}
	if err := replaceFile(path, nd); err != nil {
		return err
	}
	back, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, on, err := Flag(back); err != nil || on != want {
		return fmt.Errorf("flag check after write failed")
	}
	return nil
}

// replaceFile writes a temp file next to path, keeps owner and mode, renames.
func replaceFile(path string, data []byte) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".Preferences.cfg.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), st.Mode().Perm()); err != nil {
		return err
	}
	copyOwner(tmp.Name(), st)
	return os.Rename(tmp.Name(), path)
}

// copyOwner gives path the owner of st. Best effort: off Push we are not root.
func copyOwner(path string, st os.FileInfo) {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(path, int(s.Uid), int(s.Gid))
	}
}

// set runs the checks, then the restart in a goroutine. The HTTP answer goes
// out first, because the stack restart takes the session down.
func (c *Ctl) Set(want bool) (restarting bool, err error) {
	c.mu.Lock()
	if c.busy {
		c.mu.Unlock()
		return false, ErrBusy
	}
	path, data, changed, err := c.prepare(want)
	if err != nil || !changed {
		c.mu.Unlock()
		return false, err
	}
	c.busy = true
	c.mu.Unlock()
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := c.apply(path, data, want); err != nil {
			log.Printf("vst3: change failed: %v", err)
		}
		c.mu.Lock()
		c.busy = false
		c.mu.Unlock()
	}()
	return true, nil
}

// Prompt is the two-press confirm of a hardware button: the first press arms
// it, a second press within TTL confirms. Not safe for concurrent use; the
// caller holds its own lock.
type Prompt struct {
	TTL     time.Duration
	armedAt time.Time
}

// Press returns true when this press confirms. Otherwise it arms the prompt.
func (p *Prompt) Press(now time.Time) bool {
	if p.Active(now) {
		p.armedAt = time.Time{}
		return true
	}
	p.armedAt = now
	return false
}

// Active reports whether the prompt waits for the second press.
func (p *Prompt) Active(now time.Time) bool {
	return !p.armedAt.IsZero() && now.Sub(p.armedAt) < p.TTL
}

// Cancel disarms the prompt.
func (p *Prompt) Cancel() { p.armedAt = time.Time{} }
