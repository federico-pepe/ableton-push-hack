// surface_io.go -- Push Hack Home on the device: the gesture, the real
// surfaceIO, the input queue and the HTTP API. The state machine itself is
// surface.go; the screen is home.go.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

// surface is push-manager's one owner of Push's surface (surface.go).
var surface = newSurface(&deviceIO{}, homeEntries, setStarPref)

// ---- the gesture ----------------------------------------------------------------

// helpGesture recognises Shift + Help and Shift + Help, Help: hold Shift,
// tap Help once or twice, let go of Shift. It fires on Shift's release, so
// Live (which sees the presses while it owns the surface) has seen Shift go
// up again and is never left thinking Shift is held. Any other button
// pressed with Shift spoils it: Shift + something else is that something's
// own shortcut (Live's, or Home's Shift + STAR).
//
// Help alone (no Shift) is the third gesture: helpAlone, on Help's release.
// Home and apps take it as "back to Live"; in Live it stays Live's Help.
// Shift going down, or another button pressed while Help is held, spoils it.
type helpGesture struct {
	shift, spoiled bool
	taps           int
	alone          bool // Help is down and was pressed without Shift
}

// helpAlone is what cc returns for Help pressed and let go without Shift.
const helpAlone = -1

// cc feeds one CC message and returns the taps when the gesture fires,
// helpAlone for Help alone, 0 when nothing fires.
func (g *helpGesture) cc(cc, val uint8) int {
	switch {
	case cc == CCShift && val > 0:
		g.shift, g.taps, g.spoiled, g.alone = true, 0, false, false
	case cc == CCShift:
		fire := 0
		if g.shift && g.taps > 0 && !g.spoiled {
			fire = g.taps
		}
		g.shift, g.taps = false, 0
		return fire
	case !g.shift && cc == CCHelp && val > 0:
		g.alone = true
	case !g.shift && cc == CCHelp:
		fire := 0
		if g.alone {
			fire = helpAlone
		}
		g.alone = false
		return fire
	case !g.shift && val > 0 && isTogglableCC(cc): // Help + something: not ours
		g.alone = false
	case !g.shift || val == 0:
	case cc == CCHelp:
		g.taps++
	case isTogglableCC(cc): // a button, not an encoder turning
		g.spoiled = true
	}
	return 0
}

// ---- input --------------------------------------------------------------------------

var (
	gesture  helpGesture
	surfaceQ = make(chan func(), 256)
)

// surfaceInputCC is every CC from Push's hardware, from the MIDI reader.
// It reports whether Home took the message, so nothing else acts on it.
func surfaceInputCC(cc, val uint8) bool {
	if directDisplayOn() { // another app owns screen and buttons (display.go)
		return false
	}
	if cc == CCShift { // Home's Shift + STAR needs Shift's real state, whoever had the press
		down := val > 0
		queueSurface(func() { surface.SetShift(down) })
	}
	switch taps := gesture.cc(cc, val); {
	case taps > 0:
		queueSurface(func() { surface.Gesture(taps) })
		return true
	case taps == helpAlone:
		if o, _ := surface.Owner(); o == ownHome || o == ownApp {
			queueSurface(surface.HelpAlone)
			return true
		}
	}
	if o, _ := surface.Owner(); o == ownHome {
		queueSurface(func() { surface.HomeCC(cc, val) })
		return true
	}
	return false
}

// surfaceInputPad is a pad from Push's hardware.
func surfaceInputPad(note, vel uint8) bool {
	if directDisplayOn() {
		return false
	}
	if o, _ := surface.Owner(); o != ownHome {
		return false
	}
	queueSurface(func() { surface.HomePad(note, vel) })
	return true
}

// queueSurface hands work to the one goroutine that runs the surface, so
// presses act in order and the MIDI reader never waits on the screen or
// the LEDs.
func queueSurface(f func()) {
	select {
	case surfaceQ <- f:
	default:
		log.Printf("surface: input queue full, a press was dropped")
	}
}

func runSurface() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case f := <-surfaceQ:
			f()
		case <-tick.C:
			surface.Tick()
		}
	}
}

// ---- the device -------------------------------------------------------------------

// deviceIO is surfaceIO on Push: the intercept (midiflt), the display shm,
// the LEDs, the Shadow UI and push-catalog.
type deviceIO struct {
	mu   sync.Mutex
	pads map[uint8]uint8 // what Home last lit, so a redraw sends only changes
	ccs  map[uint8]uint8
}

func (d *deviceIO) intercept(on bool, pass []int) {
	data := ensureMidiFilt()
	if data == nil {
		return
	}
	setMidiFiltPass(data, pass)
	data[4] = map[bool]byte{true: 1, false: 0}[on]
}

