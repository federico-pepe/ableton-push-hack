package main

// midi.go — MIDI monitor via ALSA sequencer subscription
//
// Subscribes to the Push 3 ALSA sequencer port (kernel client 16, port 0:
// "Ableton Push 3 Live Port") to receive a broadcast copy of all MIDI input:
// pads, buttons, encoders, aftertouch, SysEx.
//
// Uses /dev/snd/seq ioctls directly (no cgo, no subprocess):
//   ioctl(fd, SNDRV_SEQ_IOCTL_CLIENT_ID, &id)         → get our client ID
//   ioctl(fd, SNDRV_SEQ_IOCTL_CREATE_PORT, &portInfo)  → create receive port
//   ioctl(fd, SNDRV_SEQ_IOCTL_SUBSCRIBE_PORT, &sub)    → subscribe to 16:0
//   read(fd, buf)                                       → receive snd_seq_event stream
//
// Struct sizes (Linux 5.x, x86-64, confirmed against kernel headers):
//   snd_seq_port_info:      168 bytes (2 addr + 64 name + 2 pad + 6×uint32 + ptr + ...)
//   snd_seq_port_subscribe:  80 bytes (4 addrs + 2×uint32 + 1+3 + 64 reserved)
//   snd_seq_event:           28 bytes (4 hdr + 8 time + 4 src/dst + 12 data)
//
// HTTP endpoints (same API as before, UI unchanged):
//   GET /api/midi/events?n=<count>  — last n events as JSON
//   GET /api/midi/stream            — SSE live stream

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

// ALSA seq kernel ABI constants (ioctl numbers, struct offsets, event
// types) live in core/alsaseq — see that package for the source of truth.

// ── Mutable subscription target + cancellation ────────────────────────────

// midiSeqFd is the currently open /dev/snd/seq fd used by readAlsaSeq.
// Closing it from outside interrupts the blocked syscall.Read and causes the
// goroutine to exit (error path), then restart with the new target.
// Protected by midiTargetMu.
var (
	midiTargetMu     sync.Mutex
	midiTargetClient = alsaseq.Push3ClientDefault
	midiTargetPort   = alsaseq.Push3PortDefault
	midiSeqFd        = -1   // -1 = not connected
	midiAutoDetect   = true // false once user manually selects a port
)

// ── ALSA seq output (LED / button light control) ──────────────────────────
//
// A separate persistent fd — independent of the reader fd which gets closed
// on subscription changes. Sends snd_seq_event with queue=QUEUE_DIRECT to the
// Push 3 Live Port (16:0) for immediate delivery. No subscription needed for
// direct-addressed events.

var (
	midiOutMu sync.Mutex
	midiOut   *alsaseq.Client // nil if not initialized
)

// ── MIDI forwarding ──────────────────────────────────────────────────────────
var (
	midiForwardMu      sync.RWMutex
	midiForwardEnabled bool
	midiForwardClient  byte = 16
	midiForwardPort    byte = 2
)

// ── Boot-settle gate (USB-A safety) ───────────────────────────────────────────
//
// Opening /dev/snd (ALSA seq) while a USB-Audio device plugged into the USB-A
// port is still enumerating — the ~3-15s window after a COLD power-on — can
// prevent that device from enumerating at all, wedging the USB-A port until a
// power cycle. push-manager starts early at boot (init.d S20), squarely inside
// that window. waitForBootSettle defers our first /dev/snd access until the
// system has been up long enough for USB-A enumeration to finish. It is a no-op
// when push-manager is (re)started later on an already-running system.
const bootSettleSecs = 30.0

func waitForBootSettle() {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return // can't read uptime — proceed without delay
	}
	var up float64
	if _, err := fmt.Sscanf(string(data), "%f", &up); err != nil {
		return
	}
	if up < bootSettleSecs {
		wait := time.Duration((bootSettleSecs - up) * float64(time.Second))
		log.Printf("midi: deferring ALSA init %.1fs (uptime %.1fs < %.0fs boot-settle, USB-A safety)",
			wait.Seconds(), up, bootSettleSecs)
		time.Sleep(wait)
	}
}

func initMidiOut() {
	c, err := alsaseq.Open()
	if err != nil {
		log.Printf("midi_out: %v (LED control disabled)", err)
		return
	}
	if _, err := c.CreatePort("Push Manager", alsaseq.CapRead|alsaseq.CapSubsRead, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
		c.Close()
		log.Printf("midi_out: %v (LED control disabled)", err)
		return
	}

	midiOutMu.Lock()
	midiOut = c
	midiOutMu.Unlock()

	log.Printf("midi_out: ready (client %d port %d)", c.Addr().Client, c.Addr().Port)
}

// sendSeqCC sends a MIDI CC event directly to the Push 3 ALSA seq port.
// push3Dest returns the current detected Push 3 ALSA client and port.
// Uses midiTargetClient/Port which are updated by detectPush3Port() on each
// readAlsaSeq attempt, so LED output tracks the same client as the subscriber.
func push3Dest() (client, port byte) {
	midiTargetMu.Lock()
	c, p := midiTargetClient, midiTargetPort
	midiTargetMu.Unlock()
	return c, p
}

// channel is 0-indexed (0 = MIDI ch 1). cc and value are 0–127.
func sendSeqCC(channel, cc byte, value int32) error {
	midiOutMu.Lock()
	c := midiOut
	midiOutMu.Unlock()
	if c == nil {
		return fmt.Errorf("midi_out not initialized")
	}
	dstClient, dstPort := push3Dest()
	return c.SendCC(alsaseq.Addr{Client: dstClient, Port: dstPort}, channel, cc, value)
}

// sendSeqNote sends a MIDI Note On event directly to the Push 3 ALSA seq port.
// channel 0-indexed, note and velocity 0–127. velocity=0 acts as Note Off.
func sendSeqNote(channel, note, velocity byte) error {
	midiOutMu.Lock()
	c := midiOut
	midiOutMu.Unlock()
	if c == nil {
		return fmt.Errorf("midi_out not initialized")
	}
	dstClient, dstPort := push3Dest()
	return c.SendNote(alsaseq.Addr{Client: dstClient, Port: dstPort}, channel, note, velocity)
}

// sendSeqSysEx sends a variable-length SysEx event to the Push 3 ALSA seq port.
// sysex must include the leading F0 and trailing F7.
func sendSeqSysEx(sysex []byte) error {
	midiOutMu.Lock()
	c := midiOut
	midiOutMu.Unlock()
	if c == nil {
		return fmt.Errorf("midi_out not initialized")
	}
	dstClient, dstPort := push3Dest()
	return c.SendSysEx(alsaseq.Addr{Client: dstClient, Port: dstPort}, sysex)
}

// ── LED Color Palette query ────────────────────────────────────────────────

// Ableton Push SysEx header (manufacturer 00 21 1D, device 01 01).
var abletonSysExHdr = []byte{0xF0, 0x00, 0x21, 0x1D, 0x01, 0x01}

const sysExCmdPalette = byte(0x04)

// paletteRespCh receives SysEx bytes for "Get LED Color Palette Entry" responses.
var paletteRespCh = make(chan []byte, 8)

// PaletteEntry holds one decoded LED color palette entry.
type PaletteEntry struct {
	Index int    `json:"index"`
	R     int    `json:"r"`   // 15-bit (0–16383)
	G     int    `json:"g"`
	B     int    `json:"b"`
	W     int    `json:"w"`   // white channel (15-bit)
	Hex   string `json:"hex"` // #RRGGBB (8-bit scaled, display only)
}

// isPaletteResponse returns true if data is a "Get LED Color Palette Entry"
// response SysEx from Ableton Push.
func isPaletteResponse(data []byte) bool {
	if len(data) < 17 {
		return false
	}
	for i, b := range abletonSysExHdr {
		if data[i] != b {
			return false
		}
	}
	return data[6] == sysExCmdPalette
}

