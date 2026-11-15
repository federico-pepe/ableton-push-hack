package livedevice

import (
	"math"
	"unsafe"
)

// The block layout. Same numbers as vst3/source/livedevice_ipc.h;
// ipc_test.go checks that they agree.
const (
	ipcMagic   = 0x5645444C // "LDEV"
	ipcVersion = 2          // 2: parameter changes from the hack back to Live
	MaxFrames  = 4096       // frames per block, at most
	MaxEvents  = 256        // MIDI events per block, each way

	offMagic   = 0
	offVersion = 4
	offReq     = 8
	offDone    = 12
	offFrames  = 16
	offFlags   = 20
	offBPM     = 24
	offBeat    = 32
	offNEv     = 40
	offNPar    = 44
	offLate    = 48
	offAlive   = 52
	offEvents  = 64
	offParams  = offEvents + MaxEvents*8
	offInL     = offParams + MaxParams*8
	offInR     = offInL + MaxFrames*4
	offOutL    = offInR + MaxFrames*4
	offOutR    = offOutL + MaxFrames*4
	offONEv    = offOutR + MaxFrames*4
	offOEvents = offONEv + 64
	offONPar   = offOEvents + MaxEvents*8
	offOParams = offONPar + 64
	ipcSize    = offOParams + MaxParams*8
)

// Block is one audio block of one device, as Device.Process sees it. It is
// the shared memory itself: read and write it only inside Process.
type Block struct {
	mem   []byte
	class *Class

	Frames int
	// Transport, when Live sends it (TransportValid).
	TransportValid, Playing bool
	BPM, Beat               float64 // Beat: quarter notes since song start
	// In: the device's audio input (effects), Out: its output. Planar,
	// Frames long. Out starts silent.
	In, Out [2][]float32

	nOut, nOutPar     int
	notesIn, notesOut int
}

func (b *Block) u32(off int) *uint32 { return (*uint32)(unsafe.Pointer(&b.mem[off])) }
func (b *Block) f64(off int) float64 { return *(*float64)(unsafe.Pointer(&b.mem[off])) }
func (b *Block) f32s(off, n int) []float32 {
	return unsafe.Slice((*float32)(unsafe.Pointer(&b.mem[off])), n)
}

// load reads the plugin's half of the block.
func (b *Block) load() {
	b.Frames = int(*b.u32(offFrames))
	if b.Frames < 0 || b.Frames > MaxFrames {
		b.Frames = 0
	}
	flags := *b.u32(offFlags)
	b.TransportValid, b.Playing = flags&2 != 0, flags&1 != 0
	b.BPM, b.Beat = b.f64(offBPM), b.f64(offBeat)
	b.In = [2][]float32{b.f32s(offInL, b.Frames), b.f32s(offInR, b.Frames)}
	b.Out = [2][]float32{b.f32s(offOutL, b.Frames), b.f32s(offOutR, b.Frames)}
	clear(b.Out[0])
	clear(b.Out[1])
	b.nOut, b.nOutPar, b.notesIn, b.notesOut = 0, 0, 0, 0
}

// Events calls fn for each MIDI message Live sent this block: its sample
// offset in the block and 1 to 3 bytes (valid only during the call).
func (b *Block) Events(fn func(offset int, msg []byte)) {
	n := min(int(*b.u32(offNEv)), MaxEvents)
	for i := 0; i < n; i++ {
		off := offEvents + i*8
		l := int(b.mem[off+4])
		if l < 1 || l > 3 {
			continue
		}
		msg := b.mem[off+5 : off+5+l]
		if l == 3 && msg[0]&0xF0 == 0x90 && msg[2] > 0 {
			b.notesIn++
		}
		fn(int(*b.u32(off)), msg)
	}
}

// Params calls fn for each parameter Live changed this block: its index in
// the class's Params and its normalized value. Placeholders are left out.
func (b *Block) Params(fn func(index int, norm float64)) {
	n := min(int(*b.u32(offNPar)), MaxParams)
	for i := 0; i < n; i++ {
		off := offParams + i*8
		idx := int(*b.u32(off))
		if idx < 0 || idx >= len(b.class.Params) || b.class.Params[idx].Type == PadType {
			continue
		}
		norm := float64(math.Float32frombits(*b.u32(off + 4)))
		fn(idx, math.Max(0, math.Min(1, norm)))
	}
}

// Emit sends one MIDI message (1 to 3 bytes, no SysEx) out of the device at a
// sample offset: Live gets it in this block. Only MIDI kinds have a MIDI
// output. False when the block's output is full.
func (b *Block) Emit(offset int, msg []byte) bool {
	if b.nOut >= MaxEvents || len(msg) < 1 || len(msg) > 3 || msg[0] < 0x80 || msg[0] >= 0xF0 {
		return false
	}
	off := offOEvents + b.nOut*8
	*b.u32(off) = uint32(max(0, min(offset, b.Frames)))
	b.mem[off+4] = byte(len(msg))
	copy(b.mem[off+5:off+8], msg)
	if len(msg) == 3 && msg[0]&0xF0 == 0x90 && msg[2] > 0 {
		b.notesOut++
	}
	b.nOut++
	return true
}

// Report tells Live that parameter index (of the class's Params) is now norm:
// a change made on the hack's side (its web page, its own view), so Live,
// Push's device view and automation follow it. Changes Live made itself
// need no report. False when the block's report list is full.
func (b *Block) Report(index int, norm float64) bool {
	if b.nOutPar >= MaxParams || index < 0 || index >= len(b.class.Params) || b.class.Params[index].Type == PadType {
		return false
	}
	off := offOParams + b.nOutPar*8
	*b.u32(off) = uint32(index)
	*b.u32(off + 4) = math.Float32bits(float32(math.Max(0, math.Min(1, norm))))
	b.nOutPar++
	return true
}

// finish publishes the hack's half of the block.
func (b *Block) finish() {
	*b.u32(offONEv) = uint32(b.nOut)
	*b.u32(offONPar) = uint32(b.nOutPar)
}
