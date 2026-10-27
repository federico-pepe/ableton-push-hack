package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fin struct {
	t     *testing.T
	dir   string
	inst  string
	dests map[string]string
	now   time.Time
	calls []string
	// health answers by URL; a function so a test can change over time.
	health func(url string, calls int) (string, error)
	hcalls map[string]int
	svcErr func(name, action string) error
	alive  func(name string) bool
	cfg    FinishConfig
}

func newFin(t *testing.T) *fin {
	t.Helper()
	f := &fin{t: t, dir: t.TempDir(), inst: t.TempDir(), now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), hcalls: map[string]int{}}
	mgr, cat := filepath.Join(f.inst, "push-manager"), filepath.Join(f.inst, "push-catalog")
	os.WriteFile(mgr, []byte("NEW"), 0o755)
	os.WriteFile(mgr+".prev", []byte("OLD"), 0o755)
	os.WriteFile(cat, []byte("NEW"), 0o755)
	os.WriteFile(cat+".prev", []byte("OLD"), 0o755)
	f.dests = map[string]string{"m": mgr, "c": cat}
	savePending(f.dir, Pending{
		Target: "0.1.11-alpha", From: "0.1.10-alpha", ManagerChanged: true, CatalogChanged: true,
		Swapped: []Swapped{{Dest: mgr, HadPrev: true}, {Dest: cat, HadPrev: true}},
	})
	f.cfg = FinishConfig{
		Dir: f.dir, ManagerURL: "mgr", CatalogURL: "cat",
		Health: func(u string) (string, error) {
			f.hcalls[u]++
			return f.health(u, f.hcalls[u])
		},
		Service: func(n, a string) error {
			f.calls = append(f.calls, a+" "+n)
			if f.svcErr != nil {
				return f.svcErr(n, a)
			}
			return nil
		},
		Alive: func(n string) bool {
			if f.alive == nil {
				return true
			}
			return f.alive(n)
		},
		WaitExit: func(n string) error { f.calls = append(f.calls, "wait "+n); return nil },
		Now:      func() time.Time { return f.now },
		Sleep:    func(d time.Duration) { f.now = f.now.Add(d) },
	}
	return f
}

func (f *fin) content(k string) string {
	b, _ := os.ReadFile(f.dests[k])
	return string(b)
}

func TestFinishSuccess(t *testing.T) {
	f := newFin(t)
	f.health = func(u string, n int) (string, error) {
		if u == "mgr" {
			return "0.1.11-alpha", nil
		}
		return "", nil
	}
	if err := Finish(f.cfg); err != nil {
		t.Fatal(err)
	}
	want := "stop push-hack-push-catalog|wait push-hack-push-catalog|start push-hack-push-catalog|stop push-hack-push-manager|wait push-hack-push-manager|start push-hack-push-manager"
	if got := strings.Join(f.calls, "|"); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
	r, ok := LoadResult(f.dir)
	if !ok || !r.OK || r.Version != "0.1.11-alpha" {
		t.Fatalf("result = %+v %v", r, ok)
	}
	if _, err := LoadPending(f.dir); err == nil {
		t.Fatal("pending.json still there")
	}
	if f.content("m") != "NEW" {
		t.Fatal("files changed on success")
	}
}