// parsePaletteEntry decodes a 17-byte palette response SysEx into a PaletteEntry.
// Format: F0 00 21 1D 01 01 04 <idx> <R_lo> <R_hi> <G_lo> <G_hi> <B_lo> <B_hi> <W_lo> <W_hi> F7
func parsePaletteEntry(data []byte) (PaletteEntry, error) {
	if len(data) < 17 {
		return PaletteEntry{}, fmt.Errorf("palette response too short: %d bytes", len(data))
	}
	idx := int(data[7])
	r := int(data[8]) | (int(data[9]) << 7)
	g := int(data[10]) | (int(data[11]) << 7)
	b := int(data[12]) | (int(data[13]) << 7)
	w := int(data[14]) | (int(data[15]) << 7)
	// R/G/B are 8-bit (0–255); W is white balance 0–1024. Use directly for hex.
	return PaletteEntry{
		Index: idx, R: r, G: g, B: b, W: w,
		Hex: fmt.Sprintf("#%02X%02X%02X", r, g, b),
	}, nil
}

// queryColorPalette sends "Get LED Color Palette Entry" SysEx for indices 0–127
// and collects the responses. Blocks for up to ~13s (100ms × 128).
func queryColorPalette() ([]PaletteEntry, error) {
	// Drain any stale responses.
	for {
		select {
		case <-paletteRespCh:
		default:
			goto drained
		}
	}
drained:
	entries := make([]PaletteEntry, 0, 128)
	for idx := 0; idx < 128; idx++ {
		query := append(append([]byte(nil), abletonSysExHdr...), sysExCmdPalette, byte(idx), 0xF7)
		if err := sendSeqSysEx(query); err != nil {
			return nil, fmt.Errorf("send query %d: %w", idx, err)
		}
		select {
		case resp := <-paletteRespCh:
			entry, err := parsePaletteEntry(resp)
			if err != nil {
				return nil, fmt.Errorf("parse response %d: %w", idx, err)
			}
			entries = append(entries, entry)
		case <-time.After(100 * time.Millisecond):
			return nil, fmt.Errorf("timeout waiting for palette entry %d", idx)
		}
	}
	return entries, nil
}

// forwardSeqEvent relays a fixed-length ALSA seq event to the configured forward port.
// data is the 12-byte data union from the original event.
func forwardSeqEvent(evType uint8, data []byte) {
	midiForwardMu.RLock()
	enabled := midiForwardEnabled
	dstClient := midiForwardClient
	dstPort := midiForwardPort
	midiForwardMu.RUnlock()
	if !enabled {
		return
	}
	midiOutMu.Lock()
	c := midiOut
	midiOutMu.Unlock()
	if c == nil {
		return
	}
	c.WriteEvent(evType, alsaseq.Addr{Client: dstClient, Port: dstPort}, data) //nolint:errcheck
}

// POST /api/midi/led — set a button/pad LED colour on Push 3.
// Body: {"type":"cc","channel":0,"cc":102,"value":127}
//
//	{"type":"note","channel":0,"note":36,"velocity":127}
//
// channel is 0-indexed. value/velocity = Push colour palette index (0=off, 127=brightest red).
// Use the Push 2 MIDI spec palette table for colour indices.
func handleMidiLed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Type     string `json:"type"`
		Channel  int    `json:"channel"`
		CC       int    `json:"cc"`
		Value    int    `json:"value"`
		Note     int    `json:"note"`
		Velocity int    `json:"velocity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}

	var sendErr error
	switch body.Type {
	case "cc":
		if body.CC < 0 || body.CC > 127 || body.Value < 0 || body.Value > 127 || body.Channel < 0 || body.Channel > 15 {
			http.Error(w, "cc/value must be 0–127, channel 0–15", http.StatusBadRequest)
			return
		}
		sendErr = sendSeqCC(byte(body.Channel), byte(body.CC), int32(body.Value))
	case "note":
		if body.Note < 0 || body.Note > 127 || body.Velocity < 0 || body.Velocity > 127 || body.Channel < 0 || body.Channel > 15 {
			http.Error(w, "note/velocity must be 0–127, channel 0–15", http.StatusBadRequest)
			return
		}
		sendErr = sendSeqNote(byte(body.Channel), byte(body.Note), byte(body.Velocity))
	default:
		http.Error(w, `type must be "cc" or "note"`, http.StatusBadRequest)
		return
	}
	if sendErr != nil {
		http.Error(w, sendErr.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
}

// GET /api/midi/led/states — returns current LED toggle state for all CC buttons.
// Response: {"states": {"102": 127, "20": 0, ...}}
// DELETE /api/midi/led/states — turn off all active LEDs and clear the state map.
func handleMidiLedStates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ledToggleMu.Lock()
		snapshot := make(map[string]uint8, len(ledToggleState))
		for cc, val := range ledToggleState {
			snapshot[fmt.Sprintf("%d", cc)] = val
		}
		ledToggleMu.Unlock()
		jsonResponse(w, map[string]interface{}{"states": snapshot})

	case http.MethodDelete:
		ledToggleMu.Lock()
		toOff := make([]uint8, 0, len(ledToggleState))
		for cc, val := range ledToggleState {
			if val == 127 {
				toOff = append(toOff, cc)
			}
		}
		ledToggleState = map[uint8]uint8{} // replace atomically under lock
		ledToggleMu.Unlock()
		for _, cc := range toOff {
			if err := sendSeqCC(0, cc, 0); err != nil {
				log.Printf("ledStatesReset cc=%d: %v", cc, err)
			}
		}
		jsonResponse(w, map[string]interface{}{"cleared": len(toOff)})

	default:
		http.Error(w, "GET or DELETE required", http.StatusMethodNotAllowed)
	}
}

// GET    /api/midi/led/config — list all per-CC LED mode configs
// POST   /api/midi/led/config — set config: {"cc":102,"mode":"exclusive","color":"red","group":"tabs"}
//                               color: 0–127 or named string ("red","blue","green",…)
// DELETE /api/midi/led/config?cc=102 — delete config for one CC
// DELETE /api/midi/led/config        — clear all configs
func handleMidiLedConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ledConfigMu.Lock()
		out := make(map[string]LEDConfig, len(ledConfigs))
		for cc, cfg := range ledConfigs {
			out[fmt.Sprintf("%d", cc)] = cfg
		}
		ledConfigMu.Unlock()
		jsonResponse(w, map[string]interface{}{
			"configs":      out,
			"named_colors": push3.NamedColors,
		})

	case http.MethodPost:
		var body struct {
			CC         int         `json:"cc"`
			Mode       string      `json:"mode"`
			Color      interface{} `json:"color"`      // int 0–127 or named string
			Group      string      `json:"group"`
			AnimType   string      `json:"anim_type"`  // "" | "oneshot" | "pulse" | "blink"
			AnimSpeed  string      `json:"anim_speed"` // "24th"|"16th"|"8th"|"quarter"|"half"
			AnimColor  interface{} `json:"anim_color"` // target color for animation
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad JSON", http.StatusBadRequest)
			return
		}
		if body.CC < 0 || body.CC > 127 {
			http.Error(w, "cc must be 0–127", http.StatusBadRequest)
			return
		}
		switch body.Mode {
		case LEDModeTrigger, LEDModeMomentary, LEDModeExclusive:
		default:
			http.Error(w, `mode must be "trigger", "momentary", or "exclusive"`, http.StatusBadRequest)
			return
		}
		color := uint8(127) // default: full brightness
		if body.Color != nil {
			c, ok := resolveLEDColor(body.Color)
			if !ok {
				http.Error(w, "color must be 0–127 or a named color", http.StatusBadRequest)
				return
			}
			color = c
		}
		// Validate animation fields if type is set.
		animType := body.AnimType
		animSpeed := body.AnimSpeed
		animColor := uint8(0)
		if animType != "" {
			switch animType {
			case LEDAnimOneShot, LEDAnimPulse, LEDAnimBlink:
			default:
				http.Error(w, `anim_type must be "oneshot", "pulse", or "blink"`, http.StatusBadRequest)
				return
			}
			switch animSpeed {
			case LEDAnimSpeed24th, LEDAnimSpeed16th, LEDAnimSpeed8th, LEDAnimSpeedQuarter, LEDAnimSpeedHalf:
			case "": // default to 8th
				animSpeed = LEDAnimSpeed8th
			default:
				http.Error(w, `anim_speed must be "24th", "16th", "8th", "quarter", or "half"`, http.StatusBadRequest)
				return
			}
			if body.AnimColor != nil {
				ac, ok := resolveLEDColor(body.AnimColor)
				if !ok {
					http.Error(w, "anim_color must be 0–127 or a named color", http.StatusBadRequest)
					return
				}
				animColor = ac
			}
		}
		cfg := LEDConfig{
			Mode:      body.Mode,
			Color:     color,
			Group:     body.Group,
			AnimType:  animType,
			AnimSpeed: animSpeed,
			AnimColor: animColor,
		}
		ledConfigMu.Lock()
		ledConfigs[uint8(body.CC)] = cfg
		ledConfigMu.Unlock()
		go saveMidiPersist()
		jsonResponse(w, map[string]interface{}{"ok": true, "cc": body.CC, "config": cfg})

	case http.MethodDelete:
		if ccStr := r.URL.Query().Get("cc"); ccStr != "" {
			v, err := strconv.ParseUint(ccStr, 10, 8)
			if err != nil || v > 127 {
				http.Error(w, "invalid cc", http.StatusBadRequest)
				return
			}
			ledConfigMu.Lock()
			delete(ledConfigs, uint8(v))
			ledConfigMu.Unlock()
		} else {
			ledConfigMu.Lock()
			ledConfigs = map[uint8]LEDConfig{}
			ledConfigMu.Unlock()
		}
		go saveMidiPersist()
		jsonResponse(w, map[string]interface{}{"ok": true})

	default:
		http.Error(w, "GET, POST, or DELETE required", http.StatusMethodNotAllowed)
	}
}

// GET  /api/midi/forward — return current forwarding state.
// POST /api/midi/forward — set forwarding: {"enabled":true,"client":16,"port":2}
func handleMidiForward(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		midiForwardMu.RLock()
		resp := map[string]interface{}{
			"enabled": midiForwardEnabled,
			"client":  int(midiForwardClient),
			"port":    int(midiForwardPort),
		}
		midiForwardMu.RUnlock()
		jsonResponse(w, resp)

	case http.MethodPost:
		var body struct {
			Enabled bool `json:"enabled"`
			Client  *int `json:"client"`
			Port    *int `json:"port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		midiForwardMu.Lock()
		midiForwardEnabled = body.Enabled
		if body.Client != nil {
			midiForwardClient = byte(*body.Client)
		}
		if body.Port != nil {
			midiForwardPort = byte(*body.Port)
		}
		midiForwardMu.Unlock()
		jsonResponse(w, map[string]interface{}{"ok": true, "enabled": body.Enabled})

	default:
		http.Error(w, "GET or POST required", http.StatusMethodNotAllowed)
	}
}

