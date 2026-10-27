package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	pMgr  = "push-manager/push-manager"
	pCat  = "push-catalog/push-catalog"
	pHook = "push-display/push_hook.so"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type rel struct {
	t         *testing.T
	srv       *httptest.Server
	dir       string // update dir
	inst      string // install dir
	targets   map[string]string
	dirs      map[string]string // manifest dir -> installed dir
	files     map[string][]byte // new contents by manifest path
	tarEdit   func(name string, body []byte) (string, []byte, bool)
	man       Manifest
	tarGz     []byte
	done      chan error // what the fake helper's exit looks like
	startedMu sync.Mutex
	started   []string
	helper    error
	shm       func() (uint32, bool)
	run       func(string) (string, error)
	app       *Applier
	release   Release
}

func newRel(t *testing.T) *rel {
	t.Helper()
	r := &rel{t: t, dir: t.TempDir(), inst: t.TempDir()}
	r.targets = map[string]string{
		pMgr:  filepath.Join(r.inst, "push-manager"),
		pCat:  filepath.Join(r.inst, "push-catalog"),
		pHook: filepath.Join(r.inst, "push_hook.so"),
	}
	for _, d := range r.targets {
		os.WriteFile(d, []byte("old "+filepath.Base(d)), 0o755)
	}
	r.files = map[string][]byte{pMgr: []byte("new mgr"), pCat: []byte("new cat"), pHook: []byte("old push_hook.so")}
	return r
}

// build makes the manifest and tarball from r.files and starts the server.
func (r *rel) build(version string) {
	r.t.Helper()
	r.man = Manifest{Version: version, ShmProtocol: 1}
	for p, b := range r.files {
		r.man.Files = append(r.man.Files, ManifestFile{Path: p, SHA256: sum(b), Size: int64(len(b))})
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for p, b := range r.files {
		name, body := "push-hack-"+version+"/"+p, b
		if r.tarEdit != nil {
			if n, bb, ok := r.tarEdit(p, b); ok {
				name, body = n, bb
			}
		}
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write(body)
	}
	tw.Close()
	gz.Close()
	r.tarGz = buf.Bytes()
	r.man.Tarball.Name = "push-hack-" + version + ".tar.gz"
	r.man.Tarball.SHA256 = sum(r.tarGz)
	r.man.Tarball.Size = int64(len(r.tarGz))
}

func (r *rel) serve(version string) {
	r.t.Helper()
	mj, _ := json.Marshal(r.man)
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		switch {
		case strings.HasSuffix(q.URL.Path, "manifest.json"):
			w.Write(mj)
		case strings.HasSuffix(q.URL.Path, ".tar.gz"):
			w.Write(r.tarGz)
		default:
			w.WriteHeader(404)
		}
	}))
	r.t.Cleanup(r.srv.Close)
	r.release = Release{Tag: "v" + version, Version: version, Assets: []Asset{
		{Name: "manifest.json", URL: r.srv.URL + "/manifest.json"},
		{Name: r.man.Tarball.Name, URL: r.srv.URL + "/" + r.man.Tarball.Name},
	}}
	r.app = NewApplier(ApplyConfig{
		Dir: r.dir, Targets: r.targets, Dirs: r.dirs, Installed: "0.1.10-alpha",
		ExecFiles:       map[string]bool{pMgr: true, pCat: true},
		AllowedPrefixes: []string{r.srv.URL + "/"},
		LoadedShm:       r.shm,
		RunVersion: func(b string) (string, error) {
			if r.run != nil {
				return r.run(b)
			}
			return version, nil
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 30, nil },
		StartHelper: func(m string) (<-chan error, error) {
			r.startedMu.Lock()
			defer r.startedMu.Unlock()
			r.started = append(r.started, m)
			if r.done == nil {
				r.done = make(chan error, 1)
			}
			return r.done, r.helper
		},
	})
}

func (r *rel) ready(version string) { r.build(version); r.serve(version) }