func takeDisplay() {
	if shmGetMode() != ModeTakeover {
		if err := shmWritePixels(make([]byte, dispFrameB*2)); err != nil {
			log.Printf("surface: clear the screen: %v", err)
		}
		if err := shmSetMode(ModeTakeover); err != nil {
			log.Printf("surface: take the screen: %v", err)
		}
	}
}

func (d *deviceIO) forget() {
	d.mu.Lock()
	d.pads, d.ccs = nil, nil
	d.mu.Unlock()
}

// liveSettle is how long push-manager waits, after the gesture that takes
// the buttons from Live, before switching the intercept on.
const liveSettle = 120 * time.Millisecond

// settleLive runs before the intercept goes on while Live has the buttons.
// The gesture fires on Shift's release, and push-manager reads that
// release in the same moment Push3 does -- then switches the intercept on
// in microseconds, often before Push3 has taken the release, which the hook
// then swallows. Live keeps believing Shift is held, and Play arrives as
// Shift + Play: the button lights, nothing runs (seen on device in Riffs,
// 2026-10-17; Stacks hit the same before Home). So: tell Push3 Shift and
// Help are up, through its own input, and give it time to read them.
func settleLive() {
	if data := ensureMidiFilt(); data == nil || data[4] != 0 {
		return // the intercept is on already: Live sees no buttons anyway
	}
	if p, ok := alsaseq.FindByName(appInputPortName, alsaseq.CapWrite); ok {
		midiOutMu.Lock()
		c := midiOut
		midiOutMu.Unlock()
		if c != nil {
			c.SendCC(p.Addr, 0, CCHelp, 0)  //nolint:errcheck
			c.SendCC(p.Addr, 0, CCShift, 0) //nolint:errcheck
		}
	}
	time.Sleep(liveSettle)
}

func (d *deviceIO) take(pass []int) {
	settleLive()
	d.intercept(true, pass)
	d.forget()
	clearAllLEDs(CCHelp) // the owner paints its own; Help stays lit
	takeDisplay()
}

func (d *deviceIO) setPass(pass []int) { d.intercept(true, pass) }

func (d *deviceIO) screenOnly() { d.intercept(false, nil) }

// release gives the surface back to Live. Every LED goes dark first, as
// the Shadow UI's exit does: Push3 repaints only the LEDs it holds on (a
// Shift and a pad-mode toggle, led_restore.go), so whatever Home or an app
// lit that Live holds off stayed lit -- top and bottom buttons, arrows,
// pads (reported: "when exiting the Home, most of the time all the LEDs
// are still lit"). The clear also marks them cleared, so the repaint runs.
func (d *deviceIO) release() {
	d.forget()
	clearAllLEDs(0)
	releaseSurface() // release.go; watchInterceptOff repaints Live's LEDs
}

func (d *deviceIO) homeShow(v homeView) {
	if err := shmWritePixels(imageToBGR565(renderHome(v))); err != nil {
		log.Printf("surface: draw Home: %v", err)
	}
	takeDisplay()
	pads, ccs := homeLEDs(v)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pads == nil {
		d.pads, d.ccs = map[uint8]uint8{}, map[uint8]uint8{}
	}
	for n, c := range pads {
		if old, ok := d.pads[n]; !ok || old != c {
			sendSeqNote(0, n, c) //nolint:errcheck
			d.pads[n] = c
		}
	}
	for cc, c := range ccs {
		if old, ok := d.ccs[cc]; !ok || old != c {
			sendSeqCC(0, cc, int32(c)) //nolint:errcheck
			d.ccs[cc] = c
		}
	}
}

func (d *deviceIO) homeHide() { d.forget() }

func (d *deviceIO) openTool(id string) {
	settleLive()
	d.intercept(true, nil)
	d.forget()
	if !shadowUIIsActive() {
		clearAllLEDs(CCSettings)
		shadowUIStart()
	}
	shadowUISwitchTo(id)
}

func (d *deviceIO) closeTool() {
	if shadowUIIsActive() {
		shadowUIStop()
	}
}

// startApp asks push-catalog to start a stopped app: enable restarts its
// service (and switches its boot start back on).
func (d *deviceIO) startApp(id string) {
	go func() {
		c := http.Client{Timeout: 30 * time.Second}
		resp, err := c.Post(fmt.Sprintf("%s/api/enable?id=%s", catalogAPIBase, id), "application/json", nil)
		if err != nil {
			log.Printf("surface: start %s: %v", id, err)
			return
		}
		resp.Body.Close()
		log.Printf("surface: asked push-catalog to start %s", id)
	}()
}

