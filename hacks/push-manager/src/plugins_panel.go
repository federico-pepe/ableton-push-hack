// plugins_panel.go — "PLUGINS" tab for push-manager's Shadow UI.
//
// Push's own browser hides VST3 plugins, but Live still loads them. This tab
// lists the plugins Live has scanned, and the preset files it has indexed for
// each, then loads the pick onto the selected track. Everything goes through
// the PushHack Remote Script's list_plugins and load_plugin commands (see
// live_bridge.go), so it needs that script. Levels: folders (see package
// plugintree and /data/push-hack/plugin-folders.json), then one plugin's
// presets (first row is the plugin's own default patch). A folder lists its
// sub-folders first, then its plugins.
package main

import (
	"fmt"
	"image"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"

	"push-manager/plugintree"
	"push-manager/vst3scan"
)

// Same row geometry as the other list panels.
const pluginsRowH = 18
const pluginsVisibleRows = 6

// First row of the preset level. Not a real preset name, so it is never sent.
const pluginsDefaultRow = "(default patch)"

// Same icons as Push's own Browser, loaded from the Push3 assets.
const (
	pluginsFolderIcon = "Sidebar_Folder.png"
	pluginsPluginIcon = "Browser_VST3Effect.png"
)

// navPos is the cursor of a folder level, kept while a child is open.
type navPos struct{ cursor, scroll int }

type PluginsPanel struct {
	mu          sync.Mutex
	root        *plugintree.Node
	trail       []string // folder names from the root to the open folder
	saved       []navPos // one per folder opened, same order as trail
	note        string   // folders file problem, shown when no status is set
	inPresets   bool     // false: folder level; true: presets of cur().Plugins[pluginIdx]
	pluginIdx   int      // index in the open folder's Plugins
	cursor      int
	scroll      int
	savedCursor int // folder cursor/scroll to restore when leaving the presets
	savedScroll int
	status      string
	busy        bool
	lastRefresh time.Time

	// VST3 scan switch on button 8 (vst3.go). Two presses, because Live restarts.
	vst3State  string // on | off | unknown; empty until the first read
	vst3Busy   bool
	vst3Prompt vst3scan.Prompt
}

func newPluginsPanel() *PluginsPanel {
	p := &PluginsPanel{status: "loading...", vst3Prompt: vst3scan.Prompt{TTL: vst3ConfirmTTL}}
	go p.refresh()
	return p
}

func (p *PluginsPanel) Label() string { return "PLUGINS" }

// ── data ──────────────────────────────────────────────────────────────────────

func (p *PluginsPanel) refresh() {
	tree, note, err := loadPluginTree()
	st := vst3.State() // file reads: outside the lock
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRefresh = time.Now()
	p.vst3State, p.vst3Busy = st.State, st.Busy
	if !st.Busy && p.status == vst3RestartMsg {
		p.status = ""
	}
	if err != nil {
		p.status = "PushHack offline or too old?"
		return
	}
	p.root, p.note = tree, note
	if p.cur() == nil { // the open folder is gone: back to the top
		p.trail, p.saved, p.inPresets = nil, nil, false
		p.cursor, p.scroll = 0, 0
	}
	if cur := p.cur(); p.inPresets && p.pluginIdx >= len(cur.Plugins) {
		p.inPresets = false
	}
	if n := p.rowCount(); p.cursor >= n {
		p.cursor, p.scroll = 0, 0
	}
	switch p.status {
	case "loading...", "PushHack offline or too old?":
		p.status = ""
	}
}

// cur is the open folder, nil when the tree is not loaded or the trail is
// stale. Caller holds p.mu.
func (p *PluginsPanel) cur() *plugintree.Node {
	if p.root == nil {
		return nil
	}
	return p.root.Find(p.trail)
}

// rowCount is the length of the visible level. Caller holds p.mu.
func (p *PluginsPanel) rowCount() int {
	cur := p.cur()
	if cur == nil {
		return 0
	}
	if p.inPresets {
		return len(cur.Plugins[p.pluginIdx].Presets) + 1
	}
	return len(cur.Folders) + len(cur.Plugins)
}