func (r *rel) read(p string) string {
	b, err := os.ReadFile(r.targets[p])
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func (r *rel) untouched() {
	r.t.Helper()
	for p, d := range r.targets {
		if got := r.read(p); got != "old "+filepath.Base(d) {
			r.t.Fatalf("%s was changed: %q", p, got)
		}
	}
}

func TestApplyHappyPath(t *testing.T) {
	r := newRel(t)
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	if r.read(pMgr) != "new mgr" || r.read(pCat) != "new cat" {
		t.Fatal("binaries not replaced")
	}
	if b, _ := os.ReadFile(r.targets[pMgr] + ".prev"); string(b) != "old push-manager" {
		t.Fatalf(".prev = %q", b)
	}
	if _, err := os.Stat(r.targets[pHook] + ".prev"); err == nil {
		t.Fatal("unchanged hook was swapped")
	}
	if len(r.started) != 1 || r.started[0] != r.targets[pMgr]+".prev" {
		t.Fatalf("helper = %v", r.started)
	}
	p, err := LoadPending(r.dir)
	if err != nil || p.Target != "0.1.11-alpha" || p.From != "0.1.10-alpha" || !p.ManagerChanged || !p.CatalogChanged || p.HookChanged || len(p.Swapped) != 2 {
		t.Fatalf("pending = %+v %v", p, err)
	}
	if st, _ := os.Stat(r.targets[pMgr]); st.Mode().Perm()&0o111 == 0 {
		t.Fatal("binary is not executable")
	}
	if _, err := os.Stat(filepath.Join(r.dir, "staging")); err == nil {
		t.Fatal("staging left behind")
	}
}

func TestApplyHookChangeIsReported(t *testing.T) {
	r := newRel(t)
	r.files[pHook] = []byte("new hook")
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	if p, _ := LoadPending(r.dir); !p.HookChanged {
		t.Fatal("hook change not recorded")
	}
}

func TestApplyRefusals(t *testing.T) {
	cases := map[string]struct {
		setup func(r *rel)
		want  error
	}{
		"needs reinstall": {func(r *rel) { r.build("0.1.11-alpha"); r.man.NeedsReinstall = true }, ErrNeedsReinstall},
		"hook mismatch":   {func(r *rel) { r.shm = func() (uint32, bool) { return 2, true } }, ErrHookMismatch},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRel(t)
			if c.setup != nil {
				c.setup(r)
			}
			if r.tarGz == nil {
				r.build("0.1.11-alpha")
			}
			r.serve("0.1.11-alpha")
			if err := r.app.Run(r.release); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			r.untouched()
		})
	}
}

func TestApplyHookMatchOrNoHookIsFine(t *testing.T) {
	for _, shm := range []func() (uint32, bool){func() (uint32, bool) { return 1, true }, func() (uint32, bool) { return 0, false }} {
		r := newRel(t)
		r.shm = shm
		r.ready("0.1.11-alpha")
		if err := r.app.Run(r.release); err != nil {
			t.Fatal(err)
		}
	}
}

func TestApplyBadInputsLeaveFilesAlone(t *testing.T) {
	type tc struct {
		name  string
		setup func(r *rel)
	}
	for _, c := range []tc{
		{"tarball hash wrong", func(r *rel) { r.build("0.1.11-alpha"); r.man.Tarball.SHA256 = strings.Repeat("0", 64) }},
		{"tarball size wrong", func(r *rel) { r.build("0.1.11-alpha"); r.man.Tarball.Size++ }},
		{"file hash wrong", func(r *rel) {
			r.build("0.1.11-alpha")
			r.man.Files[0].SHA256 = strings.Repeat("a", 64)
		}},
		{"path traversal", func(r *rel) {
			r.tarEdit = func(p string, b []byte) (string, []byte, bool) {
				if p == pCat {
					return "push-hack-0.1.11-alpha/../../evil", b, true
				}
				return "", nil, false
			}
			r.build("0.1.11-alpha")
		}},
		{"file not in manifest", func(r *rel) {
			r.tarEdit = func(p string, b []byte) (string, []byte, bool) {
				if p == pCat {
					return "push-hack-0.1.11-alpha/push-catalog/extra", b, true
				}
				return "", nil, false
			}
			r.build("0.1.11-alpha")
		}},
		{"manifest version differs", func(r *rel) { r.build("0.1.11-alpha"); r.man.Version = "0.1.12-alpha" }},
		{"manifest lists unknown file", func(r *rel) {
			r.build("0.1.11-alpha")
			r.man.Files = append(r.man.Files, ManifestFile{Path: "etc/passwd", SHA256: strings.Repeat("b", 64), Size: 1})
		}},
		{"new binary reports wrong version", func(r *rel) {
			r.build("0.1.11-alpha")
			r.run = func(string) (string, error) { return "0.1.9-alpha", nil }
		}},
		{"new binary does not run", func(r *rel) {
			r.build("0.1.11-alpha")
			r.run = func(string) (string, error) { return "", errors.New("exec format error") }
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRel(t)
			c.setup(r)
			if r.tarGz == nil {
				r.build("0.1.11-alpha")
			}
			r.serve("0.1.11-alpha")
			if err := r.app.Run(r.release); err == nil {
				t.Fatal("want error")
			}
			r.untouched()
			if len(r.started) != 0 {
				t.Fatal("helper started")
			}
		})
	}
}

