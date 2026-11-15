package main

import (
	"encoding/binary"
	"testing"
)

func TestWriteMidiPass(t *testing.T) {
	data := make([]byte, midiPassSize)
	binary.LittleEndian.PutUint32(data, midiPassMagic)
	writeMidiPass(data, []int{85, 88, 118, 127, passPads, 200})
	if data[midiPassPads] != 1 {
		t.Fatal("pads not set")
	}
	for cc, want := range map[int]bool{88: true, 118: true, 127: true, 89: false, 117: false} {
		n := cc - midiFiltPassMax
		if got := data[midiPassHiOff+n/8]&(1<<(n%8)) != 0; got != want {
			t.Errorf("CC %d: %v, want %v", cc, got, want)
		}
	}
	writeMidiPass(data, nil) // the next owner passes nothing
	for i := midiPassPads; i < midiPassHiOff+midiPassHiMax/8; i++ {
		if data[i] != 0 {
			t.Fatalf("byte %d left at %d", i, data[i])
		}
	}
	if binary.LittleEndian.Uint32(data) != midiPassMagic {
		t.Fatal("magic overwritten")
	}
}

func TestPassKeeps(t *testing.T) {
	pads, ccs := passKeeps([]int{50, 118, passPads})
	if !pads || !ccs[50] || !ccs[118] || len(ccs) != 2 {
		t.Fatal(pads, ccs)
	}
	if pads, ccs := passKeeps([]int{85}); pads || !ccs[85] {
		t.Fatal(pads, ccs)
	}
}
