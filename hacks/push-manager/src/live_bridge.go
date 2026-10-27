// live_bridge.go — client to the PushHack Remote Script running inside
// Live. push-manager runs on-device, so it reaches the script over localhost.
// Browse/search/filter of presets are filesystem-based (presets.go); the script
// resolves name+scope -> BrowserItem and loads it on Live's engine thread.
//
// Protocol: JSON lines (plans/2026-10-27-pushhack-shared-remote-script.md).
// One request per connection: {"id":1,"ns":"transport","cmd":"get"} and one
// reply: {"id":1,"ok":true,"data":{...}} or {"ok":false,"error":"code"}.

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// liveBridgeAddr is a var so a test can use its own port.
var liveBridgeAddr = "127.0.0.1:7704"

const (
	bridgeShortTimeout = 3 * time.Second
	// A load walks Live's browser on the engine thread and answers when it is
	// done, so it may take a while on a big library.
	bridgeLoadTimeout = 25 * time.Second
)

var errOldScript = errors.New("the Remote Script does not answer like PushHack (install PushHack, or update it)")

// bridgeErrText turns the script's error codes into words for a user.
func bridgeErrText(code string) string {
	switch code {
	case "not_found":
		return "no matching item in Live's browser"
	case "not_loadable":
		return "that browser item cannot be loaded"
	case "unknown_ns", "unknown_cmd":
		return "the Remote Script is too old (install PushHack, or update it)"
	case "no_port":
		return "a MIDI port name is needed"
	case "no_such_root":
		return "Live's browser has no such root"
	case "failed":
		return "Live refused (see Live's Log.txt)"
	}
	return code
}

// bridgeCall sends one request and returns the reply's data. ns "" is a
// built-in command (hello). A short timeout keeps a missing or stuck script
// from blocking the caller (the Shadow UI runs this in a goroutine).
func bridgeCall(ns, cmd string, args map[string]any, timeout time.Duration) (json.RawMessage, error) {
	req := map[string]any{"id": 1, "cmd": cmd}
	if ns != "" {
		req["ns"] = ns
	}
	for k, v := range args {
		req[k] = v
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", liveBridgeAddr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if len(line) == 0 && err != nil {
		return nil, err
	}
	var r struct {
		OK    *bool           `json:"ok"`
		Error string          `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	// An old PushHackBrowser answers "OK" or "ERROR", not JSON.
	if json.Unmarshal(line, &r) != nil || r.OK == nil {
		return nil, errOldScript
	}
	if !*r.OK {
		return nil, errors.New(bridgeErrText(r.Error))
	}
	return r.Data, nil
}

// bridgeHello asks the script who it is: its script version and protocol.
func bridgeHello() (version string, protocol int, err error) {
	data, err := bridgeCall("", "hello", nil, bridgeShortTimeout)
	if err != nil {
		return "", 0, err
	}
	var d struct {
		Protocol      int    `json:"protocol"`
		ScriptVersion string `json:"script_version"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return "", 0, errOldScript
	}
	return d.ScriptVersion, d.Protocol, nil
}

// liveLoad asks the script to load a preset by name onto the selected track,
// scoped to the category's browser root when known, else searching every root.
// It returns when Live has loaded it, or why it could not.
func liveLoad(name string, cat PresetCategory) error {
	args := map[string]any{"name": name}
	if root := cat.browserRoot(); root != "" {
		args["scope"] = root
	}
	_, err := bridgeCall("browser", "load", args, bridgeLoadTimeout)
	return err
}

// liveSampleLoad asks the script to load an audio sample onto the selected
// track. Live routes context-aware: Simpler on MIDI tracks, audio clip on
// audio tracks, hot-swap replacement when hot-swap is active.
func liveSampleLoad(name string) error {
	_, err := bridgeCall("browser", "load_sample", map[string]any{"name": name}, bridgeLoadTimeout)
	return err
}

// bridgePlugin is one entry of the script's list_plugins reply: a scanned
// plugin and the preset files Live has indexed for it.
type bridgePlugin struct {
	Vendor  string   `json:"vendor"`
	Name    string   `json:"name"`
	Presets []string `json:"presets"`
}

