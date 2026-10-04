// plugins_panel.go — "PLUGINS" tab for push-manager's Shadow UI.
//
// Push's own browser hides VST3 plugins, but Live still loads them. This tab
// lists the plugins Live has scanned, and the preset files it has indexed for
// each, then loads the pick onto the selected track. Everything goes through
// Browser Bridge's list_plugins and load_plugin commands (see live_bridge.go),
// so it needs the browser-bridge hack. Two levels: plugins, then one plugin's
// presets (first row is the plugin's own default patch).
package main

import (
	"fmt"
	"image"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
)

// Same row geometry as the other list panels.
const pluginsRowH = 18
const pluginsVisibleRows = 6

// First row of the preset level. Not a real preset name, so it is never sent.
const pluginsDefaultRow = "(default patch)"

type PluginsPanel struct {
	mu          sync.Mutex
	plugins     []bridgePlugin
	inPresets   bool // false: plugin list; true: presets of plugins[pluginIdx]
	pluginIdx   int  // remembered while inside the preset level
	cursor      int
	scroll      int
	savedCursor int // list cursor/scroll to restore on back
	savedScroll int
	status      string
	busy        bool
	lastRefresh time.Time
}

func newPluginsPanel() *PluginsPanel {
	p := &PluginsPanel{status: "loading..."}
	go p.refresh()
	return p
}

func (p *PluginsPanel) Label() string { return "PLUGINS" }

// ── data ──────────────────────────────────────────────────────────────────────

func (p *PluginsPanel) refresh() {
	plugins, err := livePlugins()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRefresh = time.Now()
	if err != nil {
		p.status = "Browser Bridge offline or too old?"
		return
	}
	p.plugins = plugins
	if p.inPresets && p.pluginIdx >= len(plugins) {
		p.inPresets = false
	}
	if n := p.rowCount(); p.cursor >= n {
		p.cursor, p.scroll = 0, 0
	}
	switch p.status {
	case "loading...", "Browser Bridge offline or too old?":
		p.status = ""
	}
}

// rowCount is the length of the visible level. Caller holds p.mu.
func (p *PluginsPanel) rowCount() int {
	if p.inPresets {
		return len(p.plugins[p.pluginIdx].Presets) + 1
	}
	return len(p.plugins)
}

// load sends the load command without blocking the MIDI or render thread.
func (p *PluginsPanel) load(plugin, preset string) {
	p.mu.Lock()
	if p.busy {
		p.mu.Unlock()
		return
	}
	p.busy = true
	label := plugin
	if preset != "" {
		label += " / " + preset
	}
	p.status = "loading " + label + "..."
	p.mu.Unlock()

	go func() {
		err := livePluginLoad(plugin, preset)
		p.mu.Lock()
		p.busy = false
		if err != nil {
			p.status = "load FAILED: " + label
			p.mu.Unlock()
			return
		}
		p.status = "sent: " + label
		p.mu.Unlock()
		// Same as the preset Browser: leave the Shadow UI so the loaded
		// device can be played. The socket OK is only an enqueue ack.
		time.Sleep(500 * time.Millisecond)
		shadowUIExitAfterLoad(label)
	}()
}

// ── input ─────────────────────────────────────────────────────────────────────

func (p *PluginsPanel) moveCursor(d int) { // caller holds p.mu
	n := p.rowCount()
	if n == 0 {
		return
	}
	p.cursor = clampInt(p.cursor+d, 0, n-1)
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	}
	if p.cursor >= p.scroll+pluginsVisibleRows {
		p.scroll = p.cursor - pluginsVisibleRows + 1
	}
}

// enter acts on the cursor row: open a plugin's presets, or load. In the
// plugin list, loadNow loads the plugin itself instead of opening it.
func (p *PluginsPanel) enter(loadNow bool) {
	p.mu.Lock()
	if !p.inPresets {
		if p.cursor < 0 || p.cursor >= len(p.plugins) {
			p.mu.Unlock()
			return
		}
		pl := p.plugins[p.cursor]
		if loadNow || len(pl.Presets) == 0 {
			p.mu.Unlock()
			p.load(pl.Name, "")
			return
		}
		p.savedCursor, p.savedScroll = p.cursor, p.scroll
		p.pluginIdx = p.cursor
		p.inPresets = true
		p.cursor, p.scroll = 0, 0
		p.mu.Unlock()
		return
	}
	pl := p.plugins[p.pluginIdx]
	preset := ""
	if p.cursor > 0 && p.cursor-1 < len(pl.Presets) {
		preset = pl.Presets[p.cursor-1]
	}
	p.mu.Unlock()
	p.load(pl.Name, preset)
}

