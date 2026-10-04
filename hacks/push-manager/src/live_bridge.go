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

// bridgePlugin is one entry of the Remote Script's list_plugins reply: a
// scanned plugin and the preset files Live has indexed for it. Needs
// PushHackBrowser with plugin support (load_plugin / list_plugins).
type bridgePlugin struct {
	Vendor  string   `json:"vendor"`
	Name    string   `json:"name"`
	Presets []string `json:"presets"`
}

// livePlugins asks the Remote Script which plugins Live has scanned.
func livePlugins() ([]bridgePlugin, error) {
	reply, err := bridgeSend("list_plugins")
	if err != nil {
		return nil, err
	}
	var r struct {
		Plugins []bridgePlugin `json:"plugins"`
	}
	if err := json.Unmarshal([]byte(reply), &r); err != nil {
		// An older script answers an unknown command with "OK" or "ERROR".
		return nil, fmt.Errorf("list_plugins: unexpected reply %q (update Browser Bridge)", reply)
	}
	return r.Plugins, nil
}

// livePluginLoad loads a plugin onto the selected track, with one of its
// presets when preset is non-empty, else with its default patch. The command
// is colon-separated, so a colon in either name cannot be sent.
func livePluginLoad(plugin, preset string) error {
	if strings.Contains(plugin, ":") || strings.Contains(preset, ":") {
		return fmt.Errorf("a name with ':' cannot be loaded this way")
	}
	cmd := "load_plugin:" + plugin
	if preset != "" {
		cmd += ":" + preset
	}
	_, err := bridgeSend(cmd)
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
		Type     string `json:"type"`   // "sample", "plugin", or "" / "preset"
		Preset   string `json:"preset"` // type "plugin" only: a preset of that plugin
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	var err error
	if body.Type == "sample" {
		err = liveSampleLoad(body.Name)
	} else if body.Type == "plugin" {
		err = livePluginLoad(body.Name, body.Preset)
	} else {
		err = liveLoad(body.Name, PresetCategory(body.Category))
	}
	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
}

// GET /api/live/plugins — plugins Live has scanned, each with its presets.
func handleLivePlugins(w http.ResponseWriter, r *http.Request) {
	plugins, err := livePlugins()
	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if plugins == nil {
		plugins = []bridgePlugin{}
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "plugins": plugins})
}
