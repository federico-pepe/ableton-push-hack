// home.go -- Push Hack Home's screen, pads and buttons.
//
// Eight entries a page, each in the screen column under its top button:
// the superstar first, then the starred entries, then the rest in the
// user's order (ui_tabs.go). An entry is an app (a hack that takes the
// surface) or a tool (a Shadow UI panel). The top pad row is a quiet hint
// of the columns: one pad per entry, in its colour, opens it; every other
// pad is dark and does nothing. Drawn only when something changes.
//
//	top 1-8, top pad 1-8      open that entry
//	jog turn / press          choose / open
//	page < >                  the next or previous page
//	bottom 1  < LAST          back to the app you came from
//	bottom 2  STAR            star or unstar the chosen entry
//	Shift + STAR              make it the superstar (again: none)
//	bottom 8  LIVE            back to Live
package main

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

// homeEntry is one card.
type homeEntry struct {
	ID, Label string
	App       bool   // takes the surface; else a Shadow UI tool
	Color     string // a push3 palette name
	Star      bool
	Super     bool
	About     [2]string // a tool's own lines
	// An app's live state, filled by Surface.view.
	Running, Starting bool
	Status            [2]string
}

type homeView struct {
	Entries []homeEntry
	Cursor  int
	Last    string
	Notice  string
}

const homePer = 8 // entries a page: one per column

func (v homeView) page() int  { return v.Cursor / homePer }
func (v homeView) pages() int { return max((len(v.Entries)+homePer-1)/homePer, 1) }

// onPage is the entries of the cursor's page, and the index of the first.
func (v homeView) onPage() ([]homeEntry, int) {
	from := v.page() * homePer
	return v.Entries[from:min(from+homePer, len(v.Entries))], from
}

func (v homeView) label(id string) string {
	for _, e := range v.Entries {
		if e.ID == id {
			return e.Label
		}
	}
	return ""
}

// ---- the screen -------------------------------------------------------------

const (
	homeW, homeH = 960, 160
	homeColW     = 120
	homeBotY     = 142
)

var (
	homeGround = color.NRGBA{8, 10, 12, 255}
	homeRule   = color.NRGBA{44, 50, 56, 255}
	homeDim    = color.NRGBA{110, 120, 128, 255}
	homeInk    = color.NRGBA{214, 220, 226, 255}
	homeBright = color.NRGBA{250, 250, 250, 255}
	homeAmber  = color.NRGBA{255, 180, 84, 255}
)

func paletteRGB(name string) color.NRGBA {
	i, ok := push3.ColorByName(name)
	if !ok {
		i, _ = push3.ColorByName("gray_blue")
	}
	return push3.ColorForIndex(i).RGB
}

func blend(a, b color.NRGBA, t float64) color.NRGBA {
	f := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 255}
}

func homeCenter(img *image.NRGBA, x, w, base int, s string, c color.NRGBA) {
	text.Draw(img, x+(w-text.Width(s))/2, base, s, c)
}

