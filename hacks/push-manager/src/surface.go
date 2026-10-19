// surface.go -- Push Hack Home: who owns Push's screen, buttons and pads.
//
// Hacks that take the whole surface (Stacks, Riffs) used to open with a
// gesture each and switch the intercept and the display on themselves.
// Gestures collided (Riffs' Shift + Set was also push-manager's own
// browser_open chord: one press, two programs drawing on one screen), and
// a hack that crashed while it owned the surface left it frozen.
//
// Now push-manager owns the surface and lends it out. There is one owner:
//
//   - outside: everything as it was before Home -- Live, push-manager's own
//     Shadow UI (a "tool"), or an old hack that takes the surface through
//     /api/display/mode and /api/midi/filter itself;
//   - home: the switcher, drawn by push-manager (home.go);
//   - app: one hack that holds a session (GET /api/surface/session) and
//     was opened from Home.
//
// Shift + Help opens Home from anywhere and goes back to Live from Home.
// Shift + Help, Help (two taps of Help while Shift is held) opens the
// superstar entry, and from inside it goes back to Live. Both fire when
// Shift is let go (gesture.go). An app's session ending while it owns the
// surface lands the user on Home, which says so. The power button
// (release.go) gives everything to Live.
//
// All side effects go through surfaceIO, so the state machine is tested
// against a fake (surface_test.go) with random event sequences. The design
// is plans/2026-10-17-push-hack-switcher.md.
package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type owner int

const (
	ownOutside owner = iota
	ownHome
	ownApp
)

func (o owner) String() string {
	return [...]string{"outside", "home", "app"}[o]
}

// startTimeout is how long Home waits for an app it started to connect.
const startTimeout = 15 * time.Second

// surfaceIO is everything the state machine does to the device.
type surfaceIO interface {
	take(pass []int)     // intercept on with these pass-through CCs, display taken, LEDs cleared
	setPass(pass []int)  // intercept on, a new pass-through list
	screenOnly()         // intercept off, display still taken (an app's listen mode)
	release()            // intercept off, display back to Push3: Live
	homeShow(v homeView) // draw Home and its LEDs
	homeHide()           // stop drawing Home
	openTool(id string)  // the Shadow UI, on this panel
	closeTool()          // stop the Shadow UI, if it runs
	startApp(id string)  // ask push-catalog to start a stopped app
	now() time.Time
}

// session is one app's connection: open while the app runs.
type session struct {
	app    string
	pass   []int
	status [2]string
	events chan surfaceEvent
	done   chan struct{} // closed when a newer session replaces this one
}

type surfaceEvent struct {
	Name string `json:"-"`
	Data any    `json:"-"`
}

// Surface is the owner state machine. One per push-manager.
type Surface struct {
	mu       sync.Mutex
	io       surfaceIO
	entries  func() []homeEntry // the resolved, ordered entries (ui_tabs.go)
	setStar  func(id string, star, super bool)
	own      owner
	app      string // the app that owns the surface (ownApp)
	input    bool   // ownApp: the app has the buttons too, not only the screen
	last     string // the app the user came from
	sessions map[string]*session
	notice   string // Home's message line: what just happened
	starting string // an app Home started and waits for
	startAt  time.Time
	cursor   int // Home's chosen entry, an index into entries()
	shift    bool
	down     bool // the power button was pressed: Live until off
}

func newSurface(io surfaceIO, entries func() []homeEntry, setStar func(string, bool, bool)) *Surface {
	return &Surface{io: io, entries: entries, setStar: setStar, sessions: map[string]*session{}}
}

// ---- what other parts of push-manager ask ---------------------------------

// Owner is the current owner, and the app if an app owns the surface.
func (s *Surface) Owner() (owner, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.own, s.app
}

// Outside reports whether the surface is outside Home's hands: Live, the
// Shadow UI, or an old hack. push-manager's own chords and the old display
// and intercept calls work only then.
func (s *Surface) Outside() bool {
	o, _ := s.Owner()
	return o == ownOutside
}

