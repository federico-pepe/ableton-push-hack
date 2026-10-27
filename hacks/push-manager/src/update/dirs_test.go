package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pScript = "remote-script/PushHack"

// scriptRel is a release that also carries the Remote Script directory.
// lib is a fake User Library; the script goes to lib/Remote Scripts/PushHack.
func scriptRel(t *testing.T, files map[string]string) (*rel, string) {
	t.Helper()
	r := newRel(t)
	lib := filepath.Join(t.TempDir(), "User Library")
	if err := os.MkdirAll(filepath.Join(lib, "Remote Scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(lib, "Remote Scripts", "PushHack")
	r.dirs = map[string]string{pScript: dest}
	for name, body := range files {
		r.files[pScript+"/"+name] = []byte(body)
	}
	return r, dest
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestScriptFreshInstall(t *testing.T) {
	r, dest := scriptRel(t, map[string]string{"__init__.py": "init", "ns/transport.py": "tr"})
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dest, "__init__.py")) != "init" || readFile(t, filepath.Join(dest, "ns", "transport.py")) != "tr" {
		t.Fatal("script files missing")
	}
	if st, _ := os.Stat(filepath.Join(dest, "ns", "transport.py")); st.Mode().Perm() != 0o644 {
		t.Fatalf("script file mode = %v", st.Mode())
	}
	p, err := LoadPending(r.dir)
	if err != nil || !p.ScriptChanged || len(p.Dirs) != 1 || p.Dirs[0].HadPrev {
		t.Fatalf("pending = %+v %v", p, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "PushHack.prev")); err == nil {
		t.Fatal("a backup sits in Remote Scripts: Live would list it")
	}
}

func TestScriptUpgradeReplacesWholeDirAndKeepsOld(t *testing.T) {
	r, dest := scriptRel(t, map[string]string{"__init__.py": "new init"})
	os.MkdirAll(dest, 0o755)
	os.WriteFile(filepath.Join(dest, "__init__.py"), []byte("old init"), 0o644)
	os.WriteFile(filepath.Join(dest, "stale.py"), []byte("gone in the new release"), 0o644)
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dest, "__init__.py")) != "new init" {
		t.Fatal("not replaced")
	}
	if _, err := os.Stat(filepath.Join(dest, "stale.py")); err == nil {
		t.Fatal("a file the release dropped is still there")
	}
	prev := filepath.Join(r.dir, "remote-script.prev")
	if readFile(t, filepath.Join(prev, "stale.py")) == "" {
		t.Fatal("old directory not kept")
	}
	// The rollback puts the old one back, stale file and all.
	p, _ := LoadPending(r.dir)
	restoreFiles(p, t.Logf)
	if readFile(t, filepath.Join(dest, "__init__.py")) != "old init" || readFile(t, filepath.Join(dest, "stale.py")) == "" {
		t.Fatal("rollback did not restore the old directory")
	}
}

func TestScriptRollbackOfFreshInstallRemovesIt(t *testing.T) {
	r, dest := scriptRel(t, map[string]string{"__init__.py": "init"})
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	p, _ := LoadPending(r.dir)
	restoreFiles(p, t.Logf)
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("a rolled-back first install left the script behind")
	}
}

func TestScriptOnlyReleaseStillInstalls(t *testing.T) {
	r, dest := scriptRel(t, map[string]string{"__init__.py": "init"})
	r.files[pMgr], r.files[pCat], r.files[pHook] = []byte("old push-manager"), []byte("old push-catalog"), []byte("old push_hook.so")
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dest, "__init__.py")) != "init" {
		t.Fatal("not installed")
	}
	p, _ := LoadPending(r.dir)
	if p.ManagerChanged || p.CatalogChanged || len(p.Swapped) != 0 || !p.ScriptChanged {
		t.Fatalf("pending = %+v", p)
	}
}

func TestScriptUnchangedIsSkipped(t *testing.T) {
	r, dest := scriptRel(t, map[string]string{"__init__.py": "init", "a.py": "a"})
	os.MkdirAll(dest, 0o755)
	os.WriteFile(filepath.Join(dest, "__init__.py"), []byte("init"), 0o644)
	os.WriteFile(filepath.Join(dest, "a.py"), []byte("a"), 0o644)
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	p, _ := LoadPending(r.dir)
	if p.ScriptChanged || len(p.Dirs) != 0 {
		t.Fatalf("an identical script was swapped: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "remote-script.prev")); err == nil {
		t.Fatal("backup made for nothing")
	}
}

func TestScriptSkippedWithoutUserLibrary(t *testing.T) {
	r, dest := scriptRel(t, map[string]string{"__init__.py": "init"})
	os.RemoveAll(filepath.Dir(filepath.Dir(dest))) // no User Library at all
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(filepath.Dir(dest))); err == nil {
		t.Fatal("the User Library was created")
	}
	if p, _ := LoadPending(r.dir); p.ScriptChanged {
		t.Fatal("reported a change that did not happen")
	}
}

func TestScriptCreatesRemoteScriptsFolder(t *testing.T) {
	r, dest := scriptRel(t, map[string]string{"__init__.py": "init"})
	os.RemoveAll(filepath.Dir(dest)) // User Library exists, Remote Scripts does not
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dest, "__init__.py")) != "init" {
		t.Fatal("not installed")
	}
}

func TestScriptManifestNeedsInit(t *testing.T) {
	r, _ := scriptRel(t, map[string]string{"hub.py": "x"})
	r.ready("0.1.11-alpha")
	err := r.app.Run(r.release)
	if err == nil || !strings.Contains(err.Error(), "__init__.py") {
		t.Fatalf("err = %v", err)
	}
}

func TestScriptFilesOutsideTheDirAreStillRefused(t *testing.T) {
	r, _ := scriptRel(t, map[string]string{"__init__.py": "init"})
	r.files["remote-script/Other/__init__.py"] = []byte("x")
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err == nil {
		t.Fatal("unknown path accepted")
	}
}

// 0.1.16 carries no script. Its updater must leave an installed one alone, and
// a release without the directory must not trip the __init__.py check.
func TestScriptNotInReleaseLeavesInstalledOneAlone(t *testing.T) {
	r, dest := scriptRel(t, nil)
	os.MkdirAll(dest, 0o755)
	os.WriteFile(filepath.Join(dest, "__init__.py"), []byte("installed"), 0o644)
	os.WriteFile(filepath.Join(dest, "extra.py"), []byte("kept"), 0o644)
	r.ready("0.1.11-alpha")
	if err := r.app.Run(r.release); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dest, "__init__.py")) != "installed" || readFile(t, filepath.Join(dest, "extra.py")) != "kept" {
		t.Fatal("an installed script was touched by a release that does not carry it")
	}
	p, _ := LoadPending(r.dir)
	if p.ScriptChanged || len(p.Dirs) != 0 {
		t.Fatalf("pending = %+v", p)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "remote-script.prev")); err == nil {
		t.Fatal("a backup was made for nothing")
	}
}
