// ldexample is the smallest hack with Live devices, and the provider the
// livedevice tests run against. It shows three devices in Live:
//
//	Example Sine    an instrument: one sine voice per note
//	Example Gain    an effect: its input times Gain
//	Example Octave  a MIDI device: every note again, Shift semitones away;
//	                another track takes them with "MIDI From"
//
// Run it as root on the Push (it installs the plugin into /data/.vst3):
//
//	ldexample -so livedevice.so [-presets "/data/Music/Ableton/User Library/Presets"]
//
// or in a test, with -vst3 and -socket somewhere temporary.
package main

import (
	"flag"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/federico-pepe/ableton-push-hack/core/livedevice"
)

const ns = "push-hack-ldexample"

func classes() []*livedevice.Class {
	sine := &livedevice.Class{Name: "Example Sine", Kind: livedevice.Instrument, Ref: "sine",
		Params: []livedevice.Param{
			{Name: "Level", Type: "float", Max: 1, Unit: "%", Default: 0.5},
			{Name: "Octave", Type: "int", Min: -2, Max: 2, Default: 0.5},
			{Name: "Release", Type: "float", Max: 1, Unit: "%", Default: 0.2},
		},
		Banks: [][]int{{0, 1}, {2}}, // two banks: Push shows them as Bank 1 and Bank 2
	}
	gain := &livedevice.Class{Name: "Example Gain", Kind: livedevice.Effect, Ref: "gain",
		// ">" on purpose: Go's JSON writes it as \u003e, and the plugin
		// must read that back as ">" (build.sh checks the title Live gets)
		Params: []livedevice.Param{{Name: "In>Out", Type: "float", Max: 1, Unit: "%", Default: 1}},
	}
	octave := &livedevice.Class{Name: "Example Octave", Kind: livedevice.MIDI, Ref: "octave",
		Params: []livedevice.Param{{Name: "Shift", Type: "enum", Options: []string{"-12", "+7", "+12"}, Max: 2, Default: 1}},
	}
	out := []*livedevice.Class{sine, gain, octave}
	for _, c := range out {
		c.UID = livedevice.ClassUID(ns, c.Kind, c.Ref)
		livedevice.PadBanks(c)
	}
	return out
}

type provider struct{}

func (provider) New(c *livedevice.Class) (livedevice.Device, error) {
	switch c.Ref {
	case "sine":
		return &sine{level: 0.5, release: 0.2, note: -1}, nil
	case "gain":
		return &gain{gain: 1}, nil
	default:
		return &octave{shift: 7}, nil
	}
}

// ---- Example Sine -------------------------------------------------------------

type sine struct {
	level, release float64
	octave         int
	note           int // sounding note, -1 none
	phase, amp     float64
	gate           bool
}

func (s *sine) Process(b *livedevice.Block) {
	b.Params(func(i int, v float64) {
		switch i {
		case 0:
			s.level = v
		case 1:
			s.octave = int(math.Round(v*4)) - 2
		case 2:
			s.release = v
		}
	})
	b.Events(func(_ int, m []byte) {
		if len(m) < 3 {
			return
		}
		switch {
		case m[0]&0xF0 == 0x90 && m[2] > 0:
			s.note, s.gate = int(m[1]), true
		case m[0]&0xF0 == 0x80 || m[0]&0xF0 == 0x90:
			if int(m[1]) == s.note {
				s.gate = false
			}
		}
	})
	if s.note < 0 && !s.gate {
		return
	}
	freq := 440 * math.Pow(2, float64(s.note+12*s.octave-69)/12)
	decay := math.Exp(-1 / (44100 * (0.01 + s.release)))
	for i := 0; i < b.Frames; i++ {
		if s.gate {
			s.amp += (1 - s.amp) * 0.01
		} else {
			s.amp *= decay
		}
		v := float32(math.Sin(s.phase) * s.amp * s.level * 0.5)
		b.Out[0][i], b.Out[1][i] = v, v
		s.phase += 2 * math.Pi * freq / 44100
	}
	s.phase = math.Mod(s.phase, 2*math.Pi)
}