// livePlugins asks the script which plugins Live has scanned.
func livePlugins() ([]bridgePlugin, error) {
	data, err := bridgeCall("browser", "list_plugins", nil, bridgeLoadTimeout)
	if err != nil {
		return nil, err
	}
	var r struct {
		Plugins []bridgePlugin `json:"plugins"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, errOldScript
	}
	return r.Plugins, nil
}

// livePluginLoad loads a plugin onto the selected track, with one of its
// presets when preset is non-empty, else with its default patch. A name may
// hold any character: the JSON protocol needs no separator.
func livePluginLoad(plugin, preset string) error {
	args := map[string]any{"plugin": plugin}
	if preset != "" {
		args["preset"] = preset
	}
	_, err := bridgeCall("browser", "load_plugin", args, bridgeLoadTimeout)
	return err
}

// liveBridgeAlive reports whether PushHack is reachable and answers hello.
func liveBridgeAlive() bool {
	_, _, err := bridgeHello()
	return err == nil
}

// liveTransport is the transport state the script reports.
type liveTransport struct {
	Playing bool    `json:"playing"`
	Tempo   float64 `json:"tempo"`
	Beat    float64 `json:"beat"`
}

func liveTransportGet() (liveTransport, error) {
	var t liveTransport
	data, err := bridgeCall("transport", "get", nil, bridgeShortTimeout)
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(data, &t); err != nil {
		return t, errOldScript
	}
	return t, nil
}

// liveIsPlaying asks the script whether Live's transport is playing.
func liveIsPlaying() (bool, error) {
	t, err := liveTransportGet()
	return t.Playing, err
}

// liveTempo asks the script for the current song tempo (BPM).
func liveTempo() (float64, error) {
	t, err := liveTransportGet()
	return t.Tempo, err
}

// liveBeat asks the script for the current song time in beats.
func liveBeat() (float64, error) {
	t, err := liveTransportGet()
	return t.Beat, err
}

// LiveScale is the Set's scale -- the one Push's Scale button edits --
// as the PushHack Remote Script's notes.scale reports it.
type LiveScale struct {
	Supported bool   `json:"supported"`
	Root      int    `json:"root"` // 0-11, 0 = C
	Name      string `json:"name"`
	Intervals []int  `json:"intervals"` // semitones from the root
	Mode      bool   `json:"mode"`      // Scale Mode, Push's "In Key"
}

// liveScale asks the script for the Set's scale.
func liveScale() (LiveScale, error) {
	var sc LiveScale
	data, err := bridgeCall("notes", "scale", nil, bridgeShortTimeout)
	if err != nil {
		return sc, err
	}
	if err := json.Unmarshal(data, &sc); err != nil {
		return sc, errOldScript
	}
	if sc.Supported && (sc.Root < 0 || sc.Root > 11 || len(sc.Intervals) == 0) {
		return sc, fmt.Errorf("scale: implausible scale %+v", sc)
	}
	return sc, nil
}

// livePlay starts Live's transport.
func livePlay() error {
	_, err := bridgeCall("transport", "play", nil, bridgeShortTimeout)
	return err
}

// liveStop stops Live's transport.
func liveStop() error {
	_, err := bridgeCall("transport", "stop", nil, bridgeShortTimeout)
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

// GET  /api/live/listen — the listen track's state (PushHack get_listen).
// POST /api/live/listen {"on": true, "port": "Stacks Listen"} — make or find
// a silent MIDI track whose input follows the selected track and whose output
// is that MIDI port (listen_on), or stop it ({"on": false}, listen_off). The
// reply is the script's own JSON: on, track, input, channel, output,
// monitoring, source -- and on failure ok:false, error and the outputs Live
// offers. Needs the PushHack Remote Script.
func handleLiveListen(w http.ResponseWriter, r *http.Request) {
	cmd, args := "get", map[string]any(nil)
	if r.Method == http.MethodPost {
		var body struct {
			On   bool   `json:"on"`
			Port string `json:"port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch {
		case !body.On:
			cmd = "off"
		case body.Port == "" || strings.ContainsAny(body.Port, "\n\r"):
			http.Error(w, "port: a MIDI port name is needed", http.StatusBadRequest)
			return
		default:
			cmd, args = "on", map[string]any{"port": body.Port}
		}
	}
	data, err := bridgeCall("listen", cmd, args, bridgeShortTimeout)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	var st map[string]interface{}
	if err := json.Unmarshal(data, &st); err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": errOldScript.Error()})
		return
	}
	jsonResponse(w, st)
}

// GET /api/live/scale — the Set's scale, the one Push's Scale button edits:
// {ok, supported, root, name, intervals, mode}. supported is false on a
// Live without the scale API.
func handleLiveScale(w http.ResponseWriter, r *http.Request) {
	sc, err := liveScale()
	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "supported": sc.Supported, "root": sc.Root,
		"name": sc.Name, "intervals": sc.Intervals, "mode": sc.Mode})
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
