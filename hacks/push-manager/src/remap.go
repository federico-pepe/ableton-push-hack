package main

// remap.go — user-defined MIDI remapping.
//
// Re-maps a Push control (button / pad / knob) to a user-chosen MIDI CC or Note
// and emits the transformed value to a user-selected destination ALSA seq port.
// All logic lives here + a single call site in processFixedEvent (midi.go); the
// LD_PRELOAD hook cannot transform MIDI, only neutralize it.
//
// Sending reuses the existing output fd (midiOutFd) via writeSeqEvent with an
// explicit destination — no new ALSA port is created.

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// MidiMapping is one source→output rule. Keyed in remapMappings by srcKey().
type MidiMapping struct {
	SrcType  string `json:"src_type"` // "cc" | "note"
	SrcCh    uint8  `json:"src_ch"`
	SrcNum   uint8  `json:"src_num"`
	Relative bool   `json:"relative"` // true for encoder/knob sources (relative delta)
	OutType  string `json:"out_type"` // "cc" | "note"
	OutCh    uint8  `json:"out_ch"`
	OutNum   uint8  `json:"out_num"`
	OutMin   uint8  `json:"out_min"`
	OutMax   uint8  `json:"out_max"`
	Name     string `json:"name,omitempty"`
}

var (
	remapMu               sync.Mutex
	remapMappings         = map[string]MidiMapping{}
	remapAccum            = map[string]int{} // runtime accumulators for relative sources
	remapEnabled          bool
	remapRequireIntercept bool
	remapOutClient        byte
	remapOutPort          byte
)

// srcKey builds the lookup key for a source control.
func srcKey(srcType string, ch, num uint8) string {
	return fmt.Sprintf("%s:%d:%d", srcType, ch, num)
}

// isEncoderCC reports whether a CC number is a Push 3 relative encoder
// (encoders 1–8 = CC 71–78, volume wheel = CC 79, tempo wheel = CC 14).
func isEncoderCC(cc uint8) bool {
	return (cc >= 71 && cc <= 79) || cc == 14
}

// decodeRel converts a Push relative-encoder CC value to a signed delta.
// Two's-complement encoding: 1..63 positive, 65..127 negative (127 = -1).
// NOTE: verify rotation direction on device; flip sign here if inverted.
func decodeRel(v uint8) int {
	if v < 64 {
		return int(v)
	}
	return int(v) - 128
}

// scaleVal maps an incoming 0–127 value into [lo,hi].
func scaleVal(v, lo, hi uint8) uint8 {
	if hi >= lo {
		return uint8(int(lo) + int(v)*(int(hi)-int(lo))/127)
	}
	// inverted range: hi < lo
	return uint8(int(lo) - int(v)*(int(lo)-int(hi))/127)
}