// renderHome draws Home. Every string is ASCII: the font has nothing else,
// so the stars are drawn, not typed.
func renderHome(v homeView) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, homeW, homeH))
	gfx.FillRect(img, 0, 0, homeW, homeH, homeGround)
	page, from := v.onPage()
	for i := 0; i < homePer; i++ {
		x := i * homeColW
		if i > 0 {
			gfx.FillRect(img, x, 4, 1, homeBotY-8, homeRule)
		}
		if i >= len(page) {
			continue
		}
		e := page[i]
		hue := paletteRGB(e.Color)
		stopped := e.App && !e.Running && !e.Starting
		stripe := hue
		if stopped {
			stripe = blend(hue, homeGround, 0.72)
		}
		gfx.FillRect(img, x+6, 0, homeColW-12, 4, stripe) // under the card's top button
		if from+i == v.Cursor {
			gfx.FillRect(img, x+3, 7, homeColW-6, homeBotY-11, blend(homeGround, hue, 0.16))
			gfx.FillRect(img, x+3, 7, homeColW-6, 1, homeAmber)
			gfx.FillRect(img, x+3, homeBotY-5, homeColW-6, 1, homeAmber)
			gfx.FillRect(img, x+3, 7, 1, homeBotY-11, homeAmber)
			gfx.FillRect(img, x+homeColW-4, 7, 1, homeBotY-11, homeAmber)
		}
		nc := homeBright
		if stopped {
			nc = homeDim
		}
		name := strings.ToUpper(e.Label)
		if text.WidthScaled(name, 2) <= homeColW-18 {
			text.DrawScaled(img, x+10, 33, 2, name, nc)
		} else {
			text.Draw(img, x+10, 29, text.Truncate(name, 15), nc)
		}
		kind, kc := "TOOL", homeDim
		if e.App {
			kind = "APP"
			if !stopped {
				kc = hue
			}
		}
		text.Draw(img, x+10, 50, kind, kc)
		switch {
		case e.Super:
			drawRing(img, x+homeColW-18, 46, 8, homeAmber)
			drawStar(img, x+homeColW-18, 46, 5, homeAmber)
		case e.Star:
			drawStar(img, x+homeColW-18, 46, 7, homeAmber)
		}
		lines := e.About
		if e.App {
			lines = e.Status
			if stopped {
				lines = [2]string{"STOPPED", ""}
			}
		}
		for k, line := range lines {
			c := homeInk
			if stopped {
				c = homeDim
			}
			text.Draw(img, x+10, 76+k*16, text.Truncate(strings.ToUpper(line), 14), c)
		}
		switch {
		case e.Starting:
			gfx.FillRect(img, x+10, 119, 7, 7, homeAmber)
			text.Draw(img, x+22, 128, "STARTING...", homeAmber)
		case stopped:
			text.Draw(img, x+10, 128, "PRESS TO START", homeInk)
		case e.App:
			gfx.FillRect(img, x+10, 119, 7, 7, hue)
			text.Draw(img, x+22, 128, "RUNNING", hue)
		}
		if e.App && e.ID == v.Last && !stopped && !e.Starting { // a stopped card's last line is its action
			tw := text.Width("LAST") + 8
			gfx.FillRect(img, x+homeColW-10-tw, 118, tw, 13, homeAmber)
			text.Draw(img, x+homeColW-6-tw, 128, "LAST", homeGround)
		}
	}
	// the bottom buttons' row
	gfx.FillRect(img, 0, homeBotY, homeW, 1, homeRule)
	if l := v.label(v.Last); l != "" {
		homeCenter(img, 0, homeColW, homeBotY+13, "< "+strings.ToUpper(l), homeInk)
	}
	if v.Cursor < len(v.Entries) {
		sc := homeInk
		if v.Entries[v.Cursor].Star {
			gfx.FillRect(img, homeColW+8, homeBotY+2, homeColW-16, 15, homeAmber)
			sc = homeGround
		}
		homeCenter(img, homeColW, homeColW, homeBotY+13, "STAR", sc)
	}
	if v.Notice != "" {
		gfx.FillRect(img, 2*homeColW+4, homeBotY+2, 5*homeColW-8, 15, homeAmber)
		homeCenter(img, 2*homeColW, 5*homeColW, homeBotY+13, text.Truncate(v.Notice, 80), homeGround)
	} else {
		hint := ""
		for _, e := range v.Entries {
			if e.Super {
				hint = "SHIFT+HELP, HELP: " + strings.ToUpper(e.Label)
			}
		}
		if v.pages() > 1 {
			hint += "     PAGE " + itoa(v.page()+1) + "/" + itoa(v.pages())
		}
		homeCenter(img, 2*homeColW, 5*homeColW, homeBotY+13, strings.TrimSpace(hint), homeDim)
	}
	homeCenter(img, 7*homeColW, homeColW, homeBotY+13, "LIVE", homeInk)
	return img
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func drawRing(img *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	for y := cy - r - 1; y <= cy+r+1; y++ {
		for x := cx - r - 1; x <= cx+r+1; x++ {
			d := math.Hypot(float64(x-cx)+0.5, float64(y-cy)+0.5)
			if d <= float64(r)+0.5 && d >= float64(r)-1.5 {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

// drawStar fills a five-pointed star r pixels from its centre to a point.
func drawStar(img *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	var pts [10][2]float64
	for i := range pts {
		a := -math.Pi/2 + float64(i)*math.Pi/5
		rr := float64(r)
		if i%2 == 1 {
			rr *= 0.45
		}
		pts[i] = [2]float64{float64(cx) + rr*math.Cos(a), float64(cy) + rr*math.Sin(a)}
	}
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			in, fy := false, float64(y)+0.5
			for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
				xi, yi, xj, yj := pts[i][0], pts[i][1], pts[j][0], pts[j][1]
				if (yi > fy) != (yj > fy) && float64(x)+0.5 < (xj-xi)*(fy-yi)/(yj-yi)+xi {
					in = !in
				}
			}
			if in {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

// ---- LEDs -----------------------------------------------------------------------

// homeLEDs is what Home lights: pad notes and button CCs to palette
// indexes. One pad per entry, on the top row, in its entry's colour (a
// near-black grey while the app is stopped; white for the chosen entry);
// every other pad dark. A whole lit grid read as something to play.
func homeLEDs(v homeView) (pads map[uint8]uint8, ccs map[uint8]uint8) {
	idx := func(name string) uint8 { i, _ := push3.ColorByName(name); return i }
	pads, ccs = map[uint8]uint8{}, map[uint8]uint8{}
	page, from := v.onPage()
	for col := 0; col < homePer; col++ {
		c := uint8(0)
		if col < len(page) {
			e := page[col]
			c, _ = push3.ColorByName(e.Color)
			if c == 0 {
				c = idx("gray_blue")
			}
			if e.App && !e.Running && !e.Starting {
				c = idx("dgray") // not "dim": index 1 is a button brightness, red on a pad
			}
		}
		for row := 0; row < 8; row++ {
			p := uint8(0)
			if row == homePadRow {
				p = c
				if col < len(page) && from+col == v.Cursor {
					p = idx("white_btn")
				}
			}
			pads[push3.PadNote(col, row)] = p
		}
		ccs[CCScreenTopN(col)] = map[bool]uint8{true: idx("white_btn"), false: 0}[col < len(page)]
		if col < len(page) && from+col == v.Cursor {
			ccs[CCScreenTopN(col)] = idx("amber")
		}
	}
	for i := 0; i < 8; i++ {
		ccs[CCScreenBotN(i)] = 0
	}
	if v.label(v.Last) != "" {
		ccs[CCScreenBotN(0)] = idx("white_btn")
	}
	if v.Cursor < len(v.Entries) {
		ccs[CCScreenBotN(1)] = map[bool]uint8{true: idx("amber"), false: idx("white_btn")}[v.Entries[v.Cursor].Star]
	}
	ccs[CCScreenBotN(7)] = idx("white_btn")
	ccs[CCPageLeft] = map[bool]uint8{true: 127, false: 0}[v.page() > 0]
	ccs[CCPageRight] = map[bool]uint8{true: 127, false: 0}[v.page() < v.pages()-1]
	ccs[CCHelp] = 127 // Home's own button, lit while Home is open
	return pads, ccs
}

// ---- input ----------------------------------------------------------------------

// HomeCC is a button or the jog while Home owns the surface. It reports
// whether Home used it.
func (s *Surface) HomeCC(cc, val uint8) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.own != ownHome {
		return false
	}
	if cc == CCShift {
		return true // SetShift has it, from every Shift press
	}
	if cc == CCJogWheel {
		if d := push3.DecodeRel(val); d != 0 {
			s.move(map[bool]int{true: 1, false: -1}[d > 0])
		}
		return true
	}
	if val == 0 {
		return true
	}
	v := s.view()
	page, from := v.onPage()
	switch {
	case cc >= CCScreenTop1 && cc < CCScreenTop1+homePer:
		if i := int(cc - CCScreenTop1); i < len(page) {
			s.cursor = from + i
			s.open(page[i].ID)
		}
	case cc == CCJogPress:
		if s.cursor < len(v.Entries) {
			s.open(v.Entries[s.cursor].ID)
		}
	case cc == CCPageLeft:
		s.move(-homePer)
	case cc == CCPageRight:
		s.move(homePer)
	case cc == CCScreenBotN(0):
		if s.find(s.last) >= 0 {
			s.open(s.last)
		}
	case cc == CCScreenBotN(1):
		if s.cursor < len(v.Entries) && s.setStar != nil {
			e := v.Entries[s.cursor]
			id := e.ID
			if s.shift { // the superstar, or none
				s.setStar(id, true, !e.Super)
			} else {
				s.setStar(id, !e.Star, e.Super && !e.Star)
			}
			s.cursor = max(s.find(id), 0) // the entry moves with its star
			s.redraw()
		}
	case cc == CCScreenBotN(7):
		s.toLive()
	}
	return true
}

// homePadRow is the pad row Home lights and listens to: the top one.
const homePadRow = 7

// HomePad is a pad while Home owns the surface: a top-row pad opens its
// column's entry; the other pads are dark and do nothing.
func (s *Surface) HomePad(note, vel uint8) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.own != ownHome {
		return false
	}
	if vel == 0 || !push3.IsPadNote(note) {
		return true
	}
	col, row := push3.PadCoord(note)
	if row != homePadRow {
		return true
	}
	v := s.view()
	if page, from := v.onPage(); col < len(page) {
		s.cursor = from + col
		s.open(page[col].ID)
	}
	return true
}

func (s *Surface) move(d int) {
	n := len(s.entries())
	if n == 0 {
		return
	}
	s.cursor = min(max(s.cursor+d, 0), n-1)
	s.redraw()
}

// SetShift is Shift's state, fed from every Shift press (surface_io.go)
// -- not only those Home receives: the release that fires Shift + Help
// goes to the gesture, and Home must not go on thinking Shift is held
// (found on the device: the next STAR became Shift + STAR).
func (s *Surface) SetShift(down bool) {
	s.mu.Lock()
	s.shift = down
	s.mu.Unlock()
}
