//go:build linux

package livedevice

import (
	"bufio"
	"encoding/json"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// testDevice: an effect that multiplies its input by the gain parameter and
// sends every note back one octave up.
type testDevice struct {
	gain   float64
	state  string
	closed *atomic.Bool
}

func (d *testDevice) Process(b *Block) {
	b.Params(func(i int, norm float64) { d.gain = norm; b.Report(1, norm/2) }) // Spare follows Gain, reported
	b.Events(func(off int, msg []byte) {
		if len(msg) == 3 && msg[0]&0xF0 == 0x90 {
			b.Emit(off, []byte{msg[0], msg[1] + 12, msg[2]})
		}
	})
	for ch := range 2 {
		for i := range b.Frames {
			b.Out[ch][i] = b.In[ch][i] * float32(d.gain)
		}
	}
}
func (d *testDevice) State() (string, error)  { return d.state, nil }
func (d *testDevice) SetState(s string) error { d.state = s; return nil }
func (d *testDevice) Close()                  { d.closed.Store(true) }

type testProvider struct{ closed atomic.Bool }

func (p *testProvider) New(c *Class) (Device, error) {
	return &testDevice{gain: 1, closed: &p.closed}, nil
}

// fakePlugin does what vst3/source/proxy.cpp does for one block.
type fakePlugin struct {
	t   *testing.T
	enc *json.Encoder
	dec *bufio.Reader
	b   *Block
}

func (p *fakePlugin) call(req map[string]any) map[string]any {
	p.enc.Encode(req)
	line, err := p.dec.ReadBytes('\n')
	if err != nil {
		p.t.Fatal(err)
	}
	var r map[string]any
	json.Unmarshal(line, &r)
	return r
}

func (p *fakePlugin) block(frames int, in float32, params map[int]float32, notes [][]byte) {
	b := p.b
	*b.u32(offFrames) = uint32(frames)
	for i := 0; i < frames; i++ {
		b.f32s(offInL, frames)[i] = in
		b.f32s(offInR, frames)[i] = in
	}
	n := 0
	for idx, v := range params {
		off := offParams + n*8
		*b.u32(off) = uint32(idx)
		*b.u32(off + 4) = math.Float32bits(v)
		n++
	}
	*b.u32(offNPar) = uint32(n)
	for i, m := range notes {
		off := offEvents + i*8
		*b.u32(off) = 0
		b.mem[off+4] = byte(len(m))
		copy(b.mem[off+5:], m)
	}
	*b.u32(offNEv) = uint32(len(notes))
	req := atomic.AddUint32(b.u32(offReq), 1)
	futexWake(b.u32(offReq))
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadUint32(b.u32(offDone)) != req {
		if time.Now().After(deadline) {
			p.t.Fatal("no answer to a block")
		}
		futexWait(b.u32(offDone), atomic.LoadUint32(b.u32(offDone)), 10*time.Millisecond)
	}
}

func TestServerBlock(t *testing.T) {
	if _, err := os.Stat("/dev/shm"); err != nil {
		t.Skip("no /dev/shm")
	}
	sock := filepath.Join(t.TempDir(), "dev.sock")
	cls := &Class{UID: ClassUID("t", Effect, "gain"), Name: "Gain", Kind: Effect,
		Params: []Param{{Name: "Gain", Type: "float", Max: 1, Default: 1}, {Name: "Spare", Type: "float", Max: 1}},
		Banks:  [][]int{{0}, {1}}} // bank 1 padded to a page: 7 placeholders
	PadBanks(cls)
	prov := &testProvider{}
	s := NewServer(Config{Name: "ldtest", Socket: sock, Logf: t.Logf}, prov, []*Class{cls})
	if err := s.Listen(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakePlugin{t: t, enc: json.NewEncoder(c), dec: bufio.NewReader(c)}
	r := p.call(map[string]any{"op": "create", "uid": cls.UID})
	if r["ok"] != true {
		t.Fatal(r)
	}
	f, err := os.OpenFile(r["shm"].(string), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	mem, err := syscall.Mmap(int(f.Fd()), 0, ipcSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	p.b = &Block{mem: mem}
	if *p.b.u32(offMagic) != ipcMagic || *p.b.u32(offVersion) != ipcVersion {
		t.Fatal("bad header")
	}

	p.block(128, 0.5, map[int]float32{0: 0.5}, [][]byte{{0x90, 60, 100}})
	if got := p.b.f32s(offOutL, 128)[5]; got != 0.25 {
		t.Fatalf("out %v, want 0.25", got)
	}
	if *p.b.u32(offONEv) != 1 || mem[offOEvents+5] != 0x90 || mem[offOEvents+6] != 72 {
		t.Fatalf("MIDI out %d %v", *p.b.u32(offONEv), mem[offOEvents:offOEvents+8])
	}
	if *p.b.u32(offONPar) != 1 || *p.b.u32(offOParams) != 1 || math.Float32frombits(*p.b.u32(offOParams + 4)) != 0.25 {
		t.Fatalf("reported %d: index %d norm %v", *p.b.u32(offONPar), *p.b.u32(offOParams), math.Float32frombits(*p.b.u32(offOParams + 4)))
	}
	// a placeholder's change never reaches the device
	pad := len(cls.Params) - 1
	if cls.Params[pad].Type == PadType {
		p.block(128, 1, map[int]float32{pad: 0}, nil)
		if got := p.b.f32s(offOutL, 128)[0]; got != 0.5 {
			t.Fatalf("a placeholder changed the gain: out %v", got)
		}
	}

	id := r["id"]
	if r := p.call(map[string]any{"op": "set_state", "id": id, "state": "hello"}); r["ok"] != true {
		t.Fatal(r)
	}
	if r := p.call(map[string]any{"op": "get_state", "id": id}); r["state"] != "hello" {
		t.Fatal(r)
	}
	st := s.Stats()
	if len(st) != 1 || st[0]["notes_in"].(uint64) != 1 || st[0]["notes_out"].(uint64) != 1 {
		t.Fatalf("stats %v", st)
	}
	// the plugin's connection closes: its device goes
	c.Close()
	syscall.Munmap(mem)
	deadline := time.Now().Add(2 * time.Second)
	for !prov.closed.Load() {
		if time.Now().After(deadline) {
			t.Fatal("device not closed with its connection")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(r["shm"].(string)); err == nil {
		t.Fatal("shared memory file left behind")
	}
}
