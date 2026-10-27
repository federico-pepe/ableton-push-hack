package update

// apply.go -- download a release, check it, and put it in place.
//
// Steps: manifest, then tarball, then hash and extract, then a trial run of
// each new binary, then an atomic swap that keeps the old file as <file>.prev.
// Nothing here restarts anything: the swap ends by starting a detached helper
// (finish.go) that restarts the services and rolls back if they do not come up.
//
// Every file in the tarball must be named in the manifest, and its SHA-256
// must match. Plan: plans/2026-10-20-push-hack-self-update.md.

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"push-manager/semver"
)

const (
	defaultMaxTarball = 64 << 20
	manifestAsset     = "manifest.json"
)

// Refusals the user should read. Anything else is a failure.
var (
	ErrNeedsReinstall = errors.New("this release changes system files: run install.sh from a computer")
	ErrHookMismatch   = errors.New("this release needs a new display hook: restart Push first, then update")
	ErrBusy           = errors.New("an update is already running")
)

// Manifest is manifest.json from the release.
type Manifest struct {
	Version        string         `json:"version"`
	ShmProtocol    uint32         `json:"shm_protocol"`
	NeedsReinstall bool           `json:"needs_reinstall"`
	Files          []ManifestFile `json:"files"`
	Tarball        struct {
		Name   string `json:"name"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	} `json:"tarball"`
}

// ManifestFile is one file inside the tarball.
type ManifestFile struct {
	Path   string `json:"path"` // e.g. "push-manager/push-manager"
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// ApplyConfig sets up an Applier.
type ApplyConfig struct {
	Dir     string            // update dir, e.g. /data/push-hack/update
	Targets map[string]string // manifest path -> installed path
	// Dirs maps a manifest directory to an installed directory, e.g.
	// "remote-script/PushHack" -> Live's User Library. Its files are checked
	// like any other, then the whole directory is swapped at once, so a
	// removed file does not linger. The old one is kept in Dir/remote-script.prev.
	Dirs            map[string]string
	ExecFiles       map[string]bool // manifest paths that must be executable
	AllowedPrefixes []string        // a download URL must start with one of these
	Installed       string
	Client          *http.Client
	MaxTarball      int64

	// Hooks, so tests need no device. Nil means the real thing.
	RunVersion func(binary string) (string, error)
	LoadedShm  func() (uint32, bool) // the display hook loaded in Push3, if any
	FreeBytes  func(dir string) (uint64, error)
	// StartHelper runs "<binary> -finish-update" in its own session. The
	// channel gets the helper's exit if it ends while this process still runs.
	StartHelper func(binary string) (<-chan error, error)
	Logf        func(format string, args ...any)
}

// ApplyState is what the screen and the page show.
type ApplyState struct {
	Phase   string `json:"phase"` // "", downloading, verifying, installing, restarting, failed
	Version string `json:"version"`
	Error   string `json:"error"`
}

// Applier runs one update at a time.
type Applier struct {
	cfg ApplyConfig
	mu  sync.Mutex
	st  ApplyState
}

// NewApplier fills in defaults.
func NewApplier(cfg ApplyConfig) *Applier {
	if cfg.MaxTarball == 0 {
		cfg.MaxTarball = defaultMaxTarball
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 5 * time.Minute}
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.RunVersion == nil {
		cfg.RunVersion = runVersion
	}
	if cfg.FreeBytes == nil {
		cfg.FreeBytes = freeBytes
	}
	return &Applier{cfg: cfg}
}

// State returns a snapshot.
func (a *Applier) State() ApplyState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.st
}

func (a *Applier) set(phase, version, errMsg string) {
	a.mu.Lock()
	a.st = ApplyState{Phase: phase, Version: version, Error: errMsg}
	a.mu.Unlock()
}

// Start runs the update for rel in the background. It returns ErrBusy while
// another one runs. Progress shows in State; a failure ends in phase "failed".
func (a *Applier) Start(rel Release) error {
	a.mu.Lock()
	if p := a.st.Phase; p != "" && p != "failed" {
		a.mu.Unlock()
		return ErrBusy
	}
	a.st = ApplyState{Phase: "downloading", Version: rel.Version}
	a.mu.Unlock()
	go func() {
		if err := a.Run(rel); err != nil {
			a.cfg.Logf("update to %s failed: %v", rel.Version, err)
			a.set("failed", rel.Version, err.Error())
		}
	}()
	return nil
}

// Run does the whole update and returns when the helper is started.
func (a *Applier) Run(rel Release) error {
	c := a.cfg
	cur, err := semver.Parse(c.Installed)
	if err != nil {
		return fmt.Errorf("this is a dev build (%q): it does not update itself", c.Installed)
	}
	tgt, err := semver.Parse(rel.Version)
	if err != nil || tgt.Compare(cur) <= 0 {
		return fmt.Errorf("%s is not newer than %s", rel.Version, c.Installed)
	}

	stage := filepath.Join(c.Dir, "staging")
	defer os.RemoveAll(stage) // the files are moved out or not wanted; the tarball is 6 MB
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}

	// 1. The manifest says what the release holds and whether it may apply.
	a.set("downloading", rel.Version, "")
	mURL, ok := findAsset(rel, manifestAsset)
	if !ok {
		return fmt.Errorf("release has no %s", manifestAsset)
	}
	var m Manifest
	body, err := a.get(mURL, 1<<20)
	if err != nil {
		return fmt.Errorf("manifest: %v", err)
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return fmt.Errorf("manifest: %v", err)
	}
	if mv, err := semver.Parse(m.Version); err != nil || mv.Compare(tgt) != 0 {
		return fmt.Errorf("manifest is for %q, not %s", m.Version, rel.Version)
	}
	if m.NeedsReinstall {
		return ErrNeedsReinstall
	}
	if c.LoadedShm != nil {
		if have, ok := c.LoadedShm(); ok && have != m.ShmProtocol {
			return ErrHookMismatch
		}
	}
	if err := checkManifestFiles(m, c.Targets, c.Dirs); err != nil {
		return err
	}
	if m.Tarball.Size <= 0 || m.Tarball.Size > c.MaxTarball {
		return fmt.Errorf("tarball size %d is not acceptable", m.Tarball.Size)
	}
	if free, err := c.FreeBytes(c.Dir); err == nil && free < uint64(m.Tarball.Size)*3 {
		return fmt.Errorf("not enough free space (%d MB)", free>>20)
	}

	// 2. The tarball, checked against the manifest while it downloads.
	tURL, ok := findAsset(rel, m.Tarball.Name)
	if !ok {
		return fmt.Errorf("release has no %s", m.Tarball.Name)
	}
	tarPath := filepath.Join(stage, "release.tar.gz")
	if err := a.download(tURL, tarPath, m.Tarball.SHA256, m.Tarball.Size); err != nil {
		return fmt.Errorf("tarball: %v", err)
	}

	// 3. Extract, hash every file, and run each new binary once.
	a.set("verifying", rel.Version, "")
	files := filepath.Join(stage, "files")
	if err := extract(tarPath, files, m); err != nil {
		return fmt.Errorf("tarball: %v", err)
	}
	for p := range c.ExecFiles {
		got, err := c.RunVersion(filepath.Join(files, filepath.FromSlash(p)))
		if err != nil {
			return fmt.Errorf("%s does not run here: %v", p, err)
		}
		if gv, err := semver.Parse(got); err != nil || gv.Compare(tgt) != 0 {
			return fmt.Errorf("%s reports version %q, not %s", p, got, rel.Version)
		}
	}

	// 4. Swap in the files that differ.
	a.set("installing", rel.Version, "")
	p, err := a.install(m, files)
	if err != nil {
		return err
	}
	p.Target, p.From, p.StartedAt = rel.Version, c.Installed, time.Now()
	p.ManagerChanged = p.changed(c.Targets["push-manager/push-manager"])
	p.ScriptChanged = len(p.Dirs) > 0
	if len(p.Swapped) == 0 && len(p.Dirs) == 0 {
		return fmt.Errorf("every file is already the one in %s: nothing to install", rel.Version)
	}

	// 5. Hand over. The helper restarts the services and undoes the swap if
	// they do not come up.
	a.set("restarting", rel.Version, "")
	if c.StartHelper == nil {
		return errors.New("no helper to start")
	}
	if err := savePending(c.Dir, p); err != nil {
		a.rollback(p)
		return err
	}
	// The helper is the OLD push-manager, kept as .prev: it is known to work,
	// and it is the code that knows how to put the old files back. The new
	// binary only passed "-version", which proves little.
	helper := c.Targets["push-manager/push-manager"]
	if p.ManagerChanged {
		helper += ".prev"
	}
	done, err := c.StartHelper(helper)
	if err != nil {
		a.rollback(p)
		_ = os.Remove(filepath.Join(c.Dir, pendingFile))
		return fmt.Errorf("helper: %v", err)
	}
	go a.watchHelper(done, p, rel.Version)
	return nil
}

// watchHelper runs while this process is alive. A helper that ends now did not
// restart us: it failed, or had nothing to restart. Say which, and undo the
// swap if the helper died before it could.
func (a *Applier) watchHelper(done <-chan error, p Pending, version string) {
	err := <-done
	if r, ok := LoadResult(a.cfg.Dir); ok { // the helper finished its job and wrote the outcome
		if r.OK {
			a.set("", version, "")
		} else {
			a.set("failed", version, r.Error)
		}
		return
	}
	if _, perr := LoadPending(a.cfg.Dir); perr == nil { // it died before cleaning up
		restoreFiles(p, a.cfg.Logf)
		_ = os.Remove(filepath.Join(a.cfg.Dir, pendingFile))
	}
	msg := "the update helper stopped before it finished"
	if err != nil {
		msg += ": " + err.Error()
	}
	a.cfg.Logf("update to %s: %s", version, msg)
	a.set("failed", version, msg+". The old version was put back.")
}

// checkManifestFiles: every file must map to an install path, and the three
// we run must be listed. A file we do not know is a release we do not trust.
func checkManifestFiles(m Manifest, targets, dirs map[string]string) error {
	seen := map[string]bool{}
	inDir := map[string]int{}
	for _, f := range m.Files {
		if len(f.SHA256) != 64 || f.Size <= 0 || seen[f.Path] {
			return fmt.Errorf("manifest entry for %q is bad", f.Path)
		}
		seen[f.Path] = true
		if prefix, ok := dirOf(f.Path, dirs); ok {
			inDir[prefix]++
			continue
		}
		if _, ok := targets[f.Path]; !ok {
			return fmt.Errorf("manifest lists %q, which this version does not know", f.Path)
		}
	}
	for p := range targets {
		if !seen[p] {
			return fmt.Errorf("manifest has no entry for %q", p)
		}
	}
	for prefix := range dirs {
		if !seen[prefix+"/__init__.py"] {
			return fmt.Errorf("manifest has no %s/__init__.py", prefix)
		}
	}
	return nil
}

// dirOf says which managed directory a manifest path belongs to.
func dirOf(p string, dirs map[string]string) (string, bool) {
	for prefix := range dirs {
		if strings.HasPrefix(p, prefix+"/") {
			return prefix, true
		}
	}
	return "", false
}

func findAsset(rel Release, name string) (string, bool) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a.URL, true
		}
	}
	return "", false
}

func (a *Applier) allowed(url string) bool {
	for _, p := range a.cfg.AllowedPrefixes {
		if strings.HasPrefix(url, p) {
			return true
		}
	}
	return false
}

func (a *Applier) get(url string, max int64) ([]byte, error) {
	if !a.allowed(url) {
		return nil, fmt.Errorf("%s is not an allowed download address", url)
	}
	resp, err := a.cfg.Client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("answer is too big")
	}
	return b, nil
}

func (a *Applier) download(url, dst, wantSHA string, wantSize int64) error {
	if !a.allowed(url) {
		return fmt.Errorf("%s is not an allowed download address", url)
	}
	resp, err := a.cfg.Client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("answered %s", resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, wantSize+1))
	if err != nil {
		return err
	}
	if n != wantSize {
		return fmt.Errorf("got %d bytes, the manifest says %d", n, wantSize)
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(wantSHA) {
		return errors.New("SHA-256 does not match the manifest")
	}
	return nil
}

// extract unpacks the tarball into dst. Only regular files named in the
// manifest are written, each checked against its hash. The top directory
// (push-hack-<version>/) is dropped.
func extract(tarPath, dst string, m Manifest) error {
	want := map[string]ManifestFile{}
	for _, f := range m.Files {
		want[f.Path] = f
	}
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	got := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("%q is not a plain file", h.Name)
		}
		name := path.Clean(h.Name)
		i := strings.IndexByte(name, '/')
		if i < 0 || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return fmt.Errorf("bad name %q", h.Name)
		}
		rel := name[i+1:]
		if rel == "manifest.json" {
			continue // the loose copy is the one we read
		}
		mf, ok := want[rel]
		if !ok {
			return fmt.Errorf("%q is not in the manifest", rel)
		}
		if h.Size != mf.Size {
			return fmt.Errorf("%q has size %d, the manifest says %d", rel, h.Size, mf.Size)
		}
		out := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		of, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
		if err != nil {
			return err
		}
		hh := sha256.New()
		_, err = io.Copy(io.MultiWriter(of, hh), io.LimitReader(tr, mf.Size))
		if cerr := of.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if hex.EncodeToString(hh.Sum(nil)) != strings.ToLower(mf.SHA256) {
			return fmt.Errorf("%q: SHA-256 does not match the manifest", rel)
		}
		got[rel] = true
	}
	for p := range want {
		if !got[p] {
			return fmt.Errorf("%q is missing from the tarball", p)
		}
	}
	return nil
}

// Swapped is one file put in place.
type Swapped struct {
	Dest    string `json:"dest"`
	HadPrev bool   `json:"had_prev"` // Dest.prev holds the old file
}

// Pending is what the helper reads. It is the record of a swap in progress.
type Pending struct {
	Target         string    `json:"target"`
	From           string    `json:"from"`
	StartedAt      time.Time `json:"started_at"`
	HelperPID      int       `json:"helper_pid"`
	ManagerChanged bool      `json:"manager_changed"`
	CatalogChanged bool      `json:"catalog_changed"`
	HookChanged    bool      `json:"hook_changed"`
	ScriptChanged  bool      `json:"script_changed"` // Live loads it at its next start
	Swapped        []Swapped `json:"swapped"`
	Dirs           []DirSwap `json:"dirs"`
}

// DirSwap is one directory put in place. Prev holds the old one, if any.
type DirSwap struct {
	Dest    string `json:"dest"`
	Prev    string `json:"prev"`
	HadPrev bool   `json:"had_prev"`
}

func (p Pending) changed(dest string) bool {
	for _, s := range p.Swapped {
		if s.Dest == dest {
			return true
		}
	}
	return false
}

// install swaps each changed file in place. The new file is renamed to
// <dest>.new first, the old one is hard-linked to <dest>.prev, and a rename
// then puts the new one over <dest>. At no moment is <dest> missing.
func (a *Applier) install(m Manifest, files string) (Pending, error) {
	var p Pending
	for _, mf := range m.Files {
		if _, isDir := dirOf(mf.Path, a.cfg.Dirs); isDir {
			continue // swapped as a whole, below
		}
		dest := a.cfg.Targets[mf.Path]
		src := filepath.Join(files, filepath.FromSlash(mf.Path))
		if _, err := os.Stat(filepath.Dir(dest)); err != nil {
			continue // that hack is not installed here
		}
		if cur, err := fileSHA(dest); err == nil && cur == strings.ToLower(mf.SHA256) {
			continue // already the same file
		}
		mode, uid, gid, hadOld := os.FileMode(0o755), -1, -1, false
		if st, err := os.Stat(dest); err == nil {
			hadOld = true
			mode = st.Mode().Perm()
			if sys, ok := st.Sys().(*syscall.Stat_t); ok {
				uid, gid = int(sys.Uid), int(sys.Gid)
			}
		}
		if a.cfg.ExecFiles[mf.Path] {
			mode |= 0o111
		}
		tmp := dest + ".new"
		_ = os.Remove(tmp)
		if err := os.Rename(src, tmp); err != nil {
			a.rollback(p)
			return p, fmt.Errorf("move %s: %v (staging must be on the same disk as the install)", mf.Path, err)
		}
		if err := os.Chmod(tmp, mode); err != nil {
			a.rollback(p)
			return p, err
		}
		if uid >= 0 {
			_ = os.Chown(tmp, uid, gid) // best effort: only root can
		}
		prev := dest + ".prev"
		if hadOld {
			_ = os.Remove(prev)
			if err := os.Link(dest, prev); err != nil {
				a.rollback(p)
				return p, fmt.Errorf("keep old %s: %v", mf.Path, err)
			}
		}
		if err := os.Rename(tmp, dest); err != nil {
			a.rollback(p)
			return p, fmt.Errorf("replace %s: %v", mf.Path, err)
		}
		p.Swapped = append(p.Swapped, Swapped{Dest: dest, HadPrev: hadOld})
		switch mf.Path {
		case "push-catalog/push-catalog":
			p.CatalogChanged = true
		case "push-display/push_hook.so":
			p.HookChanged = true
		}
	}
	for prefix, dest := range a.cfg.Dirs {
		d, err := a.installDir(prefix, dest, files, m)
		if err != nil {
			a.rollback(p)
			return p, err
		}
		if d != nil {
			p.Dirs = append(p.Dirs, *d)
		}
	}
	return p, nil
}

// installDir puts the staged directory at dest. The old one goes to
// Dir/remote-script.prev, not next to dest: Live lists every folder with an
// __init__.py in Remote Scripts, so a backup there would show in its menu.
// It returns nil when nothing needs doing: the User Library is not on this
// Push, or the installed copy is already the same.
func (a *Applier) installDir(prefix, dest, files string, m Manifest) (*DirSwap, error) {
	parent := filepath.Dir(dest)
	if _, err := os.Stat(parent); err != nil {
		if _, err := os.Stat(filepath.Dir(parent)); err != nil {
			return nil, nil // no User Library here: not ours to create
		}
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return nil, err
		}
	}
	want := map[string]string{} // path relative to the directory -> sha256
	for _, f := range m.Files {
		if strings.HasPrefix(f.Path, prefix+"/") {
			want[strings.TrimPrefix(f.Path, prefix+"/")] = strings.ToLower(f.SHA256)
		}
	}
	if dirMatches(dest, want) {
		return nil, nil
	}
	src := filepath.Join(files, filepath.FromSlash(prefix))
	uid, gid := -1, -1
	if st, err := os.Stat(parent); err == nil {
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(sys.Uid), int(sys.Gid)
		}
	}
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if info.IsDir() {
			mode = 0o755
		}
		if err := os.Chmod(p, mode); err != nil {
			return err
		}
		if uid >= 0 {
			_ = os.Chown(p, uid, gid) // best effort: only root can
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	d := &DirSwap{Dest: dest, Prev: filepath.Join(a.cfg.Dir, "remote-script.prev")}
	if _, err := os.Stat(dest); err == nil {
		d.HadPrev = true
		_ = os.RemoveAll(d.Prev)
		if err := os.Rename(dest, d.Prev); err != nil {
			return nil, fmt.Errorf("keep old %s: %v (staging must be on the same disk as the install)", prefix, err)
		}
	}
	if err := os.Rename(src, dest); err != nil {
		if d.HadPrev {
			_ = os.Rename(d.Prev, dest)
		}
		return nil, fmt.Errorf("replace %s: %v", prefix, err)
	}
	return d, nil
}

// dirMatches: dir holds exactly these files with these hashes.
func dirMatches(dir string, want map[string]string) bool {
	n := 0
	ok := true
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		sum, found := want[filepath.ToSlash(rel)]
		if !found {
			ok = false
			return nil
		}
		if got, err := fileSHA(p); err != nil || got != sum {
			ok = false
		}
		n++
		return nil
	})
	return err == nil && ok && n == len(want)
}

// rollback puts the .prev files back. Best effort; used when a swap fails
// before the helper starts. The helper has its own copy (finish.go).
func (a *Applier) rollback(p Pending) { restoreFiles(p, a.cfg.Logf) }

func restoreFiles(p Pending, logf func(string, ...any)) {
	for _, d := range p.Dirs {
		if err := os.RemoveAll(d.Dest); err != nil {
			logf("rollback %s: %v", d.Dest, err)
			continue
		}
		if d.HadPrev {
			if err := os.Rename(d.Prev, d.Dest); err != nil {
				logf("rollback %s: %v", d.Dest, err)
			}
		}
	}
	for _, s := range p.Swapped {
		if !s.HadPrev {
			continue
		}
		if err := os.Rename(s.Dest+".prev", s.Dest); err != nil {
			logf("rollback %s: %v", s.Dest, err)
		}
	}
}

func fileSHA(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

const pendingFile = "pending.json"

func savePending(dir string, p Pending) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, pendingFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, pendingFile))
}

// LoadPending reads the swap record, if one exists.
func LoadPending(dir string) (Pending, error) {
	var p Pending
	b, err := os.ReadFile(filepath.Join(dir, pendingFile))
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal(b, &p)
}