// GET /api/midi/palette — query the full 128-entry RGB color palette via SysEx.
// Blocks while querying (~1–2s on hardware). Returns JSON array of PaletteEntry.
func handleMidiPalette(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	entries, err := queryColorPalette()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, entries)
}

// MidiPort is an ALSA sequencer port returned by GET /api/midi/ports.
type MidiPort struct {
	Client   int    `json:"client"`
	Port     int    `json:"port"`
	Name     string `json:"name"`
	Active   bool   `json:"active"`   // currently subscribed
	Writable bool   `json:"writable"` // capability contains 'W' (can receive output)
}

// ── In-memory ring buffer ──────────────────────────────────────────────────

const midiRingSize = 256

type midiEventJSON struct {
	TsMs    int64  `json:"ts_ms"`
	Dir     string `json:"dir"`
	Len     int    `json:"len"`
	Data    []int  `json:"data"`
	Decoded string `json:"decoded"`
}

var (
	midiRing     [midiRingSize]midiEventJSON
	midiRingMu   sync.RWMutex
	midiWriteIdx uint32
	midiTotal    uint32
	midiStart    = time.Now()
	midiOnline   bool
)

func midiAppend(ev midiEventJSON) {
	midiRingMu.Lock()
	midiRing[midiWriteIdx%midiRingSize] = ev
	midiWriteIdx++
	midiTotal++
	midiRingMu.Unlock()
}

// ── ALSA sequencer reader ──────────────────────────────────────────────────

// detectPush3Port scans /proc/asound/seq/clients for "Ableton Push 3 Live Port"
// and updates midiTargetClient/Port if the actual client number differs from the
// current target. Called on each readAlsaSeq attempt so a shifted client number
// (e.g. 20 when a USB MIDI device is connected at boot) is found automatically.
func detectPush3Port() {
	const push3PortName = "Ableton Push 3 Live Port"
	p, ok := alsaseq.FindByName(push3PortName, alsaseq.CapRead)
	if !ok {
		log.Printf("midi: Push 3 Live Port not found in /proc/asound/seq/clients — using %d:%d",
			midiTargetClient, midiTargetPort)
		return
	}
	c, port := p.Addr.Client, p.Addr.Port
	midiTargetMu.Lock()
	if c != midiTargetClient || port != midiTargetPort {
		log.Printf("midi: auto-detected Push 3 at %d:%d (was %d:%d)",
			c, port, midiTargetClient, midiTargetPort)
		midiTargetClient = c
		midiTargetPort = port
	}
	midiTargetMu.Unlock()
}

func startMidiReader() {
	go func() {
		for {
			if err := readAlsaSeq(); err != nil {
				midiRingMu.Lock()
				midiOnline = false
				midiRingMu.Unlock()
				log.Printf("midi: %v — retry in 5s", err)
				time.Sleep(5 * time.Second)
			}
		}
	}()
}

// midiSeqHandler adapts push-manager's MIDI processing to alsaseq.Handler.
type midiSeqHandler struct{}

func (midiSeqHandler) Fixed(evType uint8, src alsaseq.Addr, data []byte) {
	processFixedEvent(evType, data)
}

func (midiSeqHandler) VarLen(evType uint8, src alsaseq.Addr, payload []byte) {
	if evType != alsaseq.EvSysEx {
		return
	}
	// Route Ableton palette responses to query waiter before emitting.
	if isPaletteResponse(payload) {
		select {
		case paletteRespCh <- append([]byte(nil), payload...):
		default:
		}
	}
	emitMidi(payload)
}

func readAlsaSeq() error {
	// Auto-detect Push 3 client by name unless the user has manually subscribed.
	midiTargetMu.Lock()
	autoDetect := midiAutoDetect
	midiTargetMu.Unlock()
	if autoDetect {
		detectPush3Port()
	}

	// Snapshot current target before opening (may change again later — that's OK,
	// the subscription-change handler will close this fd and trigger a restart).
	midiTargetMu.Lock()
	targetClient := midiTargetClient
	targetPort := midiTargetPort
	midiTargetMu.Unlock()

	c, err := alsaseq.Open()
	if err != nil {
		return err
	}

	// Register fd so handleMidiSubscribe can close it to interrupt a blocked Read.
	fd := c.FD()
	midiTargetMu.Lock()
	midiSeqFd = fd
	midiTargetMu.Unlock()

	defer func() {
		// Only close and clear if we still own the fd.
		// handleMidiSubscribe sets midiSeqFd = -1 before closing, so we check.
		midiTargetMu.Lock()
		wasOurs := midiSeqFd == fd
		if wasOurs {
			midiSeqFd = -1
		}
		midiTargetMu.Unlock()
		if wasOurs {
			c.Close()
		}
	}()

	if _, err := c.CreatePort("Push Manager In", alsaseq.CapWrite|alsaseq.CapSubsWrite, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
		return err
	}

	target := alsaseq.Addr{Client: targetClient, Port: targetPort}
	if err := c.Subscribe(target); err != nil {
		return fmt.Errorf("SUBSCRIBE_PORT ioctl (target %d:%d): %w", targetClient, targetPort, err)
	}

	log.Printf("midi: subscribed to ALSA seq %d:%d → client %d port %d",
		targetClient, targetPort, c.Addr().Client, c.Addr().Port)

	midiRingMu.Lock()
	midiOnline = true
	midiRingMu.Unlock()

	return c.ReadLoop(midiSeqHandler{})
}

