// remote_panel.go — a Shadow UI tab owned by another hack.
//
// push-manager is a dumb terminal here. The hack declares `shadow_ui`
// ({"label", "path"}) in its own hack.json; push-manager polls
// http://127.0.0.1:<port><path> for a small JSON view and draws it with the
// same widgets its own panels use, and POSTs every button/encoder press
// straight back as {"cc", "value"}. All state — cursor, sub-views, what a
// button does — lives in the hack. That keeps the contract one GET and one
// POST wide, and means a new tab ships with the hack, not with push-manager.
//
// Deliberately not a pixel protocol: a PNG per frame would cost an encode +
// decode 30 times a second on a device whose whole point is not stealing CPU
// from Live.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
)

// remoteView is what the hack serves. Every field is optional — an empty
// object renders an empty tab rather than an error.
type remoteView struct {
	Title   string   `json:"title"`   // breadcrumb line
	Status  string   `json:"status"`  // overrides Title when set (errors, progress)
	Rows    []string `json:"rows"`    // list body, ASCII only (basicfont has no more)
	Cursor  int      `json:"cursor"`  // highlighted row; the hack owns it
	Buttons []string `json:"buttons"` // up to 8 soft-button labels, "" = unused
	Hint    string   `json:"hint"`    // right-hand nav hint on the bottom strip
}

const (
	remoteRowH        = 18
	remotePollEvery   = 300 * time.Millisecond
	remoteVisibleRows = 6
)

var remoteClient = &http.Client{Timeout: 2 * time.Second}

type RemotePanel struct {
	mu     sync.Mutex
	label  string
	base   string // http://127.0.0.1:<port><path>
	view   remoteView
	scroll int
	err    string
	last   time.Time
}

func newRemotePanel(label string, port int, path string) *RemotePanel {
	if path == "" {
		path = "/"
	}
	p := &RemotePanel{label: label, base: fmt.Sprintf("http://127.0.0.1:%d%s", port, path)}
	p.err = "connecting..."
	go p.refresh()
	return p
}

func (p *RemotePanel) Label() string { return p.label }

func (p *RemotePanel) refresh() {
	var v remoteView
	resp, err := remoteClient.Get(p.base)
	if err == nil {
		defer resp.Body.Close()
		err = json.NewDecoder(resp.Body).Decode(&v)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = time.Now()
	if err != nil {
		p.err = "hack not responding - is it running?"
		return
	}
	p.err = ""
	p.view = v
	// Keep the hack's cursor on screen. It owns the cursor, we own the
	// window onto the list.
	if v.Cursor < p.scroll {
		p.scroll = v.Cursor
	}
	if v.Cursor >= p.scroll+remoteVisibleRows {
		p.scroll = v.Cursor - remoteVisibleRows + 1
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
}

// send forwards one control event and immediately re-polls, so a press shows
// its result on the next frame instead of after the poll interval.
func (p *RemotePanel) send(cc, val uint8) {
	body, _ := json.Marshal(map[string]uint8{"cc": cc, "value": val})
	resp, err := remoteClient.Post(p.base, "application/json", bytes.NewReader(body))
	if err == nil {
		resp.Body.Close()
	}
	p.refresh()
}

func (p *RemotePanel) HandleCC(cc, val uint8) { go p.send(cc, val) }
func (p *RemotePanel) handleJog(val uint8)    { go p.send(CCJogWheel, val) }

func (p *RemotePanel) Render(img *image.NRGBA) {
	p.mu.Lock()
	if time.Since(p.last) > remotePollEvery {
		p.last = time.Now() // set before spawning so frames don't stampede
		go p.refresh()
	}
	v, scroll, errMsg := p.view, p.scroll, p.err
	p.mu.Unlock()

	rows := make([]widgets.ListRow, len(v.Rows))
	for i, r := range v.Rows {
		rows[i] = widgets.ListRow{Text: r, TextCol: widgets.Default.White}
	}
	status := v.Status
	if errMsg != "" {
		status = errMsg
	}
	widgets.RenderList(img, widgets.Default, widgets.ListView{
		Rows:       rows,
		Cursor:     v.Cursor,
		Scroll:     scroll,
		Breadcrumb: v.Title,
		Status:     status,
		EmptyText:  "Nothing to show",
	}, suiContentY, suiW, remoteRowH, suiContentBot)
}

func (p *RemotePanel) SoftBotStrip() ([8]widgets.SoftButton, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var b [8]widgets.SoftButton
	for i, l := range p.view.Buttons {
		if i >= 8 {
			break
		}
		state := widgets.SoftNeutral
		if l != "" {
			state = widgets.SoftOn
		}
		b[i] = widgets.SoftButton{Label: l, State: state}
	}
	return b, p.view.Hint
}

func (p *RemotePanel) BotLEDColors() [8]uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var c [8]uint8
	for i, l := range p.view.Buttons {
		if i >= 8 {
			break
		}
		if l != "" {
			c[i] = suiBotWhite
		}
	}
	return c
}
