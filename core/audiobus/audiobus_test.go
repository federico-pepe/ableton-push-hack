package audiobus

import (
	"os"
	"testing"
	"time"
)

func testServer(t *testing.T, f Format) (*Server, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "abus")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s := NewServer(dir)
	s.SetFormat(f)
	if err := s.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, SocketPath(dir)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout: " + what)
}

func block(n int, l, r float32) []float32 {
	b := make([]float32, n*2)
	for i := 0; i < n; i++ {
		b[i*2], b[i*2+1] = l, r
	}
	return b
}

func TestRingWrapAndFull(t *testing.T) {
	dir := t.TempDir()
	g, err := createRing(dir+"/r", 8)
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()
	if !g.write(block(6, 1, 2)) || g.write(block(3, 1, 2)) {
		t.Fatal("full check")
	}
	dst := make([]float32, 8*4)
	got, peak := g.addTo(dst, 4, 4)
	if got != 4 || peak != 2 || dst[0] != 1 || dst[1] != 2 || dst[4] != 1 {
		t.Fatalf("got %d peak %v dst %v", got, peak, dst[:8])
	}
	if !g.write(block(6, 3, 4)) { // wraps
		t.Fatal("wrap write")
	}
	if got, _ := g.addTo(dst, 4, 99); got != 8 {
		t.Fatalf("drained %d", got)
	}
}

func TestMixPutsEachClientOnItsPair(t *testing.T) {
	f := Format{Rate: 48000, Period: 16, Channels: 8}
	s, sock := testServer(t, f)
	a := Open(sock, "a", 0)
	b := Open(sock, "b", 4)
	defer a.Close()
	defer b.Close()
	waitFor(t, "connect", func() bool { return a.Connected() && b.Connected() })
	if a.Format() != f || b.Pair() != 4 {
		t.Fatalf("format %+v pair %d", a.Format(), b.Pair())
	}
	if err := a.Write(block(16, 0.25, -0.25)); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(block(16, 0.5, 0.5)); err != nil {
		t.Fatal(err)
	}
	out := make([]float32, f.Period*f.Channels)
	s.Mix(out)
	for fr := 0; fr < f.Period; fr++ {
		row := out[fr*8 : fr*8+8]
		want := [8]float32{0.25, -0.25, 0, 0, 0.5, 0.5, 0, 0}
		if [8]float32(row) != want {
			t.Fatalf("frame %d: %v", fr, row)
		}
	}
}

func TestSamePairSums(t *testing.T) {
	f := Format{Rate: 48000, Period: 8, Channels: 4}
	s, sock := testServer(t, f)
	a, b := Open(sock, "a", 2), Open(sock, "b", 2)
	defer a.Close()
	defer b.Close()
	waitFor(t, "connect", func() bool { return a.Connected() && b.Connected() })
	a.Write(block(8, 0.5, 0.5))
	b.Write(block(8, 0.25, 0.25))
	out := make([]float32, 8*4)
	s.Mix(out)
	if out[2] != 0.75 {
		t.Fatalf("sum %v", out[:4])
	}
}

func TestFreePairsAndSavedPair(t *testing.T) {
	f := Format{Rate: 48000, Period: 8, Channels: 4}
	s, sock := testServer(t, f)
	a, b := Open(sock, "a", -1), Open(sock, "b", -1)
	waitFor(t, "connect", func() bool { return a.Connected() && b.Connected() })
	if a.Pair() == b.Pair() {
		t.Fatalf("both on %d", a.Pair())
	}
	third := Open(sock, "c", -1) // card has two pairs; none left
	defer third.Close()
	time.Sleep(100 * time.Millisecond)
	if third.Connected() {
		t.Fatal("third client got a pair on a 4-channel card")
	}
	pb := b.Pair()
	b.Close()
	waitFor(t, "b gone", func() bool { return len(s.Clients()) == 1 })
	b2 := Open(sock, "b", -1)
	defer b2.Close()
	defer a.Close()
	waitFor(t, "b back", func() bool { return b2.Connected() })
	if b2.Pair() != pb {
		t.Fatalf("saved pair %d, got %d", pb, b2.Pair())
	}
}

func TestUnderrunCountsOnlyAfterSound(t *testing.T) {
	f := Format{Rate: 48000, Period: 8, Channels: 2}
	s, sock := testServer(t, f)
	a := Open(sock, "a", 0)
	defer a.Close()
	waitFor(t, "connect", func() bool { return a.Connected() })
	out := make([]float32, 16)
	s.Mix(out)
	if u := s.Clients()[0].Underruns; u != 0 {
		t.Fatalf("underrun before first sound: %d", u)
	}
	a.Write(block(8, 1, 1))
	s.Mix(out)
	s.Mix(out)
	if u := s.Clients()[0].Underruns; u != 1 {
		t.Fatalf("underruns %d", u)
	}
}

