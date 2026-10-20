package main

import (
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

// fakeIO is the device as the state machine sees it.
type fakeIO struct {
	intercept, display, home bool
	pass                     []int
	tool                     string
	started                  []string
	t                        time.Time
	view                     homeView
	helpTaps, heldTaps       int
	covers                   int
}

func (f *fakeIO) take(pass []int)    { f.intercept, f.display, f.pass = true, true, pass }
func (f *fakeIO) setPass(pass []int) { f.intercept, f.pass = true, pass }
func (f *fakeIO) screenOnly()        { f.intercept, f.pass = false, nil }
func (f *fakeIO) release() {
	f.intercept, f.display, f.home, f.tool, f.pass = false, false, false, "", nil
}
func (f *fakeIO) closeLiveHelp(held bool) {
	if held {
		f.heldTaps++
	} else {
		f.helpTaps++
	}
}
func (f *fakeIO) homeCover(v homeView) { f.display, f.covers = true, f.covers+1 }
func (f *fakeIO) homeShow(v homeView)  { f.home, f.display, f.view = true, true, v }
func (f *fakeIO) homeHide()            { f.home = false }
func (f *fakeIO) openTool(id string)   { f.intercept, f.display, f.tool = true, true, id }
func (f *fakeIO) startApp(id string)   { f.started = append(f.started, id) }
func (f *fakeIO) releaseHeld() {
	f.closeTool()
	if f.intercept || f.display {
		f.release()
	}
}
func (f *fakeIO) now() time.Time { return f.t }
func (f *fakeIO) closeTool() {
	if f.tool != "" {
		f.tool, f.display = "", false // the Shadow UI gives the screen back when it stops
	}
}

// testBench is a Surface over a fake device with two apps and two tools.
type testBench struct {
	*Surface
	io      *fakeIO
	entries []homeEntry
}

func newBench() *testBench {
	b := &testBench{io: &fakeIO{t: time.Unix(1000, 0)}}
	b.entries = []homeEntry{
		{ID: "riffs", Label: "RIFFS", App: true, Color: "sky"},
		{ID: "stacks", Label: "STACKS", App: true, Color: "amber"},
		{ID: "browser", Label: "BROWSER", Color: "lime"},
		{ID: "files", Label: "FILES", Color: "steel"},
	}
	list := func() []homeEntry {
		out := append([]homeEntry(nil), b.entries...)
		slices.SortStableFunc(out, func(x, y homeEntry) int { return rankOf(x) - rankOf(y) })
		return out
	}
	star := func(id string, star, super bool) {
		for i := range b.entries {
			if super {
				b.entries[i].Super = false
			}
			if b.entries[i].ID == id {
				b.entries[i].Star, b.entries[i].Super = star || super, super
			}
		}
	}
	b.Surface = newSurface(b.io, list, star)
	return b
}

func rankOf(e homeEntry) int {
	switch {
	case e.Super:
		return 0
	case e.Star:
		return 1
	}
	return 2
}

func (b *testBench) events(app string) []string {
	var out []string
	ss := b.sessions[app]
	if ss == nil {
		return nil
	}
	for {
		select {
		case ev := <-ss.events:
			out = append(out, ev.Name)
		default:
			return out
		}
	}
}

// rules checks the six rules of plans/2026-10-17-push-hack-switcher.md
// that the state alone can show.
func (b *testBench) rules(t *testing.T, where string) {
	t.Helper()
	s, f := b.Surface, b.io
	switch s.own {
	case ownHome:
		if !f.home || !f.intercept || !f.display || f.tool != "" {
			t.Fatalf("%s: Home owns the surface but the device has home=%v intercept=%v display=%v tool=%q", where, f.home, f.intercept, f.display, f.tool)
		}
		if s.app != "" {
			t.Fatalf("%s: Home owns the surface and app is %q", where, s.app)
		}
	case ownApp:
		if f.home || !f.display || f.intercept != s.input || f.tool != "" || s.sessions[s.app] == nil {
			t.Fatalf("%s: %s owns the surface but home=%v display=%v intercept=%v (input %v) tool=%q session=%v",
				where, s.app, f.home, f.display, f.intercept, s.input, f.tool, s.sessions[s.app] != nil)
		}
	case ownOutside:
		if f.home {
			t.Fatalf("%s: outside, and Home is still drawn", where)
		}
	}
	for _, app := range []string{"", "riffs", "stacks"} {
		want := (app == "" && s.own == ownOutside) || (app != "" && s.own == ownApp && s.app == app)
		if got := s.FrameAllowed(app); got != want {
			t.Fatalf("%s: a frame from %q allowed=%v, want %v (owner %s %s)", where, app, got, want, s.own, s.app)
		}
	}
}

func TestSurfaceTransitions(t *testing.T) {
	b := newBench()
	riffs := b.SessionOpen("riffs", []int{85, 86})
	b.SessionOpen("stacks", nil)
	b.rules(t, "start")

	b.Gesture(1)
	if b.own != ownHome || !b.io.home {
		t.Fatalf("Shift + Help from Live: owner %s, Home drawn %v", b.own, b.io.home)
	}
	b.rules(t, "home")

	b.HomeCC(CCScreenTop1, 127) // RIFFS, column 1
	if b.own != ownApp || b.app != "riffs" || !slices.Equal(b.io.pass, []int{85, 86}) {
		t.Fatalf("top button 1: owner %s %s, pass %v", b.own, b.app, b.io.pass)
	}
	if ev := b.events("riffs"); !slices.Equal(ev, []string{"focus"}) {
		t.Fatalf("riffs got %v, want focus", ev)
	}
	b.rules(t, "riffs open")

	b.Gesture(1)
	if b.own != ownHome || b.last != "riffs" {
		t.Fatalf("Shift + Help from riffs: owner %s, last %q", b.own, b.last)
	}
	if ev := b.events("riffs"); !slices.Equal(ev, []string{"blur"}) {
		t.Fatalf("riffs got %v, want blur", ev)
	}
	if b.io.view.Entries[b.io.view.Cursor].ID != "riffs" {
		t.Error("Home should open on the app you came from")
	}
	b.rules(t, "home again")

	b.HomePad(36+8, 100) // a dark pad (row 2, column 1): nothing
	if b.own != ownHome {
		t.Fatalf("a dark pad opened something: owner %s", b.own)
	}
	b.HomePad(36+7*8+1, 100) // column 2's top pad: STACKS
	if b.own != ownApp || b.app != "stacks" {
		t.Fatalf("pad column 2: owner %s %s", b.own, b.app)
	}
	b.rules(t, "stacks open")

	// riffs crashes while stacks owns: nothing changes for stacks
	b.SessionClosed(riffs)
	if b.own != ownApp || b.app != "stacks" {
		t.Fatal("another app's session ending moved the surface")
	}
	// stacks crashes while it owns: Home, with a notice
	b.SessionClosed(b.sessions["stacks"])
	if b.own != ownHome || !strings.Contains(b.notice, "STACKS STOPPED") {
		t.Fatalf("stacks stopped while open: owner %s, notice %q", b.own, b.notice)
	}
	b.rules(t, "stacks crashed")

	// pressing a stopped app starts it, and it opens when it connects
	b.HomeCC(CCScreenTop1+1, 127)
	if !slices.Contains(b.io.started, "stacks") || b.starting != "stacks" || b.own != ownHome {
		t.Fatalf("press to start: started %v, starting %q, owner %s", b.io.started, b.starting, b.own)
	}
	b.SessionOpen("stacks", nil)
	if b.own != ownApp || b.app != "stacks" {
		t.Fatalf("a started app should open when it connects: owner %s %s", b.own, b.app)
	}
	b.rules(t, "stacks started")

	// LIVE from Home
	b.Gesture(1)
	b.HomeCC(CCScreenBotN(7), 127)
	if b.own != ownOutside || b.io.intercept || b.io.display {
		t.Fatalf("LIVE: owner %s, intercept %v, display %v", b.own, b.io.intercept, b.io.display)
	}
	b.rules(t, "live")
}

func TestSurfaceSuperstar(t *testing.T) {
	b := newBench()
	b.SessionOpen("riffs", nil)
	b.Gesture(2)
	if b.own != ownHome || !strings.Contains(b.notice, "NO SUPERSTAR") {
		t.Fatalf("no superstar yet: owner %s, notice %q", b.own, b.notice)
	}
	// Shift + STAR on RIFFS makes it the superstar, and it moves first
	b.cursor = b.find("riffs")
	b.SetShift(true)
	b.HomeCC(CCScreenBotN(1), 127)
	b.SetShift(false)
	if b.superstar() != "riffs" || b.find("riffs") != 0 {
		t.Fatalf("superstar %q, riffs at %d", b.superstar(), b.find("riffs"))
	}
	b.HomeCC(CCScreenBotN(7), 127) // LIVE
	b.Gesture(2)
	if b.own != ownApp || b.app != "riffs" {
		t.Fatalf("Shift + Help, Help from Live: owner %s %s", b.own, b.app)
	}
	b.Gesture(2)
	if b.own != ownOutside {
		t.Fatalf("Shift + Help, Help from the superstar: owner %s, want Live", b.own)
	}
	b.rules(t, "superstar back to live")
	// a tool as the superstar opens the Shadow UI on it
	b.setStar("files", true, true)
	b.Gesture(2)
	if b.own != ownOutside || b.io.tool != "files" || !b.io.intercept {
		t.Fatalf("a tool superstar: owner %s, tool %q, intercept %v", b.own, b.io.tool, b.io.intercept)
	}
	b.Gesture(1)
	if b.own != ownHome || b.io.tool != "" {
		t.Fatalf("Shift + Help from a tool: owner %s, tool %q", b.own, b.io.tool)
	}
	b.rules(t, "home from a tool")
}

func TestSurfaceStars(t *testing.T) {
	b := newBench()
	b.Gesture(1)
	b.cursor = b.find("files")
	b.HomeCC(CCScreenBotN(1), 127) // STAR
	if b.find("files") != 0 || !b.entries[3].Star {
		t.Fatalf("a starred entry goes first: files at %d", b.find("files"))
	}
	if b.io.view.Entries[b.io.view.Cursor].ID != "files" {
		t.Error("the cursor should follow the entry it starred")
	}
	b.HomeCC(CCScreenBotN(1), 127)
	if b.entries[3].Star {
		t.Error("STAR again should unstar")
	}
}

func TestSurfaceStartTimeout(t *testing.T) {
	b := newBench()
	b.Gesture(1)
	b.HomeCC(CCScreenTop1, 127) // riffs, stopped
	b.io.t = b.io.t.Add(16 * time.Second)
	b.Tick()
	if b.starting != "" || !strings.Contains(b.notice, "DID NOT START") {
		t.Fatalf("after 16 s: starting %q, notice %q", b.starting, b.notice)
	}
}

func TestSurfaceLegacyAndOpen(t *testing.T) {
	b := newBench()
	b.SessionOpen("riffs", nil)
	b.SessionOpen("stacks", nil)
	if ok, _ := b.LegacyAllowed(""); !ok {
		t.Error("outside, an old hack may take the surface")
	}
	b.Gesture(1)
	if ok, why := b.LegacyAllowed(""); ok || !strings.Contains(why, "Home") {
		t.Errorf("in Home, an old hack must not: %v %q", ok, why)
	}
	if err := b.Open("riffs"); err != nil || b.app != "riffs" {
		t.Fatalf("open riffs from the web UI: %v", err)
	}
	if ok, _ := b.LegacyAllowed("riffs"); !ok {
		t.Error("the owning app may use the display and intercept calls")
	}
	if ok, _ := b.LegacyAllowed("stacks"); ok {
		t.Error("another app may not")
	}
	if err := b.Open("stacks"); err == nil {
		t.Error("one app must not take the surface from another through Open")
	}
	if err := b.Input("riffs", false); err != nil || b.io.intercept {
		t.Fatalf("screen only: %v, intercept %v", err, b.io.intercept)
	}
	b.rules(t, "screen only")
	if err := b.Leave("riffs", "live"); err != nil || b.own != ownOutside {
		t.Fatalf("leave to live: %v, owner %s", err, b.own)
	}
	if err := b.Leave("riffs", "home"); err == nil {
		t.Error("an app that does not own the surface cannot leave it")
	}
}

func TestSurfacePowerPress(t *testing.T) {
	b := newBench()
	b.SessionOpen("riffs", nil)
	b.Gesture(1)
	b.HomeCC(CCScreenTop1, 127)
	b.PowerPress()
	if b.own != ownOutside || b.io.intercept || b.io.display {
		t.Fatalf("power press: owner %s, intercept %v, display %v", b.own, b.io.intercept, b.io.display)
	}
	b.PowerPress() // a second message does nothing
	b.Gesture(1)   // nothing latches: Home opens again
	if b.own != ownHome {
		t.Fatalf("after a power press Home must open again, owner %s", b.own)
	}
	b.PowerPress()
	if b.own != ownOutside || b.io.home {
		t.Fatalf("power press from Home: owner %s, home drawn %v", b.own, b.io.home)
	}
}

// A press in Live changes nothing; one with the Shadow UI or an old hack
// holding the surface gives it back.
func TestSurfacePowerPressOutside(t *testing.T) {
	b := newBench()
	b.PowerPress()
	if b.io.intercept || b.io.display || b.io.tool != "" {
		t.Fatal("a press in Live must change nothing")
	}
	b.io.intercept, b.io.display = true, true // an old hack took the surface itself
	b.PowerPress()
	if b.io.intercept || b.io.display {
		t.Errorf("an old hack kept the surface: intercept %v, display %v", b.io.intercept, b.io.display)
	}
	b.io.openTool("browser") // the Shadow UI, opened by Shift + Preferences
	b.PowerPress()
	if b.io.intercept || b.io.display || b.io.tool != "" {
		t.Errorf("the Shadow UI kept the surface: %+v", b.io)
	}
}

func TestIsPowerPress(t *testing.T) {
	press := []byte{0xF0, 0x00, 0x21, 0x1D, 0x01, 0x01, 0x3A, 0x2A, 0x64, 0xF7}
	rest := []byte{0xF0, 0x00, 0x21, 0x1D, 0x01, 0x01, 0x3A, 0x22, 0x64, 0xF7}
	if !isPowerPress(press) || isPowerPress(rest) || isPowerPress(press[:9]) || isPowerPress(nil) {
		t.Error("only F0 00 21 1D 01 01 3A 2A 64 F7 is a power press")
	}
}

// 10,000 random sequences of everything that can happen, the rules
// checked after every event.
func TestSurfaceRandom(t *testing.T) {
	apps := []string{"riffs", "stacks"}
	seen := map[owner]int{}
	inputOff, stoppedNotice := 0, 0
	for run := 0; run < 10000; run++ {
		rng := rand.New(rand.NewPCG(uint64(run), 7))
		b := newBench()
		var old []*session
		for step := 0; step < 60; step++ {
			app := apps[rng.IntN(2)]
			var what string
			switch k := rng.IntN(100); {
			case k < 14:
				what = "gesture 1"
				b.Gesture(1)
			case k < 17:
				what = "gesture 2"
				b.Gesture(2)
			case k < 19:
				what = "help alone"
				was := b.own
				b.HelpAlone()
				if (was == ownHome || was == ownApp) && b.own != ownOutside {
					t.Fatalf("run %d step %d: Help alone from %s left the owner %s", run, step, was, b.own)
				}
			case k < 20:
				what = "help held"
				was := b.own
				b.LongHelp(rng.IntN(2) == 0)
				if b.own != ownHome {
					t.Fatalf("run %d step %d: Help held from %s left the owner %s", run, step, was, b.own)
				}
			case k < 32:
				what = "session open " + app
				old = append(old, b.SessionOpen(app, []int{rng.IntN(88)}))
			case k < 40:
				if len(old) == 0 {
					continue
				}
				ss := old[rng.IntN(len(old))]
				what = "session closed " + ss.app
				owned := b.own == ownApp && b.app == ss.app && b.sessions[ss.app] == ss
				b.SessionClosed(ss)
				if owned && b.own != ownHome {
					t.Fatalf("run %d step %d: the owning app's session ended and the owner is %s", run, step, b.own)
				}
			case k < 55:
				what = "top button"
				b.HomeCC(CCScreenTop1+uint8(rng.IntN(8)), 127)
			case k < 62:
				what = "pad"
				b.HomePad(uint8(36+rng.IntN(64)), 100)
			case k < 68:
				what = "bottom button"
				b.HomeCC(CCScreenBotN(rng.IntN(8)), 127)
			case k < 72:
				what = "shift"
				b.SetShift(rng.IntN(2) == 0)
			case k < 76:
				what = "jog"
				b.HomeCC(CCJogWheel, []uint8{1, 127}[rng.IntN(2)])
			case k < 80:
				what = "input " + app
				b.Input(app, rng.IntN(2) == 0) //nolint:errcheck
			case k < 84:
				what = "leave " + app
				b.Leave(app, []string{"live", "home"}[rng.IntN(2)]) //nolint:errcheck
			case k < 90:
				what = "open"
				b.Open([]string{"riffs", "stacks", "browser", "files"}[rng.IntN(4)]) //nolint:errcheck
			case k < 92:
				what = "status " + app
				b.Status(app, [2]string{"A", "B"}) //nolint:errcheck
			case k < 97:
				what = "tick"
				b.io.t = b.io.t.Add(time.Duration(rng.IntN(20)) * time.Second)
				b.Tick()
			case k < 98:
				what = "power press"
				b.PowerPress()
			default:
				what = "star"
				b.setStar([]string{"riffs", "stacks", "browser", "files"}[rng.IntN(4)], rng.IntN(2) == 0, rng.IntN(3) == 0)
			}
			b.rules(t, "run "+itoa(run)+" step "+itoa(step)+" ("+what+")")
			seen[b.own]++
			if b.own == ownApp && !b.input {
				inputOff++
			}
			if strings.Contains(b.notice, "STOPPED") {
				stoppedNotice++
			}
		}
	}
	// The sequences must reach every state often, or passing proves nothing.
	t.Logf("states visited: outside %d, home %d, app %d (screen only %d); stopped notices %d",
		seen[ownOutside], seen[ownHome], seen[ownApp], inputOff, stoppedNotice)
	for _, o := range []owner{ownOutside, ownHome, ownApp} {
		if seen[o] < 20000 {
			t.Errorf("only %d events with %s owning the surface", seen[o], o)
		}
	}
	if inputOff < 1000 || stoppedNotice < 1000 {
		t.Errorf("screen-only %d, stopped notices %d: too rare to trust", inputOff, stoppedNotice)
	}
}

func TestHelpGesture(t *testing.T) {
	cases := []struct {
		name string
		seq  [][2]uint8
		want []int
	}{
		{"Shift + Help", [][2]uint8{{CCShift, 127}, {CCHelp, 127}, {CCHelp, 0}, {CCShift, 0}}, []int{1}},
		{"Shift + Help, Help", [][2]uint8{{CCShift, 127}, {CCHelp, 127}, {CCHelp, 0}, {CCHelp, 127}, {CCHelp, 0}, {CCShift, 0}}, []int{2}},
		{"fires on Shift's release only", [][2]uint8{{CCShift, 127}, {CCHelp, 127}, {CCHelp, 0}}, nil},
		{"Shift alone", [][2]uint8{{CCShift, 127}, {CCShift, 0}}, nil},
		{"Help alone", [][2]uint8{{CCHelp, 127}, {CCHelp, 0}}, []int{helpAlone}},
		{"Help alone fires on Help's release only", [][2]uint8{{CCHelp, 127}}, nil},
		{"Help + another button is not ours", [][2]uint8{{CCHelp, 127}, {CCUndo, 127}, {CCUndo, 0}, {CCHelp, 0}}, nil},
		{"an encoder turned with Help held does not spoil it", [][2]uint8{{CCHelp, 127}, {71, 1}, {CCHelp, 0}}, []int{helpAlone}},
		{"Shift pressed while Help is held", [][2]uint8{{CCHelp, 127}, {CCShift, 127}, {CCShift, 0}, {CCHelp, 0}}, nil},
		{"Shift let go before Help: Shift + Help only", [][2]uint8{{CCShift, 127}, {CCHelp, 127}, {CCShift, 0}, {CCHelp, 0}}, []int{1}},
		{"Shift + Help + another button is not ours", [][2]uint8{{CCShift, 127}, {CCHelp, 127}, {CCUndo, 127}, {CCShift, 0}}, nil},
		{"an encoder turned with Shift does not spoil it", [][2]uint8{{CCShift, 127}, {71, 1}, {CCHelp, 127}, {CCShift, 0}}, []int{1}},
		{"two gestures", [][2]uint8{{CCShift, 127}, {CCHelp, 127}, {CCShift, 0}, {CCShift, 127}, {CCHelp, 127}, {CCShift, 0}}, []int{1, 1}},
	}
	for _, c := range cases {
		var g helpGesture
		var got []int
		for _, m := range c.seq {
			if n := g.cc(m[0], m[1]); n > 0 || n == helpAlone {
				got = append(got, n)
			}
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: fired %v, want %v", c.name, got, c.want)
		}
	}
}

// Help held: the timer of its own press fires once, and the release is then
// nothing; a release, Shift or another button first and it never fires.
func TestHelpHold(t *testing.T) {
	var g helpGesture
	if g.cc(CCHelp, 127) != helpDown || !g.hold(g.gen) || g.hold(g.gen) || g.cc(CCHelp, 0) != 0 {
		t.Error("held: want one hold, then a silent release")
	}
	g.cc(CCHelp, 127)
	gen := g.gen
	if g.cc(CCHelp, 0) != helpAlone || g.hold(gen) {
		t.Error("let go first: want Help alone and no hold")
	}
	g.cc(CCHelp, 127)
	gen = g.gen
	g.cc(CCHelp, 0)
	g.cc(CCHelp, 127) // a new press: the old timer is not its
	if g.hold(gen) || !g.hold(g.gen) {
		t.Error("an old press's timer fired on a new press")
	}
	g.cc(CCHelp, 0)
	g.cc(CCHelp, 127)
	g.cc(CCUndo, 127)
	if g.hold(g.gen) {
		t.Error("Help + another button: no hold")
	}
	g.cc(CCUndo, 0)
	g.cc(CCHelp, 0)
	g.cc(CCHelp, 127)
	g.cc(CCShift, 127)
	if g.hold(g.gen) {
		t.Error("Shift pressed while Help is held: no hold")
	}
}

// Help held is Shift + Help: Home from Live and from an app, nothing from
// Home. Where Live had the buttons, its help is closed first.
func TestLongHelp(t *testing.T) {
	b := newBench()
	b.SessionOpen("stacks", nil)
	b.LongHelp(true) // from Live
	if b.own != ownHome || b.io.heldTaps != 1 || b.io.covers < 1 {
		t.Fatalf("held in Live: owner %s, taps %d, Home drawn first %d", b.own, b.io.heldTaps, b.io.covers)
	}
	b.LongHelp(false) // from Home: nothing
	if b.own != ownHome {
		t.Fatalf("held in Home: owner %s", b.own)
	}
	b.Open("stacks") //nolint:errcheck
	b.events("stacks")
	b.LongHelp(false)
	if b.own != ownHome || b.io.heldTaps != 1 {
		t.Fatalf("held in an app: owner %s, taps %d", b.own, b.io.heldTaps)
	}
	if ev := b.events("stacks"); !slices.Equal(ev, []string{"blur"}) {
		t.Fatalf("stacks got %v, want blur", ev)
	}
	b.rules(t, "held in an app")
	b.Open("stacks")         //nolint:errcheck
	b.Input("stacks", false) //nolint:errcheck
	b.LongHelp(true)
	if b.own != ownHome || b.io.heldTaps != 2 {
		t.Fatalf("held, screen only: owner %s, taps %d", b.own, b.io.heldTaps)
	}
	b.rules(t, "held, screen only")
}

// Help alone goes back to Live from Home and from an app, on its release;
// in Live it is Live's own Help and push-manager does not take it.
func TestHelpAloneToLive(t *testing.T) {
	b := newBench()
	saved := surface
	surface = b.Surface
	defer func() { surface = saved }()
	gesture.reset()
	feed := func(cc, val uint8) bool {
		took := surfaceInputCC(cc, val)
		for len(surfaceQ) > 0 {
			(<-surfaceQ)()
		}
		return took
	}
	b.SessionOpen("stacks", nil)
	if feed(CCHelp, 127) || feed(CCHelp, 0) || b.own != ownOutside {
		t.Fatalf("Help in Live: owner %s, or push-manager took it", b.own)
	}
	b.Gesture(1) // Home
	feed(CCHelp, 127)
	if b.own != ownHome {
		t.Fatalf("Help's press left Home: owner %s", b.own)
	}
	if !feed(CCHelp, 0) || b.own != ownOutside {
		t.Fatalf("Help alone from Home: owner %s", b.own)
	}
	b.rules(t, "help from home")
	b.Open("stacks") //nolint:errcheck
	if b.own != ownApp {
		t.Fatalf("open stacks: owner %s", b.own)
	}
	b.events("stacks")
	feed(CCHelp, 127)
	if !feed(CCHelp, 0) || b.own != ownOutside {
		t.Fatalf("Help alone from an app: owner %s", b.own)
	}
	if b.io.helpTaps != 0 {
		t.Fatalf("Live had no buttons, yet push-manager tapped its Help %d times", b.io.helpTaps)
	}
	if ev := b.events("stacks"); !slices.Equal(ev, []string{"blur"}) {
		t.Fatalf("stacks got %v, want blur", ev)
	}
	b.rules(t, "help from stacks")

	// screen only (Live has the buttons): back to Live as well
	b.Open("stacks")         //nolint:errcheck
	b.Input("stacks", false) //nolint:errcheck
	feed(CCHelp, 127)
	feed(CCHelp, 0)
	if b.own != ownOutside {
		t.Fatalf("Help with the screen only: owner %s, want outside", b.own)
	}
	if b.io.helpTaps != 1 {
		t.Fatalf("Live saw Help and opened its help: %d taps to close it, want 1", b.io.helpTaps)
	}
	b.rules(t, "help, screen only")
}

// Home's screen: every page draws, and nothing but ASCII is ever typed.
func TestRenderHome(t *testing.T) {
	var es []homeEntry
	for i := 0; i < 11; i++ {
		es = append(es, homeEntry{ID: "e" + itoa(i), Label: "ENTRY" + itoa(i), App: i%2 == 0, Running: i%4 == 0, Color: "sky", Star: i < 3, Super: i == 0,
			Status: [2]string{"A LONG STATUS LINE THAT IS CUT", "SECOND"}})
	}
	for cur := 0; cur < len(es); cur++ {
		img := renderHome(homeView{Entries: es, Cursor: cur, Last: "e0", Notice: map[bool]string{true: "SOMETHING HAPPENED"}[cur == 3]})
		if img.Bounds().Dx() != 960 || img.Bounds().Dy() != 160 {
			t.Fatal("Home is 960x160")
		}
		pads, ccs := homeLEDs(homeView{Entries: es, Cursor: cur})
		if len(pads) != 64 {
			t.Fatalf("Home sets %d pads, want all 64 (the dark ones too)", len(pads))
		}
		lit := 0
		for n, c := range pads {
			col, row := push3.PadCoord(n)
			if c != 0 {
				lit++
				if row != homePadRow {
					t.Fatalf("pad %d (row %d) is lit: only the top row is", n, row)
				}
			}
			if row == homePadRow && col < 8 && cur/8*8+col < len(es) && c == 0 {
				t.Fatalf("entry %d's top pad is dark", cur/8*8+col)
			}
		}
		if page := min(8, len(es)-cur/8*8); lit != page {
			t.Fatalf("%d pads lit, want %d (one per entry on the page)", lit, page)
		}
		for n, c := range pads {
			if c == 1 { // "dim" is a button brightness; on a pad index 1 is red
				t.Fatalf("pad %d is index 1, red on the hardware", n)
			}
		}
		if ccs[CCPageRight] == 0 && cur < 8 {
			t.Error("page > should be lit on page 1 of 2")
		}
	}
}

// homeEntries: an app is a hack declaring "surface", the favorites come
// first, and there is one superstar, saved in ui_tabs.json.
func TestHomeEntriesAndStars(t *testing.T) {
	dir := t.TempDir()
	writeHack(t, dir, "riffs", `{"id":"riffs","name":"Riffs","port":7712,"surface":{"label":"RIFFS","color":"sky"}}`)
	writeHack(t, dir, "stacks", `{"id":"stacks","name":"Stacks","port":7711,"surface":{"label":"STACKS","color":"amber"},"web_ui":{"label":"Stacks","path":"/"}}`)
	hacksDir = dir
	uiTabsPath = filepath.Join(dir, "ui_tabs.json")
	initdDir = filepath.Join(dir, "init.d")
	rcdGlob = filepath.Join(dir, "rc*.d")
	defer func() {
		hacksDir = "/data/push-hack/hacks"
		initdDir, rcdGlob = "/etc/init.d", "/etc/rc*.d"
		uiPrefs = nil
	}()
	uiPrefs = nil

	es := homeEntries()
	ids := func() (out []string) {
		for _, e := range homeEntries() {
			out = append(out, e.ID)
		}
		return
	}
	var riffs homeEntry
	for _, e := range es {
		if e.ID == "riffs" {
			riffs = e
		}
	}
	if !riffs.App || riffs.Label != "RIFFS" || riffs.Color != "sky" {
		t.Fatalf("riffs as an app: %+v", riffs)
	}
	setStarPref("stacks", true, false)
	setStarPref("riffs", true, true)
	if got := ids(); got[0] != "riffs" || got[1] != "stacks" {
		t.Fatalf("superstar then stars first: %v", got)
	}
	setStarPref("stacks", true, true) // a new superstar moves it
	supers := 0
	for _, e := range homeEntries() {
		if e.Super {
			supers++
		}
	}
	if got := ids(); supers != 1 || got[0] != "stacks" || got[1] != "riffs" {
		t.Fatalf("one superstar: %d, order %v", supers, got)
	}
	loadUITabs() // it was saved
	if got := ids(); got[0] != "stacks" {
		t.Fatalf("stars did not survive a reload: %v", got)
	}
}

// Found on the device: Shift + Help's release went to the gesture, Home
// kept thinking Shift was held, and the next STAR became Shift + STAR (the
// superstar). Shift's state now comes from every Shift press.
func TestShiftAfterGesture(t *testing.T) {
	b := newBench()
	saved := surface
	surface = b.Surface
	defer func() { surface = saved }()
	gesture.reset()
	feed := func(cc, val uint8) {
		surfaceInputCC(cc, val)
		for len(surfaceQ) > 0 {
			(<-surfaceQ)()
		}
	}
	feed(CCShift, 127)
	feed(CCHelp, 127)
	feed(CCHelp, 0)
	feed(CCShift, 0) // Home opens
	feed(CCShift, 127)
	feed(CCHelp, 127)
	feed(CCHelp, 0)
	feed(CCShift, 0) // back to Live, from Home
	feed(CCShift, 127)
	feed(CCHelp, 127)
	feed(CCHelp, 0)
	feed(CCShift, 0) // Home again
	if b.own != ownHome {
		t.Fatalf("owner %s, want home", b.own)
	}
	b.cursor = b.find("stacks")
	feed(CCScreenBotN(1), 127) // STAR, Shift not held
	feed(CCScreenBotN(1), 0)
	if b.superstar() == "stacks" || !b.entries[1].Star {
		t.Fatalf("STAR after Shift + Help: superstar %q, stacks starred %v", b.superstar(), b.entries[1].Star)
	}
}

// reset clears the shared gesture between tests, under its lock: a timer
// from an earlier test may still be running.
func (g *helpGesture) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.shift, g.spoiled, g.taps, g.alone = false, false, 0, false
	g.gen++
}