// processFixedEvent converts an ALSA seq event to raw MIDI bytes and emits it.
// data = 12-byte data union starting at alsaseq.EventOffData.
//
// Fixed-length event data layouts:
//   Note (snd_seq_ev_note):    data[0]=ch, data[1]=note, data[2]=vel, data[3]=off_vel
//   Control (snd_seq_ev_ctrl): data[0]=ch, data[1..3]=unused, data[4..7]=param, data[8..11]=value
func processFixedEvent(evType uint8, data []byte) {
	switch evType {
	case alsaseq.EvNoteOn:
		ch, note, vel := data[0], data[1], data[2]
		if vel == 0 {
			emitMidi([]byte{0x80 | ch, note, vel})
		} else {
			emitMidi([]byte{0x90 | ch, note, vel})
		}
		applyRemap("note", ch, note, vel)
	case alsaseq.EvNoteOff:
		emitMidi([]byte{0x80 | data[0], data[1], data[2]})
		applyRemap("note", data[0], data[1], 0)
	case alsaseq.EvKeyPress:
		emitMidi([]byte{0xA0 | data[0], data[1], data[2]})
	case alsaseq.EvController:
		ch := data[0]
		param := binary.LittleEndian.Uint32(data[4:])
		value := binary.LittleEndian.Uint32(data[8:])
		emitMidi([]byte{0xB0 | ch, byte(param & 0x7F), byte(value & 0x7F)})
		// Feed CC into chord detector and shadow UI (if active)
		ccNum := uint8(param & 0x7F)
		ccVal := uint8(value & 0x7F)
		applyRemap("cc", ch, ccNum, ccVal)
		if shadowUIIsActive() {
			go shadowUIHandleCC(ccNum, ccVal)
		}
		if ccVal == 0x7F {
			chordCCPressed(ccNum)
			// Dispatch LED mode when MIDI intercept is active.
			// When intercept is off, Push firmware manages its own LED state.
			// While the Shadow UI is active it fully owns the 4 under-screen
			// buttons (press + release), so skip the generic toggle for them.
			if isTogglableCC(ccNum) && !(shadowUIIsActive() && isScreenBotCC(ccNum)) {
				if filt := ensureMidiFilt(); filt != nil && filt[4] == 1 {
					ledConfigMu.Lock()
					cfg, hasCfg := ledConfigs[ccNum]
					ledConfigMu.Unlock()
					if !hasCfg {
						cfg = LEDConfig{Mode: LEDModeTrigger, Color: 127}
					}
					switch cfg.Mode {
					case LEDModeMomentary:
						go momentaryLED(ccNum, cfg.Color, true)
					case LEDModeExclusive:
						go exclusiveLED(ccNum, cfg.Color)
					default: // LEDModeTrigger
						go triggerLED(ccNum, cfg.Color)
					}
				}
			}
		} else if ccVal == 0 {
			chordCCReleased(ccNum)
			// Handle momentary release — turn off LED when button is released.
			if isTogglableCC(ccNum) && !(shadowUIIsActive() && isScreenBotCC(ccNum)) {
				if filt := ensureMidiFilt(); filt != nil && filt[4] == 1 {
					ledConfigMu.Lock()
					cfg, ok := ledConfigs[ccNum]
					ledConfigMu.Unlock()
					if ok && cfg.Mode == LEDModeMomentary {
						go momentaryLED(ccNum, cfg.Color, false)
					}
				}
			}
		}
	case alsaseq.EvPgmChange:
		ch := data[0]
		param := binary.LittleEndian.Uint32(data[4:])
		emitMidi([]byte{0xC0 | ch, byte(param & 0x7F)})
	case alsaseq.EvChanPress:
		ch := data[0]
		value := int32(binary.LittleEndian.Uint32(data[8:]))
		if value < 0 {
			value = 0
		}
		emitMidi([]byte{0xD0 | ch, byte(value & 0x7F)})
	case alsaseq.EvPitchBend:
		ch := data[0]
		// ALSA stores pitch bend as signed -8192..+8191; reconstruct 14-bit
		v := int32(binary.LittleEndian.Uint32(data[8:])) + 8192
		if v < 0 {
			v = 0
		} else if v > 16383 {
			v = 16383
		}
		emitMidi([]byte{0xE0 | ch, byte(v & 0x7F), byte((v >> 7) & 0x7F)})
	case alsaseq.EvSensing:
		emitMidi([]byte{0xFE})
	}
	if evType != alsaseq.EvSensing {
		forwardSeqEvent(evType, data)
	}
}

func emitMidi(data []byte) {
	if len(data) == 0 {
		return
	}
	ints := make([]int, len(data))
	for i, b := range data {
		ints[i] = int(b)
	}
	midiAppend(midiEventJSON{
		TsMs:    time.Since(midiStart).Milliseconds(),
		Dir:     "IN",
		Len:     len(data),
		Data:    ints,
		Decoded: decodeMIDI(data),
	})
}

// ── MIDI decoder ───────────────────────────────────────────────────────────

func decodeMIDI(data []byte) string {
	if len(data) == 0 {
		return "empty"
	}
	status := data[0]

	if status == 0xF0 {
		if isPaletteResponse(data) && len(data) >= 17 {
			idx := int(data[7])
			r := int(data[8]) | (int(data[9]) << 7)
			g := int(data[10]) | (int(data[11]) << 7)
			b := int(data[12]) | (int(data[13]) << 7)
			return fmt.Sprintf("Palette[%d] R=%d G=%d B=%d", idx, r, g, b)
		}
		return fmt.Sprintf("SysEx (%d bytes)", len(data))
	}
	if status >= 0xF1 {
		switch status {
		case 0xF8:
			return "Clock"
		case 0xFA:
			return "Start"
		case 0xFB:
			return "Continue"
		case 0xFC:
			return "Stop"
		case 0xFE:
			return "Active Sensing"
		case 0xFF:
			return "System Reset"
		default:
			return fmt.Sprintf("System 0x%02X", status)
		}
	}

	ch := (status & 0x0F) + 1
	switch status & 0xF0 {
	case 0x80:
		if len(data) >= 3 {
			return fmt.Sprintf("Note Off  ch=%d note=%d vel=%d", ch, data[1], data[2])
		}
	case 0x90:
		if len(data) >= 3 {
			if data[2] == 0 {
				return fmt.Sprintf("Note Off  ch=%d note=%d (vel=0)", ch, data[1])
			}
			return fmt.Sprintf("Note On   ch=%d note=%d vel=%d", ch, data[1], data[2])
		}
	case 0xA0:
		if len(data) >= 3 {
			return fmt.Sprintf("Poly Pres ch=%d note=%d val=%d", ch, data[1], data[2])
		}
	case 0xB0:
		if len(data) >= 3 {
			return fmt.Sprintf("CC        ch=%d cc=%d val=%d", ch, data[1], data[2])
		}
	case 0xC0:
		if len(data) >= 2 {
			return fmt.Sprintf("Prog Ch   ch=%d prog=%d", ch, data[1])
		}
	case 0xD0:
		if len(data) >= 2 {
			return fmt.Sprintf("Chan Pres ch=%d val=%d", ch, data[1])
		}
	case 0xE0:
		if len(data) >= 3 {
			bend := (int(data[2])<<7 | int(data[1])) - 8192
			return fmt.Sprintf("Pitch Bend ch=%d val=%d", ch, bend)
		}
	}

	hex := ""
	for i, b := range data {
		if i > 0 {
			hex += " "
		}
		hex += fmt.Sprintf("%02X", b)
	}
	return hex
}

