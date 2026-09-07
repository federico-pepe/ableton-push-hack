// catalog_panel.go — "CATALOG" tab for push-manager's Shadow UI.
//
// It is a THIN client: it never installs anything itself. All work goes to the
// push-catalog daemon over localhost HTTP (the same daemon that serves the
// web UI), which runs as root and owns the install logic. That keeps one
// install engine behind three faces — CLI, web, and this screen. Requires the
// `push-catalog` hack to be installed and running.
package main

import (
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
)

// push-catalog is one of the three core hacks, so its port is a fixed fact
// of the device rather than something to discover — same reasoning that lets
// ui_tabs.go hand the built-in CATALOG entry a menu-bar link without the
// installed hack having to declare web_ui.
const catalogPort = 7702

var catalogAPIBase = fmt.Sprintf("http://127.0.0.1:%d", catalogPort)

// Rows visible in the content area: (suiContentBot-suiContentY - breadcrumb 13)
// / rowH(18) ≈ 6. Kept as a const so cursor/scroll math needs no render pass.
const catalogRowH = 18
const catalogVisibleRows = 6

// catalogHack mirrors the daemon's /api/catalog JSON entry.
type catalogHack struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	InstalledVersion string   `json:"installed_version"`
	UpdateAvailable  bool     `json:"update_available"`
	Description      string   `json:"description"`
	Author           string   `json:"author"`
	GithubRepo       string   `json:"github_repo"`
	Requires         []string `json:"requires"`
}

type CatalogPanel struct {
	mu        sync.Mutex
	hacks     []catalogHack
	installed map[string]bool
	enabled   map[string]bool
	cursor    int
	scroll    int
	status      string    // transient line shown in the breadcrumb (errors, progress)
	busy        bool      // an install/remove is in flight — block re-entry
	lastRefresh time.Time // for render-triggered auto-refresh while the tab is visible
}

func newCatalogPanel() *CatalogPanel {
	p := &CatalogPanel{installed: map[string]bool{}, enabled: map[string]bool{}, status: "loading..."}
	go p.refresh()
	return p
}

func (p *CatalogPanel) Label() string { return "CATALOG" }

// ── data ──────────────────────────────────────────────────────────────────────

var catalogGetClient = &http.Client{Timeout: 5 * time.Second}
var catalogActClient = &http.Client{Timeout: 3 * time.Minute} // installs download binaries

func (p *CatalogPanel) refresh() {
	var cat []catalogHack
	catErr := catalogGetJSON("/api/catalog", &cat)
	var inst []struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	instErr := catalogGetJSON("/api/installed", &inst) // best-effort

	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRefresh = time.Now()
	if catErr != nil {
		// Distinguish daemon-down from an empty/unreachable catalog: if
		// /api/installed answered, the daemon is fine and the registry is the
		// problem, not the daemon.
		if instErr == nil {
			p.status = "catalog unavailable - set registry URL"
		} else {
			p.status = "catalog daemon offline?"
		}
		return
	}
	p.hacks = cat
	p.installed = make(map[string]bool, len(inst))
	p.enabled = make(map[string]bool, len(inst))
	for _, h := range inst {
		p.installed[h.ID] = true
		p.enabled[h.ID] = h.Enabled
	}
	if p.cursor >= len(cat) {
		p.cursor = 0
		p.scroll = 0
	}
	// clear any prior load/error status once the catalog loads
	switch p.status {
	case "loading...", "catalog daemon offline?", "catalog unavailable - set registry URL":
		p.status = ""
	}
}

