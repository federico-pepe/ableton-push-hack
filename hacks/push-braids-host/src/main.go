// push-braids-host — a Push3 standalone host proving the audio+MIDI+DSP
// chain end to end: reads pad/button MIDI straight off Push3's own ALSA
// sequencer port (the same port push-manager/automation/keyboard-visualizer
// already subscribe to, via the shared core/alsaseq package), feeds Note
// On/Off into a Move Anything plugin_api_v2 DSP module (Braids, loaded via
// cgo/dlopen — see bridge.c), and writes the rendered audio into the
// "Push Hack Virtual Audio" ALSA Loopback card built in
// hacks/push-audio-loopback.
//
// Catalog-installed hacks only ever get "-config <hack.json path>" as an
// argument (see hacks/push-catalog/push-catalog.sh's install_service), so
// this binary is fully config/file driven: no CLI flags for the PCM
// device, channels, rate, or buffer size. Channels/rate/period/buffer are
// negotiated live from whatever Live's own audio track actually opened
// (see hwparams/), not set by a human.
package main

/*
#cgo LDFLAGS: -lasound -ldl -lm
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/binary"
	"flag"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"

	"github.com/federico-pepe/ableton-push-hack/core/alsapcm"
	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

const (
	cardID                = "PHVAudio"
	defaultPushManagerURL = "http://localhost:7701"

	// How often the supervisor re-checks card presence / hw_params once
	// a session is already running — catches Live restarting with
	// different params, or the loopback card disappearing.
	steadyPollInterval = 2 * time.Second
	// Backoff while waiting for a dependency (card not present, or
	// present but not yet opened by Live) — deliberately short: both
	// conditions are expected to clear within a few seconds of normal
	// operation, not the kind of thing that benefits from a long wait.
	waitPollInterval = time.Second
)

// defaultParams mirrors the harness that validated this module
// (schwung-braids-main's own test) — a sustained FM voice, not silence,
// so a first run proves something audible without needing MIDI input at
// all. Real notes from Push3's pads override this via on_midi.
var defaultParams = [][2]string{
	{"engine", "24"}, // FM
	{"timbre", "0.5"},
	{"color", "0.5"},
	{"attack", "0.0"},
	{"decay", "0.3"},
	{"sustain", "0.8"},
	{"release", "0.3"},
	{"volume", "0.9"},
}

// midiHandler implements alsaseq.Handler, translating Push3's pad/button
// events into raw MIDI bytes for the DSP plugin's on_midi. Only note
// events matter for a sound-generator module like Braids; everything
// else (CC, aftertouch, clock) is ignored for this first pass.
//
// Fixed() runs on the ALSA read-loop goroutine; it must NOT call into the
// plugin directly. Braids' C++ instance state (voice envelopes,
// oscillators) is not thread-safe, and the render loop calls into the
// same instance from its own dedicated goroutine — two goroutines hitting
// the same C++ object with no synchronization is a real data race, not a
// hypothetical one. So Fixed() only parses and forwards raw bytes over a
// channel; every bridge_plugin_* call happens on the render goroutine,
// which drains this channel first.
type midiHandler struct {
	out    chan<- [3]byte
	ctl    chan<- controlEvent
	pmURL  string
	params *paramState
}

// controlEvent is a CC-derived UI action — an encoder turn or a page
// change — decoded on the ALSA read-loop goroutine and applied on the
// render goroutine, the same split as note messages and for the same
// reason: every bridge_plugin_* call must happen from the one goroutine
// that owns the plugin instance (see midiHandler's doc comment above).
// encoderIdx is -1 for a page-change event.
type controlEvent struct {
	encoderIdx int
	delta      int
}

func (h *midiHandler) Fixed(evType uint8, src alsaseq.Addr, data []byte) {
	if evType == alsaseq.EvController {
		// Push's own control-surface CCs (encoders, D-Pad, screen buttons)
		// are always channel 0. Every other channel carries per-pad MPE
		// expression from the pad grid — Push 3 assigns each held pad its
		// own channel and streams Channel Pressure plus CC 74 (MPE
		// "timbre"/Y-axis) on it — and CC 74 falls inside CCEncoder1-8's
		// range (71-78). Without this channel check, holding a pad reads
		// as encoder 4 spinning wildly (DecodeRel misreading an absolute
		// 0-127 MPE value as a relative delta), slamming whatever param
		// sits there to one extreme in a single block. This was the
		// reported "velocity jumps from 0% to 100%" bug — a pad's MPE
		// stream, not the pad's actual Note On velocity, was hitting the
		// encoder decoder.
		if data[0]&0x0F != 0 {
			return
		}
		cc := uint8(binary.LittleEndian.Uint32(data[4:]) & 0x7F)
		val := uint8(binary.LittleEndian.Uint32(data[8:]) & 0x7F)

		if cc == ccShift || cc == ccDevice {
			onChordCC(cc, val, h.pmURL, h.params)
			return
		}

		var ev controlEvent
		switch {
		case cc >= push3.CCEncoder1 && cc <= push3.CCEncoder8:
			ev = controlEvent{encoderIdx: int(cc) - push3.CCEncoder1, delta: push3.DecodeRel(val)}
		case cc == push3.CCDPadLeft && val == 127:
			ev = controlEvent{encoderIdx: -1, delta: -1}
		case cc == push3.CCDPadRight && val == 127:
			ev = controlEvent{encoderIdx: -1, delta: 1}
		default:
			return
		}
		select {
		case h.ctl <- ev:
		default:
			log.Printf("control channel full, dropped CC event cc=%d val=%d", cc, val)
		}
		return
	}

	var status byte
	switch evType {
	case alsaseq.EvNoteOn:
		status = 0x90
	case alsaseq.EvNoteOff:
		status = 0x80
	default:
		return
	}
	channel := data[0] & 0x0F
	note := data[1]
	velocity := data[2]

	// Push's own touch-sensitive controls (encoder touch = notes 0-7, D-Pad
	// center touch = note 13, etc. — docs/push3-button-map.md) send real
	// Note On/Off outside the pad grid's 36-99 range. Forwarding those to
	// Braids played them as low, unwanted notes on every encoder touch —
	// the reported "noise when touching encoders." Only the pad grid
	// should ever trigger a voice.
	if note < 36 || note > 99 {
		return
	}

	msg := [3]byte{status | channel, note, velocity}

	select {
	case h.out <- msg:
	default:
		log.Printf("MIDI channel full, dropped event type=%d note=%d vel=%d", evType, note, velocity)
	}
}

func (h *midiHandler) VarLen(evType uint8, src alsaseq.Addr, payload []byte) {
	// SysEx etc. — not relevant to a sound-generator module, ignored.
}

func main() {
	// A catalog install only ever respawns this process by restarting the
	// init.d service, which does not happen on its own if the process
	// merely crashes. Re-exec as a supervised child so a crash in the
	// cgo/dlopen/ALSA code below (the real crash-prone surface) gets
	// retried without needing a human or a service restart. The parent
	// here does no cgo of its own, so it is extremely unlikely to crash
	// itself.
	if os.Getenv("PBH_SUPERVISED") != "1" {
		runSupervisor()
		return
	}
	runSupervised()
}

func runSupervisor() {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		cmd.Env = append(os.Environ(), "PBH_SUPERVISED=1")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		start := time.Now()
		err := cmd.Run()
		ran := time.Since(start)
		log.Printf("supervised child exited after %v: %v", ran, err)

		// A child that ran a good while before dying gets a fast retry —
		// treat it as an isolated crash, not a boot loop.
		if ran > 30*time.Second {
			backoff = time.Second
		}
		log.Printf("respawning in %v", backoff)
		time.Sleep(backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func runSupervised() {
	configPath := flag.String("config", "hack.json", "path to hack.json config file")
	flag.Parse()
	hackDir, err := filepath.Abs(filepath.Dir(*configPath))
	if err != nil {
		log.Fatalf("resolving hack dir: %v", err)
	}

	// Defers all /dev/snd access until system uptime clears the cold-boot
	// USB-A enumeration window — see core/alsaseq/bootsettle.go. A
	// catalog-installed hack starts at boot, squarely inside that window.
	alsaseq.WaitForBootSettle()

	cfg, err := loadConfig(hackDir)
	if err != nil {
		log.Fatalf("loading %s: %v", configFileName, err)
	}
	pmURL := cfg.PushManagerURL
	if pmURL == "" {
		pmURL = defaultPushManagerURL
	}

	// The render goroutine(s) below are allocation-free (all buffers
	// pre-allocated, no per-iteration heap traffic), so GC should already
	// run rarely — but "should" isn't "does." Disabling it outright
	// removes GC as a variable entirely, rather than leaving it as an
	// unmeasured maybe.
	debug.SetGCPercent(-1)

	dspPath := filepath.Join(hackDir, "dsp.so")
	moduleDir := filepath.Join(hackDir, "module")

	log.Printf("loading DSP plugin: %s (module dir: %s)", dspPath, moduleDir)
	cSo := C.CString(dspPath)
	defer C.free(unsafe.Pointer(cSo))
	cDir := C.CString(moduleDir)
	defer C.free(unsafe.Pointer(cDir))

	// sample_rate/frames_per_block here only size the plugin's own
	// internal buffers at load time — the actual PCM session's real rate
	// and period (negotiated live, per device) are supplied separately to
	// each bridge_pcm_open call in audiosession.go.
	const pluginInitRate = 44100
	const pluginInitBlock = 128
	plugin := C.bridge_plugin_load(cSo, cDir, C.int(pluginInitRate), C.int(pluginInitBlock))
	if plugin == nil {
		log.Fatalf("bridge_plugin_load failed: %s", C.GoString(C.bridge_last_error()))
	}
	defer C.bridge_plugin_unload(plugin)
	log.Printf("plugin loaded and instance created")

	for _, kv := range defaultParams {
		k, v := C.CString(kv[0]), C.CString(kv[1])
		C.bridge_plugin_set_param(plugin, k, v)
		C.free(unsafe.Pointer(k))
		C.free(unsafe.Pointer(v))
	}

	metas, err := fetchChainParams(plugin)
	if err != nil {
		log.Fatalf("fetchChainParams: %v", err)
	}
	params := newParamState(metas)

	go runDependencyWatcher(pmURL)
	go runDisplayLoop(pmURL, params)

	// MIDI: subscribe to the configured source (Push3's own Live Port by
	// default), same pattern as push-manager/automation/keyboard-visualizer
	// via core/alsaseq.
	seq, err := alsaseq.Open()
	if err != nil {
		log.Fatalf("alsaseq.Open: %v", err)
	}
	defer seq.Close()

	if _, err := seq.CreatePort("Push Braids Host In",
		alsaseq.CapWrite|alsaseq.CapSubsWrite, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
		log.Fatalf("CreatePort: %v", err)
	}
	midiSrc := alsaseq.Addr{Client: cfg.MidiClient, Port: cfg.MidiPort}
	if err := seq.Subscribe(midiSrc); err != nil {
		log.Fatalf("Subscribe to %d:%d: %v", midiSrc.Client, midiSrc.Port, err)
	}
	log.Printf("subscribed to MIDI source %d:%d for pad/button input", midiSrc.Client, midiSrc.Port)

	midiCh := make(chan [3]byte, 256)
	ctlCh := make(chan controlEvent, 64)
	handler := &midiHandler{out: midiCh, ctl: ctlCh, pmURL: pmURL, params: params}
	go func() {
		if err := seq.ReadLoop(handler); err != nil {
			log.Printf("MIDI read loop ended: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	shutdown := make(chan struct{})
	go func() {
		sig := <-sigCh
		log.Printf("signal received (%v), stopping...", sig)
		close(shutdown)
	}()

	// The audio session itself — PCM open/close, channels/rate/period —
	// is fully owned by this supervisor loop, which blocks until
	// shutdown fires. It negotiates live off Live's own hw_params instead
	// of a value someone guessed and hardcoded, and reopens whenever
	// those params (or the user's chosen PCM device) change. See
	// audiosession.go.
	watchHWParams(cardID, cfg.PCMDevice, plugin, midiCh, ctlCh, params, shutdown)

	// Best-effort: leaving push-manager's MIDI intercept or display
	// takeover stuck on after this process exits would silently block pad
	// input to Live / freeze the screen with no process left to blame.
	shutdownUI(pmURL)
	log.Printf("stopped")
}

func cardPresent(id string) bool {
	cards, err := alsapcm.EnumCards()
	if err != nil {
		return false
	}
	for _, c := range cards {
		if c.ID == id {
			return true
		}
	}
	return false
}