// ── HTTP handlers ──────────────────────────────────────────────────────────

// GET /api/midi/events?n=<count>
func handleMidiEvents(w http.ResponseWriter, r *http.Request) {
	n := 50
	if s := r.URL.Query().Get("n"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			n = v
		}
	}
	if n < 1 {
		n = 1
	}
	if n > midiRingSize {
		n = midiRingSize
	}

	midiRingMu.RLock()
	writeIdx := midiWriteIdx
	total := midiTotal
	online := midiOnline
	available := int(total)
	if available > midiRingSize {
		available = midiRingSize
	}
	if n > available {
		n = available
	}
	events := make([]midiEventJSON, 0, n)
	for i := n - 1; i >= 0; i-- {
		slot := (uint32(int(writeIdx)-1-i) + midiRingSize*4) % midiRingSize
		events = append(events, midiRing[slot])
	}
	midiRingMu.RUnlock()

	jsonResponse(w, map[string]interface{}{
		"connected": online,
		"write_idx": writeIdx,
		"total":     total,
		"events":    events,
	})
}

// ── MIDI chord detector ───────────────────────────────────────────────────
//
// A "chord" = set of CC numbers that must all be held simultaneously.
// When the last CC in a chord reaches val=127 while all others are already
// held, the chord's action fires. 500ms debounce prevents repeat fires while
// buttons are held down.
//
// Chord events are appended to the MIDI ring buffer with Dir="CHORD" so the
// SSE stream delivers them to the UI for real-time checkbox/state sync.

type midiChord struct {
	Name        string
	CCs         []uint8
	Description string
	action      func()
}

// midiChordJSON is the public shape returned by GET /api/midi/chords.
type midiChordJSON struct {
	Name        string  `json:"name"`
	CCs         []uint8 `json:"ccs"`
	Description string  `json:"description"`
}

var (
	midiChordMu       sync.Mutex
	midiHeldCCs       = map[uint8]bool{}
	midiChordLastFire = map[string]time.Time{}
	midiChordList     []midiChord
)

// ── Button LED toggle state ───────────────────────────────────────────────────
// Maps CC number → last sent value (0=off, 127=on).
// Protected by ledToggleMu; never held across a sendSeqCC call.
var (
	ledToggleMu    sync.Mutex
	ledToggleState = map[uint8]uint8{}
)

// ── LED mode config ──────────────────────────────────────────────────────────

// LED behavior modes for CC buttons under MIDI intercept.
const (
	LEDModeTrigger   = "trigger"   // press toggles on↔off (default)
	LEDModeMomentary = "momentary" // on while held, off on release
	LEDModeExclusive = "exclusive" // radio group: lights this, clears rest of group
)

// LED animation types (MIDI channel 1–15 selects type+speed; ch 0 = no animation).
// Push 2/3 MIDI spec §LED Animation: base color on ch 0, target+type+speed on ch 1–15.
const (
	LEDAnimNone    = ""         // no animation (ch 0 only)
	LEDAnimOneShot = "oneshot"  // fade once from base to target, then hold target
	LEDAnimPulse   = "pulse"    // continuous pulse between base and target
	LEDAnimBlink   = "blink"    // continuous blink between base and target
)

// LED animation speeds (duration per cycle).
const (
	LEDAnimSpeed24th    = "24th"    // 4 MIDI clocks
	LEDAnimSpeed16th    = "16th"    // 6 MIDI clocks
	LEDAnimSpeed8th     = "8th"     // 12 MIDI clocks
	LEDAnimSpeedQuarter = "quarter" // 24 MIDI clocks
	LEDAnimSpeedHalf    = "half"    // 48 MIDI clocks
)

// ledAnimChannel returns the MIDI channel (1–15) for an animation type+speed pair.
// Returns 0 if type is empty (no animation).
func ledAnimChannel(animType, animSpeed string) byte {
	typeBase := map[string]byte{
		LEDAnimOneShot: 0,
		LEDAnimPulse:   5,
		LEDAnimBlink:   10,
	}
	speedOff := map[string]byte{
		LEDAnimSpeed24th:    1,
		LEDAnimSpeed16th:    2,
		LEDAnimSpeed8th:     3,
		LEDAnimSpeedQuarter: 4,
		LEDAnimSpeedHalf:    5,
	}
	base, okT := typeBase[animType]
	off, okS := speedOff[animSpeed]
	if !okT || !okS {
		return 0
	}
	return base + off
}

// LEDConfig defines how a specific CC LED responds to button presses.
type LEDConfig struct {
	Mode       string `json:"mode"`                  // LEDModeTrigger | LEDModeMomentary | LEDModeExclusive
	Color      uint8  `json:"color"`                 // palette index for "on" state (0=off)
	Group      string `json:"group,omitempty"`        // exclusive-mode group name
	AnimType   string `json:"anim_type,omitempty"`   // LEDAnimOneShot | LEDAnimPulse | LEDAnimBlink
	AnimSpeed  string `json:"anim_speed,omitempty"`  // LEDAnimSpeed* constant
	AnimColor  uint8  `json:"anim_color,omitempty"`  // target palette index for animation
}

// namedColors (core/push3.NamedColors) maps convenience names to Push 3
// palette velocity/CC indices — see core/push3/colors.go for the source of
// truth and docs/push3-led-colors.md for the full 128-entry table.

var (
	ledConfigMu sync.Mutex // guards ledConfigs
	ledConfigs  = map[uint8]LEDConfig{}
)

// midiConfigPath is set by main() to <hackdir>/midi.json before initMidiChords().
var midiConfigPath string

// midiPersistData is the on-disk format for persisted MIDI configuration.
type midiPersistData struct {
	LEDConfigs map[string]LEDConfig `json:"led_configs"` // keyed by CC string e.g. "102"

	// MIDI remapping (see remap.go). Mappings keyed by "<src_type>:<ch>:<num>".
	Mappings              map[string]MidiMapping `json:"mappings,omitempty"`
	RemapEnabled          bool                   `json:"remap_enabled,omitempty"`
	RemapRequireIntercept bool                   `json:"remap_require_intercept,omitempty"`
	RemapOutClient        uint8                  `json:"remap_out_client,omitempty"`
	RemapOutPort          uint8                  `json:"remap_out_port,omitempty"`
}

// loadMidiPersist reads midiConfigPath into ledConfigs.
// No-op if path is empty or file does not exist.
func loadMidiPersist() {
	if midiConfigPath == "" {
		return
	}
	f, err := os.Open(midiConfigPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("midi config: load: %v", err)
		}
		return
	}
	defer f.Close()
	var data midiPersistData
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		log.Printf("midi config: parse: %v", err)
		return
	}
	remapLoadFromPersist(data)
	ledConfigMu.Lock()
	defer ledConfigMu.Unlock()
	for ccStr, cfg := range data.LEDConfigs {
		v, err := strconv.ParseUint(ccStr, 10, 8)
		if err != nil {
			continue
		}
		ledConfigs[uint8(v)] = cfg
	}
	log.Printf("midi config: loaded %d LED configs, %d remaps from %s", len(ledConfigs), len(data.Mappings), midiConfigPath)
}