// crumb is "Plugins / A / B", cut at the start when it is too long.
func crumb(trail []string) string {
	s := "Plugins"
	for _, t := range trail {
		s += " / " + asciiOnly(t)
	}
	const max = 60
	if len(s) > max {
		s = "..." + s[len(s)-(max-3):]
	}
	return s
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

// enter acts on the cursor row: open a folder, open a plugin's presets, or
// load. In a folder, loadNow loads a plugin itself instead of opening it; on
// a folder row it still opens the folder.
func (p *PluginsPanel) enter(loadNow bool) {
	p.mu.Lock()
	cur := p.cur()
	if cur == nil {
		p.mu.Unlock()
		return
	}
	if !p.inPresets {
		nf := len(cur.Folders)
		if p.cursor < 0 || p.cursor >= nf+len(cur.Plugins) {
			p.mu.Unlock()
			return
		}
		if p.cursor < nf {
			p.saved = append(p.saved, navPos{p.cursor, p.scroll})
			p.trail = append(p.trail, cur.Folders[p.cursor].Name)
			p.cursor, p.scroll = 0, 0
			p.mu.Unlock()
			return
		}
		pl := cur.Plugins[p.cursor-nf]
		if loadNow || len(pl.Presets) == 0 {
			p.mu.Unlock()
			p.load(pl.Name, "")
			return
		}
		p.savedCursor, p.savedScroll = p.cursor, p.scroll
		p.pluginIdx = p.cursor - nf
		p.inPresets = true
		p.cursor, p.scroll = 0, 0
		p.mu.Unlock()
		return
	}
	pl := cur.Plugins[p.pluginIdx]
	preset := ""
	if p.cursor > 0 && p.cursor-1 < len(pl.Presets) {
		preset = pl.Presets[p.cursor-1]
	}
	p.mu.Unlock()
	p.load(pl.Name, preset)
}

// back leaves the presets, else goes up one folder. At the top it does nothing.
func (p *PluginsPanel) back() { // caller holds p.mu
	if p.inPresets {
		p.inPresets = false
		p.cursor, p.scroll = p.savedCursor, p.savedScroll
		return
	}
	if n := len(p.trail); n > 0 {
		pos := p.saved[n-1]
		p.trail, p.saved = p.trail[:n-1], p.saved[:n-1]
		p.cursor, p.scroll = pos.cursor, pos.scroll
	}
}

func (p *PluginsPanel) handleJog(val uint8) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vst3Prompt.Cancel()
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
	if cc == CCScreenBot8 {
		p.vst3Press()
		return
	}
	p.mu.Lock()
	p.vst3Prompt.Cancel() // any other key cancels the VST3 prompt
	p.mu.Unlock()
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
	title := crumb(p.trail)
	if cur := p.cur(); cur != nil {
		if p.inPresets {
			pl := cur.Plugins[p.pluginIdx]
			rows = append(rows, widgets.ListRow{Text: pluginsDefaultRow, TextCol: widgets.Default.White})
			for _, name := range pl.Presets {
				rows = append(rows, widgets.ListRow{Text: name, TextCol: widgets.Default.White})
			}
			title = pl.Name
			if pl.Vendor != "" {
				title = pl.Vendor + " - " + pl.Name
			}
		} else {
			for _, f := range cur.Folders {
				rows = append(rows, widgets.ListRow{Icon: loadSuiIcon(pluginsFolderIcon), Text: fmt.Sprintf("%s  %d", asciiOnly(f.Name), f.Count()), TextCol: widgets.Default.White})
			}
			for _, pl := range cur.Plugins {
				text := pl.Name
				if n := len(pl.Presets); n > 0 {
					text += fmt.Sprintf("  (%d presets)", n)
				}
				rows = append(rows, widgets.ListRow{Icon: loadSuiIcon(pluginsPluginIcon), Text: text, TextCol: widgets.Default.White})
			}
		}
	}
	status := p.status // when non-empty, overrides the breadcrumb
	if status == "" {
		status = p.note
	}
	widgets.RenderList(img, widgets.Default, widgets.ListView{
		Rows:       rows,
		Cursor:     p.cursor,
		Scroll:     p.scroll,
		Breadcrumb: title,
		Status:     status,
		EmptyText:  "No plugins - turn on VST3 scanning",
	}, suiContentY, suiW, pluginsRowH, suiContentBot)
	if p.vst3Prompt.Active(time.Now()) {
		p.renderVst3Prompt(img)
	}
}

// ── bottom strip ──────────────────────────────────────────────────────────────

