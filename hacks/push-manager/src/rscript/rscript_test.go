package rscript

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, name string) {
	t.Helper()
	os.MkdirAll(filepath.Join(root, name), 0o755)
	os.WriteFile(filepath.Join(root, name, "__init__.py"), []byte("x"), 0o644)
}

func none() (string, int, error) { return "", 0, os.ErrNotExist }

func TestReadNothingInstalled(t *testing.T) {
	st := Read(t.TempDir(), none)
	if st.Installed || st.Running || st.NeedsRestart || len(st.OldScripts) != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestReadVersionsAndRestart(t *testing.T) {
	root := t.TempDir()
	write(t, root, "PushHack")
	os.WriteFile(filepath.Join(root, "PushHack", "protocol.py"), []byte("PROTOCOL = 1\nSCRIPT_VERSION = \"0.2.0\"\n"), 0o644)
	if st := Read(root, none); !st.Installed || st.InstalledVersion != "0.2.0" || st.Running || !st.NeedsRestart {
		t.Fatalf("installed, not loaded yet: %+v", st)
	}
	st := Read(root, func() (string, int, error) { return "0.1.0", 1, nil })
	if !st.Running || st.RunningVersion != "0.1.0" || !st.NeedsRestart {
		t.Fatalf("older one running: %+v", st)
	}
	st = Read(root, func() (string, int, error) { return "0.2.0", 1, nil })
	if st.NeedsRestart || st.Protocol != 1 {
		t.Fatalf("same version running: %+v", st)
	}
}

func TestReadListsOnlyOldScripts(t *testing.T) {
	root := t.TempDir()
	write(t, root, "PushHackBrowser")
	write(t, root, "PushHackArrangement")
	write(t, root, "ClyphX_Pro")
	if got := strings.Join(Read(root, none).OldScripts, ","); got != "PushHackArrangement,PushHackBrowser" {
		t.Fatalf("old = %s", got)
	}
}

func TestRemoveOldNeedsPushHackFirst(t *testing.T) {
	root := t.TempDir()
	write(t, root, "PushHackBrowser")
	if err := RemoveOld(root, "PushHackBrowser"); !errors.Is(err, ErrNoPushHack) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "PushHackBrowser")); err != nil {
		t.Fatal("removed before PushHack was there")
	}
}

func TestRemoveOldRemovesOnlyThatOne(t *testing.T) {
	root := t.TempDir()
	write(t, root, "PushHack")
	write(t, root, "PushHackBrowser")
	write(t, root, "PushHackArrangement")
	if err := RemoveOld(root, "PushHackBrowser"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "PushHackBrowser")); err == nil {
		t.Fatal("not removed")
	}
	for _, keep := range []string{"PushHack", "PushHackArrangement"} {
		if _, err := os.Stat(filepath.Join(root, keep)); err != nil {
			t.Fatalf("%s was removed", keep)
		}
	}
	if err := RemoveOld(root, "PushHackBrowser"); !errors.Is(err, ErrNotThere) {
		t.Fatalf("second time: %v", err)
	}
}

func TestRemoveOldRefusesOtherNames(t *testing.T) {
	root := t.TempDir()
	write(t, root, "PushHack")
	write(t, root, "ClyphX_Pro")
	for _, name := range []string{"ClyphX_Pro", "PushHack", "../PushHack", "", "PushHackBrowser/../PushHack"} {
		if err := RemoveOld(root, name); !errors.Is(err, ErrUnknownName) {
			t.Fatalf("%q: %v", name, err)
		}
	}
	for _, keep := range []string{"PushHack", "ClyphX_Pro"} {
		if _, err := os.Stat(filepath.Join(root, keep)); err != nil {
			t.Fatalf("%s was removed", keep)
		}
	}
}

func TestRemoveOldDoesNotFollowASymlink(t *testing.T) {
	root := t.TempDir()
	write(t, root, "PushHack")
	elsewhere := t.TempDir()
	os.WriteFile(filepath.Join(elsewhere, "keep"), []byte("x"), 0o644)
	os.Symlink(elsewhere, filepath.Join(root, "PushHackBrowser"))
	if err := RemoveOld(root, "PushHackBrowser"); !errors.Is(err, ErrNotThere) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "keep")); err != nil {
		t.Fatal("followed a symlink out of Remote Scripts")
	}
}