func (s *sine) State() (string, error) {
	return strconv.FormatFloat(s.level, 'f', -1, 64), nil
}
func (s *sine) SetState(st string) error {
	v, err := strconv.ParseFloat(st, 64)
	if err == nil {
		s.level = v
	}
	return err
}
func (s *sine) Close() {}

// ---- Example Gain -------------------------------------------------------------

type gain struct{ gain float64 }

func (g *gain) Process(b *livedevice.Block) {
	b.Params(func(_ int, v float64) { g.gain = v })
	for ch := range 2 {
		for i := 0; i < b.Frames; i++ {
			b.Out[ch][i] = b.In[ch][i] * float32(g.gain)
		}
	}
}
func (g *gain) State() (string, error) { return strconv.FormatFloat(g.gain, 'f', -1, 64), nil }
func (g *gain) SetState(st string) error {
	v, err := strconv.ParseFloat(st, 64)
	g.gain = v
	return err
}
func (g *gain) Close() {}

// ---- Example Octave -----------------------------------------------------------

type octave struct{ shift int }

func (o *octave) Process(b *livedevice.Block) {
	b.Params(func(_ int, v float64) { o.shift = []int{-12, 7, 12}[int(math.Round(v*2))] })
	b.Events(func(off int, m []byte) {
		if len(m) == 3 && (m[0]&0xF0 == 0x90 || m[0]&0xF0 == 0x80) {
			b.Emit(off, m)
			if n := int(m[1]) + o.shift; n >= 0 && n < 128 {
				b.Emit(off, []byte{m[0], byte(n), m[2]})
			}
		}
	})
}
func (o *octave) State() (string, error)   { return strconv.Itoa(o.shift), nil }
func (o *octave) SetState(st string) error { v, err := strconv.Atoi(st); o.shift = v; return err }
func (o *octave) Close()                   {}

// ---- main ---------------------------------------------------------------------

func main() {
	so := flag.String("so", "", "the livedevice plugin binary to install (livedevice.so)")
	vst3 := flag.String("vst3", livedevice.VST3Dir, "where to install the plugin")
	socket := flag.String("socket", "/data/push-hack/data/ldexample/devices.sock", "control socket")
	classFile := flag.String("classes", "", "class file (default: in the plugin bundle)")
	presets := flag.String("presets", "", "write browser entries here (Live's User Library Presets folder)")
	httpAddr := flag.String("http", "", "serve GET /api/devices here (host:port)")
	flag.Parse()

	cls := classes()
	soPath, path := "", *classFile
	if *so != "" {
		cfg := livedevice.BundleConfig{Vendor: "Push Hack Example", Socket: *socket, Classes: *classFile}
		if *presets != "" {
			cfg.Presets = livedevice.PresetFolders(*presets, "Push Hack Example", cls)
		}
		var err error
		soPath, cfg, _, err = livedevice.InstallBundle(*vst3, "PushHackExample", *so, cfg)
		if err != nil {
			log.Fatal(err)
		}
		path = cfg.Classes
	}
	if path == "" {
		log.Fatal("-classes or -so is needed")
	}
	if _, err := livedevice.WriteClasses(path, soPath, cls); err != nil {
		log.Fatal(err)
	}
	if *presets != "" {
		if _, err := livedevice.WritePresets(*presets, "Push Hack Example", cls); err != nil {
			log.Fatal(err)
		}
	}
	srv := livedevice.NewServer(livedevice.Config{Name: "ldexample", Socket: *socket}, provider{}, cls)
	if err := srv.Listen(); err != nil {
		log.Fatal(err)
	}
	if *httpAddr != "" {
		go http.ListenAndServe(*httpAddr, srv.Handler())
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	srv.Close()
}
