package vst3scan

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleCfg(flag byte) []byte {
	b := []byte("head-bytes")
	b = append(b, marker...)
	b = append(b, 4, 0, 0, 0, flag, 'x', 'y', 'z')
	return b
}

func TestVst3Flag(t *testing.T) {
	for _, on := range []bool{false, true} {
		f := byte(0)
		if on {
			f = 1
		}
		_, got, err := Flag(sampleCfg(f))
		if err != nil || got != on {
			t.Fatalf("flag %d: got %v, %v", f, got, err)
		}
	}
}

func TestVst3FlagBadLayout(t *testing.T) {
	cases := map[string][]byte{
		"no marker":   []byte("nothing here"),
		"bad count":   append(append([]byte{}, marker...), 5, 0, 0, 0, 1),
		"bad value":   append(append([]byte{}, marker...), 4, 0, 0, 0, 7),
		"cut after":   append(append([]byte{}, marker...), 4, 0, 0, 0),
		"empty":       nil,
		"marker only": marker,
	}
	for name, d := range cases {
		if _, _, err := Flag(d); !errors.Is(err, ErrLayout) {
			t.Errorf("%s: want ErrLayout, got %v", name, err)
		}
	}
}

func TestVst3LastMarkerWins(t *testing.T) {
	d := append(sampleCfg(1), sampleCfg(0)...)
	_, on, err := Flag(d)
	if err != nil || on {
		t.Fatalf("last marker has 0: got %v, %v", on, err)
	}
	nd, err := WithFlag(d, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(nd[:len(sampleCfg(1))], sampleCfg(1)) {
		t.Fatal("first block changed")
	}
}

func TestVst3WithFlagChangesOneByte(t *testing.T) {
	d := sampleCfg(0)
	nd, err := WithFlag(d, true)
	if err != nil {
		t.Fatal(err)
	}
	diff := 0
	for i := range d {
		if d[i] != nd[i] {
			diff++
		}
	}
	if diff != 1 || len(nd) != len(d) {
		t.Fatalf("want 1 byte changed, got %d", diff)
	}
	if d[len(d)-4] != 0 {
		t.Fatal("input slice was modified")
	}
}

func TestLiveVersionOrder(t *testing.T) {
	order := []string{"Live 12.3.1", "Live 12.4b7", "Live 12.4b17", "Live 12.4.5b1",
		"Live 12.4.5b11", "Live 12.4.15b7", "Live 12.4.25b1", "Live 12.4.25", "Live 13.0b1"}
	for i := 1; i < len(order); i++ {
		a, ok1 := liveVersion(order[i-1])
		b, ok2 := liveVersion(order[i])
		if !ok1 || !ok2 || !liveVersionLess(a, b) || liveVersionLess(b, a) {
			t.Errorf("%s should sort below %s", order[i-1], order[i])
		}
	}
	for _, bad := range []string{"Live", "Live x", "Crash", "Live 12", "Live 12.4.5b"} {
		if _, ok := liveVersion(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func fakeRoot(t *testing.T, flags map[string]byte) string {
	root := t.TempDir()
	for name, f := range flags {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Preferences.cfg"), sampleCfg(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestVst3PrefsPathHighestVersion(t *testing.T) {
	root := fakeRoot(t, map[string]byte{"Live 12.4.5b11": 0, "Live 12.4.25b1": 1, "Live 12.4.15b7": 0})
	// A newer mtime on an old folder must not matter.
	old := filepath.Join(root, "Live 12.4.5b11", "Preferences.cfg")
	_ = os.Chtimes(old, time.Now().Add(time.Hour), time.Now().Add(time.Hour))
	p, err := PrefsPath(root)
	if err != nil || filepath.Base(filepath.Dir(p)) != "Live 12.4.25b1" {
		t.Fatalf("got %q, %v", p, err)
	}
	if _, err := PrefsPath(t.TempDir()); !errors.Is(err, ErrNoFile) {
		t.Fatalf("empty root: %v", err)
	}
}

type calls struct{ list []string }

func testCtl(t *testing.T, flags map[string]byte, c *calls) *Ctl {
	root := fakeRoot(t, flags)
	return &Ctl{
		configRoot: root,
		backupPath: filepath.Join(t.TempDir(), "backup", "Preferences.cfg.bak"),
		pluginDir:  t.TempDir(),
		stack:      func(a string) error { c.list = append(c.list, a); return nil },
	}
}

func TestVst3StateAndPlugins(t *testing.T) {
	ctl := testCtl(t, map[string]byte{"Live 12.4.25b1": 1}, &calls{})
	_ = os.Mkdir(filepath.Join(ctl.pluginDir, "A.vst3"), 0o755)
	_ = os.Mkdir(filepath.Join(ctl.pluginDir, "B.vst3"), 0o755)
	_ = os.Mkdir(filepath.Join(ctl.pluginDir, "other"), 0o755)
	st := ctl.State()
	if st.State != "on" || st.Plugins != 2 {
		t.Fatalf("got %+v", st)
	}
}

func TestVst3SetChangesFlagAndRestarts(t *testing.T) {
	var c calls
	ctl := testCtl(t, map[string]byte{"Live 12.4.25b1": 0}, &c)
	path, data, changed, err := ctl.prepare(true)
	if err != nil || !changed {
		t.Fatalf("prepare: %v %v", changed, err)
	}
	if err := ctl.apply(path, data, true); err != nil {
		t.Fatal(err)
	}
	if got := ctl.State().State; got != "on" {
		t.Fatalf("state %s", got)
	}
	if len(c.list) != 2 || c.list[0] != "stop" || c.list[1] != "start" {
		t.Fatalf("stack calls %v", c.list)
	}
	bak, err := os.ReadFile(ctl.backupPath)
	if err != nil || !bytes.Equal(bak, sampleCfg(0)) {
		t.Fatalf("backup wrong: %v", err)
	}
}

func TestVst3SameValueDoesNothing(t *testing.T) {
	var c calls
	ctl := testCtl(t, map[string]byte{"Live 12.4.25b1": 1}, &c)
	restarting, err := ctl.Set(true)
	if err != nil || restarting || len(c.list) != 0 {
		t.Fatalf("restarting=%v err=%v calls=%v", restarting, err, c.list)
	}
	if _, err := os.Stat(ctl.backupPath); err == nil {
		t.Fatal("backup written for a no-op")
	}
}

func TestVst3BadLayoutNeverStopsLive(t *testing.T) {
	var c calls
	ctl := testCtl(t, nil, &c)
	dir := filepath.Join(ctl.configRoot, "Live 12.4.25b1")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "Preferences.cfg"), []byte("garbage"), 0o644)
	restarting, err := ctl.Set(true)
	if !errors.Is(err, ErrLayout) || restarting || len(c.list) != 0 {
		t.Fatalf("restarting=%v err=%v calls=%v", restarting, err, c.list)
	}
	if st := ctl.State(); st.State != "unknown" || st.Error == "" {
		t.Fatalf("state %+v", st)
	}
}

func TestVst3BackupFailureNeverStopsLive(t *testing.T) {
	var c calls
	ctl := testCtl(t, map[string]byte{"Live 12.4.25b1": 0}, &c)
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, nil, 0o644)
	ctl.backupPath = filepath.Join(blocker, "sub", "bak")
	if _, err := ctl.Set(true); err == nil || len(c.list) != 0 {
		t.Fatalf("err=%v calls=%v", err, c.list)
	}
}

func TestVst3StopFailureStillStarts(t *testing.T) {
	var c calls
	ctl := testCtl(t, map[string]byte{"Live 12.4.25b1": 0}, &c)
	ctl.stack = func(a string) error {
		c.list = append(c.list, a)
		if a == "stop" {
			return errors.New("boom")
		}
		return nil
	}
	path, data, _, _ := ctl.prepare(true)
	if err := ctl.apply(path, data, true); err == nil {
		t.Fatal("want error")
	}
	if len(c.list) != 2 || c.list[1] != "start" {
		t.Fatalf("calls %v", c.list)
	}
	if ctl.State().State != "off" {
		t.Fatal("file changed although stop failed")
	}
}

func TestVst3BusyRejectsSecondChange(t *testing.T) {
	ctl := testCtl(t, map[string]byte{"Live 12.4.25b1": 0}, &calls{})
	ctl.busy = true
	if _, err := ctl.Set(true); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v", err)
	}
}

func TestVst3BeforeStopRunsBeforeStack(t *testing.T) {
	var c calls
	ctl := testCtl(t, map[string]byte{"Live 12.4.25b1": 0}, &c)
	ctl.BeforeStop = func() { c.list = append(c.list, "before") }
	path, data, _, _ := ctl.prepare(true)
	if err := ctl.apply(path, data, true); err != nil {
		t.Fatal(err)
	}
	if len(c.list) != 3 || c.list[0] != "before" || c.list[1] != "stop" {
		t.Fatalf("calls %v", c.list)
	}
}

func TestVst3BeforeStopNotRunForNoOpOrBadLayout(t *testing.T) {
	var c calls
	ctl := testCtl(t, map[string]byte{"Live 12.4.25b1": 1}, &c)
	ctl.BeforeStop = func() { c.list = append(c.list, "before") }
	if _, err := ctl.Set(true); err != nil || len(c.list) != 0 {
		t.Fatalf("err=%v calls=%v", err, c.list)
	}
}

func TestPromptTwoPress(t *testing.T) {
	p := Prompt{TTL: 8 * time.Second}
	t0 := time.Unix(1000, 0)
	if p.Active(t0) {
		t.Fatal("active before first press")
	}
	if p.Press(t0) || !p.Active(t0.Add(time.Second)) {
		t.Fatal("first press must arm, not confirm")
	}
	if !p.Press(t0.Add(2 * time.Second)) {
		t.Fatal("second press must confirm")
	}
	if p.Active(t0.Add(3 * time.Second)) {
		t.Fatal("still armed after confirm")
	}
}

func TestPromptExpiresAndCancels(t *testing.T) {
	p := Prompt{TTL: 8 * time.Second}
	t0 := time.Unix(1000, 0)
	p.Press(t0)
	if p.Active(t0.Add(9 * time.Second)) {
		t.Fatal("must expire after TTL")
	}
	if p.Press(t0.Add(10 * time.Second)) {
		t.Fatal("press after expiry must re-arm, not confirm")
	}
	p.Cancel()
	if p.Active(t0.Add(11*time.Second)) || p.Press(t0.Add(11*time.Second)) {
		t.Fatal("cancel must disarm")
	}
}
