package main

// midi.go — minimal ALSA sequencer CC output for the automation engine.
// Sends MIDI CC events directly to "Ableton Push 3 Live Port" via /dev/snd/seq.
// No subscription, no LED control, no ring buffer — CC output only.
//
// Patterns copied from push-manager/src/midi.go.

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

// ALSA seq kernel ABI constants live in core/alsaseq.

// ── Global MIDI output state ───────────────────────────────────────────────

var (
	midiTargetMu     sync.Mutex
	midiTargetClient = alsaseq.Push3ClientDefault
	midiTargetPort   = alsaseq.Push3PortDefault

	liveDestMu     sync.Mutex
	liveDestClient = byte(128) // Ableton Live — may shift, updated by detectLivePort
	liveDestPort   = byte(2)
	liveDestFound  bool

	midiOutMu sync.Mutex
	midiOut   *alsaseq.Client // nil if not initialized

	clockMu    sync.Mutex
	clockRing  [24]int64 // nanosecond timestamps, ring indexed by clockTotal%24
	clockTotal int64     // total MIDI clock ticks received

	midiInMu sync.Mutex
	midiIn   *alsaseq.Client // nil if not initialized
)

// ── Boot-settle gate (USB-A safety) ───────────────────────────────────────

const bootSettleSecs = 30.0

func waitForBootSettle() {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return
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
	detectLivePort()

	c, err := alsaseq.Open()
	if err != nil {
		log.Printf("midi_out: %v (CC output disabled)", err)
		return
	}
	if _, err := c.CreatePort("Push Hack Automation", alsaseq.CapRead|alsaseq.CapSubsRead, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
		c.Close()
		log.Printf("midi_out: %v (CC output disabled)", err)
		return
	}

	midiOutMu.Lock()
	midiOut = c
	midiOutMu.Unlock()

	liveDestMu.Lock()
	lc, lp := liveDestClient, liveDestPort
	liveDestMu.Unlock()
	log.Printf("midi_out: ready (client %d port %d → Live at %d:%d)", c.Addr().Client, c.Addr().Port, lc, lp)
}

func push3Dest() (client, port byte) {
	midiTargetMu.Lock()
	c, p := midiTargetClient, midiTargetPort
	midiTargetMu.Unlock()
	return c, p
}

func liveDest() (client, port byte) {
	liveDestMu.Lock()
	c, p := liveDestClient, liveDestPort
	liveDestMu.Unlock()
	return c, p
}

// detectLivePort scans /proc/asound/seq/clients for "Ableton Live" and finds
// its first writable non-Announce port — that is Live's MIDI input.
func detectLivePort() {
	ports, err := alsaseq.EnumPorts(alsaseq.CapWrite)
	if err != nil {
		return
	}
	for _, p := range ports {
		if p.ClientName != "Ableton Live" || p.PortName == "Announce" {
			continue
		}
		liveDestMu.Lock()
		oldC, oldP, oldFound := liveDestClient, liveDestPort, liveDestFound
		liveDestClient = p.Addr.Client
		liveDestPort = p.Addr.Port
		liveDestFound = true
		liveDestMu.Unlock()
		if !oldFound || oldC != p.Addr.Client || oldP != p.Addr.Port {
			log.Printf("midi: detected Ableton Live MIDI input at %d:%d", p.Addr.Client, p.Addr.Port)
		}
		return
	}

	liveDestMu.Lock()
	found := liveDestFound
	liveDestMu.Unlock()
	if !found {
		log.Printf("midi: Ableton Live writable port not found — CC inactive until Live starts")
	}
}

// sendSeqCC sends a single MIDI CC event to Live's ALSA seq input port.
// channel is 0-indexed; cc and value are 0–127.
func sendSeqCC(channel, cc byte, value int32) error {
	midiOutMu.Lock()
	c := midiOut
	midiOutMu.Unlock()
	if c == nil {
		return fmt.Errorf("midi_out not initialized")
	}

	dstClient, dstPort := liveDest()
	return c.SendCC(alsaseq.Addr{Client: dstClient, Port: dstPort}, channel, cc, value)
}