func TestFormatChangeReconnects(t *testing.T) {
	s, sock := testServer(t, Format{Rate: 44100, Period: 8, Channels: 4})
	a := Open(sock, "a", -1)
	defer a.Close()
	waitFor(t, "connect", func() bool { return a.Connected() })
	g := a.Generation()
	s.SetFormat(Format{Rate: 48000, Period: 16, Channels: 4})
	waitFor(t, "reconnect", func() bool { return a.Generation() != g && a.Connected() })
	if a.Format().Rate != 48000 {
		t.Fatalf("format %+v", a.Format())
	}
}

func TestWriteWithoutServerKeepsPace(t *testing.T) {
	a := Open("/tmp/no-such-abus.sock", "a", -1)
	defer a.Close()
	start := time.Now()
	if err := a.Write(block(441, 0, 0)); err != ErrNotConnected {
		t.Fatalf("err %v", err)
	}
	if d := time.Since(start); d < 8*time.Millisecond {
		t.Fatalf("returned after %v; must sleep one block (10 ms)", d)
	}
}

func TestMixAllocatesNothing(t *testing.T) {
	f := Format{Rate: 48000, Period: 64, Channels: 8}
	s, sock := testServer(t, f)
	a := Open(sock, "a", 0)
	defer a.Close()
	waitFor(t, "connect", func() bool { return a.Connected() })
	out := make([]float32, f.Period*f.Channels)
	a.Write(block(64, 0.1, 0.1))
	if n := testing.AllocsPerRun(100, func() { s.Mix(out) }); n != 0 {
		t.Fatalf("%v allocs per Mix", n)
	}
}

// ---- inputs: the other direction ----

func TestInputDeliversAudioToTheHack(t *testing.T) {
	f := Format{Rate: 44100, Period: 16, Channels: 4}
	s, sock := testServer(t, f)
	c := OpenWithInputs(sock, "rec", -1, []string{"line", "aux"})
	defer c.Close()
	waitFor(t, "connect", func() bool { return c.Connected() })
	if got := c.Inputs(); len(got) != 2 || got[0] != "line" || got[1] != "aux" {
		t.Fatalf("inputs = %v", got)
	}
	ins := s.Inputs()
	if len(ins) != 2 || ins[0].Client != "rec" || ins[0].Name != "line" || ins[1].Name != "aux" {
		t.Fatalf("server inputs = %+v", ins)
	}
	if !ins[0].Write(block(16, 0.5, -0.5)) || !ins[1].Write(block(16, 0.1, 0.2)) {
		t.Fatal("write refused")
	}
	if p := ins[0].Peak(); p != 0.5 {
		t.Errorf("peak = %v, want 0.5", p)
	}
	buf := make([]float32, 16*2)
	n, err := c.Read(0, buf)
	if err != nil || n != 16 || buf[0] != 0.5 || buf[1] != -0.5 || buf[30] != 0.5 {
		t.Fatalf("line: n=%d err=%v buf=%v", n, err, buf[:4])
	}
	n, err = c.Read(1, buf)
	if err != nil || n != 16 || buf[0] != 0.1 || buf[1] != 0.2 {
		t.Fatalf("aux: n=%d err=%v buf=%v", n, err, buf[:4])
	}
}