func TestFinishRollsBackWhenManagerNeverAnswers(t *testing.T) {
	f := newFin(t)
	f.health = func(u string, n int) (string, error) {
		if u == "mgr" && f.content("m") == "NEW" {
			return "", errors.New("connection refused")
		}
		if u == "mgr" {
			return "0.1.10-alpha", nil
		}
		return "", nil
	}
	if err := Finish(f.cfg); err == nil {
		t.Fatal("want error")
	}
	if f.content("m") != "OLD" || f.content("c") != "OLD" {
		t.Fatalf("files = %q %q", f.content("m"), f.content("c"))
	}
	r, _ := LoadResult(f.dir)
	if r.OK || !strings.Contains(r.Error, "push-manager did not come back") {
		t.Fatalf("result = %+v", r)
	}
	starts := 0
	for _, c := range f.calls {
		if c == "start push-hack-push-manager" {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("manager started %d times, want 2 (new, then old)", starts)
	}
}

func TestFinishRollsBackWhenVersionIsWrong(t *testing.T) {
	f := newFin(t)
	f.health = func(u string, n int) (string, error) {
		if u == "mgr" {
			return "0.1.10-alpha", nil // new binary still says the old version
		}
		return "", nil
	}
	if err := Finish(f.cfg); err == nil || !strings.Contains(err.Error(), "reports") {
		t.Fatalf("err = %v", err)
	}
}

func TestFinishCatchesCrashLoop(t *testing.T) {
	f := newFin(t)
	f.health = func(u string, n int) (string, error) {
		if u != "mgr" {
			return "", nil
		}
		if f.content("m") == "NEW" && n >= 2 { // up once, then gone
			return "", errors.New("connection refused")
		}
		if f.content("m") == "NEW" {
			return "0.1.11-alpha", nil
		}
		return "0.1.10-alpha", nil
	}
	if err := Finish(f.cfg); err == nil {
		t.Fatal("want error")
	}
	if f.content("m") != "OLD" {
		t.Fatal("not rolled back")
	}
}

func TestFinishCatalogFailureRollsBackBoth(t *testing.T) {
	f := newFin(t)
	f.health = func(u string, n int) (string, error) {
		if u == "cat" && f.content("c") == "NEW" {
			return "", errors.New("refused")
		}
		return "0.1.10-alpha", nil
	}
	if err := Finish(f.cfg); err == nil {
		t.Fatal("want error")
	}
	if f.content("m") != "OLD" || f.content("c") != "OLD" {
		t.Fatalf("files = %q %q", f.content("m"), f.content("c"))
	}
}

func TestFinishReportsFailedRollback(t *testing.T) {
	f := newFin(t)
	f.health = func(string, int) (string, error) { return "", errors.New("down") }
	err := Finish(f.cfg)
	r, _ := LoadResult(f.dir)
	if err == nil || !strings.Contains(r.Error, "old version did not come back") {
		t.Fatalf("err=%v result=%+v", err, r)
	}
}

func TestFinishServiceErrorRollsBack(t *testing.T) {
	f := newFin(t)
	f.health = func(u string, n int) (string, error) { return "0.1.10-alpha", nil }
	first := true
	f.svcErr = func(n, a string) error {
		if first && a == "start" {
			first = false
			return errors.New("init script failed")
		}
		return nil
	}
	if err := Finish(f.cfg); err == nil {
		t.Fatal("want error")
	}
	if f.content("m") != "OLD" {
		t.Fatal("not rolled back")
	}
}

func TestFinishOnlyRestartsWhatChanged(t *testing.T) {
	f := newFin(t)
	p, _ := LoadPending(f.dir)
	p.CatalogChanged = false
	savePending(f.dir, p)
	f.health = func(u string, n int) (string, error) { return "0.1.11-alpha", nil }
	Finish(f.cfg)
	for _, c := range f.calls {
		if strings.Contains(c, "catalog") {
			t.Fatalf("catalog restarted: %v", f.calls)
		}
	}
}

func TestRecover(t *testing.T) {
	// Helper gone, new version runs: success.
	f := newFin(t)
	p, _ := LoadPending(f.dir)
	p.HelperPID = 99999999
	savePending(f.dir, p)
	Recover(f.dir, "0.1.11-alpha", f.now)
	if r, ok := LoadResult(f.dir); !ok || !r.OK {
		t.Fatalf("result = %+v", r)
	}
	if _, err := LoadPending(f.dir); err == nil {
		t.Fatal("pending kept")
	}

	// Helper gone, old version runs: cut off, files go back.
	f2 := newFin(t)
	p, _ = LoadPending(f2.dir)
	p.HelperPID = 99999999
	savePending(f2.dir, p)
	Recover(f2.dir, "0.1.10-alpha", f2.now)
	if r, _ := LoadResult(f2.dir); r.OK || r.Error == "" {
		t.Fatalf("result = %+v", r)
	}
	if f2.content("m") != "OLD" {
		t.Fatal("files not restored")
	}

	// A live helper owns the file.
	f3 := newFin(t)
	p, _ = LoadPending(f3.dir)
	p.HelperPID = os.Getpid()
	savePending(f3.dir, p)
	Recover(f3.dir, "0.1.11-alpha", f3.now)
	if _, err := LoadPending(f3.dir); err != nil {
		t.Fatal("touched a live helper's file")
	}
	if _, ok := LoadResult(f3.dir); ok {
		t.Fatal("result written while helper runs")
	}

	// Nothing pending: nothing happens.
	Recover(t.TempDir(), "0.1.11-alpha", f.now)
}

func TestResultClear(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, resultFile), []byte(`{"ok":true,"version":"1"}`), 0o644)
	if _, ok := LoadResult(d); !ok {
		t.Fatal("not read")
	}
	ClearResult(d)
	if _, ok := LoadResult(d); ok {
		t.Fatal("not cleared")
	}
}

func TestFinishFailsFastWhenTheNewServiceCrashes(t *testing.T) {
	f := newFin(t)
	f.alive = func(n string) bool { return f.content("m") != "NEW" || n != "push-hack-push-manager" } // new manager dies at once
	f.health = func(u string, n int) (string, error) {
		if u == "mgr" && f.content("m") == "NEW" {
			return "", errors.New("connection refused")
		}
		if u == "mgr" {
			return "0.1.10-alpha", nil
		}
		return "", nil
	}
	start := f.now
	err := Finish(f.cfg)
	if err == nil || !strings.Contains(err.Error(), "stopped right after it started") {
		t.Fatalf("err = %v", err)
	}
	if f.now.Sub(start) > 25*time.Second {
		t.Fatalf("took %v of (fake) time, want under 25 s (30 s wait skipped)", f.now.Sub(start))
	}
	if f.content("m") != "OLD" {
		t.Fatal("not rolled back")
	}
}

func TestFinishReportsScriptChangeOnlyOnSuccess(t *testing.T) {
	f := newFin(t)
	p, _ := LoadPending(f.dir)
	p.ScriptChanged = true
	savePending(f.dir, p)
	f.health = func(u string, n int) (string, error) {
		if u == "mgr" {
			return "0.1.11-alpha", nil
		}
		return "", nil
	}
	if err := Finish(f.cfg); err != nil {
		t.Fatal(err)
	}
	if r, _ := LoadResult(f.dir); !r.OK || !r.ScriptChanged {
		t.Fatalf("result = %+v", r)
	}

	f2 := newFin(t)
	p, _ = LoadPending(f2.dir)
	p.ScriptChanged = true
	savePending(f2.dir, p)
	f2.health = func(u string, n int) (string, error) { return "", errors.New("down") }
	f2.cfg.HealthWait, f2.cfg.StableFor = time.Second, time.Second
	_ = Finish(f2.cfg)
	if r, _ := LoadResult(f2.dir); r.OK || r.ScriptChanged {
		t.Fatalf("a failed update claims a script change: %+v", r)
	}
}
