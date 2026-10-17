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