func TestApplyRejectsOtherAddresses(t *testing.T) {
	r := newRel(t)
	r.ready("0.1.11-alpha")
	r.app.cfg.AllowedPrefixes = []string{"https://github.com/federico-pepe/ableton-push-hack/releases/download/"}
	if err := r.app.Run(r.release); err == nil || !strings.Contains(err.Error(), "allowed") {
		t.Fatalf("err = %v", err)
	}
	r.untouched()
}

func TestApplyNotNewerOrDev(t *testing.T) {
	r := newRel(t)
	r.ready("0.1.10-alpha")
	if err := r.app.Run(r.release); err == nil {
		t.Fatal("same version accepted")
	}
	r2 := newRel(t)
	r2.ready("0.1.11-alpha")
	r2.app.cfg.Installed = "dev"
	if err := r2.app.Run(r2.release); err == nil {
		t.Fatal("dev build accepted")
	}
}

func TestApplyHelperFailureRestoresFiles(t *testing.T) {
	r := newRel(t)
	r.helper = errors.New("no such file")
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err == nil {
		t.Fatal("want error")
	}
	r.untouched()
	if _, err := LoadPending(r.dir); err == nil {
		t.Fatal("pending.json left behind")
	}
}

func TestApplyNotEnoughSpace(t *testing.T) {
	r := newRel(t)
	r.ready("0.1.11-alpha")
	r.app.cfg.FreeBytes = func(string) (uint64, error) { return 10, nil }
	if err := r.app.Run(r.release); err == nil || !strings.Contains(err.Error(), "space") {
		t.Fatalf("err = %v", err)
	}
	r.untouched()
}

func TestStartIsOneAtATime(t *testing.T) {
	r := newRel(t)
	r.ready("0.1.11-alpha")
	r.app.mu.Lock()
	r.app.st.Phase = "downloading"
	r.app.mu.Unlock()
	if err := r.app.Start(r.release); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestApplySkipsHackThatIsNotInstalled(t *testing.T) {
	r := newRel(t)
	r.targets[pCat] = filepath.Join(r.inst, "no-such-dir", "push-catalog")
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	p, _ := LoadPending(r.dir)
	if p.CatalogChanged || len(p.Swapped) != 1 {
		t.Fatalf("pending = %+v", p)
	}
	if _, err := os.Stat(filepath.Join(r.inst, "no-such-dir")); err == nil {
		t.Fatal("created the missing directory")
	}
}

func (r *rel) startedN() int {
	r.startedMu.Lock()
	defer r.startedMu.Unlock()
	return len(r.started)
}

func TestHelperThatDiesEarlyIsRolledBack(t *testing.T) {
	r := newRel(t)
	r.done = make(chan error, 1)
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	if r.read(pMgr) != "new mgr" {
		t.Fatal("swap did not happen")
	}
	r.done <- errors.New("exit status 1") // died, wrote no result
	waitPhase(t, r.app, "failed")
	r.untouched()
	if _, err := LoadPending(r.dir); err == nil {
		t.Fatal("pending.json left behind")
	}
	if !strings.Contains(r.app.State().Error, "helper stopped") {
		t.Fatalf("state = %+v", r.app.State())
	}
}

func TestHelperThatReportsFailureShowsIt(t *testing.T) {
	r := newRel(t)
	r.done = make(chan error, 1)
	r.ready("0.1.11-alpha")
	r.app.Run(r.release)
	os.WriteFile(filepath.Join(r.dir, resultFile), []byte(`{"ok":false,"version":"0.1.11-alpha","error":"push-manager did not come back"}`), 0o644)
	r.done <- errors.New("exit status 1")
	waitPhase(t, r.app, "failed")
	if r.app.State().Error != "push-manager did not come back" {
		t.Fatalf("state = %+v", r.app.State())
	}
}

func TestHelperThatSucceedsWithoutRestartingUs(t *testing.T) {
	r := newRel(t)
	r.done = make(chan error, 1)
	r.ready("0.1.11-alpha")
	r.app.Run(r.release)
	os.WriteFile(filepath.Join(r.dir, resultFile), []byte(`{"ok":true,"version":"0.1.11-alpha"}`), 0o644)
	r.done <- nil
	waitPhase(t, r.app, "")
}

func waitPhase(t *testing.T, a *Applier, want string) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if a.State().Phase == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("phase = %q, want %q", a.State().Phase, want)
}