func (p *PluginsPanel) back() { // caller holds p.mu
	if !p.inPresets {
		return
	}
	p.inPresets = false
	p.cursor, p.scroll = p.savedCursor, p.savedScroll
}

func (p *PluginsPanel) handleJog(val uint8) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch val {
	case 127: // CW → up (same as the other list panels)
		p.moveCursor(-1)
	case 1: // CCW → down
		p.moveCursor(1)
	}
}

func (p *PluginsPanel) HandleCC(cc, val uint8) {
	if val != 127 {
		return // press events only
	}
	switch cc {
	case CCJogWheel:
		return // handled by handleJog
	case CCDPadDown:
		p.mu.Lock()
		p.moveCursor(1)
		p.mu.Unlock()
	case CCDPadUp:
		p.mu.Lock()
		p.moveCursor(-1)
		p.mu.Unlock()
	case CCJogPress, CCDPadCenter, CCDPadRight: // open, or load
		p.enter(false)
	case CCScreenBot1: // LOAD (plugin list: load now, do not open)
		p.enter(true)
	case CCJogClickLeft, CCDPadLeft, CCScreenBot2: // BACK
		p.mu.Lock()
		p.back()
		p.mu.Unlock()
	case CCScreenBot3: // REFRESH
		go p.refresh()
	}
}

// ── render ────────────────────────────────────────────────────────────────────

func (p *PluginsPanel) Render(img *image.NRGBA) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Self-heal: while the tab is visible, re-poll every 10s so a plugin
	// scanned or a preset indexed after startup shows up without a restart.
	if !p.busy && time.Since(p.lastRefresh) > 10*time.Second {
		p.lastRefresh = time.Now()
		go p.refresh()
	}

	var rows []widgets.ListRow
	crumb := fmt.Sprintf("Plugins - %d", len(p.plugins))
	if p.inPresets {
		pl := p.plugins[p.pluginIdx]
		rows = append(rows, widgets.ListRow{Text: pluginsDefaultRow, TextCol: widgets.Default.White})
		for _, name := range pl.Presets {
			rows = append(rows, widgets.ListRow{Text: name, TextCol: widgets.Default.White})
		}
		crumb = pl.Name
		if pl.Vendor != "" {
			crumb = pl.Vendor + " - " + pl.Name
		}
	} else {
		for _, pl := range p.plugins {
			text := pl.Name
			if n := len(pl.Presets); n > 0 {
				text += fmt.Sprintf("  (%d presets)", n)
			}
			rows = append(rows, widgets.ListRow{Text: text, TextCol: widgets.Default.White})
		}
	}
	widgets.RenderList(img, widgets.Default, widgets.ListView{
		Rows:       rows,
		Cursor:     p.cursor,
		Scroll:     p.scroll,
		Breadcrumb: crumb,
		Status:     p.status, // when non-empty, overrides breadcrumb
		EmptyText:  "No plugins - turn on VST3 scanning (docs/vst3-on-push3.md)",
	}, suiContentY, suiW, pluginsRowH, suiContentBot)
}

// ── bottom strip ──────────────────────────────────────────────────────────────

func (p *PluginsPanel) SoftBotStrip() ([8]widgets.SoftButton, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var b [8]widgets.SoftButton
	b[0] = widgets.SoftButton{Label: "Load", State: widgets.SoftOn}
	backState := widgets.SoftNeutral
	if p.inPresets {
		backState = widgets.SoftOn
	}
	b[1] = widgets.SoftButton{Label: "Back", State: backState}
	b[2] = widgets.SoftButton{Label: "Refresh", State: widgets.SoftNeutral}

	hint := "jog / up-down move, press to open"
	if p.inPresets {
		hint = "press to load, left to go back"
	}
	if p.busy {
		hint = "working..."
	}
	return b, hint
}

func (p *PluginsPanel) BotLEDColors() [8]uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var c [8]uint8
	c[0] = suiBotGreen // Load always lit: it acts on the cursor row
	if p.inPresets {
		c[1] = suiBotWhite
	}
	c[2] = suiBotWhite
	return c
}
