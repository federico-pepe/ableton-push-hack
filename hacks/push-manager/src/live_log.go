package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// live_log.go — write a one-line marker into Live's own Log.txt once Live is up,
// so Ableton support can confirm push-hack is installed just by reading the
// standard Live log (no extra tooling, no SSH into our hack dirs).
//
// Why a separate "Live up" detector instead of piggy-backing on the display
// splash: the splash only fires when the push-display hook is loaded. A user may
// run push-manager alone — we still want the marker. This polls for the Live
// process directly (findWatchedPIDs), so it is independent of push-display.
//
// Race note: Live TRUNCATES Log.txt on each launch. We must append AFTER Live
// has opened the file or the line is lost on the next truncate. We key off the
// Live PID: when a new Live instance appears we wait a grace window (Live opens
// Log.txt within the first second) and then append. Re-appends on Live restart.

const (
	liveLogGrace    = 8 * time.Second // let a freshly-started Live open Log.txt
	liveLogPollUp   = 5 * time.Second // poll cadence while Live is running
	liveLogPollDown = 3 * time.Second // poll cadence while waiting for Live
)

// startLiveLogMarker runs for the life of the process (cheap: a /proc scan every
// few seconds). Launch from main() in a goroutine.
func startLiveLogMarker() {
	var lastLivePID int
	for {
		pid := findWatchedPIDs()["Live"]
		if pid == 0 {
			lastLivePID = 0 // Live gone — re-mark when it returns
			time.Sleep(liveLogPollDown)
			continue
		}
		if pid == lastLivePID {
			time.Sleep(liveLogPollUp)
			continue
		}
		// New Live instance. Give it time to (re)create + open Log.txt before we
		// append, so our line lands after the launch-time truncate.
		time.Sleep(liveLogGrace)
		if err := appendLiveLogMarker(); err != nil {
			log.Printf("live log marker: %v", err)
			time.Sleep(liveLogPollUp) // retry next loop (lastLivePID unchanged)
			continue
		}
		lastLivePID = pid
	}
}

// newestLiveLog returns the most-recently-modified versioned Live Log.txt
// (Push can carry several Live versions; the active one has the freshest mtime).
func newestLiveLog() (string, error) {
	matches, err := filepath.Glob("/data/.config/Ableton/Live */Log.txt")
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no Live Log.txt found")
	}
	sort.Slice(matches, func(i, j int) bool {
		fi, ei := os.Stat(matches[i])
		fj, ej := os.Stat(matches[j])
		if ei != nil || ej != nil {
			return false
		}
		return fi.ModTime().After(fj.ModTime())
	})
	return matches[0], nil
}

// appendLiveLogMarker writes one line in Live's own log format
// ("<ts>: info: <msg>") so it reads as a native entry and is greppable by
// "push-hack".
func appendLiveLogMarker() error {
	path, err := newestLiveLog()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	ts := time.Now().Format("2006-01-02T15:04:05.000000")
	line := fmt.Sprintf("%s: info: push-hack loaded: %s\n", ts, installedHacksSummary())
	if _, err := f.WriteString(line); err != nil {
		return err
	}
	log.Printf("live log marker: wrote to %s", path)
	return nil
}

// installedHacksSummary scans the deployed hack dirs and lists each hack with
// its version, e.g. "automation v0.1.0, push-display v0.1.0, push-manager v0.1.0".
func installedHacksSummary() string {
	matches, _ := filepath.Glob("/data/push-hack/hacks/*/hack.json")
	var parts []string
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var hc struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		}
		if json.Unmarshal(data, &hc) != nil || hc.ID == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s v%s", hc.ID, hc.Version))
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		// Fallback: at minimum we are push-manager, and we know our own version.
		return fmt.Sprintf("push-manager v%s", config.Version)
	}
	return strings.Join(parts, ", ")
}
