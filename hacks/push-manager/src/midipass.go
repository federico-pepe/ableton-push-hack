// midipass.go -- what an app that owns the surface still leaves to Live,
// beyond midiflt's pass_cc (push_hook.c, "midipass"):
//
//   - the pads: notes 36-99, their pressure and the touch strip go to Live,
//     and Live keeps lighting the pads. An app that edits a device (the
//     screen, the encoders, the rows above and below it) keeps Push's whole
//     playing UX under it: scales, layouts, Note Repeat, drum racks, the
//     sequencer. A session asks with pads=1 (pmclient.SurfaceHandler.Pads).
//   - buttons above CC 87 (Duplicate, Delete, ...): pass_cc ends at 87.
//
// A file of its own, because midiflt is truncated to its 16 bytes here and
// the hook would read past a longer struct (SIGBUS in Push3). This file is
// created here, is never made shorter, and the hook maps it read-only after
// checking its size. An older hook does not read it: nothing more passes.
package main

import (
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

const (
	midiPassMagic = 0x5341504D // "MPAS"
	midiPassSize  = 16
	midiPassPads  = 4  // offset: 1 = the pads go to Live
	midiPassHiOff = 5  // offset: bit n = CC 88+n goes to Live
	midiPassHiMax = 40 // CCs 88..127
	// passPads, in a session's pass list, asks for the pads (not a CC).
	passPads = 1000
)

var (
	midiPassMu   sync.Mutex
	midiPassData []byte
)

func midiPassFile() string { return filepath.Join(filepath.Dir(midiFiltFile), "midipass") }

// ensureMidiPass maps the file, creating it if needed. Never shorter than
// midiPassSize, never truncated: the hook may have it mapped.
func ensureMidiPass() []byte {
	midiPassMu.Lock()
	defer midiPassMu.Unlock()
	if midiPassData != nil {
		return midiPassData
	}
	path := midiPassFile()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		log.Printf("midi_pass: open %s: %v", path, err)
		return nil
	}
	defer f.Close()
	f.Chmod(0666) //nolint:errcheck // the hook runs as ableton
	if uid, gid := ownerOf(filepath.Dir(path)); uid >= 0 {
		f.Chown(uid, gid) //nolint:errcheck
	}
	if st, err := f.Stat(); err == nil && st.Size() < midiPassSize {
		if err := f.Truncate(midiPassSize); err != nil { // grow only
			log.Printf("midi_pass: grow: %v", err)
			return nil
		}
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, midiPassSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		log.Printf("midi_pass: mmap: %v", err)
		return nil
	}
	if binary.LittleEndian.Uint32(data) != midiPassMagic {
		clear(data)
		binary.LittleEndian.PutUint32(data, midiPassMagic)
	}
	midiPassData = data
	return data
}

// setMidiPass writes the pads flag and the CCs above 87 of a pass list.
// Below 88 is midiflt's (setMidiFiltPass).
func setMidiPass(pass []int) {
	data := ensureMidiPass()
	if data == nil {
		return
	}
	writeMidiPass(data, pass)
}

func writeMidiPass(data []byte, pass []int) {
	hi := make([]byte, midiPassHiMax/8)
	pads := byte(0)
	for _, cc := range pass {
		switch {
		case cc == passPads:
			pads = 1
		case cc >= midiFiltPassMax && cc < midiFiltPassMax+midiPassHiMax:
			n := cc - midiFiltPassMax
			hi[n/8] |= 1 << (n % 8)
		}
	}
	copy(data[midiPassHiOff:], hi)
	data[midiPassPads] = pads
}

// passKeeps reports what a pass list leaves to Live, for the LEDs: those
// keep Live's lights when an app takes the surface.
func passKeeps(pass []int) (pads bool, ccs map[uint8]bool) {
	ccs = map[uint8]bool{}
	for _, cc := range pass {
		switch {
		case cc == passPads:
			pads = true
		case cc >= 0 && cc < 128:
			ccs[uint8(cc)] = true
		}
	}
	return pads, ccs
}
