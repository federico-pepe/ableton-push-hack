// store_panel.go — "STORE" tab for push-manager's Shadow UI.
//
// Drop this file into push-manager's src/ (it's `package main`, same as the
// other panels) and wire it in with the three edits in INTEGRATION.md. It
// implements the Panel interface exactly like FilePanel/StatsPanel.
//
// It is a THIN client: it never installs anything itself. All work goes to the
// push-store daemon over localhost HTTP (the same daemon that serves the web
// UI), which runs as root and owns the install logic. That keeps one install
// engine behind three faces — CLI, web, and this screen. Requires the
// `push-store` hack to be installed and running.
package main

import (
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
)

const storeAPIBase = "http://127.0.0.1:7705"

// Rows visible in the content area: (suiContentBot-suiContentY - breadcrumb 13)
// / rowH(18) ≈ 6. Kept as a const so cursor/scroll math needs no render pass.
const storeRowH = 18
const storeVisibleRows = 6

// storeHack mirrors the daemon's /api/catalog JSON entry.
type storeHack struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Requires    []string `json:"requires"`
}

type StorePanel struct {
	mu        sync.Mutex
	hacks     []storeHack
	installed map[string]bool
	cursor    int
	scroll    int
	status      string    // transient line shown in the breadcrumb (errors, progress)
	busy        bool      // an install/remove is in flight — block re-entry
	lastRefresh time.Time // for render-triggered auto-refresh while the tab is visible
}

func newStorePanel() *StorePanel {
	p := &StorePanel{installed: map[string]bool{}, status: "loading…"}
	go p.refresh()
	return p
}

func (p *StorePanel) Label() string { return "STORE" }

// ── data ──────────────────────────────────────────────────────────────────────

var storeGetClient = &http.Client{Timeout: 5 * time.Second}
var storeActClient = &http.Client{Timeout: 3 * time.Minute} // installs download binaries

func (p *StorePanel) refresh() {
	var cat []storeHack
	catErr := storeGetJSON("/api/catalog", &cat)
	var inst []string
	_ = storeGetJSON("/api/installed", &inst) // best-effort

	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRefresh = time.Now()
	if catErr != nil {
		// Distinguish daemon-down from an empty/unreachable catalog: if
		// /api/installed answered, the daemon is fine and the registry is the
		// problem, not the daemon.
		if inst != nil {
			p.status = "catalog unavailable — set registry URL"
		} else {
			p.status = "store daemon offline?"
		}
		return
	}
	p.hacks = cat
	p.installed = make(map[string]bool, len(inst))
	for _, id := range inst {
		p.installed[id] = true
	}
	if p.cursor >= len(cat) {
		p.cursor = 0
		p.scroll = 0
	}
	// clear any prior load/error status once the catalog loads
	switch p.status {
	case "loading…", "store daemon offline?", "catalog unavailable — set registry URL":
		p.status = ""
	}
}