func catalogGetJSON(path string, v any) error {
	resp, err := catalogGetClient.Get(catalogAPIBase + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// act runs install/remove/enable/disable on the selected hack, asynchronously
// so the render loop and MIDI thread never block on a multi-second download.
func (p *CatalogPanel) act(verb, id string) {
	p.mu.Lock()
	if p.busy || id == "" {
		p.mu.Unlock()
		return
	}
	p.busy = true
	p.status = verb + "ing " + id + "..."
	p.mu.Unlock()

	go func() {
		ok := catalogAct(verb, id)
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

func catalogAct(verb, id string) bool {
	u := catalogAPIBase + "/api/" + verb + "?id=" + url.QueryEscape(id)
	resp, err := catalogActClient.Post(u, "", nil)
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

func (p *CatalogPanel) moveCursor(d int) { // caller holds p.mu
	n := len(p.hacks)
	if n == 0 {
		return
	}
	p.cursor = clampInt(p.cursor+d, 0, n-1)
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	}
	if p.cursor >= p.scroll+catalogVisibleRows {
		p.scroll = p.cursor - catalogVisibleRows + 1
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

func (p *CatalogPanel) selected() (catalogHack, bool) { // caller holds p.mu
	if p.cursor < 0 || p.cursor >= len(p.hacks) {
		return catalogHack{}, false
	}
	return p.hacks[p.cursor], true
}

func (p *CatalogPanel) handleJog(val uint8) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch val {
	case 127: // CW → up (matches FilePanel)
		p.moveCursor(-1)
	case 1: // CCW → down
		p.moveCursor(1)
	}
}

func (p *CatalogPanel) HandleCC(cc, val uint8) {
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
	case CCScreenBot1, CCJogPress, CCDPadCenter, CCDPadRight: // INSTALL / UPDATE (no-op if present and current)
		h, ok := p.selected()
		on := ok && p.installed[h.ID]
		update := ok && on && h.UpdateAvailable
		p.mu.Unlock()
		if ok && (!on || update) {
			p.act("install", h.ID)
		}
	case CCScreenBot2: // REMOVE (only acts if installed)
		h, ok := p.selected()
		on := ok && p.installed[h.ID]
		p.mu.Unlock()
		if ok && on {
			p.act("remove", h.ID)
		}
	case CCScreenBot3: // ENABLE/DISABLE (only acts if installed)
		h, ok := p.selected()
		on := ok && p.installed[h.ID]
		en := ok && p.enabled[h.ID]
		p.mu.Unlock()
		if ok && on {
			if en {
				p.act("disable", h.ID)
			} else {
				p.act("enable", h.ID)
			}
		}
	default:
		p.mu.Unlock()
	}
}

// ── render ────────────────────────────────────────────────────────────────────

func (p *CatalogPanel) Render(img *image.NRGBA) {
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
			if h.UpdateAvailable {
				text += fmt.Sprintf("  [update: v%s]", h.InstalledVersion)
				tc = widgets.Default.Accent // stands out against installed/available
			} else {
				text += "  [installed]"
				tc = widgets.Default.OnColor // green
			}
			if !p.enabled[h.ID] {
				text += " [disabled]"
				tc = widgets.Default.White // disabled overrides the green/accent installed tint
			}
		}
		rows[i] = widgets.ListRow{Text: text, TextCol: tc}
	}

	// Breadcrumb doubles as the detail line for whatever is under the
	// cursor: who maintains this hack and which repo it installs from. The
	// hack count was the same number the list already shows.
	crumb := fmt.Sprintf("Catalog - %d hacks", len(p.hacks))
	if h, ok := p.selected(); ok {
		crumb = catalogOrigin(h)
	}
	widgets.RenderList(img, widgets.Default, widgets.ListView{
		Rows:       rows,
		Cursor:     p.cursor,
		Scroll:     p.scroll,
		Breadcrumb: crumb,
		Status:     p.status, // when non-empty, overrides breadcrumb
		EmptyText:  "No hacks - is the push-catalog daemon running?",
	}, suiContentY, suiW, catalogRowH, suiContentBot)
}

// catalogOrigin is the "who made this / where does it come from" line.
// Author is dropped when it is just the repo owner again, so the common case
// reads as one thing rather than the same name twice.
func catalogOrigin(h catalogHack) string {
	owner, _, _ := strings.Cut(h.GithubRepo, "/")
	switch {
	case h.GithubRepo != "" && (h.Author == "" || h.Author == owner):
		return h.GithubRepo
	case h.GithubRepo != "":
		return fmt.Sprintf("%s - by %s", h.GithubRepo, h.Author)
	case h.Author != "":
		return "by " + h.Author
	}
	return "Catalog"
}

// ── bottom strip ──────────────────────────────────────────────────────────────

func (p *CatalogPanel) SoftBotStrip() ([8]widgets.SoftButton, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var b [8]widgets.SoftButton
	h, ok := p.selected()
	on := ok && p.installed[h.ID]
	en := ok && p.enabled[h.ID]
	update := ok && on && h.UpdateAvailable

	installLabel := "Install"
	installState := widgets.SoftNeutral
	switch {
	case ok && !on:
		installState = widgets.SoftOn
	case update:
		installLabel = "Update"
		installState = widgets.SoftOn
	}
	removeState := widgets.SoftNeutral
	if on {
		removeState = widgets.SoftOff
	}
	b[0] = widgets.SoftButton{Label: installLabel, State: installState}
	b[1] = widgets.SoftButton{Label: "Remove", State: removeState}

	toggleLabel := "Enable"
	toggleState := widgets.SoftNeutral
	if on {
		if en {
			toggleLabel = "Disable"
			toggleState = widgets.SoftOff
		} else {
			toggleState = widgets.SoftOn
		}
	}
	b[2] = widgets.SoftButton{Label: toggleLabel, State: toggleState}

	hint := "jog / up-down move, press to install"
	if p.busy {
		hint = "working..."
	}
	return b, hint
}

func (p *CatalogPanel) BotLEDColors() [8]uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	h, ok := p.selected()
	on := ok && p.installed[h.ID]
	en := ok && p.enabled[h.ID]
	update := ok && on && h.UpdateAvailable
	var c [8]uint8
	if ok && (!on || update) {
		c[0] = suiBotGreen // Install/Update lit when actionable
	}
	if on {
		c[1] = suiBotWhite // Remove lit when installed
		if en {
			c[2] = suiBotWhite // Disable lit when currently enabled
		} else {
			c[2] = suiBotGreen // Enable lit when currently disabled
		}
	}
	return c
}