// LegacyAllowed is whether a call that takes or gives back the surface
// directly (/api/display/mode, /api/midi/filter) may run: outside, or from
// the app that owns the surface. app is the caller's X-Push-Hack-App.
func (s *Surface) LegacyAllowed(app string) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.own == ownOutside:
		return true, ""
	case s.own == ownApp && app != "" && app == s.app:
		return true, ""
	case s.own == ownApp:
		return false, "the surface belongs to " + s.app
	}
	return false, "the surface belongs to Push Hack Home"
}

// FrameAllowed is whether a frame may reach the screen: an app's only while
// it owns the surface, an old hack's (no app id) only outside.
func (s *Surface) FrameAllowed(app string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if app == "" {
		return s.own == ownOutside
	}
	return s.own == ownApp && s.app == app
}

// ---- events -----------------------------------------------------------------

// Gesture is Shift + Help (taps 1) or Shift + Help, Help (taps 2 or more),
// fired when Shift was let go.
func (s *Surface) Gesture(taps int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.down || taps < 1 {
		return
	}
	if taps == 1 {
		switch s.own {
		case ownHome:
			s.toLive()
		default:
			s.toHome("")
		}
		return
	}
	sup := s.superstar()
	switch {
	case sup == "":
		s.toHome("NO SUPERSTAR -- SHIFT + STAR SETS ONE")
	case s.own == ownApp && s.app == sup:
		s.toLive()
	default:
		s.open(sup)
	}
}

// SessionOpen registers an app's session; a newer one for the same app
// replaces the older.
func (s *Surface) SessionOpen(app string, pass []int) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.sessions[app]; old != nil {
		close(old.done)
	}
	ss := &session{app: app, pass: pass, events: make(chan surfaceEvent, 16), done: make(chan struct{})}
	s.sessions[app] = ss
	log.Printf("surface: %s connected", app)
	if s.starting == app {
		s.starting = ""
		if s.own == ownHome {
			s.focus(app)
			return ss
		}
	}
	s.redraw()
	return ss
}

// SessionClosed is an app's session ending: the app stopped, crashed or
// restarted. If it owned the surface, Home takes over and says so.
func (s *Surface) SessionClosed(ss *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[ss.app] != ss {
		return // replaced by a newer session
	}
	delete(s.sessions, ss.app)
	log.Printf("surface: %s disconnected", ss.app)
	if s.own == ownApp && s.app == ss.app {
		s.toHome(strings.ToUpper(s.label(ss.app)) + " STOPPED -- PRESS IT TO START AGAIN")
		return
	}
	s.redraw()
}

// Status is an app's two lines for its card.
func (s *Surface) Status(app string, lines [2]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[app]
	if ss == nil {
		return fmt.Errorf("%s has no session", app)
	}
	ss.status = lines
	if s.own == ownHome {
		s.redraw()
	}
	return nil
}

// Input switches an owning app between the whole surface and the screen
// only (Stacks' listen mode: the pads and buttons stay with Live).
func (s *Surface) Input(app string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.own != ownApp || s.app != app {
		return fmt.Errorf("%s does not own the surface", app)
	}
	s.input = on
	if on {
		s.io.setPass(s.sessions[app].pass)
	} else {
		s.io.screenOnly()
	}
	return nil
}

// Leave is an owning app giving the surface up: to Live (its LIVE tab) or
// to Home.
func (s *Surface) Leave(app, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.own != ownApp || s.app != app {
		return fmt.Errorf("%s does not own the surface", app)
	}
	if to == "home" {
		s.toHome("")
	} else {
		s.toLive()
	}
	return nil
}

// Open opens an entry from outside Home's own buttons (the web UI). It is
// refused while another app owns the surface, so one app never takes it
// from another.
func (s *Surface) Open(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.down {
		return fmt.Errorf("Push is shutting down")
	}
	if s.own == ownApp && s.app != id {
		return fmt.Errorf("the surface belongs to %s", s.app)
	}
	if s.find(id) < 0 {
		return fmt.Errorf("no entry %q", id)
	}
	if s.own == ownOutside {
		s.toHome("")
	}
	s.open(id)
	return nil
}

// Power is the power button: Live, until the device is off.
func (s *Surface) Power() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.down {
		return
	}
	s.down = true
	if s.own != ownOutside {
		s.toLive()
	}
}

