// live_bridge.go — client to the PushHackBrowser Remote Script running inside
// Live. push-manager runs on-device, so it reaches the script over localhost.
// Used only to LOAD a preset (browse/search/filter are filesystem-based; see
// presets.go). The Remote Script resolves name+category -> BrowserItem and calls
// browser.load_item() on Live's engine thread.

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const liveBridgeAddr = "127.0.0.1:7704"

// bridgeSend opens a one-shot connection, writes a single command line, and
// returns the trimmed reply ("OK"). Short timeout so a missing/stuck script
// never blocks the caller (the Shadow UI runs this in a goroutine).
func bridgeSend(cmd string) (string, error) {
	conn, err := net.DialTimeout("tcp", liveBridgeAddr, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" && err != nil {
		return "", err
	}
	return line, nil
}

// liveLoad asks the Remote Script to load a preset by name onto the selected
// track, scoped to the category's browser root when known (load:<root>:<name>),
// falling back to an unscoped lookup (load:<name>).
func liveLoad(name string, cat PresetCategory) error {
	root := cat.browserRoot()
	cmd := "load:" + name
	if root != "" {
		cmd = fmt.Sprintf("load:%s:%s", root, name)
	}
	_, err := bridgeSend(cmd)
	return err
}

// liveSampleLoad asks the Remote Script to load an audio sample onto the
// selected track. Live routes context-aware: Simpler on MIDI tracks, audio
// clip on audio tracks, hot-swap replacement when hot-swap is active.
func liveSampleLoad(name string) error {
	_, err := bridgeSend("load_sample:" + name)
	return err
}

// liveBridgeAlive reports whether the Remote Script is reachable (ping/pong).
func liveBridgeAlive() bool {
	reply, err := bridgeSend("ping")
	return err == nil && reply != ""
}

// liveIsPlaying asks the Remote Script whether Live's transport is playing.
func liveIsPlaying() (bool, error) {
	reply, err := bridgeSend("get_playing")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(reply) == "1", nil
}

// liveTempo asks the Remote Script for the current song tempo (BPM).
// Requires PushHackBrowser ≥ 0.2 (get_tempo query command support).
func liveTempo() (float64, error) {
	reply, err := bridgeSend("get_tempo")
	if err != nil {
		return 0, err
	}
	bpm, err := strconv.ParseFloat(strings.TrimSpace(reply), 64)
	if err != nil {
		return 0, fmt.Errorf("parse tempo %q: %w", reply, err)
	}
	return bpm, nil
}

// liveBeat asks the Remote Script for the current song time in beats.
func liveBeat() (float64, error) {
	reply, err := bridgeSend("get_beat")
	if err != nil {
		return 0, err
	}
	beat, err := strconv.ParseFloat(strings.TrimSpace(reply), 64)
	if err != nil {
		return 0, fmt.Errorf("parse beat %q: %w", reply, err)
	}
	return beat, nil
}

// livePlay starts Live's transport.
func livePlay() error {
	_, err := bridgeSend("play")
	return err
}

// liveStop stops Live's transport.
func liveStop() error {
	_, err := bridgeSend("stop")
	return err
}


// GET /api/live/play
func handleLivePlay(w http.ResponseWriter, r *http.Request) {
	if err := livePlay(); err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
}

// GET /api/live/stop
func handleLiveStop(w http.ResponseWriter, r *http.Request) {
	if err := liveStop(); err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
}


// GET /api/live/playing — returns whether Live's transport is playing.
func handleLivePlaying(w http.ResponseWriter, r *http.Request) {
	playing, err := liveIsPlaying()
	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "playing": playing})
}

// GET /api/live/tempo — returns the current Live song tempo as JSON.
// Used by the automation hack to sync its playback rate to Live's BPM.
func handleLiveTempo(w http.ResponseWriter, r *http.Request) {
	bpm, err := liveTempo()
	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "bpm": bpm})
}

// POST /api/live/load {"name": "...", "category": "...", "type": "preset"|"sample"}
func handleLiveLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name     string `json:"name"`
		Category string `json:"category"`
		Type     string `json:"type"` // "sample" or "" / "preset"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	var err error
	if body.Type == "sample" {
		err = liveSampleLoad(body.Name)
	} else {
		err = liveLoad(body.Name, PresetCategory(body.Category))
	}
	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
}
