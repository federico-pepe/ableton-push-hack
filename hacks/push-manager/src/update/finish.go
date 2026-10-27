package update

// finish.go -- the helper that restarts the services after a swap.
//
// push-manager starts it detached (its own session) with the new binary, then
// carries on. The helper stops and starts push-catalog and push-manager, waits
// for the new push-manager to answer with the target version, and puts the old
// files back if it does not. It must not depend on the old push-manager, which
// it ends.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const resultFile = "last_result.json"

// Result is how the last update ended. The page and the screen show it once.
type Result struct {
	OK      bool      `json:"ok"`
	Version string    `json:"version"` // the version tried
	From    string    `json:"from"`
	Error   string    `json:"error"`
	At      time.Time `json:"at"`
	// ScriptChanged: the PushHack Remote Script was replaced. Live loads it
	// at its next start; push-manager never restarts Live.
	ScriptChanged bool `json:"script_changed"`
}

// FinishConfig sets up the helper. Hooks are for tests.
type FinishConfig struct {
	Dir        string
	ManagerURL string // e.g. http://127.0.0.1:7701/api/status
	CatalogURL string // e.g. http://127.0.0.1:7702/api/installed
	Health     func(url string) (version string, err error)
	Service    func(name, action string) error // "push-hack-push-manager", "start"
	WaitExit   func(name string) error         // block until the service's process is gone
	Alive      func(name string) bool          // is the service's process running? nil: not checked
	Now        func() time.Time
	Sleep      func(time.Duration)
	HealthWait time.Duration // how long to wait for a service to answer
	StableFor  time.Duration // it must still answer after this long
	Logf       func(format string, args ...any)
}

const (
	svcManager = "push-hack-push-manager"
	svcCatalog = "push-hack-push-catalog"
)

func (c *FinishConfig) defaults() {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Sleep == nil {
		c.Sleep = time.Sleep
	}
	if c.HealthWait == 0 {
		c.HealthWait = 30 * time.Second
	}
	if c.StableFor == 0 {
		c.StableFor = 5 * time.Second
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
}

// Finish restarts what changed and checks it. It returns nil when the new
// version runs, and an error after it has rolled back. The result file is
// written either way. A panic or kill leaves pending.json for Recover.
func Finish(c FinishConfig) error {
	c.defaults()
	p, err := LoadPending(c.Dir)
	if err != nil {
		return fmt.Errorf("no update to finish: %v", err)
	}
	p.HelperPID = os.Getpid()
	if err := savePending(c.Dir, p); err != nil {
		return err
	}

	err = c.restartAndCheck(p, p.Target)
	if err == nil {
		c.Logf("update to %s works", p.Target)
		return c.finishWith(p, Result{OK: true, Version: p.Target, From: p.From, At: c.Now()})
	}
	c.Logf("update to %s failed: %v. Putting %s back.", p.Target, err, p.From)
	restoreFiles(p, c.Logf)
	rerr := c.restartAndCheck(p, p.From)
	msg := err.Error()
	if rerr != nil {
		msg += "; the old version did not come back either: " + rerr.Error()
		c.Logf("ROLLBACK FAILED: %v", rerr)
	}
	_ = c.finishWith(p, Result{OK: false, Version: p.Target, From: p.From, Error: msg, At: c.Now()})
	return err
}

// restartAndCheck restarts the changed services and waits for them to answer.
// wantVersion is what push-manager must report, when its binary changed or was
// just rolled back.
func (c *FinishConfig) restartAndCheck(p Pending, wantVersion string) error {
	if p.CatalogChanged {
		if err := c.restart(svcCatalog); err != nil {
			return fmt.Errorf("push-catalog: %v", err)
		}
		if _, err := c.waitHealthy(c.CatalogURL, "", svcCatalog); err != nil {
			return fmt.Errorf("push-catalog did not come back: %v", err)
		}
	}
	if p.ManagerChanged {
		if err := c.restart(svcManager); err != nil {
			return fmt.Errorf("push-manager: %v", err)
		}
		if _, err := c.waitHealthy(c.ManagerURL, wantVersion, svcManager); err != nil {
			return fmt.Errorf("push-manager did not come back as %s: %v", wantVersion, err)
		}
	}
	return nil
}

func (c *FinishConfig) restart(svc string) error {
	c.Logf("restarting %s", svc)
	if err := c.Service(svc, "stop"); err != nil {
		return err
	}
	if c.WaitExit != nil {
		if err := c.WaitExit(svc); err != nil {
			return err
		}
	}
	return c.Service(svc, "start")
}

// waitHealthy polls url until it answers (with want as its version, if want is
// set), then checks it again after StableFor so a crash loop is caught.
//
// A service that is not running for two checks in a row, after a first second
// to start, has crashed: fail at once instead of waiting out HealthWait.
func (c *FinishConfig) waitHealthy(url, want, svc string) (string, error) {
	ok := func() (string, error) {
		v, err := c.Health(url)
		if err == nil && want != "" && v != want {
			err = fmt.Errorf("it reports %q", v)
		}
		return v, err
	}
	deadline := c.Now().Add(c.HealthWait)
	var last error
	start, dead := c.Now(), 0
	for {
		if c.Alive != nil && c.Now().Sub(start) >= time.Second {
			if c.Alive(svc) {
				dead = 0
			} else if dead++; dead >= 2 {
				return "", fmt.Errorf("%s stopped right after it started", svc)
			}
		}
		if v, err := ok(); err == nil {
			c.Sleep(c.StableFor)
			if v, err = ok(); err == nil {
				return v, nil
			}
			last = fmt.Errorf("it stopped again: %v", err)
		} else {
			last = err
		}
		if !c.Now().Before(deadline) {
			return "", last
		}
		c.Sleep(500 * time.Millisecond)
	}
}

func (c *FinishConfig) finishWith(p Pending, r Result) error {
	r.ScriptChanged = r.OK && p.ScriptChanged
	b, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(filepath.Join(c.Dir, resultFile), b, 0o644); err != nil {
		return err
	}
	return os.Remove(filepath.Join(c.Dir, pendingFile))
}

// LoadResult reads how the last update ended, if it did.
func LoadResult(dir string) (Result, bool) {
	var r Result
	b, err := os.ReadFile(filepath.Join(dir, resultFile))
	if err != nil || json.Unmarshal(b, &r) != nil {
		return r, false
	}
	return r, true
}

// ClearResult removes the result file once the user has seen it.
func ClearResult(dir string) { _ = os.Remove(filepath.Join(dir, resultFile)) }

// Recover runs at push-manager start. A pending.json whose helper is gone is
// an update that was cut off (power loss, kill). The running version says how
// it ended. A live helper owns the file: leave it alone.
func Recover(dir, running string, now time.Time) {
	p, err := LoadPending(dir)
	if err != nil {
		return
	}
	if p.HelperPID > 0 && processAlive(p.HelperPID) {
		return
	}
	r := Result{Version: p.Target, From: p.From, At: now}
	if running == p.Target {
		r.OK = true
	} else {
		r.Error = "the update was cut off; push-manager runs " + running
		restoreFiles(p, func(string, ...any) {}) // best effort: files back to the old version
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, resultFile), b, 0o644)
	_ = os.Remove(filepath.Join(dir, pendingFile))
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
