package main

/*
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"log"
	"runtime"
	"time"
	"unsafe"

	"push-braids-host/hwparams"
)

// audioSession owns one open PCM handle and the goroutine rendering into
// it. The DSP plugin instance and MIDI subscription live independently in
// runSupervised and outlive any number of sessions — only the PCM side
// needs to restart when Live renegotiates its buffer or the user picks a
// different output device, since bridge.h already separates
// bridge_pcm_open/close from the plugin's own lifecycle.
type audioSession struct {
	pcm      *C.bridge_pcm_t
	period   int
	channels int
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// startAudioSession opens the PCM device and starts its dedicated render
// goroutine. rate is passed separately from hp because the caller decides
// which rate to request; in practice it is always hp.Rate.
func startAudioSession(plugin *C.bridge_plugin_t, device string, hp hwparams.Params,
	midiCh <-chan [3]byte, ctlCh <-chan controlEvent, params *paramState, io *ioState, rt *sharedConfig) (*audioSession, error) {

	cDev := C.CString(device)
	defer C.free(unsafe.Pointer(cDev))
	pcm := C.bridge_pcm_open(cDev, C.uint(hp.Channels), C.uint(hp.Rate), C.uint(hp.Period), C.uint(hp.Buffer))
	if pcm == nil {
		return nil, errBridge("bridge_pcm_open")
	}

	s := &audioSession{
		pcm:      pcm,
		period:   int(C.bridge_pcm_period(pcm)),
		channels: int(C.bridge_pcm_channels(pcm)),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	log.Printf("audio session opened: device=%s channels=%d rate=%d period=%d (requested period=%d buffer=%d)",
		device, s.channels, hp.Rate, s.period, hp.Period, hp.Buffer)

	go s.run(plugin, midiCh, ctlCh, params, io, rt, hp.Rate)
	return s, nil
}

// stop signals the render goroutine and blocks until it has actually
// exited and closed the PCM handle — so the caller can safely open a new
// session against the same device right after this returns.
func (s *audioSession) stop() {
	close(s.stopCh)
	<-s.doneCh
}

// run is the real-time render loop: drain MIDI/control events, render a
// block, write it out. Each session gets its own goroutine pinned to its
// own OS thread — SCHED_FIFO (bridge_set_realtime) is a per-thread kernel
// attribute, not per-process, so it must be reapplied here on every new
// session, not just once at startup. Losing this on a session restart
// would silently reintroduce the "clean short taps, glitches on held
// notes" bug already fixed once (see main.go's history / CHANGELOG).
func (s *audioSession) run(plugin *C.bridge_plugin_t, midiCh <-chan [3]byte, ctlCh <-chan controlEvent,
	params *paramState, io *ioState, rt *sharedConfig, rate int) {
	defer close(s.doneCh)
	defer C.bridge_pcm_close(s.pcm)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if rc := C.bridge_set_realtime(50); rc != 0 {
		log.Printf("warning: SCHED_FIFO unavailable (rc=%d) — continuing SCHED_OTHER", rc)
	}

	stereo := make([]int16, s.period*2)
	wide := make([]int16, s.period*s.channels)
	budget := time.Duration(s.period) * time.Second / time.Duration(rate)

	var blocks, xrunRetries, notesReceived, slowBlocks int64
	var maxPre, maxWrite time.Duration
	lastReport := time.Now()

	for {
		select {
		case <-s.stopCh:
			log.Printf("audio session stopped after %d blocks (%d xrun retries, %d notes, %d slow blocks)",
				blocks, xrunRetries, notesReceived, slowBlocks)
			return
		default:
		}

		blockStart := time.Now()

		// Drain any MIDI that arrived since the last block. Every
		// bridge_plugin_* call happens here, on this one goroutine — see
		// midiHandler's doc comment in main.go for why that matters.
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

		// Drain any encoder turns / page changes / I/O-picker actions the
		// same way — applying them here keeps every bridge_plugin_* call
		// on this one goroutine, same reasoning as the MIDI drain above.
	drainCtl:
		for {
			select {
			case ev := <-ctlCh:
				switch ev.encoderIdx {
				case -1:
					params.changePage(ev.delta)
					continue
				case -2:
					if params.IsIOPage() {
						io.moveCursor(ev.delta)
						params.MarkDirty()
					}
					continue
				case -3:
					if params.IsIOPage() {
						io.commit()
						params.MarkDirty()
					}
					continue
				}
				if params.IsIOPage() {
					continue // encoders are inert on the I/O page
				}
				key, val, ok := params.applyEncoder(ev.encoderIdx, ev.delta)
				if !ok {
					continue
				}
				k, v := C.CString(key), C.CString(val)
				C.bridge_plugin_set_param(plugin, k, v)
				C.free(unsafe.Pointer(k))
				C.free(unsafe.Pointer(v))
			default:
				break drainCtl
			}
		}

		C.bridge_plugin_render(plugin,
			(*C.int16_t)(unsafe.Pointer(&stereo[0])), C.int(s.period))

		for i := range wide {
			wide[i] = 0
		}
		// Which channel pair the stereo signal lands on is user-selectable
		// (I/O picker's "AUDIO CHANNEL" section) and read fresh every
		// block — applying it needs no PCM reopen, unlike a device or
		// hw_params change, since the channel count itself doesn't change.
		offset := rt.getChannelOffset()
		if offset < 0 || offset+1 >= s.channels {
			offset = 0 // defensive: picker only ever offers valid pairs for s.channels
		}
		for f := 0; f < s.period; f++ {
			base := f*s.channels + offset
			wide[base] = stereo[f*2+0]
			if offset+1 < s.channels {
				wide[base+1] = stereo[f*2+1]
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
		written := C.bridge_pcm_writei(s.pcm,
			(*C.int16_t)(unsafe.Pointer(&wide[0])), C.uint(s.period))
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
}

func errBridge(what string) error {
	return &bridgeError{what: what, msg: C.GoString(C.bridge_last_error())}
}

type bridgeError struct {
	what string
	msg  string
}

func (e *bridgeError) Error() string { return e.what + ": " + e.msg }

// watchHWParams is the top-level audio supervisor: it waits for
// push-audio-loopback's card to exist, waits for Live to actually open its
// side, and (re)opens an audioSession whenever the negotiated params (or
// the target device) change — continuously, for the process's life, so
// Live restarting mid-session with different params is handled the same
// way as the very first negotiation. push-braids-host waits on
// push-audio-loopback's effect on kernel/ALSA state here, not on its
// process, because catalog's `requires` only orders installation, not
// boot-time service start order (see catalog/schema.md).
func watchHWParams(cardID string, rt *sharedConfig, plugin *C.bridge_plugin_t,
	midiCh <-chan [3]byte, ctlCh <-chan controlEvent, params *paramState, io *ioState, shutdown <-chan struct{}) {

	var sess *audioSession
	var lastParams hwparams.Params
	var lastDevice string
	haveSess := false
	lastState := ""

	logTransition := func(state string) {
		if lastState == state {
			return
		}
		lastState = state
		log.Print(state)
	}

	stopSession := func() {
		if haveSess {
			sess.stop()
			sess = nil
			haveSess = false
		}
	}
	defer stopSession()

	for {
		select {
		case <-shutdown:
			return
		default:
		}

		if !cardPresent(cardID) {
			logTransition("waiting for " + cardID + " (push-audio-loopback not loaded yet)")
			stopSession()
			if !sleepOrStop(waitPollInterval, shutdown) {
				return
			}
			continue
		}

		hp, ok, err := hwparams.Read(cardID)
		if err != nil {
			log.Printf("reading hw_params for %s: %v", cardID, err)
			stopSession()
			if !sleepOrStop(waitPollInterval, shutdown) {
				return
			}
			continue
		}
		if !ok {
			logTransition("waiting for Live to open " + cardID + "...")
			stopSession()
			if !sleepOrStop(waitPollInterval, shutdown) {
				return
			}
			continue
		}

		device := rt.getPCM()
		if !haveSess || hp != lastParams || device != lastDevice {
			stopSession()
			newSess, err := startAudioSession(plugin, device, hp, midiCh, ctlCh, params, io, rt)
			if err != nil {
				log.Printf("opening PCM %s: %v — will retry", device, err)
				if !sleepOrStop(waitPollInterval, shutdown) {
					return
				}
				continue
			}
			sess = newSess
			haveSess = true
			lastParams = hp
			lastDevice = device
			logTransition("running")
		}

		if !sleepOrStop(steadyPollInterval, shutdown) {
			return
		}
	}
}

// sleepOrStop waits for d or shutdown, whichever comes first. Returns
// false if shutdown fired, so callers can bail out immediately instead of
// finishing a stale wait.
func sleepOrStop(d time.Duration, shutdown <-chan struct{}) bool {
	select {
	case <-time.After(d):
		return true
	case <-shutdown:
		return false
	}
}
