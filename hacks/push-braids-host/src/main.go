// push-braids-host — a minimal Push3 standalone host proving the
// audio+MIDI+DSP chain end to end: reads pad/button MIDI straight off
// Push3's own ALSA sequencer port (client 16, "Live Port" — the same
// port push-manager/automation/keyboard-visualizer already subscribe
// to, via the shared core/alsaseq package), feeds Note On/Off into a
// Move Anything plugin_api_v2 DSP module (Braids, loaded via cgo/dlopen
// — see bridge.c), and writes the rendered audio into the
// "Push Hack Virtual Audio" ALSA Loopback card built in
// hacks/push-audio-loopback.
//
// Deliberately has no display output yet — see the plan this hack
// implements: push-tethered-app's
// plans/2026-08-27-schwung-on-push3-feasibility.md, "Next steps" step 3.
package main

/*
#cgo LDFLAGS: -lasound -ldl -lm
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
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

type config struct {
	dspPath    string
	moduleDir  string
	pcmDevice  string
	channels   int
	rate       int
	periodHint int
	bufferHint int
}

func parseArgs() config {
	cfg := config{
		pcmDevice:  "hw:PHVAudio,1,0",
		channels:   32,
		rate:       44100,
		// 1024/3072, not 512/1536 — confirmed on real hardware
		// (2026-08-28) to make this kernel's occasional write-latency
		// spikes (see docs/push3-dsp-hosting.md) far less audible, at
		// the cost of extra latency (~23ms vs ~11.6ms per block).
		periodHint: 1024,
		bufferHint: 3072,
	}
	args := os.Args[1:]
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <dsp.so> <module_dir> [pcm_device] [channels] [rate] [period] [buffer]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "example: %s dsp.so . hw:PHVAudio,1,0 32 44100 512 1536\n", os.Args[0])
		os.Exit(2)
	}
	cfg.dspPath = args[0]
	cfg.moduleDir = args[1]
	if len(args) > 2 {
		cfg.pcmDevice = args[2]
	}
	if len(args) > 3 {
		cfg.channels, _ = strconv.Atoi(args[3])
	}
	if len(args) > 4 {
		cfg.rate, _ = strconv.Atoi(args[4])
	}
	if len(args) > 5 {
		cfg.periodHint, _ = strconv.Atoi(args[5])
	}
	if len(args) > 6 {
		cfg.bufferHint, _ = strconv.Atoi(args[6])
	}
	return cfg
}

// midiHandler implements alsaseq.Handler, translating Push3's pad/button
// events into raw MIDI bytes for the DSP plugin's on_midi. Only note
// events matter for a sound-generator module like Braids; everything
// else (CC, aftertouch, clock) is ignored for this first pass.
//
// Fixed() runs on the ALSA read-loop goroutine; it must NOT call into the
// plugin directly. Braids' C++ instance state (voice envelopes,
// oscillators) is not thread-safe, and the render loop below already
// calls into the same instance from the main goroutine — two goroutines
// hitting the same C++ object with no synchronization is a real data
// race, not a hypothetical one. So Fixed() only parses and forwards raw
// bytes over a channel; every bridge_plugin_* call happens on the main
// goroutine, in the render loop, which drains this channel first.
type midiHandler struct {
	out chan<- [3]byte
}

func (h *midiHandler) Fixed(evType uint8, src alsaseq.Addr, data []byte) {
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
	// Pin main() to one OS thread for its whole lifetime, before
	// bridge_set_realtime below. Without this, Go's scheduler is free
	// to move this goroutine's later cgo calls (the render loop) onto
	// a different OS thread than the one SCHED_FIFO actually got
	// applied to — sched_setscheduler(0, ...) sets the CALLING
	// thread's policy, not the whole process. That mismatch is a real,
	// documented Go+cgo real-time gotcha, not a hypothetical one: it
	// would explain exactly the "clean short taps, glitches on held
	// notes" symptom seen on real hardware (2026-08-28) — the render
	// loop occasionally landing on a plain SCHED_OTHER thread and
	// missing its ~11.6ms period deadline under scheduler contention.
	runtime.LockOSThread()

	// The render loop below is already allocation-free (all buffers
	// pre-allocated, no per-iteration heap traffic), so GC should
	// already run rarely — but "should" isn't "does." Disabling it
	// outright removes GC as a variable entirely for this debugging
	// pass, rather than leaving it as an unmeasured maybe.
	debug.SetGCPercent(-1)

	cfg := parseArgs()

	log.Printf("loading DSP plugin: %s (module dir: %s)", cfg.dspPath, cfg.moduleDir)
	cSo := C.CString(cfg.dspPath)
	defer C.free(unsafe.Pointer(cSo))
	cDir := C.CString(cfg.moduleDir)
	defer C.free(unsafe.Pointer(cDir))

	plugin := C.bridge_plugin_load(cSo, cDir, C.int(cfg.rate), C.int(cfg.periodHint))
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

	log.Printf("opening PCM device: %s (%dch @ %dHz, period hint %d, buffer hint %d)",
		cfg.pcmDevice, cfg.channels, cfg.rate, cfg.periodHint, cfg.bufferHint)
	cDev := C.CString(cfg.pcmDevice)
	defer C.free(unsafe.Pointer(cDev))
	pcm := C.bridge_pcm_open(cDev, C.uint(cfg.channels), C.uint(cfg.rate),
		C.uint(cfg.periodHint), C.uint(cfg.bufferHint))
	if pcm == nil {
		log.Fatalf("bridge_pcm_open failed: %s", C.GoString(C.bridge_last_error()))
	}
	defer C.bridge_pcm_close(pcm)

	period := int(C.bridge_pcm_period(pcm))
	channels := int(C.bridge_pcm_channels(pcm))
	log.Printf("PCM negotiated: period=%d channels=%d", period, channels)

	if rc := C.bridge_set_realtime(50); rc != 0 {
		log.Printf("warning: SCHED_FIFO unavailable (rc=%d) — continuing SCHED_OTHER", rc)
	}

	// MIDI: subscribe to Push3's Live Port, same pattern as
	// push-manager/automation/keyboard-visualizer via core/alsaseq.
	seq, err := alsaseq.Open()
	if err != nil {
		log.Fatalf("alsaseq.Open: %v", err)
	}
	defer seq.Close()

	if _, err := seq.CreatePort("Push Braids Host In",
		alsaseq.CapWrite|alsaseq.CapSubsWrite, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
		log.Fatalf("CreatePort: %v", err)
	}
	if err := seq.Subscribe(alsaseq.Addr{Client: alsaseq.Push3ClientDefault, Port: alsaseq.Push3PortDefault}); err != nil {
		log.Fatalf("Subscribe to Push3 Live Port: %v", err)
	}
	log.Printf("subscribed to Push3 Live Port (%d:%d) for pad/button MIDI",
		alsaseq.Push3ClientDefault, alsaseq.Push3PortDefault)

	midiCh := make(chan [3]byte, 256)
	handler := &midiHandler{out: midiCh}
	go func() {
		if err := seq.ReadLoop(handler); err != nil {
			log.Printf("MIDI read loop ended: %v", err)
		}
	}()

	// Render loop: stereo from the plugin, expanded into the PCM
	// device's actual (wider) channel count — same channel-0/1-only
	// pattern already proven audible in loopback_feed.c/the manual
	// hardware test, since Live listens on channels 1/2.
	stereo := make([]int16, period*2)
	wide := make([]int16, period*channels)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	stop := false
	go func() {
		<-sigCh
		log.Printf("signal received, stopping...")
		stop = true
	}()

	// budget is how long one period is worth of audio, wall-clock —
	// the deadline the producer side (drain+render+expand, everything
	// before bridge_pcm_writei) must beat. writei's own blocking time
	// is deliberately timed separately: it waits on ALSA backpressure
	// by design, so a long writei is normal pacing, not a stall, while
	// a long pre-writei time is this program failing to keep up.
	budget := time.Duration(period) * time.Second / time.Duration(cfg.rate)
	log.Printf("per-block budget: %v (period=%d, rate=%d)", budget, period, cfg.rate)

	log.Printf("render loop starting (Ctrl-C to stop)")
	var blocks, xrunRetries, notesReceived, slowBlocks int64
	var maxPre, maxWrite time.Duration
	lastReport := time.Now()

	for !stop {
		blockStart := time.Now()

		// Drain any MIDI that arrived since the last block. All
		// bridge_plugin_* calls happen here, on this one goroutine —
		// see midiHandler's doc comment for why that matters.
	drainMIDI:
		for {
			select {
			case msg := <-midiCh:
				notesReceived++
				cMsg := (*C.uint8_t)(unsafe.Pointer(&msg[0]))
				C.bridge_plugin_on_midi(plugin, cMsg, 3)
			default:
				break drainMIDI
			}
		}

		C.bridge_plugin_render(plugin,
			(*C.int16_t)(unsafe.Pointer(&stereo[0])), C.int(period))

		for i := range wide {
			wide[i] = 0
		}
		for f := 0; f < period; f++ {
			wide[f*channels+0] = stereo[f*2+0]
			if channels > 1 {
				wide[f*channels+1] = stereo[f*2+1]
			}
		}

		preElapsed := time.Since(blockStart)
		if preElapsed > maxPre {
			maxPre = preElapsed
		}
		if preElapsed > budget {
			slowBlocks++
			log.Printf("SLOW BLOCK #%d: drain+render+expand took %v, budget %v",
				blocks, preElapsed, budget)
		}

		writeStart := time.Now()
		written := C.bridge_pcm_writei(pcm,
			(*C.int16_t)(unsafe.Pointer(&wide[0])), C.uint(period))
		writeElapsed := time.Since(writeStart)
		if writeElapsed > maxWrite {
			maxWrite = writeElapsed
		}

		if time.Since(lastReport) > 2*time.Second {
			log.Printf("progress: blocks=%d slow=%d maxPre=%v maxWrite=%v",
				blocks, slowBlocks, maxPre, maxWrite)
			maxPre, maxWrite = 0, 0
			lastReport = time.Now()
		}

		if int(written) < 0 {
			xrunRetries++
			log.Printf("bridge_pcm_writei error (retry #%d): rc=%d", xrunRetries, written)
			continue
		}
		blocks++
	}

	log.Printf("stopped after %d blocks (%d xrun retries, %d notes received, %d slow blocks)",
		blocks, xrunRetries, notesReceived, slowBlocks)
}