// Tick runs once a second: an app Home started and that never connected.
func (s *Surface) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.starting != "" && s.io.now().Sub(s.startAt) > startTimeout {
		s.notice = strings.ToUpper(s.label(s.starting)) + " DID NOT START -- SEE ITS LOG"
		s.starting = ""
		s.redraw()
	}
}

// ---- transitions (s.mu held) --------------------------------------------------

func (s *Surface) toHome(notice string) {
	switch s.own {
	case ownApp:
		s.send(s.app, surfaceEvent{Name: "blur"})
		s.io.setPass(nil)
	case ownOutside:
		s.io.closeTool()
		s.io.take(nil)
	}
	if s.own != ownHome {
		s.cursor = 0
		if i := s.find(s.last); i >= 0 {
			s.cursor = i
		}
	}
	s.own, s.app, s.notice = ownHome, "", notice
	s.redraw()
}

func (s *Surface) toLive() {
	if s.own == ownApp {
		s.send(s.app, surfaceEvent{Name: "blur"})
	}
	if s.own == ownHome {
		s.io.homeHide()
	}
	s.io.release()
	s.own, s.app, s.notice = ownOutside, "", ""
}

// open is an entry chosen: a tool opens the Shadow UI on it, a running app
// gets the surface, a stopped one is started and opens when it connects.
func (s *Surface) open(id string) {
	i := s.find(id)
	if i < 0 {
		return
	}
	e := s.entries()[i]
	if !e.App {
		if s.own == ownApp {
			s.send(s.app, surfaceEvent{Name: "blur"})
		}
		if s.own == ownHome {
			s.io.homeHide()
		}
		s.own, s.app, s.notice = ownOutside, "", ""
		s.io.openTool(id)
		return
	}
	if s.sessions[id] == nil {
		if s.own != ownHome {
			s.toHome("")
		}
		s.starting, s.startAt = id, s.io.now()
		s.notice = "STARTING " + strings.ToUpper(e.Label) + " -- IT OPENS WHEN READY"
		s.cursor = i
		s.io.startApp(id)
		s.redraw()
		return
	}
	if s.own == ownOutside {
		s.io.closeTool()
		s.io.take(s.sessions[id].pass)
	}
	s.focus(id)
}

func (s *Surface) focus(id string) {
	if s.own == ownApp && s.app != id {
		s.send(s.app, surfaceEvent{Name: "blur"})
	}
	if s.own == ownHome {
		s.io.homeHide()
	}
	s.io.setPass(s.sessions[id].pass)
	s.own, s.app, s.input, s.last, s.notice = ownApp, id, true, id, ""
	s.send(id, surfaceEvent{Name: "focus", Data: map[string]bool{"input": true}})
	log.Printf("surface: %s has the surface", id)
}

func (s *Surface) send(app string, ev surfaceEvent) {
	ss := s.sessions[app]
	if ss == nil {
		return
	}
	select {
	case ss.events <- ev:
	default:
		log.Printf("surface: %s is not reading its session; %s dropped", app, ev.Name)
	}
}

func (s *Surface) redraw() {
	if s.own == ownHome {
		s.io.homeShow(s.view())
	}
}

// ---- Home's entries -------------------------------------------------------------

func (s *Surface) find(id string) int {
	if id == "" {
		return -1
	}
	for i, e := range s.entries() {
		if e.ID == id {
			return i
		}
	}
	return -1
}

func (s *Surface) label(id string) string {
	for _, e := range s.entries() {
		if e.ID == id {
			return e.Label
		}
	}
	return id
}

func (s *Surface) superstar() string {
	for _, e := range s.entries() {
		if e.Super {
			return e.ID
		}
	}
	return ""
}

// view is what Home shows: the entries with the apps' live state.
func (s *Surface) view() homeView {
	es := s.entries()
	for i := range es {
		if !es[i].App {
			continue
		}
		if ss := s.sessions[es[i].ID]; ss != nil {
			es[i].Running, es[i].Status = true, ss.status
		}
		es[i].Starting = es[i].ID == s.starting
	}
	if s.cursor >= len(es) {
		s.cursor = max(len(es)-1, 0)
	}
	return homeView{Entries: es, Cursor: s.cursor, Last: s.last, Notice: s.notice}
}
