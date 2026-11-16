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
// Shift is let go (surface_io.go). Help alone, from Home or an app, goes
// back to Live, on Help's release; in Live it is Live's Help. Help held
// helpHold is Shift + Help: Home, from anywhere but Home. An app's session
// ending while it owns the surface lands the user on Home, which says so.
// The power button (release.go) gives everything to Live.
//
// All side effects go through surfaceIO, so the state machine is tested
// against a fake (surface_test.go) with random event sequences. The design
// is plans/2026-10-17-push-hack-switcher.md.
package main

import (
	"fmt"
	"log"
	"slices"
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
	take(pass []int)         // intercept on with these pass-through CCs, display taken, LEDs cleared
	setPass(pass []int)      // intercept on, a new pass-through list
	screenOnly()             // intercept off, display still taken (an app's listen mode)
	release()                // intercept off, display back to Push3: Live
	closeLiveHelp(held bool) // tap Help into Live: it saw the user's Help and opened its help
	homeCover(v homeView)    // Home's frame on the screen at once, before the buttons are taken
	homeShow(v homeView)     // draw Home and its LEDs
	homeHide()               // stop drawing Home
	openTool(id string)      // the Shadow UI, on this panel
	closeTool()              // stop the Shadow UI, if it runs
	releaseHeld()            // the Shadow UI or an old hack holds the screen or the buttons: give them back to Live; nothing held, nothing done
	startApp(id string)      // ask push-catalog to start a stopped app
	liveOwns() bool          // Live has screen and buttons: no tool or old hack holds them
	tapLive(cc uint8)        // press and release a button in Live, once it has the buttons again
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

	// The device view (devview.go): an app's view replacing Push's own
	// while one of its Live devices is selected and Push shows its device
	// view. auto: the owning app was opened that way. autoKey: the
	// selection it shows. hold: the selection the user left the app on
	// (Help, Home, Device View); it does not open again for that one until
	// the selection changes or Push leaves its device view.
	setDevView    func(id string, on bool)
	auto          bool
	autoKey, hold string
	autoAt        time.Time // when Follow opened the app
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
	if taps < 1 {
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

// HelpAlone is Help pressed and let go without Shift: from Home or an app,
// back to Live. Elsewhere it is Live's own Help, and nothing here. An app
// with only the screen (Stacks' listening mode) goes back too, but Live has
// the buttons then: it saw the same press and opened its help screen
// (seen on device). Help toggles it, so one more tap closes it, while the
// app's screen still hides Live's.
func (s *Surface) HelpAlone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.own != ownHome && s.own != ownApp {
		return
	}
	if s.own == ownApp && !s.input {
		s.io.closeLiveHelp(false)
	}
	s.toLive()
}

// LongHelp is Help held without Shift: Shift + Help's Home, from Live, a
// tool or an app. From Home it is nothing: the short press goes to Live,
// and going there now would hand Live a release it never saw pressed.
// liveSaw: Live had the buttons (Live itself, or an app's screen-only)
// and opened its help screen on the press; close it before Home takes
// the buttons, while the tap still gets through.
func (s *Surface) LongHelp(liveSaw bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.own == ownHome {
		return
	}
	if liveSaw && s.own == ownOutside {
		// Home over Live before the tap: else Live's screen shows between
		// its help closing and Home (seen on device: help, Live, Home).
		s.homeCursor()
		s.notice = ""
		s.io.homeCover(s.view())
	}
	if liveSaw {
		s.io.closeLiveHelp(true)
	}
	s.toHome("")
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
		if s.auto { // it was only standing in for Push's device view
			s.auto = false
			s.toLive()
			return
		}
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

// PowerPress is the power button going down (release.go): Push3 is about
// to ask "shut down?", so Home or the app gives the surface to Live. Nothing
// latches. After Cancel, Home opens again as usual. Outside Home's hands,
// the Shadow UI or an old hack (Keyboard Visualizer) may hold the surface
// too: it goes back to Live the same way.
func (s *Surface) PowerPress() {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Printf("surface: power button, owner %s", s.own)
	if s.own != ownOutside {
		s.toLive()
		return
	}
	s.io.releaseHeld()
}

// Open opens an entry from outside Home's own buttons (the web UI). It is
// refused while another app owns the surface, so one app never takes it
// from another.
func (s *Surface) Open(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// ---- the device view (devview.go) ----------------------------------------------

// Follow is what Live and Push show now: target is the app whose device is
// selected, with its device view switched on ("" for none), key names the
// selection, deviceView is whether Push shows its own device view. While
// all three hold, the app's view replaces Push's: Follow opens it if Live
// has the surface, and closes it when they stop holding. It never takes
// the surface from Home, an app the user opened, the Shadow UI or an old
// hack.
func (s *Surface) Follow(target, key string, deviceView bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !deviceView || key != s.hold {
		s.hold = "" // a new selection, or Push left its device view
	}
	if s.own == ownApp && s.auto {
		switch {
		case target == "" || s.sessions[target] == nil:
			s.auto = false // closed by Live's side: no hold
			s.toLive()
		case target != s.app:
			s.focusAs(target, true)
			s.autoKey = key
		default:
			s.autoKey = key
		}
		return
	}
	if target == "" || !deviceView || s.hold != "" {
		return
	}
	if s.own != ownOutside || s.sessions[target] == nil || !s.io.liveOwns() {
		return
	}
	s.io.take(s.sessions[target].pass)
	s.focusAs(target, true)
	s.autoKey, s.autoAt = key, s.io.now()
	log.Printf("surface: %s stands in for Push's device view", target)
}

// ClearHold: the user asked for the app's view again (Device View pressed in
// Push's own device view); the next Follow opens it.
func (s *Surface) ClearHold() {
	s.mu.Lock()
	s.hold = ""
	s.mu.Unlock()
}

// standInButtons leave Push's device view in Push itself; while an app stands
// in for that view they are push-manager's (ViewButton), never the app's,
// except the ones the app keeps (keepableButtons, StandInTakes).
// Track navigation (lower row, arrows) stays the app's: it moves Live's
// selection, and Follow does the rest.
var standInButtons = map[uint8]bool{
	CCDeviceView: true, CCMixerView: true, CCClipView: true, CCSessionView: true,
	CCAdd: true, CCSwap: true, CCSettings: true, CCSet: true, CCUserMode: true, CCNew: true,
}

// keepableButtons: what an app may keep of standInButtons while it stands
// in (hack.json surface.device_view_keep): the ones that act ON the device.
// Pung's Swap opens its sounds, as Push's Swap offers another sound for a
// device. The buttons that leave the device view are not here: the user
// must always get out with them.
var keepableButtons = map[string]uint8{"add": CCAdd, "swap": CCSwap, "new": CCNew}

// keepCCs: the buttons a hack.json names, the unknown ones left out.
func keepCCs(names []string) []uint8 {
	var out []uint8
	for _, n := range names {
		if cc, ok := keepableButtons[strings.ToLower(strings.TrimSpace(n))]; ok && !slices.Contains(out, cc) {
			out = append(out, cc)
		}
	}
	return out
}

// keeps: the buttons app id keeps while it stands in.
func (s *Surface) keeps(id string) []uint8 {
	for _, e := range s.entries() {
		if e.ID == id {
			return e.Keep
		}
	}
	return nil
}

// StandInTakes reports whether button cc is push-manager's now: an app
// stands in for Push's device view, cc is one that leaves that view
// (standInButtons), and the app does not keep it. The press is then
// ViewButton's, and never the app's or Live's.
func (s *Surface) StandInTakes(cc uint8) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return standInButtons[cc] && s.own == ownApp && s.auto && !slices.Contains(s.keeps(s.app), cc)
}

// viewPressGrace: a view button this soon after Follow opened the app is
// the press that opened it.
const viewPressGrace = 300 * time.Millisecond

// ViewButton is a button that leaves Push's device view, pressed while an
// app stands in for it (standInButtons). Device View: Push's own device
// view, for this device (held). Any other: back to Live, and the press goes
// on to Push, which then does what it does (another view, its browser,
// settings, the Set menu).
func (s *Surface) ViewButton(cc uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.own != ownApp || !s.auto {
		return
	}
	// The press that switched Push to its device view, read late: it opened
	// the app, it must not close it again.
	if s.io.now().Sub(s.autoAt) < viewPressGrace {
		return
	}
	if cc == CCDeviceView {
		s.toLive()
		return
	}
	s.auto = false
	s.toLive()
	s.io.tapLive(cc)
}

// DeviceViewState is what /api/surface reports about it.
func (s *Surface) DeviceViewState() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"auto": s.own == ownApp && s.auto, "key": s.autoKey, "hold": s.hold}
}

