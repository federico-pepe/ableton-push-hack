//go:build linux

package display

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// fakeMirror writes a file the way push_hook.c lays it out: one frame of
// raw pixels, XORed as on the wire.
func fakeMirror(t *testing.T, raw []byte, seq uint32) string {
	t.Helper()
	file := make([]byte, mirrorSize)
	binary.LittleEndian.PutUint32(file, mirrorMagic)
	binary.LittleEndian.PutUint32(file[4:], mirrorVersion)
	binary.LittleEndian.PutUint32(file[8:], seq)
	ToWire(file[offMirrorPix:], raw)
	p := filepath.Join(t.TempDir(), "mirror")
	if err := os.WriteFile(p, file, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMirrorReadUndoesWireXOR(t *testing.T) {
	raw := make([]byte, FrameBytes)
	for i := range raw {
		raw[i] = byte(i * 13)
	}
	m := &Mirror{Path: fakeMirror(t, raw, 6)}
	m.Ensure()
	if !m.Connected() {
		t.Fatal("not connected")
	}
	got := make([]byte, FrameBytes)
	seq, ok := m.Read(got)
	if !ok || seq != 6 {
		t.Fatalf("Read = seq %d ok %v, want 6 true", seq, ok)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("pixels differ from the original raw frame")
	}
	if m.Seq() != 6 {
		t.Fatalf("Seq = %d, want 6", m.Seq())
	}
}

func TestMirrorMidFrameGivesUp(t *testing.T) {
	m := &Mirror{Path: fakeMirror(t, make([]byte, FrameBytes), 7)} // odd: hook mid-write
	m.Ensure()
	if _, ok := m.Read(make([]byte, FrameBytes)); ok {
		t.Fatal("Read succeeded while seq was odd")
	}
}

func TestMirrorMissingFile(t *testing.T) {
	m := &Mirror{Path: filepath.Join(t.TempDir(), "none")}
	m.Ensure()
	if m.Connected() || m.Seq() != 0 {
		t.Fatal("connected to a missing file")
	}
}