// saveMidiPersist writes current ledConfigs to midiConfigPath atomically.
// Safe to call in a goroutine.
func saveMidiPersist() {
	if midiConfigPath == "" {
		return
	}
	ledConfigMu.Lock()
	data := midiPersistData{
		LEDConfigs: make(map[string]LEDConfig, len(ledConfigs)),
	}
	for cc, cfg := range ledConfigs {
		data.LEDConfigs[fmt.Sprintf("%d", cc)] = cfg
	}
	ledConfigMu.Unlock()

	// Always persist shadow UI tab defaults even when shadow UI is inactive
	// (shadowUnregisterLEDs removes them from ledConfigs on intercept OFF, so
	// without this they would be lost from the file on any save while inactive).
	shadowDefaults := map[uint8]LEDConfig{
		CCScreenTop1: {Mode: LEDModeExclusive, Color: shadowTabColor, Group: "shadow-tabs"},
		CCScreenTop2: {Mode: LEDModeExclusive, Color: shadowTabColor, Group: "shadow-tabs"},
		CCSettings:   {Mode: LEDModeExclusive, Color: 127, Group: "settings-anchor"},
		CCScreenBot4: {Mode: LEDModeMomentary, Color: 127},
	}
	for cc, cfg := range shadowDefaults {
		data.LEDConfigs[fmt.Sprintf("%d", cc)] = cfg
	}

	remapFillPersist(&data)

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("midi config: marshal: %v", err)
		return
	}
	tmp := midiConfigPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		log.Printf("midi config: write: %v", err)
		return
	}
	if err := os.Rename(tmp, midiConfigPath); err != nil {
		log.Printf("midi config: rename: %v", err)
		_ = os.Remove(tmp)
	}
}

// resolveLEDColor parses a color value: float64 (JSON number) or string (name or numeric).
// Returns (value, true) on success; value is clamped to 0–127.
func resolveLEDColor(v interface{}) (uint8, bool) {
	switch c := v.(type) {
	case float64:
		if c < 0 || c > 127 {
			return 0, false
		}
		return uint8(c), true
	case string:
		if idx, ok := push3.ColorByName(c); ok {
			return idx, true
		}
		n, err := strconv.ParseUint(c, 10, 8)
		if err != nil || n > 127 {
			return 0, false
		}
		return uint8(n), true
	}
	return 0, false
}

// isTogglableCC returns true for CCs that represent buttons with LEDs.
// Encoder rotation CCs (70–79) send value=127 for rotation, not press, and have no LEDs.
// Jog wheel (70) and tempo (14) also excluded.
// isScreenBotCC reports whether cc is one of the 8 under-screen buttons the
// Shadow UI uses as soft-buttons (LOAD/SEARCH/FILTER/REFRESH, filter toggles, etc.).
func isScreenBotCC(cc uint8) bool {
	return cc >= CCScreenBot1 && cc <= CCScreenBot8
}

func isTogglableCC(cc uint8) bool {
	switch {
	case cc >= 70 && cc <= 79:
		return false // encoder rotation CCs
	case cc == 14:
		return false // tempo encoder
	default:
		return true
	}
}

func initMidiChords() {
	midiChordList = []midiChord{
		{
			Name:        "intercept_toggle",
			CCs:         []uint8{49, 30}, // cc=49 (0x31) + cc=30 (0x1E)
			Description: "Toggle MIDI intercept",
			action: func() {
				data := ensureMidiFilt()
				newState := false
				if data != nil {
					if data[4] == 0 {
						data[4] = 1
						newState = true
					} else {
						data[4] = 0
					}
				}
				log.Printf("midi chord: intercept_toggle → enabled=%v", newState)
				decoded := "intercept_toggle:false"
				if newState {
					decoded = "intercept_toggle:true"
				}
				midiAppend(midiEventJSON{
					TsMs:    time.Since(midiStart).Milliseconds(),
					Dir:     "CHORD",
					Len:     0,
					Data:    []int{},
					Decoded: decoded,
				})
				if newState {
					// Intercept ON: clear LEDs immediately, show OSD for 2s,
					// then start shadow UI via OnComplete — avoids framebuf race.
					go clearAllLEDs(CCSettings)
					sent := false
					select {
					case osdCh <- OSDRequest{
						Lines: []OSDLine{
							{Text: "PUSH HACK", Scale: 3},
							{Text: "Shadow UI ON", Scale: 1},
							{Text: "MIDI INTERCEPT ON", Scale: 1},
						},
						Duration:   2 * time.Second,
						OnComplete: shadowUIStart,
					}:
						sent = true
					default:
						log.Printf("osd: channel full, dropping intercept-ON OSD")
					}
					if !sent {
						shadowUIStart()
					}
				} else {
					// Intercept OFF: stop shadow UI, then show brief OSD.
					shadowUIStop()
					select {
					case osdCh <- OSDRequest{
						Lines: []OSDLine{
							{Text: "PUSH HACK", Scale: 3},
							{Text: "Shadow UI OFF", Scale: 1},
							{Text: "MIDI INTERCEPT OFF", Scale: 1},
						},
						Duration: 2 * time.Second,
					}:
					default:
						log.Printf("osd: channel full, dropping OSD request")
					}
				}
			},
		},
		{
			Name:        "browser_open",
			CCs:         []uint8{CCShift, CCSet}, // cc=49 (Shift) + cc=80 (Set)
			Description: "Open preset browser (Shadow UI Browse tab)",
			action: func() {
				data := ensureMidiFilt()
				interceptOn := data != nil && data[4] == 1
				if interceptOn {
					// Already active — toggle off: stop shadow UI and disable intercept.
					if data != nil {
						data[4] = 0
					}
					log.Printf("midi chord: browser_open → intercept off")
					shadowUIStop()
					select {
					case osdCh <- OSDRequest{
						Lines: []OSDLine{
							{Text: "PUSH HACK", Scale: 3},
							{Text: "Shadow UI OFF", Scale: 1},
							{Text: "MIDI INTERCEPT OFF", Scale: 1},
						},
						Duration: 2 * time.Second,
					}:
					default:
						log.Printf("osd: channel full, dropping OSD request")
					}
					return
				}
				// Turn intercept ON, clear LEDs, show OSD, then start the shadow
				// UI on the Browse tab via OnComplete (avoids the framebuf race).
				if data != nil {
					data[4] = 1
				}
				log.Printf("midi chord: browser_open → intercept on, Browse tab")
				go clearAllLEDs(CCSettings)
				openBrowse := func() {
					shadowUIStart()
					shadowUISwitchToBrowse()
				}
				select {
				case osdCh <- OSDRequest{
					Lines: []OSDLine{
						{Text: "PUSH HACK", Scale: 3},
						{Text: "Browser", Scale: 1},
						{Text: "Shift + Set", Scale: 1},
					},
					Duration:   2 * time.Second,
					OnComplete: openBrowse,
				}:
				default:
					log.Printf("osd: channel full, opening Browse without OSD")
					openBrowse()
				}
			},
		},
	}
}

// chordCCPressed is called when a CC message with val=127 is received.
// It adds the CC to the held set and checks whether any chord is now complete.
func chordCCPressed(cc uint8) {
	midiChordMu.Lock()
	midiHeldCCs[cc] = true
	now := time.Now()
	var toFire []midiChord
	for _, chord := range midiChordList {
		allHeld := true
		for _, c := range chord.CCs {
			if !midiHeldCCs[c] {
				allHeld = false
				break
			}
		}
		if !allHeld {
			continue
		}
		if last, ok := midiChordLastFire[chord.Name]; ok && now.Sub(last) < 500*time.Millisecond {
			continue // debounce
		}
		midiChordLastFire[chord.Name] = now
		toFire = append(toFire, chord)
	}
	midiChordMu.Unlock()

	for _, chord := range toFire {
		go chord.action()
	}
}