func TestInputReadGivesSilenceWhenEmptyAndNeverBlocks(t *testing.T) {
	_, sock := testServer(t, Format{Rate: 44100, Period: 16, Channels: 4})
	c := OpenWithInputs(sock, "rec", -1, []string{"line"})
	defer c.Close()
	waitFor(t, "connect", func() bool { return c.Connected() })
	buf := block(16, 9, 9) // not silence before the call
	done := make(chan struct{})
	go func() {
		n, err := c.Read(0, buf)
		if n != 0 || err != nil {
			t.Errorf("n=%d err=%v, want 0 frames and no error", n, err)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Read blocked")
	}
	for i, v := range buf {
		if v != 0 {
			t.Fatalf("buf[%d] = %v, want silence", i, v)
		}
	}
}

func TestInputPartialBlockIsPaddedWithSilence(t *testing.T) {
	s, sock := testServer(t, Format{Rate: 44100, Period: 16, Channels: 4})
	c := OpenWithInputs(sock, "rec", -1, []string{"line"})
	defer c.Close()
	waitFor(t, "connect", func() bool { return c.Connected() })
	s.Inputs()[0].Write(block(5, 1, 1))
	buf := block(16, 9, 9)
	n, err := c.Read(0, buf)
	if err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if buf[9] != 1 || buf[10] != 0 || buf[31] != 0 {
		t.Fatalf("buf = %v", buf)
	}
}

func TestInputRingFullRefusesAndCounts(t *testing.T) {
	s, sock := testServer(t, Format{Rate: 44100, Period: 16, Channels: 4})
	c := OpenWithInputs(sock, "rec", -1, []string{"line"})
	defer c.Close()
	waitFor(t, "connect", func() bool { return c.Connected() })
	in := s.Inputs()[0]
	ok := 0
	for in.Write(block(16, 1, 1)) {
		if ok++; ok > 100 {
			t.Fatal("ring never filled")
		}
	}
	if ok != inputPeriods || in.Refused() != 1 || in.Fill() != 16*inputPeriods {
		t.Errorf("accepted %d blocks, refused %d, fill %d", ok, in.Refused(), in.Fill())
	}
}

func TestClientWithoutInputsStillWorks(t *testing.T) {
	s, sock := testServer(t, Format{Rate: 44100, Period: 16, Channels: 4})
	c := Open(sock, "old", -1)
	defer c.Close()
	waitFor(t, "connect", func() bool { return c.Connected() })
	if len(c.Inputs()) != 0 || len(s.Inputs()) != 0 {
		t.Errorf("inputs = %v / %v", c.Inputs(), s.Inputs())
	}
	buf := block(16, 7, 7)
	if n, err := c.Read(0, buf); n != 0 || err != ErrNoInput {
		t.Errorf("Read = %d, %v, want 0, ErrNoInput", n, err)
	}
	if buf[0] != 0 {
		t.Error("buf must be silence after an error")
	}
	if err := c.Write(block(16, 0.25, 0.25)); err != nil {
		t.Fatal(err)
	}
	out := make([]float32, 16*4)
	s.Mix(out)
	if out[0] != 0.25 {
		t.Errorf("Mix = %v", out[:4])
	}
}

func TestBadInputNamesAreRefused(t *testing.T) {
	_, sock := testServer(t, Format{Rate: 44100, Period: 16, Channels: 4})
	for name, in := range map[string][]string{
		"empty":     {""},
		"duplicate": {"a", "a"},
		"too many":  {"1", "2", "3", "4", "5", "6", "7", "8", "9"},
	} {
		c := OpenWithInputs(sock, "bad-"+name, -1, in)
		time.Sleep(150 * time.Millisecond)
		if c.Connected() {
			t.Errorf("%s: connected with %v", name, in)
		}
		c.Close()
	}
}

func TestClientsListTheirInputs(t *testing.T) {
	s, sock := testServer(t, Format{Rate: 44100, Period: 16, Channels: 4})
	c := OpenWithInputs(sock, "rec", -1, []string{"line"})
	defer c.Close()
	waitFor(t, "connect", func() bool { return c.Connected() })
	cl := s.Clients()
	if len(cl) != 1 || len(cl[0].Inputs) != 1 || cl[0].Inputs[0] != "line" {
		t.Errorf("clients = %+v", cl)
	}
}

func TestInputsGoWhenTheClientLeaves(t *testing.T) {
	s, sock := testServer(t, Format{Rate: 44100, Period: 16, Channels: 4})
	c := OpenWithInputs(sock, "rec", -1, []string{"line"})
	waitFor(t, "connect", func() bool { return c.Connected() })
	if len(s.Inputs()) != 1 {
		t.Fatal("no input")
	}
	c.Close()
	waitFor(t, "input removed", func() bool { return len(s.Inputs()) == 0 })
}

func TestInputsComeBackAfterAReformat(t *testing.T) {
	f := Format{Rate: 44100, Period: 16, Channels: 4}
	s, sock := testServer(t, f)
	c := OpenWithInputs(sock, "rec", -1, []string{"line"})
	defer c.Close()
	waitFor(t, "connect", func() bool { return c.Connected() })
	gen := c.Generation()
	s.SetFormat(Format{Rate: 48000, Period: 32, Channels: 4}) // drops every client
	waitFor(t, "reconnect", func() bool { return c.Generation() != gen && c.Connected() })
	if got := c.Inputs(); len(got) != 1 || got[0] != "line" {
		t.Fatalf("inputs = %v", got)
	}
	ins := s.Inputs()
	if len(ins) != 1 || ins[0].Free() != 32*inputPeriods {
		t.Fatalf("server inputs after reformat: %+v", ins)
	}
}
