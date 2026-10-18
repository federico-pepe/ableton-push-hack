package main

import (
	"os"
	"testing"
	"time"
)

// The shutdown marker is recognised by change, not by clock: a marker left
// from the last shutdown does not count; touching it does.
func TestShutdownStampIgnoresAnOldMarker(t *testing.T) {
	if _, err := os.Stat("/data/settings"); err == nil {
		t.Skip("on a device: not touching the real marker")
	}
	if err := os.MkdirAll("/data/settings", 0o755); err != nil {
		t.Skipf("cannot make /data/settings here: %v", err)
	}
	defer os.RemoveAll("/data/settings")
	old := time.Now().Add(time.Hour) // a "future" mtime, as a skewed boot clock would see it
	os.WriteFile(shutdownFile, nil, 0o644)
	os.Chtimes(shutdownFile, old, old)
	start := shutdownStamp()
	if !shutdownStamp().Equal(start) {
		t.Fatal("an untouched marker changed its stamp")
	}
	now := time.Now()
	os.Chtimes(shutdownFile, now, now) // power.sh touches it
	if shutdownStamp().Equal(start) {
		t.Error("a touched marker was not noticed")
	}
}

// The boot cleaning up the last shutdown's marker is no press; a marker
// touched or made afresh is.
func TestMarkerPress(t *testing.T) {
	last := time.Date(2026, 10, 17, 23, 27, 56, 0, time.UTC)
	now := last.Add(8 * time.Hour)
	for _, c := range []struct {
		name       string
		start, cur time.Time
		want       bool
	}{
		{"the last shutdown's marker, untouched", last, last, false},
		{"the last shutdown's marker, removed at boot", last, time.Time{}, false},
		{"no marker, still none", time.Time{}, time.Time{}, false},
		{"no marker, then power.sh makes one", time.Time{}, now, true},
		{"the old marker touched by power.sh", last, now, true},
	} {
		if got := markerPress(c.start, c.cur); got != c.want {
			t.Errorf("%s: press %v, want %v", c.name, got, c.want)
		}
	}
}