func (d *deviceIO) now() time.Time { return time.Now() }

// ---- HTTP -------------------------------------------------------------------------------

// appHeader names the app a frame or a surface call comes from.
const appHeader = "X-Push-Hack-App"

func surfaceState() map[string]any {
	o, app := surface.Owner()
	surface.mu.Lock()
	v := surface.view()
	surface.mu.Unlock()
	return map[string]any{"owner": o.String(), "app": app, "last": v.Last, "entries": v.Entries}
}

// GET /api/surface
func handleSurface(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, surfaceState())
}

// isApp is whether id is an installed hack that declares `surface`.
func isApp(id string) bool {
	for _, e := range homeEntries() {
		if e.ID == id && e.App {
			return true
		}
	}
	return false
}

// GET /api/surface/session?app=ID&pass_cc=85,86,...
//
// Server-sent events, held open while the app runs: ready, focus
// ({"input": true}), blur, and a comment line every second. The deadline
// on each write ends a session whose app stopped reading.
func handleSurfaceSession(w http.ResponseWriter, r *http.Request) {
	app := r.URL.Query().Get("app")
	if !isApp(app) {
		http.Error(w, fmt.Sprintf("no app %q: an installed, enabled hack with \"surface\" in its hack.json", app), http.StatusNotFound)
		return
	}
	var pass []int
	for _, f := range strings.Split(r.URL.Query().Get("pass_cc"), ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && n >= 0 && n < midiFiltPassMax {
			pass = append(pass, n)
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	write := func(s string) error {
		rc.SetWriteDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
		if _, err := fmt.Fprint(w, s); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	event := func(name string, data any) error {
		b, _ := json.Marshal(data)
		return write("event: " + name + "\ndata: " + string(b) + "\n\n")
	}
	ss := surface.SessionOpen(app, pass)
	defer surface.SessionClosed(ss)
	o, owning := surface.Owner()
	if event("ready", map[string]string{"owner": o.String(), "app": owning}) != nil {
		return
	}
	ping := time.NewTicker(time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ss.done:
			return
		case ev := <-ss.events:
			data := ev.Data
			if data == nil {
				data = map[string]any{}
			}
			if event(ev.Name, data) != nil {
				return
			}
		case <-ping.C:
			if write(": ping\n\n") != nil {
				return
			}
		}
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(v); err != nil {
		http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func surfaceReply(w http.ResponseWriter, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	jsonResponse(w, surfaceState())
}

// POST /api/surface/status {"app", "lines": ["...", "..."]}
func handleSurfaceStatus(w http.ResponseWriter, r *http.Request) {
	var b struct {
		App   string   `json:"app"`
		Lines []string `json:"lines"`
	}
	if !decode(w, r, &b) {
		return
	}
	var lines [2]string
	for i := 0; i < len(b.Lines) && i < 2; i++ {
		lines[i] = asciiOnly(b.Lines[i])
	}
	if err := surface.Status(b.App, lines); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	jsonResponse(w, map[string]bool{"ok": true})
}

// POST /api/surface/input {"app", "input": bool}
func handleSurfaceInput(w http.ResponseWriter, r *http.Request) {
	var b struct {
		App   string `json:"app"`
		Input bool   `json:"input"`
	}
	if decode(w, r, &b) {
		surfaceReply(w, surface.Input(b.App, b.Input))
	}
}

// POST /api/surface/leave {"app", "to": "live"|"home"}
func handleSurfaceLeave(w http.ResponseWriter, r *http.Request) {
	var b struct{ App, To string }
	if decode(w, r, &b) {
		surfaceReply(w, surface.Leave(b.App, b.To))
	}
}

// POST /api/surface/open {"id"}
func handleSurfaceOpen(w http.ResponseWriter, r *http.Request) {
	var b struct{ ID string }
	if decode(w, r, &b) {
		surfaceReply(w, surface.Open(b.ID))
	}
}

// POST /api/surface/star {"id", "star": bool, "super": bool}
func handleSurfaceStar(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID    string `json:"id"`
		Star  bool   `json:"star"`
		Super bool   `json:"super"`
	}
	if !decode(w, r, &b) {
		return
	}
	setStarPref(b.ID, b.Star, b.Super)
	surface.mu.Lock()
	surface.redraw()
	surface.mu.Unlock()
	jsonResponse(w, surfaceState())
}

// asciiOnly keeps what Push's font can draw.
func asciiOnly(s string) string {
	b := []byte(s)
	out := b[:0]
	for _, c := range b {
		if c >= 32 && c < 127 {
			out = append(out, c)
		}
	}
	return string(out)
}