func clampInt(v, lo, hi int) int {
	if lo > hi {
		lo, hi = hi, lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// interceptOn reports whether the MIDI intercept (midiflt) is active.
func interceptOn() bool {
	filt := ensureMidiFilt()
	return filt != nil && filt[4] == 1
}

// applyRemap looks up a mapping for the given source event and, if one exists
// and remap is currently active, emits the transformed value to the selected
// destination port. Returns true if a mapping handled the event.
//
// srcType is "cc" or "note"; ch/num identify the control; val is the incoming
// CC value or note velocity (0 = release for note/button sources).
func applyRemap(srcType string, ch, num, val uint8) bool {
	remapMu.Lock()
	if !remapEnabled || (remapRequireIntercept && !interceptOn()) {
		remapMu.Unlock()
		return false
	}
	key := srcKey(srcType, ch, num)
	m, ok := remapMappings[key]
	if !ok {
		remapMu.Unlock()
		return false
	}
	dstClient, dstPort := remapOutClient, remapOutPort

	var out uint8
	release := false
	if m.Relative {
		acc := clampInt(remapAccum[key]+decodeRel(val), int(m.OutMin), int(m.OutMax))
		remapAccum[key] = acc
		out = uint8(acc)
	} else {
		release = val == 0
		out = scaleVal(val, m.OutMin, m.OutMax)
	}
	remapMu.Unlock()

	if dstClient == 0 { // no valid destination selected
		return true
	}

	switch m.OutType {
	case "note":
		if release {
			sendSeqNoteTo(dstClient, dstPort, m.OutCh, m.OutNum, 0) //nolint:errcheck
		} else {
			sendSeqNoteTo(dstClient, dstPort, m.OutCh, m.OutNum, out) //nolint:errcheck
		}
	default: // "cc"
		sendSeqCCTo(dstClient, dstPort, m.OutCh, m.OutNum, int32(out)) //nolint:errcheck
	}
	return true
}

// sendSeqCCTo sends a MIDI CC event to an explicit destination port.
func sendSeqCCTo(dstClient, dstPort, channel, cc byte, value int32) error {
	midiOutMu.Lock()
	fd := midiOutFd
	srcClient := midiOutClient
	srcPort := midiOutPort
	midiOutMu.Unlock()
	if fd < 0 {
		return fmt.Errorf("midi_out not initialized")
	}
	data := make([]byte, 12)
	data[0] = channel
	binary.LittleEndian.PutUint32(data[4:], uint32(cc))
	binary.LittleEndian.PutUint32(data[8:], uint32(value))
	return writeSeqEvent(fd, seqEvController, srcClient, srcPort, dstClient, dstPort, data)
}

// sendSeqNoteTo sends a MIDI Note On event to an explicit destination port.
// velocity=0 acts as Note Off.
func sendSeqNoteTo(dstClient, dstPort, channel, note, velocity byte) error {
	midiOutMu.Lock()
	fd := midiOutFd
	srcClient := midiOutClient
	srcPort := midiOutPort
	midiOutMu.Unlock()
	if fd < 0 {
		return fmt.Errorf("midi_out not initialized")
	}
	data := make([]byte, 12)
	data[0] = channel
	data[1] = note
	data[2] = velocity
	return writeSeqEvent(fd, seqEvNoteOn, srcClient, srcPort, dstClient, dstPort, data)
}

// ── Persistence hooks (called from loadMidiPersist / saveMidiPersist) ────────

// remapLoadFromPersist populates remap state from decoded on-disk data.
func remapLoadFromPersist(d midiPersistData) {
	remapMu.Lock()
	defer remapMu.Unlock()
	remapMappings = map[string]MidiMapping{}
	for k, m := range d.Mappings {
		remapMappings[k] = m
	}
	remapAccum = map[string]int{}
	remapEnabled = d.RemapEnabled
	remapRequireIntercept = d.RemapRequireIntercept
	remapOutClient = d.RemapOutClient
	remapOutPort = d.RemapOutPort
}

// remapFillPersist copies current remap state into a persist struct for writing.
func remapFillPersist(d *midiPersistData) {
	remapMu.Lock()
	defer remapMu.Unlock()
	d.Mappings = make(map[string]MidiMapping, len(remapMappings))
	for k, m := range remapMappings {
		d.Mappings[k] = m
	}
	d.RemapEnabled = remapEnabled
	d.RemapRequireIntercept = remapRequireIntercept
	d.RemapOutClient = remapOutClient
	d.RemapOutPort = remapOutPort
}

// ── HTTP handlers ────────────────────────────────────────────────────────────

// GET /api/midi/mapping — list mappings + config.
// POST /api/midi/mapping — upsert a mapping (body = MidiMapping).
// DELETE /api/midi/mapping[?key=cc:0:20] — delete one (or all if no key).
func handleMidiMapping(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		remapMu.Lock()
		out := map[string]interface{}{
			"mappings":          copyMappings(),
			"enabled":           remapEnabled,
			"require_intercept": remapRequireIntercept,
			"out_client":        remapOutClient,
			"out_port":          remapOutPort,
		}
		remapMu.Unlock()
		jsonResponse(w, out)

	case http.MethodPost:
		var m MidiMapping
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, "bad JSON", http.StatusBadRequest)
			return
		}
		if m.SrcType != "cc" && m.SrcType != "note" {
			http.Error(w, "src_type must be cc or note", http.StatusBadRequest)
			return
		}
		if m.OutType != "cc" && m.OutType != "note" {
			http.Error(w, "out_type must be cc or note", http.StatusBadRequest)
			return
		}
		key := srcKey(m.SrcType, m.SrcCh, m.SrcNum)
		remapMu.Lock()
		remapMappings[key] = m
		delete(remapAccum, key) // reset accumulator on remap change
		remapMu.Unlock()
		go saveMidiPersist()
		jsonResponse(w, map[string]string{"key": key})

	case http.MethodDelete:
		key := r.URL.Query().Get("key")
		remapMu.Lock()
		if key == "" {
			remapMappings = map[string]MidiMapping{}
			remapAccum = map[string]int{}
		} else {
			delete(remapMappings, key)
			delete(remapAccum, key)
		}
		remapMu.Unlock()
		go saveMidiPersist()
		jsonResponse(w, map[string]bool{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// POST /api/midi/mapping/config — set {enabled, require_intercept, out_client, out_port}.
func handleMidiMappingConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Enabled          bool `json:"enabled"`
		RequireIntercept bool `json:"require_intercept"`
		OutClient        int  `json:"out_client"`
		OutPort          int  `json:"out_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	if body.OutClient < 0 || body.OutClient > 255 || body.OutPort < 0 || body.OutPort > 255 {
		http.Error(w, "out_client and out_port must be 0–255", http.StatusBadRequest)
		return
	}
	remapMu.Lock()
	remapEnabled = body.Enabled
	remapRequireIntercept = body.RequireIntercept
	remapOutClient = byte(body.OutClient)
	remapOutPort = byte(body.OutPort)
	remapMu.Unlock()
	go saveMidiPersist()
	jsonResponse(w, map[string]bool{"ok": true})
}

// copyMappings returns a shallow copy of remapMappings. Caller holds remapMu.
func copyMappings() map[string]MidiMapping {
	out := make(map[string]MidiMapping, len(remapMappings))
	for k, v := range remapMappings {
		out[k] = v
	}
	return out
}