func storeGetJSON(path string, v any) error {
	resp, err := storeGetClient.Get(storeAPIBase + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// act runs install/remove on the selected hack, asynchronously so the render
// loop and MIDI thread never block on a multi-second download.
func (p *StorePanel) act(verb, id string) {
	p.mu.Lock()
	if p.busy || id == "" {
		p.mu.Unlock()
		return
	}
	p.busy = true
	p.status = verb + "ing " + id + "…"
	p.mu.Unlock()

	go func() {
		ok := storeAct(verb, id)
		p.mu.Lock()
		p.busy = false
		if ok {
			p.status = verb + " ok: " + id
		} else {
			p.status = verb + " FAILED: " + id
		}
		p.mu.Unlock()
		p.refresh() // reload installed set (keeps status unless it was a load msg)
	}()
}

func storeAct(verb, id string) bool {
	u := storeAPIBase + "/api/" + verb + "?id=" + url.QueryEscape(id)
	resp, err := storeActClient.Post(u, "", nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var r struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return false
	}
	return r.OK
}

// ── input ─────────────────────────────────────────────────────────────────────

func (p *StorePanel) moveCursor(d int) { // caller holds p.mu
	n := len(p.hacks)
	if n == 0 {
		return
	}
	p.cursor = clampInt(p.cursor+d, 0, n-1)
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	}
	if p.cursor >= p.scroll+storeVisibleRows {
		p.scroll = p.cursor - storeVisibleRows + 1
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (p *StorePanel) selected() (storeHack, bool) { // caller holds p.mu
	if p.cursor < 0 || p.cursor >= len(p.hacks) {
		return storeHack{}, false
	}
	return p.hacks[p.cursor], true
}

func (p *StorePanel) handleJog(val uint8) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch val {
	case 127: // CW → up (matches FilePanel)
		p.moveCursor(-1)
	case 1: // CCW → down
		p.moveCursor(1)
	}
}

func (p *StorePanel) HandleCC(cc, val uint8) {
	if val != 127 {
		return // press events only
	}
	p.mu.Lock()
	switch cc {
	case CCJogWheel:
		p.mu.Unlock()
		return // handled by handleJog
	case CCDPadDown:
		p.moveCursor(1)
		p.mu.Unlock()
	case CCDPadUp:
		p.moveCursor(-1)
		p.mu.Unlock()
	case CCScreenBot1, CCJogPress, CCDPadCenter, CCDPadRight: // INSTALL (safe: no-op if present)
		h, ok := p.selected()
		on := ok && p.installed[h.ID]
		p.mu.Unlock()
		if ok && !on {
			p.act("install", h.ID)
		}
	case CCScreenBot2: // REMOVE (only acts if installed)
		h, ok := p.selected()
		on := ok && p.installed[h.ID]
		p.mu.Unlock()
		if ok && on {
			p.act("remove", h.ID)
		}
	default:
		p.mu.Unlock()
	}
}

// ── render ────────────────────────────────────────────────────────────────────

func (p *StorePanel) Render(img *image.NRGBA) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Self-heal: while this tab is visible, re-poll every 10s so a registry
	// fixed after startup shows up without toggling the Shadow UI. Set the
	// timestamp before spawning so frames don't stampede refreshes.
	if !p.busy && time.Since(p.lastRefresh) > 10*time.Second {
		p.lastRefresh = time.Now()
		go p.refresh()
	}

	rows := make([]widgets.ListRow, len(p.hacks))
	for i, h := range p.hacks {
		text := fmt.Sprintf("%s  v%s", h.Name, h.Version)
		tc := widgets.Default.White
		if p.installed[h.ID] {
			text += "  [installed]"
			tc = widgets.Default.OnColor // green
		}
		rows[i] = widgets.ListRow{Text: text, TextCol: tc}
	}

	crumb := fmt.Sprintf("Store — %d hacks", len(p.hacks))
	widgets.RenderList(img, widgets.Default, widgets.ListView{
		Rows:       rows,
		Cursor:     p.cursor,
		Scroll:     p.scroll,
		Breadcrumb: crumb,
		Status:     p.status, // when non-empty, overrides breadcrumb
		EmptyText:  "No hacks — is the push-store daemon running?",
	}, suiContentY, suiW, storeRowH, suiContentBot)
}

// ── bottom strip ──────────────────────────────────────────────────────────────

func (p *StorePanel) SoftBotStrip() ([8]widgets.SoftButton, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var b [8]widgets.SoftButton
	h, ok := p.selected()
	on := ok && p.installed[h.ID]

	installState := widgets.SoftNeutral
	if ok && !on {
		installState = widgets.SoftOn
	}
	removeState := widgets.SoftNeutral
	if on {
		removeState = widgets.SoftOff
	}
	b[0] = widgets.SoftButton{Label: "Install", State: installState}
	b[1] = widgets.SoftButton{Label: "Remove", State: removeState}

	hint := "jog / ▲▼ move  ·  ● install"
	if p.busy {
		hint = "working…"
	}
	return b, hint
}

func (p *StorePanel) BotLEDColors() [8]uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	h, ok := p.selected()
	on := ok && p.installed[h.ID]
	var c [8]uint8
	if ok && !on {
		c[0] = suiBotGreen // Install lit when actionable
	}
	if on {
		c[1] = suiBotWhite // Remove lit when installed
	}
	return c
}
