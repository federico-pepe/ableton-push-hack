package alsaseq

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	msgs := [][]byte{
		{0x90, 60, 100}, {0x93, 127, 1}, {0x80, 60, 0}, {0x90, 64, 0},
		{0xA2, 40, 77}, {0xB0, 7, 127}, {0xBF, 1, 0}, {0xC5, 12}, {0xD1, 99},
		{0xE0, 0, 64}, {0xE0, 0, 0}, {0xE0, 127, 127},
		{0xF8}, {0xFA}, {0xFB}, {0xFC}, {0xFE}, {0xF2, 5, 3},
	}
	for _, m := range msgs {
		evType, data, ok := EncodeMIDI(m)
		if !ok {
			t.Errorf("% x: not encoded", m)
			continue
		}
		if got := DecodeMIDI(evType, data); !bytes.Equal(got, m) {
			t.Errorf("% x -> type %d -> % x", m, evType, got)
		}
	}
}

// The data union layouts the kernel expects, not just self-consistency.
func TestEncodeLayout(t *testing.T) {
	evType, data, _ := EncodeMIDI([]byte{0x92, 61, 99})
	if evType != EvNoteOn || data[0] != 2 || data[1] != 61 || data[2] != 99 {
		t.Errorf("note on: type %d data % x", evType, data)
	}
	evType, data, _ = EncodeMIDI([]byte{0xB1, 74, 33})
	if evType != EvController || data[0] != 1 ||
		binary.LittleEndian.Uint32(data[4:]) != 74 || binary.LittleEndian.Uint32(data[8:]) != 33 {
		t.Errorf("cc: type %d data % x", evType, data)
	}
	// Pitch bend centre (00 40) is value 0, signed.
	_, data, _ = EncodeMIDI([]byte{0xE0, 0, 0x40})
	if v := int32(binary.LittleEndian.Uint32(data[8:])); v != 0 {
		t.Errorf("bend centre = %d, want 0", v)
	}
	_, data, _ = EncodeMIDI([]byte{0xE0, 0, 0})
	if v := int32(binary.LittleEndian.Uint32(data[8:])); v != -8192 {
		t.Errorf("bend bottom = %d, want -8192", v)
	}
	// Live's Stop arrives as type 32 (measured on device, 2026-10-16).
	if evType, _, _ := EncodeMIDI([]byte{0xFC}); evType != 32 {
		t.Errorf("stop encodes to %d, want 32", evType)
	}
}

func TestEncodeRejects(t *testing.T) {
	for _, m := range [][]byte{nil, {}, {0x40}, {0x90, 60}, {0xC0}, {0xF0, 1, 0xF7}, {0xF2, 1}} {
		if _, _, ok := EncodeMIDI(m); ok {
			t.Errorf("% x: encoded, want rejected", m)
		}
	}
}

// ioctl numbers are _IOC(dir, 'S', nr, size); recompute them from the
// struct sizes so a size edit cannot leave a stale number behind.
func TestIoctlNumbers(t *testing.T) {
	ioc := func(dir, nr, size uintptr) uintptr { return dir<<30 | size<<16 | 'S'<<8 | nr }
	const r, w = 2, 1
	cases := []struct {
		name      string
		got, want uintptr
	}{
		{"CLIENT_ID", IoctlClientID, ioc(r, 0x01, 4)},
		{"GET_CLIENT_INFO", IoctlGetClientInfo, ioc(r|w, 0x10, ClientInfoSize)},
		{"SET_CLIENT_INFO", IoctlSetClientInfo, ioc(w, 0x11, ClientInfoSize)},
		{"CREATE_PORT", IoctlCreatePort, ioc(r|w, 0x20, PortInfoSize)},
		{"SUBSCRIBE_PORT", IoctlSubscribePort, ioc(w, 0x30, SubSize)},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x", c.name, c.got, c.want)
		}
	}
}