// leaveAuto: the owning app opened by Follow is being left by the user, so
// its selection is held. (Follow's own closing clears auto first.)
func (s *Surface) leaveAuto() {
	if s.own == ownApp && s.auto {
		s.hold = s.autoKey
	}
	s.auto = false
}

// ---- transitions (s.mu held) --------------------------------------------------

func (s *Surface) toHome(notice string) {
	s.leaveAuto()
	switch s.own {
	case ownApp:
		s.send(s.app, surfaceEvent{Name: "blur"})
		s.io.setPass(nil)
	case ownOutside:
		s.io.closeTool()
		// Home's frame first: take lets Live settle (120 ms) before the
		// intercept, and Live's screen showed through that, then black.
		s.homeCursor()
		s.notice = notice
		s.io.homeCover(s.view())
		s.io.take(nil)
	}
	s.homeCursor()
	s.own, s.app, s.notice = ownHome, "", notice
	s.redraw()
}

// homeCursor puts Home's cursor on the app you came from, unless Home is
// open already.
func (s *Surface) homeCursor() {
	if s.own != ownHome {
		s.cursor = 0
		if i := s.find(s.last); i >= 0 {
			s.cursor = i
		}
	}
}

func (s *Surface) toLive() {
	s.leaveAuto()
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
		s.leaveAuto()
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

func (s *Surface) focus(id string) { s.focusAs(id, false) }

// focusAs: standIn, the app opens in place of Push's device view (Follow),
// and its focus event says so (pmclient.FocusInfo.DeviceView).
func (s *Surface) focusAs(id string, standIn bool) {
	s.auto = standIn
	if s.own == ownApp && s.app != id {
		s.send(s.app, surfaceEvent{Name: "blur"})
	}
	if s.own == ownHome {
		s.io.homeHide()
	}
	s.io.setPass(s.sessions[id].pass)
	s.own, s.app, s.input, s.last, s.notice = ownApp, id, true, id, ""
	data := map[string]any{"input": true, "device_view": standIn}
	if keep := s.keeps(id); standIn && len(keep) > 0 {
		data["keep"] = keep2ints(keep) // these presses are the app's (pmclient.FocusInfo.Keep)
	}
	s.send(id, surfaceEvent{Name: "focus", Data: data})
	log.Printf("surface: %s has the surface", id)
}

func keep2ints(ccs []uint8) []int {
	out := make([]int, len(ccs))
	for i, c := range ccs {
		out[i] = int(c)
	}
	return out
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