// chordCCReleased is called when a CC message with val=0 is received.
func chordCCReleased(cc uint8) {
	midiChordMu.Lock()
	delete(midiHeldCCs, cc)
	midiChordMu.Unlock()
}

// sendLEDAnim sends the MIDI messages required to set an LED color and optionally
// start an animation. For animation: first sends base color on ch 0, then sends
// target color on the animation channel (1–15). For no animation: sends on ch 0 only.
func sendLEDAnim(cc uint8, cfg LEDConfig, colorVal uint8) {
	ch := ledAnimChannel(cfg.AnimType, cfg.AnimSpeed)
	if ch == 0 || colorVal == 0 {
		// No animation (or turning off): single message on ch 0.
		if err := sendSeqCC(0, cc, int32(colorVal)); err != nil {
			log.Printf("sendLEDAnim cc=%d: %v", cc, err)
		}
		return
	}
	// Animation: base color on ch 0, then target+type+speed on ch N.
	if err := sendSeqCC(0, cc, int32(colorVal)); err != nil {
		log.Printf("sendLEDAnim base cc=%d: %v", cc, err)
	}
	if err := sendSeqCC(ch, cc, int32(cfg.AnimColor)); err != nil {
		log.Printf("sendLEDAnim anim cc=%d ch=%d: %v", cc, ch, err)
	}
}

// triggerLED toggles the LED for a CC button on each press (trigger mode).
// Uses the configured color for "on"; 0 for "off".
// Called from a goroutine — no locks held on entry.
func triggerLED(cc, color uint8) {
	ledConfigMu.Lock()
	cfg := ledConfigs[cc]
	ledConfigMu.Unlock()

	ledToggleMu.Lock()
	cur := ledToggleState[cc]
	var next uint8
	if cur != 0 {
		next = 0
	} else {
		next = color
	}
	ledToggleState[cc] = next
	ledToggleMu.Unlock()

	sendLEDAnim(cc, cfg, next)
	midiAppend(midiEventJSON{
		TsMs:    time.Since(midiStart).Milliseconds(),
		Dir:     "LED",
		Len:     2,
		Data:    []int{int(cc), int(next)},
		Decoded: fmt.Sprintf("led_trigger cc=%d val=%d", cc, next),
	})
}

// momentaryLED turns a CC LED on while the button is held and off on release.
// pressed=true on value=127 (press), pressed=false on value=0 (release).
func momentaryLED(cc, color uint8, pressed bool) {
	ledConfigMu.Lock()
	cfg := ledConfigs[cc]
	ledConfigMu.Unlock()

	val := uint8(0)
	if pressed {
		val = color
	}
	ledToggleMu.Lock()
	ledToggleState[cc] = val
	ledToggleMu.Unlock()

	sendLEDAnim(cc, cfg, val)
	midiAppend(midiEventJSON{
		TsMs:    time.Since(midiStart).Milliseconds(),
		Dir:     "LED",
		Len:     2,
		Data:    []int{int(cc), int(val)},
		Decoded: fmt.Sprintf("led_momentary cc=%d val=%d pressed=%v", cc, val, pressed),
	})
}

// exclusiveLED lights cc at color and turns off all other CCs in the same group.
// Useful for tab/panel buttons where only one should be active at a time.
func exclusiveLED(cc, color uint8) {
	// Collect group peers + own config under lock (don't hold across sendSeqCC).
	ledConfigMu.Lock()
	cfg := ledConfigs[cc]
	var peers []uint8
	if cfg.Group != "" {
		for otherCC, otherCfg := range ledConfigs {
			if otherCC != cc && otherCfg.Mode == LEDModeExclusive && otherCfg.Group == cfg.Group {
				peers = append(peers, otherCC)
			}
		}
	}
	ledConfigMu.Unlock()

	// Update toggle state atomically.
	ledToggleMu.Lock()
	for _, peer := range peers {
		ledToggleState[peer] = 0
	}
	ledToggleState[cc] = color
	ledToggleMu.Unlock()

	// Send MIDI — peers off (no anim on off), then cc on with animation.
	for _, peer := range peers {
		if err := sendSeqCC(0, peer, 0); err != nil {
			log.Printf("exclusiveLED clear cc=%d: %v", peer, err)
		}
	}
	sendLEDAnim(cc, cfg, color)
	midiAppend(midiEventJSON{
		TsMs:    time.Since(midiStart).Milliseconds(),
		Dir:     "LED",
		Len:     2,
		Data:    []int{int(cc), int(color)},
		Decoded: fmt.Sprintf("led_exclusive cc=%d val=%d group=%q", cc, color, cfg.Group),
	})
}

// allButtonCCs is the complete list of CC-addressable buttons on Push 3 with LEDs.
// Encoder rotation CCs (71–79, 14) are omitted — they have no LED.
var allButtonCCs = []uint8{
	// Screen top buttons
	102, 103, 104, 105, 106, 107, 108, 109,
	// Screen bottom buttons
	20, 21, 22, 23, 24, 25, 26, 27,
	// Top-right cluster
	CCSet, CCSettings, CCHelp, CCUserMode,
	// View buttons
	CCDeviceView, CCMixerView, CCClipView, CCSessionView,
	// Modifiers
	CCShift, CCSelect,
	// Edit
	CCUndo, CCSave, CCAdd, CCSwap,
	// Track controls
	CCLock, CCStopClips, CCMute, CCSolo, CCSelectMain,
	// Transport
	CCTapTempo, CCMetronome, CCQuantize, CCFixedLength,
	CCAutomate, CCNew, CCCapture, CCRecord, CCPlay,
	// Mode buttons
	CCRepeat, CCAccent, CCScale, CCLayout, CCNote, CCSession,
	// Loop / clip
	CCDoubleLoop, CCDuplicate, CCConvert, CCDelete,
	// Octave / page
	CCOctaveUp, CCOctaveDown, CCPageLeft, CCPageRight,
	// D-pad
	CCDPadUp, CCDPadRight, CCDPadDown, CCDPadLeft, CCDPadCenter,
	// Jog wheel press + clicks
	CCJogPress, CCJogClickLeft, CCJogClickRight,
}

// clearAllLEDs sends CC value=0 to every known button LED except exceptCC,
// and Note velocity=0 to all 64 pad notes (36–99).
// Also resets ledToggleState, preserving exceptCC if it was active.
// Intended to be called in a goroutine.
func clearAllLEDs(exceptCC uint8) {
	// CC buttons
	for _, cc := range allButtonCCs {
		if cc == exceptCC {
			continue
		}
		if err := sendSeqCC(0, cc, 0); err != nil {
			log.Printf("clearAllLEDs cc=%d: %v", cc, err)
		}
	}
	// Pad grid (notes 36–99)
	for note := uint8(PadNoteMin); note <= PadNoteMax; note++ {
		if err := sendSeqNote(0, note, 0); err != nil {
			log.Printf("clearAllLEDs note=%d: %v", note, err)
		}
	}
	// Explicitly light up the anchor button
	if err := sendSeqCC(0, exceptCC, 127); err != nil {
		log.Printf("clearAllLEDs anchor cc=%d: %v", exceptCC, err)
	}
	// Sync toggle state map
	ledToggleMu.Lock()
	ledToggleState = map[uint8]uint8{exceptCC: 127}
	ledToggleMu.Unlock()
}

// GET /api/midi/chords — list registered chord bindings
func handleMidiChords(w http.ResponseWriter, r *http.Request) {
	out := make([]midiChordJSON, len(midiChordList))
	for i, c := range midiChordList {
		out[i] = midiChordJSON{Name: c.Name, CCs: c.CCs, Description: c.Description}
	}
	jsonResponse(w, out)
}

// ── MIDI port enumeration ─────────────────────────────────────────────────