// initMidiIn creates an ALSA seq input port and subscribes to Push 3's live port
// to receive MIDI clock, start, and stop events.
func initMidiIn() {
	midiTargetMu.Lock()
	srcClient := midiTargetClient
	srcPort := midiTargetPort
	midiTargetMu.Unlock()

	c, err := alsaseq.Open()
	if err != nil {
		log.Printf("midi_in: %v", err)
		return
	}
	if _, err := c.CreatePort("Push Hack Clock", alsaseq.CapWrite|alsaseq.CapSubsWrite, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
		c.Close()
		log.Printf("midi_in: %v", err)
		return
	}

	src := alsaseq.Addr{Client: srcClient, Port: srcPort}
	if err := c.Subscribe(src); err != nil {
		log.Printf("midi_in: %v — MIDI clock unavailable", err)
	} else {
		log.Printf("midi_in: subscribed to %d:%d (client %d port %d)", srcClient, srcPort, c.Addr().Client, c.Addr().Port)
	}

	midiInMu.Lock()
	midiIn = c
	midiInMu.Unlock()

	go func() {
		if err := c.ReadLoop(autoSeqHandler{}); err != nil {
			log.Printf("midi_in: read stopped: %v", err)
		}
	}()
}

// autoSeqHandler adapts automation's clock/transport/CC processing to
// alsaseq.Handler. Previously readMidiEvents walked the buffer with a fixed
// 28-byte stride and no variable-length branch — any SysEx byte stream from
// Push 3 (this hack subscribes to Push3 16:0 for clock) desynced the decode
// until a buffer boundary happened to realign it. Sharing alsaseq.Walk fixes
// that by construction: VarLen is a no-op here, but it correctly consumes
// the SysEx bytes instead of misreading them as fixed events.
type autoSeqHandler struct{}

func (autoSeqHandler) Fixed(evType uint8, src alsaseq.Addr, data []byte) {
	switch evType {
	case alsaseq.EvClock:
		onMidiClock()
	case alsaseq.EvStart, alsaseq.EvContinue:
		onMidiTransportStart()
	case alsaseq.EvController:
		ch := data[0]
		cc := binary.LittleEndian.Uint32(data[4:])
		val := binary.LittleEndian.Uint32(data[8:])
		if ch == 0 && cc == push3.CCPlay && val == 127 {
			onPlayButtonPress()
		}
	}
}

func (autoSeqHandler) VarLen(evType uint8, src alsaseq.Addr, payload []byte) {}

// onPlayButtonPress handles CC85 val=127 (Push Play button press).
// When TransportSync is on this is the SOLE authority over Running — Push has
// no stop button, so each press toggles play/stop. MIDI Start/Stop events and
// the tempo poll deliberately do NOT touch Running (they used to fight this
// toggle and desynced the WebUI).
func onPlayButtonPress() {
	autoMu.Lock()
	if autoState.TransportSync {
		autoState.Running = !autoState.Running
		running := autoState.Running
		if !running {
			atomic.StoreUint32(&resetPhasesRequested, 1)
		}
		log.Printf("midi: Play button (CC85) → automation %v", running)
	}
	autoMu.Unlock()
}

func onMidiClock() {
	now := time.Now().UnixNano()
	clockMu.Lock()
	i := int(clockTotal % 24)
	prev := clockRing[i]
	clockRing[i] = now
	clockTotal++
	clockMu.Unlock()

	if prev > 0 {
		elapsed := float64(now-prev) * 1e-9
		if elapsed > 0.04 && elapsed < 10.0 { // sanity: ~6–1500 BPM
			bpm := 60.0 / elapsed
			liveBPMMu.Lock()
			liveBPM = bpm
			liveBPMMu.Unlock()
		}
	}
}

// onMidiTransportStart resets the BPM clock ring so tempo measurement restarts
// cleanly on a fresh transport start. It does NOT touch Running — the Push Play
// button (CC85) is the sole transport authority (see onPlayButtonPress).
func onMidiTransportStart() {
	clockMu.Lock()
	clockTotal = 0
	for i := range clockRing {
		clockRing[i] = 0
	}
	clockMu.Unlock()
}

// detectPush3Port scans /proc/asound/seq/clients for "Ableton Push 3 Live Port"
// and updates midiTargetClient/Port. Handles shifted client numbers when extra
// USB MIDI devices are connected at boot.
func detectPush3Port() {
	const push3PortName = "Ableton Push 3 Live Port"
	p, ok := alsaseq.FindByName(push3PortName, alsaseq.CapRead)
	if !ok {
		log.Printf("midi: Push 3 Live Port not found — using %d:%d",
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
