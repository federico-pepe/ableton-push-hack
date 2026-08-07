package alsaseq

import (
	"testing"
)

type recordingHandler struct {
	fixed  []uint8
	varlen []uint8
}

func (h *recordingHandler) Fixed(evType uint8, src Addr, data []byte) {
	h.fixed = append(h.fixed, evType)
}

func (h *recordingHandler) VarLen(evType uint8, src Addr, payload []byte) {
	h.varlen = append(h.varlen, evType)
}

func fixedEvent(evType uint8) []byte {
	ev := make([]byte, EventSize)
	ev[EventOffType] = evType
	return ev
}

func sysExEvent(payload []byte) []byte {
	ev := make([]byte, EventSize+len(payload))
	ev[EventOffType] = EvSysEx
	ev[EventOffFlags] = FlagVarLen
	n := uint32(len(payload))
	ev[EventOffData] = byte(n)
	ev[EventOffData+1] = byte(n >> 8)
	ev[EventOffData+2] = byte(n >> 16)
	ev[EventOffData+3] = byte(n >> 24)
	copy(ev[EventSize:], payload)
	return ev
}

// TestWalkFixedVarlenFixed is automation's regression test: a SysEx event
// in the middle of the stream must not desync the fixed-event decode that
// follows it. automation's own event walker had no varlen branch and would
// misinterpret the SysEx payload bytes as fixed-length event headers,
// corrupting every event after it until a lucky buffer-boundary realignment.
func TestWalkFixedVarlenFixed(t *testing.T) {
	var buf []byte
	buf = append(buf, fixedEvent(EvController)...)
	buf = append(buf, fixedEvent(EvController)...)
	buf = append(buf, sysExEvent([]byte{0xF0, 0x00, 0x21, 0x1D, 0xF7})...)
	buf = append(buf, fixedEvent(EvNoteOn)...)

	h := &recordingHandler{}
	Walk(buf, h)

	wantFixed := []uint8{EvController, EvController, EvNoteOn}
	if len(h.fixed) != len(wantFixed) {
		t.Fatalf("fixed events = %v, want %v (SysEx desynced the decode)", h.fixed, wantFixed)
	}
	for i, ev := range wantFixed {
		if h.fixed[i] != ev {
			t.Errorf("fixed[%d] = %d, want %d", i, h.fixed[i], ev)
		}
	}
	if len(h.varlen) != 1 || h.varlen[0] != EvSysEx {
		t.Fatalf("varlen events = %v, want [EvSysEx]", h.varlen)
	}
}

func TestWalkSrcAddrPassedThrough(t *testing.T) {
	ev := fixedEvent(EvNoteOn)
	ev[EventOffSrcClient] = 16
	ev[EventOffSrcPort] = 0

	var gotSrc Addr
	h := &funcHandler{
		fixed: func(evType uint8, src Addr, data []byte) { gotSrc = src },
	}
	Walk(ev, h)
	if gotSrc != (Addr{Client: 16, Port: 0}) {
		t.Errorf("src = %+v, want {16 0}", gotSrc)
	}
}

func TestWalkIncompleteVarLenDropped(t *testing.T) {
	full := sysExEvent([]byte{0xF0, 0xF7})
	truncated := full[:len(full)-1] // missing last payload byte

	h := &recordingHandler{}
	Walk(truncated, h)
	if len(h.varlen) != 0 {
		t.Errorf("incomplete trailing varlen event should be dropped, got %v", h.varlen)
	}
}

type funcHandler struct {
	fixed  func(evType uint8, src Addr, data []byte)
	varlen func(evType uint8, src Addr, payload []byte)
}

func (h *funcHandler) Fixed(evType uint8, src Addr, data []byte) {
	if h.fixed != nil {
		h.fixed(evType, src, data)
	}
}
func (h *funcHandler) VarLen(evType uint8, src Addr, payload []byte) {
	if h.varlen != nil {
		h.varlen(evType, src, payload)
	}
}
