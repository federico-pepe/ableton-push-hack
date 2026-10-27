package main

// update_wire.go -- start the Push Hack update checker, its web routes, and
// the install helper. The logic lives in the update package; plan:
// plans/2026-10-20-push-hack-self-update.md.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"push-manager/update"
)

// remoteScriptsRoot is Live's Remote Scripts folder in the User Library. The
// update swaps the one folder PushHack inside it and touches nothing else.
// A variable only so tests can point it at a temp directory.
var remoteScriptsRoot = "/data/Music/Ableton/User Library/Remote Scripts"

const releaseDownloadPrefix = "https://github.com/federico-pepe/ableton-push-hack/releases/download/"

var (
	updateApplier *update.Applier // set by startUpdateChecker
	updateAPI     *update.API
)

// updateDirFor: state lives in <push-hack dir>/update/, two levels above this
// hack's own dir.
func updateDirFor(configPath string) string {
	dir := filepath.Join(filepath.Dir(configPath), "..", "..", "update")
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return dir
}

// startUpdateChecker serves /api/update/* and starts the daily check.
func startUpdateChecker(configPath string, mux *http.ServeMux) {
	dir := updateDirFor(configPath)
	hacks := filepath.Dir(filepath.Dir(filepath.Clean(configPath)))
	if abs, err := filepath.Abs(hacks); err == nil {
		hacks = abs
	}
	logDir := filepath.Join(filepath.Dir(hacks), "logs")

	update.Recover(dir, buildVersion, time.Now()) // an update cut off by a power loss
	updateDir = dir

	store := update.OpenStore(filepath.Join(dir, "settings.json"))
	checker := update.New(update.Config{
		Installed:         buildVersion,
		URL:               store.Get().URL,
		StatePath:         filepath.Join(dir, "state.json"),
		AutoCheck:         store.AutoCheck,
		IncludePrerelease: store.IncludePrerelease,
		Logf:              log.Printf,
	})
	updateChecker = checker // the Shadow UI bar reads it (ui_update.go)

	prefixes := []string{releaseDownloadPrefix}
	if u, err := url.Parse(store.Get().URL); err == nil && u.Scheme != "" && u.Host != "" {
		prefixes = append(prefixes, u.Scheme+"://"+u.Host+"/") // bench setting: a local release list
	}
	updateApplier = update.NewApplier(update.ApplyConfig{
		Dir: dir,
		Targets: map[string]string{
			"push-manager/push-manager": filepath.Join(hacks, "push-manager", "push-manager"),
			"push-catalog/push-catalog": filepath.Join(hacks, "push-catalog", "push-catalog"),
			"push-display/push_hook.so": filepath.Join(hacks, "push-display", "push_hook.so"),
		},
		Dirs:            map[string]string{"remote-script/PushHack": filepath.Join(remoteScriptsRoot, "PushHack")},
		ExecFiles:       map[string]bool{"push-manager/push-manager": true, "push-catalog/push-catalog": true},
		AllowedPrefixes: prefixes,
		Installed:       buildVersion,
		LoadedShm:       loadedHookShmVersion,
		StartHelper:     func(binary string) (<-chan error, error) { return startUpdateHelper(binary, dir, logDir) },
		Logf:            log.Printf,
	})
	updateAPI = &update.API{C: checker, S: store, A: updateApplier, Dir: dir}
	updateAPI.Register(mux)

	go func() {
		waitForBootSettle() // no network use while USB-A settles; no-op on a restart
		// 1 min, plus up to 10 min of jitter, so a fleet does not call together.
		checker.Run(context.Background(), time.Minute+time.Duration(rand.Int63n(int64(10*time.Minute))))
	}()
}

// loadedHookShmVersion reads the version the display hook in Push3 wrote at
// the head of the shared framebuffer. ok is false when no hook is loaded.
func loadedHookShmVersion() (uint32, bool) {
	f, err := os.Open("/data/push-hack/hacks/push-display/framebuf")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var h [8]byte
	if _, err := io.ReadFull(f, h[:]); err != nil {
		return 0, false
	}
	if binary.LittleEndian.Uint32(h[0:]) != 0x50555348 { // "HSUP": the hook's magic
		return 0, false
	}
	return binary.LittleEndian.Uint32(h[4:]), true
}

// startUpdateHelper runs "<binary> -finish-update" in its own session, so
// stopping push-manager does not stop it. The channel gets its exit status if
// it ends while this process still runs.
func startUpdateHelper(binary, dir, logDir string) (<-chan error, error) {
	_ = os.MkdirAll(logDir, 0o755)
	lf, err := os.OpenFile(filepath.Join(logDir, "update.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(binary, "-finish-update", "-update-dir", dir)
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		lf.Close()
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); lf.Close() }() // also reaps it
	return done, nil
}

// runFinishUpdate is the helper: restart what changed, check it, roll back if
// it fails. It returns the exit code.
func runFinishUpdate(dir string) int {
	log.SetFlags(log.LstdFlags)
	pids := map[string]int{} // read before "stop": the init script deletes the pidfile
	var mu sync.Mutex
	cfg := update.FinishConfig{
		Dir:        dir,
		ManagerURL: "http://127.0.0.1:7701/api/status",
		CatalogURL: "http://127.0.0.1:7702/api/installed",
		Health:     httpHealth,
		Logf:       log.Printf,
		Service: func(name, action string) error {
			if action == "stop" {
				if b, err := os.ReadFile("/var/run/" + name + ".pid"); err == nil {
					if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
						mu.Lock()
						pids[name] = pid
						mu.Unlock()
					}
				}
			}
			out, err := exec.Command("/etc/init.d/"+name, action).CombinedOutput()
			if err != nil {
				return fmt.Errorf("%s %s: %v: %s", name, action, err, strings.TrimSpace(string(out)))
			}
			return nil
		},
		Alive: func(name string) bool {
			b, err := os.ReadFile("/var/run/" + name + ".pid")
			if err != nil {
				return false
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			return err == nil && pid > 0 && syscall.Kill(pid, 0) == nil
		},
		WaitExit: func(name string) error {
			mu.Lock()
			pid := pids[name]
			mu.Unlock()
			if pid <= 0 {
				return nil
			}
			for i := 0; i < 80; i++ { // 8 s
				if syscall.Kill(pid, 0) != nil {
					return nil
				}
				time.Sleep(100 * time.Millisecond)
			}
			log.Printf("%s (pid %d) did not exit in 8 s; killing it", name, pid)
			_ = syscall.Kill(pid, syscall.SIGKILL)
			time.Sleep(300 * time.Millisecond)
			return nil
		},
	}
	if err := update.Finish(cfg); err != nil {
		log.Printf("finish-update: %v", err)
		return 1
	}
	return 0
}

// httpHealth GETs url; for push-manager's /api/status it returns hack_version.
func httpHealth(u string) (string, error) {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("answered %s", resp.Status)
	}
	var body struct {
		HackVersion string `json:"hack_version"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body)
	return body.HackVersion, nil
}