func (p *PluginsPanel) SoftBotStrip() ([8]widgets.SoftButton, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var b [8]widgets.SoftButton
	b[0] = widgets.SoftButton{Label: "Load", State: widgets.SoftOn}
	backState := widgets.SoftNeutral
	if p.inPresets || len(p.trail) > 0 {
		backState = widgets.SoftOn
	}
	b[1] = widgets.SoftButton{Label: "Back", State: backState}
	b[2] = widgets.SoftButton{Label: "Refresh", State: widgets.SoftNeutral}

	b[7] = p.vst3Button()

	hint := "jog / up-down move, press to open"
	if p.inPresets {
		hint = "press to load, left to go back"
	}
	if p.busy {
		hint = "working..."
	}
	if p.vst3Prompt.Active(time.Now()) {
		hint = "Live restarts"
	}
	return b, hint
}

func (p *PluginsPanel) BotLEDColors() [8]uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var c [8]uint8
	c[0] = suiBotGreen // Load always lit: it acts on the cursor row
	if p.inPresets || len(p.trail) > 0 {
		c[1] = suiBotWhite
	}
	c[2] = suiBotWhite
	switch {
	case p.vst3Prompt.Active(time.Now()):
		c[7] = suiBotGreen
	case p.vst3State == "on" || p.vst3State == "off":
		c[7] = suiBotWhite
	}
	return c
}

// ── VST3 scan switch (button 8) ──────────────────────────────────────────────

const (
	vst3ConfirmTTL = 8 * time.Second
	vst3RestartMsg = "Restarting Live..."
)

// vst3Button is the label of button 8. Caller holds p.mu.
func (p *PluginsPanel) vst3Button() widgets.SoftButton {
	switch {
	case p.vst3Busy:
		return widgets.SoftButton{Label: "..."}
	case p.vst3Prompt.Active(time.Now()):
		return widgets.SoftButton{Label: "CONFIRM?", State: widgets.SoftConfirm}
	case p.vst3State == "on":
		return widgets.SoftButton{Label: "VST3 ON", State: widgets.SoftOn}
	case p.vst3State == "off":
		return widgets.SoftButton{Label: "VST3 OFF", State: widgets.SoftOff}
	}
	return widgets.SoftButton{} // unknown layout or not read yet: no action
}

// vst3Press: first press asks, second press changes the flag and restarts Live.
func (p *PluginsPanel) vst3Press() {
	p.mu.Lock()
	if p.vst3Busy || (p.vst3State != "on" && p.vst3State != "off") {
		p.mu.Unlock()
		return
	}
	want := p.vst3State != "on"
	confirmed := p.vst3Prompt.Press(time.Now())
	if confirmed {
		p.vst3Busy = true
		p.status = vst3RestartMsg
	}
	p.mu.Unlock()
	if !confirmed {
		time.AfterFunc(vst3ConfirmTTL+200*time.Millisecond, shadowUIRefreshBotLEDs)
		go shadowUIRefreshBotLEDs()
		return
	}
	go func() {
		restarting, err := vst3.Set(want)
		p.mu.Lock()
		if err != nil || !restarting {
			p.vst3Busy = false
			p.status = ""
			if err != nil {
				p.status = asciiOnly(err.Error())
			}
		}
		p.mu.Unlock()
		shadowUIRefreshBotLEDs()
	}()
}

// renderVst3Prompt covers the list with the restart notice. Caller holds p.mu.
func (p *PluginsPanel) renderVst3Prompt(img *image.NRGBA) {
	fillRect(img, 0, suiContentY, suiW, suiContentH, suiBlack)
	fillRect(img, 0, suiContentY, suiW, 2, suiAccent)
	what := "on"
	if p.vst3State == "on" {
		what = "off"
	}
	lines := []string{
		"Turn VST3 scanning " + what + "?",
		"Live restarts. The current set is lost.",
		"The screen and MIDI are back in about 30 s.",
	}
	if what == "off" {
		lines = append(lines, "Presets of VST3 plugins stop loading until scanning is on.")
	}
	lines = append(lines, "", "Press button 8 again to confirm. Any other key cancels.")
	y := suiContentY + 20
	for i, l := range lines {
		col := suiWhite
		if i == 0 {
			col = suiAccent
		}
		drawText(img, 16, y, l, col)
		y += 17
	}
}