// enumMidiPorts parses /proc/asound/seq/clients and returns all ports whose
// capability string contains 'R' (readable / can be subscribed for input).
// Format example:
//
//	Client  16 : "Ableton Push 3 Live Port" [Kernel]
//	  Port   0 : "Ableton Push 3 Live Port" (RWe)
func enumMidiPorts(wantWritable bool) ([]MidiPort, error) {
	requireCaps := alsaseq.CapRead
	if wantWritable {
		requireCaps = alsaseq.CapWrite
	}
	raw, err := alsaseq.EnumPorts(requireCaps)
	if err != nil {
		return nil, err
	}

	midiTargetMu.Lock()
	activeClient := midiTargetClient
	activePort := midiTargetPort
	midiTargetMu.Unlock()

	ports := make([]MidiPort, 0, len(raw))
	for _, p := range raw {
		ports = append(ports, MidiPort{
			Client:   int(p.Addr.Client),
			Port:     int(p.Addr.Port),
			Name:     p.PortName,
			Active:   p.Addr.Client == activeClient && p.Addr.Port == activePort,
			Writable: p.Caps&alsaseq.CapWrite != 0,
		})
	}
	return ports, nil
}

// GET /api/midi/ports — enumerate ALSA sequencer ports.
// Default lists readable ports (for input subscription); ?writable=1 lists
// writable ports (for remap output destination selection).
func handleMidiPorts(w http.ResponseWriter, r *http.Request) {
	wantWritable := r.URL.Query().Get("writable") == "1"
	ports, err := enumMidiPorts(wantWritable)
	if err != nil {
		http.Error(w, "cannot read ALSA seq clients: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, ports)
}

// POST /api/midi/subscribe — switch the MIDI subscription target
// Body: {"client": 16, "port": 0}
// Closes the current seq fd to interrupt the blocked Read; the goroutine
// restarts and subscribes to the new client:port.
// subscribeMidiPort switches the ALSA seq input subscription to client:port.
// Takes manual control (disables auto-detect) and closes the current reader fd
// so readAlsaSeq re-subscribes to the new target. Shared by the HTTP handler and
// the Shadow UI MIDI monitor's encoder-driven port selection.
func subscribeMidiPort(client, port byte) {
	midiTargetMu.Lock()
	midiTargetClient = client
	midiTargetPort   = port
	midiAutoDetect   = false // user took manual control; don't override on reconnect
	oldFd := midiSeqFd
	if oldFd >= 0 {
		midiSeqFd = -1 // mark as externally closed so defer in readAlsaSeq skips it
	}
	midiTargetMu.Unlock()

	if oldFd >= 0 {
		syscall.Close(oldFd) // interrupt blocked Read; goroutine restarts with new target
	}

	log.Printf("midi: subscribe target changed to %d:%d", client, port)
}

func handleMidiSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Client int `json:"client"`
		Port   int `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	if body.Client < 0 || body.Client > 255 || body.Port < 0 || body.Port > 255 {
		http.Error(w, "client and port must be 0–255", http.StatusBadRequest)
		return
	}

	subscribeMidiPort(byte(body.Client), byte(body.Port))

	jsonResponse(w, map[string]interface{}{
		"client": body.Client,
		"port":   body.Port,
	})
}

// ── MIDI filter shm bridge ─────────────────────────────────────────────────
//
// Mirrors push_midi_filt_t from push_hook.c:
//   offset 0: uint32 magic  (0x4D464C54 "MFLT")
//   offset 4: uint8  enabled
//   offset 5: [11]uint8 pad
//   total: 16 bytes
//
// push-manager mmaps it R/W and writes enabled=0/1.
// push_hook.so reads it; zero-cost poll (kernel page is already in TLB).

const (
	midiFiltFile  = "/data/push-hack/hacks/push-display/midiflt"
	midiFiltMagic = uint32(0x4D464C54) // "MFLT"
	midiFiltSize  = 16
)

var (
	midiFiltMu          sync.Mutex
	midiFiltData        []byte // mmap'd slice, len=16; nil if unavailable
	midiFiltLastAttempt time.Time
)

// ensureMidiFilt returns the mapped midiflt shm, (re)connecting if needed.
// Rate-limited to once per 5s on failure — NOT a sync.Once, deliberately:
// push-manager can start before push-display's directory exists (install.sh
// has no dependency ordering between hacks, see CLAUDE.md's Risks), and a
// one-shot cache would make that failure permanent for the process's
// lifetime. Same retry pattern as core/display.Shm.Ensure.
func ensureMidiFilt() []byte {
	midiFiltMu.Lock()
	defer midiFiltMu.Unlock()
	if midiFiltData != nil {
		return midiFiltData
	}
	if time.Since(midiFiltLastAttempt) < 5*time.Second {
		return nil
	}
	midiFiltLastAttempt = time.Now()

	f, err := os.OpenFile(midiFiltFile, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		log.Printf("midi_filt: open %s: %v", midiFiltFile, err)
		return nil
	}
	defer f.Close()
	if err := f.Truncate(midiFiltSize); err != nil {
		log.Printf("midi_filt: truncate: %v", err)
		return nil
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, midiFiltSize,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		log.Printf("midi_filt: mmap: %v", err)
		return nil
	}
	// Write magic if not set
	existing := binary.LittleEndian.Uint32(data[0:4])
	if existing != midiFiltMagic {
		binary.LittleEndian.PutUint32(data[0:4], midiFiltMagic)
		data[4] = 0 // enabled=0
	}
	midiFiltData = data
	log.Printf("midi_filt: mapped %s (enabled=%d)", midiFiltFile, data[4])
	return midiFiltData
}

// POST /api/midi/filter — set MIDI intercept mode
// Body: {"enabled": true|false}
func handleMidiFilter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	data := ensureMidiFilt()
	if data == nil {
		http.Error(w, "midi_filt shm not available — is push-display deployed?", http.StatusServiceUnavailable)
		return
	}
	if body.Enabled {
		data[4] = 1
	} else {
		data[4] = 0
	}
	jsonResponse(w, map[string]interface{}{"enabled": body.Enabled})
}

// GET /api/midi/filter — current filter state
func handleMidiFilterStatus(w http.ResponseWriter, r *http.Request) {
	data := ensureMidiFilt()
	enabled := data != nil && data[4] == 1
	jsonResponse(w, map[string]interface{}{
		"enabled":   enabled,
		"available": data != nil,
	})
}

// GET /api/midi/stream — SSE live stream
func handleMidiStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	midiRingMu.RLock()
	lastIdx := midiWriteIdx
	online := midiOnline
	midiRingMu.RUnlock()

	fmt.Fprintf(w, "event: connected\ndata: %v\n\n", online)
	flusher.Flush()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			midiRingMu.RLock()
			currentIdx := midiWriteIdx
			if currentIdx == lastIdx {
				midiRingMu.RUnlock()
				continue
			}
			diff := int(currentIdx - lastIdx)
			if diff > midiRingSize {
				diff = midiRingSize
			}
			newEvents := make([]midiEventJSON, 0, diff)
			for i := diff - 1; i >= 0; i-- {
				slot := (uint32(int(currentIdx)-1-i) + midiRingSize*4) % midiRingSize
				newEvents = append(newEvents, midiRing[slot])
			}
			lastIdx = currentIdx
			midiRingMu.RUnlock()

			for _, ev := range newEvents {
				dataStr := "["
				for i, b := range ev.Data {
					if i > 0 {
						dataStr += ","
					}
					dataStr += fmt.Sprintf("%d", b)
				}
				dataStr += "]"
				line := fmt.Sprintf(
					`{"ts_ms":%d,"dir":%q,"len":%d,"data":%s,"decoded":%q}`,
					ev.TsMs, ev.Dir, ev.Len, dataStr, ev.Decoded,
				)
				fmt.Fprintf(w, "data: %s\n\n", line)
			}
			flusher.Flush()
		}
	}
}
