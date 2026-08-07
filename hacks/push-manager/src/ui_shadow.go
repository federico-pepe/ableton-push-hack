// ui_shadow.go — Shadow UI: hardware-driven interface rendered on Push 3 display.
//
// Activated when MIDI intercept is turned ON; deactivated when intercept turns OFF.
//
// Input mapping:
//   Jog wheel CW (CC70 val=127)       scroll / move down
//   Jog wheel CCW (CC70 val=1)        scroll / move up
//   Jog wheel press (CC94)            enter / select
//   Jog wheel click-left (CC93)       back
//   D-pad up/down (CC46/47)           scroll / move
//   D-pad right (CC45)                enter / select
//   D-pad center (CC91)               enter / select
//   D-pad left (CC44)                 back
//   Top button 1 (CC102)              Files panel
//   Top button 2 (CC103)              Stats panel
//
// Display layout (960×160):
//   y=0..17    top strip  — panel tabs (18px)
//   y=18..141  content    — panel content (124px)
//   y=142..159 bot strip  — action hints (18px)

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	gtext "github.com/federico-pepe/ableton-push-hack/core/gfx/text"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

// ── Layout constants ──────────────────────────────────────────────────────────

const (
	suiW          = push3.VisW
	suiH          = push3.VisH
	suiTopH       = 18  // top strip height
	suiBotH       = 18  // bottom strip height
	suiContentY   = suiTopH                  // content starts at y=18
	suiContentBot = suiH - suiBotH           // content ends at y=142
	suiContentH   = suiContentBot - suiTopH  // 124px
	suiColW       = 120                      // 960/8 per column
)

// ── Colors ────────────────────────────────────────────────────────────────────

var (
	suiBlack    = color.NRGBA{0, 0, 0, 255}
	suiWhite    = color.NRGBA{255, 255, 255, 255}
	suiGray     = color.NRGBA{120, 120, 120, 255}
	suiDarkGray = color.NRGBA{30, 30, 30, 255}
	suiSelect   = color.NRGBA{0, 90, 200, 255}
	suiDirColor = color.NRGBA{180, 210, 255, 255}
	suiAccent   = color.NRGBA{200, 40, 40, 255} // red — delete confirm highlight
)

// ── Icon loader ───────────────────────────────────────────────────────────────

const suiIconBase = "/opt/push3/products/push3/assets/Images/Browser/"
const suiIconH = 13 // target icon height in pixels

var (
	suiIconCache   = map[string]*image.NRGBA{}
	suiIconCacheMu sync.Mutex
)

// loadSuiIcon loads, scales, and caches a Push Browser PNG icon by filename.
// Returns nil if the file cannot be read or decoded.
func loadSuiIcon(name string) *image.NRGBA {
	suiIconCacheMu.Lock()
	if img, ok := suiIconCache[name]; ok {
		suiIconCacheMu.Unlock()
		return img
	}
	suiIconCacheMu.Unlock()

	f, err := os.Open(suiIconBase + name)
	if err != nil {
		return nil
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return nil
	}

	// Scale to suiIconH maintaining aspect ratio
	sb := src.Bounds()
	dstW := sb.Dx() * suiIconH / sb.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, suiIconH))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, sb, xdraw.Over, nil)

	suiIconCacheMu.Lock()
	suiIconCache[name] = dst
	suiIconCacheMu.Unlock()
	return dst
}

// iconNameForEntry returns the Browser PNG filename for a file panel entry.
func iconNameForEntry(e filePanelEntry) string {
	if e.isUSB {
		return "Sidebar_Computer.png"
	}
	if e.isDir {
		n := strings.ToLower(e.name)
		if strings.HasSuffix(n, " project") || strings.HasSuffix(n, " project files") || strings.HasSuffix(n, ".als project") {
			return "Sidebar_CurrentProject.png"
		}
		return "Sidebar_Folder.png"
	}
	switch strings.ToLower(filepath.Ext(e.name)) {
	case ".als":
		return "Set.png"
	case ".asd":
		return "DefaultSet.png"
	case ".wav", ".aif", ".aiff", ".flac", ".mp3", ".ogg", ".m4a", ".opus", ".aac":
		return "Audio.png"
	}
	return ""
}

// drawIcon composites a cached icon onto img at pixel (x, y).
func drawIcon(img *image.NRGBA, icon *image.NRGBA, x, y int) {
	gfx.DrawIcon(img, icon, x, y)
}

// ── Panel interface ────────────────────────────────────────────────────────────

// jogHandler is implemented by panels that respond to jog-wheel rotation
// (which arrives as CCJogWheel val=127 CW / val=1 CCW, not a normal press).
type jogHandler interface{ handleJog(uint8) }

// extraBots is implemented by panels that use more than the 4 primary
// under-screen soft-buttons. Slices are indexed 0-based from button 5
// (CCScreenBot5); a nil/empty slice or a 0 LED clears the button.
type extraBots interface {
	extraBotStrip() []string // labels for buttons 5..8
	extraBotLEDs() []uint8   // matching LED palette indices (0 = off)
}

// browsePanelIdx is the index of the BrowserPanel within shadowUI.panels.
const browsePanelIdx = 3

type Panel interface {
	Render(img *image.NRGBA)
	HandleCC(cc, val uint8)
	Label() string
	// BotStrip returns up to 4 action labels for bottom buttons (CC20-23)
	// and a navigation hint shown on the right side of the bottom strip.
	BotStrip() ([4]string, string)
	// BotLEDColors returns the LED color (0–127) for each of the 4 bottom buttons.
	// 0 = off; non-zero = lit at that palette index. Called on panel activation
	// and whenever state changes (e.g. clipboard filled, USB cursor moved).
	BotLEDColors() [4]uint8
}

// ── ShadowUI ──────────────────────────────────────────────────────────────────

type ShadowUI struct {
	mu       sync.Mutex
	active   bool
	panels   []Panel
	panelIdx int
	stopCh   chan struct{}
}

var shadowUI = &ShadowUI{}

// shadowUIStart activates the shadow UI: sets display takeover mode and starts
// the render loop. Safe to call multiple times — no-op if already active.
// shadowTabColor is the LED color for the active panel tab button.
const shadowTabColor = 122 // WHITE_MIDI_VALUE — standard lit-white CC button

// Shadow UI under-screen soft-button LED colors.
const (
	suiBotWhite = uint8(122) // resting lit-white
	suiBotGreen = uint8(11)  // active / momentary-press feedback
)

// shadowTabColor is the LED color for the active panel tab button.
// (already declared above — kept here for reference)

// shadowRegisterLEDs registers shadow UI LED configs and lights initial state.
// - CCScreenTop1/2/3: exclusive group "shadow-tabs" (tabs switch panels)
// - CCSettings (CC30): exclusive solo group "settings-anchor" — single press
//   re-sends same value, keeping the anchor LED lit while shadow UI is active
// Must be called without ledConfigMu held.
func shadowRegisterLEDs(activePanelIdx int) {
	ledConfigMu.Lock()
	ledConfigs[CCScreenTop1] = LEDConfig{Mode: LEDModeExclusive, Color: shadowTabColor, Group: "shadow-tabs"}
	ledConfigs[CCScreenTop2] = LEDConfig{Mode: LEDModeExclusive, Color: shadowTabColor, Group: "shadow-tabs"}
	ledConfigs[CCScreenTop3] = LEDConfig{Mode: LEDModeExclusive, Color: shadowTabColor, Group: "shadow-tabs"}
	ledConfigs[CCScreenTop4] = LEDConfig{Mode: LEDModeExclusive, Color: shadowTabColor, Group: "shadow-tabs"}
	ledConfigs[CCSettings] = LEDConfig{Mode: LEDModeExclusive, Color: 127, Group: "settings-anchor"}
	ledConfigMu.Unlock()
	// Bottom button LEDs are managed dynamically via updateBotLEDs — no static config needed.

	activeCC := uint8(CCScreenTop1)
	switch activePanelIdx {
	case 1:
		activeCC = CCScreenTop2
	case 2:
		activeCC = CCScreenTop3
	case browsePanelIdx:
		activeCC = CCScreenTop4
	}
	go exclusiveLED(activeCC, shadowTabColor)
}

// shadowUnregisterLEDs removes shadow UI LED configs.
// Tab LEDs are turned off explicitly; CCSettings is cleared by the intercept-OFF
// sequence (clearAllLEDs) so no explicit send needed here.
func shadowUnregisterLEDs() {
	ledConfigMu.Lock()
	delete(ledConfigs, CCScreenTop1)
	delete(ledConfigs, CCScreenTop2)
	delete(ledConfigs, CCScreenTop3)
	delete(ledConfigs, CCScreenTop4)
	delete(ledConfigs, CCSettings)
	ledConfigMu.Unlock()
	sendSeqCC(0, CCScreenTop1, 0) //nolint:errcheck
	sendSeqCC(0, CCScreenTop2, 0) //nolint:errcheck
	sendSeqCC(0, CCScreenTop3, 0) //nolint:errcheck
	sendSeqCC(0, CCScreenTop4, 0) //nolint:errcheck
	// Clear all bottom button LEDs.
	for _, cc := range []uint8{CCScreenBot1, CCScreenBot2, CCScreenBot3, CCScreenBot4,
		CCScreenBot5, CCScreenBot6, CCScreenBot7, CCScreenBot8} {
		sendSeqCC(0, cc, 0) //nolint:errcheck
	}
}

// updateBotLEDs sends the correct LED values for the 4 bottom buttons based on
// the active panel's current state. Call in a goroutine to avoid blocking MIDI.
func updateBotLEDs(panel Panel) {
	colors := panel.BotLEDColors()
	botCCs := [4]uint8{CCScreenBot1, CCScreenBot2, CCScreenBot3, CCScreenBot4}
	for i, cc := range botCCs {
		sendSeqCC(0, cc, int32(colors[i])) //nolint:errcheck
	}
	// Extended soft-buttons (5..8): lit by panels that implement extraBots,
	// else always cleared so a stale LED never lingers after a panel switch.
	var ext []uint8
	if eb, ok := panel.(extraBots); ok {
		ext = eb.extraBotLEDs()
	}
	for i := 0; i < 4; i++ {
		c := uint8(0)
		if i < len(ext) {
			c = ext[i]
		}
		sendSeqCC(0, CCScreenBotN(4+i), int32(c)) //nolint:errcheck
	}
}

func shadowUIStart() {
	shadowUI.mu.Lock()
	defer shadowUI.mu.Unlock()
	if shadowUI.active {
		return
	}
	shadowUI.panels = []Panel{
		newFilePanel(),
		newStatsPanel(),
		newMidiPanel(),
		newBrowserPanel(),
	}
	shadowUI.panelIdx = 0
	shadowUI.stopCh = make(chan struct{})
	shadowUI.active = true
	if err := shmSetMode(2); err != nil {
		log.Printf("shadow_ui: shmSetMode(2): %v", err)
	}
	go shadowUI.renderLoop()
	shadowRegisterLEDs(0)
	go updateBotLEDs(shadowUI.panels[0])
	log.Printf("shadow_ui: started")
}

// shadowUIStop deactivates the shadow UI and restores passthrough display mode.
func shadowUIStop() {
	shadowUI.mu.Lock()
	defer shadowUI.mu.Unlock()
	if !shadowUI.active {
		return
	}
	shadowUI.active = false
	close(shadowUI.stopCh)
	if err := shmSetMode(0); err != nil {
		log.Printf("shadow_ui: shmSetMode(0): %v", err)
	}
	shadowUnregisterLEDs()
	log.Printf("shadow_ui: stopped")
}

// shadowUISwitchToBrowse switches the active shadow UI to the Browse panel and
// lights its tab. No-op if the shadow UI is not running.
func shadowUISwitchToBrowse() {
	shadowUI.mu.Lock()
	if !shadowUI.active || len(shadowUI.panels) <= browsePanelIdx {
		shadowUI.mu.Unlock()
		return
	}
	shadowUI.panelIdx = browsePanelIdx
	panel := shadowUI.panels[browsePanelIdx]
	shadowUI.mu.Unlock()
	go exclusiveLED(uint8(CCScreenTop4), shadowTabColor)
	go updateBotLEDs(panel)
}

// shadowUIExitAfterLoad closes the shadow UI after a preset is loaded — turns
// MIDI intercept OFF (so pads/keys play the new device) and stops the UI,
// mirroring the intercept-OFF path. Shows a brief confirmation OSD.
func shadowUIExitAfterLoad(name string) {
	if filt := ensureMidiFilt(); filt != nil {
		filt[4] = 0
	}
	shadowUIStop()
	select {
	case osdCh <- OSDRequest{
		Lines: []OSDLine{
			{Text: "PUSH HACK", Scale: 2},
			{Text: "Loaded", Scale: 1},
			{Text: truncate(name, 24), Scale: 1},
		},
		Duration: 1500 * time.Millisecond,
	}:
	default:
	}
}

// shadowUIIsActive reports whether the shadow UI is currently running.
func shadowUIIsActive() bool {
	shadowUI.mu.Lock()
	defer shadowUI.mu.Unlock()
	return shadowUI.active
}

// shadowUIHandleCC routes a CC event into the active shadow UI.
// Called from the MIDI reader goroutine.
func shadowUIHandleCC(cc, val uint8) {
	shadowUI.mu.Lock()
	if !shadowUI.active {
		shadowUI.mu.Unlock()
		return
	}
	// Top buttons switch panels (press only)
	if val == 127 {
		switch cc {
		case CCScreenTop1: // Files
			shadowUI.panelIdx = 0
			panel0 := shadowUI.panels[0]
			shadowUI.mu.Unlock()
			go exclusiveLED(uint8(CCScreenTop1), shadowTabColor)
			go updateBotLEDs(panel0)
			return
		case CCScreenTop2: // Stats
			shadowUI.panelIdx = 1
			panel1 := shadowUI.panels[1]
			shadowUI.mu.Unlock()
			go exclusiveLED(uint8(CCScreenTop2), shadowTabColor)
			go updateBotLEDs(panel1)
			return
		case CCScreenTop3: // MIDI
			already := shadowUI.panelIdx == 2
			shadowUI.panelIdx = 2
			panel2 := shadowUI.panels[2]
			shadowUI.mu.Unlock()
			// Re-pressing the MIDI tab exits the monitor sub-view; entering the
			// panel fresh always lands on the main (intercept/forward) view.
			if mp, ok := panel2.(*MidiPanel); ok {
				if already {
					mp.handleTabReenter()
				} else {
					mp.monitor = false
				}
			}
			go exclusiveLED(uint8(CCScreenTop3), shadowTabColor)
			go updateBotLEDs(panel2)
			return
		case CCScreenTop4: // Browse
			shadowUI.panelIdx = browsePanelIdx
			panel3 := shadowUI.panels[browsePanelIdx]
			shadowUI.mu.Unlock()
			go exclusiveLED(uint8(CCScreenTop4), shadowTabColor)
			go updateBotLEDs(panel3)
			return
		}
	}
	panel := shadowUI.panels[shadowUI.panelIdx]
	shadowUI.mu.Unlock()
	// Jog wheel sends val=127 (CW) and val=1 (CCW) — special case
	if cc == CCJogWheel {
		if jh, ok := panel.(jogHandler); ok {
			jh.handleJog(val)
		}
		return
	}
	panel.HandleCC(cc, val)
}

func (s *ShadowUI) renderLoop() {
	ticker := time.NewTicker(100 * time.Millisecond) // 10 fps
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.renderFrame()
		}
	}
}

func (s *ShadowUI) renderFrame() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	panel := s.panels[s.panelIdx]
	panelIdx := s.panelIdx
	s.mu.Unlock()

	img := image.NewNRGBA(image.Rect(0, 0, suiW, suiH))
	fillRect(img, 0, 0, suiW, suiH, suiBlack)

	// Strips
	drawPanelTabs(img, shadowUI.panels, panelIdx)
	labels, hint := panel.BotStrip()
	drawBotStrip(img, labels, hint)
	if eb, ok := panel.(extraBots); ok {
		drawExtraBots(img, eb.extraBotStrip())
	}

	// Panel content (clipped to content area)
	panel.Render(img)

	pixels := imageToBGR565(img)
	if err := shmWritePixels(pixels); err != nil {
		log.Printf("shadow_ui: shmWritePixels: %v", err)
	}
}

// ── Drawing helpers ───────────────────────────────────────────────────────────

func fillRect(img *image.NRGBA, x, y, w, h int, c color.NRGBA) {
	gfx.FillRect(img, x, y, w, h, c)
}

// drawText draws string at pixel (x, baseline) using basicfont at 1× scale.
func drawText(img *image.NRGBA, x, baseline int, s string, col color.NRGBA) {
	gtext.Draw(img, x, baseline, s, col)
}

// textWidth returns pixel width of s at 1× scale.
func textWidth(s string) int { return gtext.Width(s) }

// drawPanelTabs renders the top 18px strip with panel labels.
// Active panel gets a white background with black text.
func drawPanelTabs(img *image.NRGBA, panels []Panel, activeIdx int) {
	fillRect(img, 0, 0, suiW, suiTopH, suiDarkGray)
	for i, p := range panels {
		x := i * suiColW
		label := p.Label()
		if i == activeIdx {
			fillRect(img, x, 0, suiColW, suiTopH, suiWhite)
			// Center label
			lx := x + (suiColW-textWidth(label))/2
			drawText(img, lx, suiTopH-4, label, suiBlack)
		} else {
			lx := x + (suiColW-textWidth(label))/2
			drawText(img, lx, suiTopH-4, label, suiGray)
		}
	}
}

// drawBotStrip renders the bottom 18px strip.
// labels: up to 4 action labels for bottom buttons 1-4 (120px columns each).
// hint: navigation text shown to the right of the labels.
func drawBotStrip(img *image.NRGBA, labels [4]string, hint string) {
	y := suiContentBot
	fillRect(img, 0, y, suiW, suiBotH, suiDarkGray)

	for i, label := range labels {
		if label == "" {
			continue
		}
		x := i * suiColW
		col := suiWhite // default: white for any non-empty label
		bg := suiDarkGray
		if label == "CONFIRM?" {
			bg = suiAccent
		} else if label == "DELETE" || label == "INTRCPT OFF" || label == "FWRD OFF" {
			col = color.NRGBA{255, 100, 100, 255} // red for destructive/off states
		} else if label == "INTRCPT ON" || label == "FWRD ON" {
			col = color.NRGBA{80, 220, 80, 255} // green for active states
		}
		if bg != suiDarkGray {
			fillRect(img, x, y, suiColW, suiBotH, bg)
		}
		lx := x + (suiColW-textWidth(label))/2
		drawText(img, lx, y+suiBotH-4, label, col)
	}

	// Navigation hint right of the 4 action columns
	if hint != "" {
		drawText(img, 4*suiColW+8, y+suiBotH-4, hint, suiGray)
	}
}

// drawExtraBots draws labels for under-screen buttons 5..8 (columns 4..7),
// on top of the bottom strip already filled by drawBotStrip.
func drawExtraBots(img *image.NRGBA, labels []string) {
	y := suiContentBot
	for i, label := range labels {
		if label == "" {
			continue
		}
		x := (4 + i) * suiColW
		if x+suiColW > suiW {
			break
		}
		lx := x + (suiColW-textWidth(label))/2
		drawText(img, lx, y+suiBotH-4, label, suiWhite)
	}
}

// truncate truncates s to at most maxRunes runes, appending "…" if cut.
func truncate(s string, maxRunes int) string {
	return gtext.Truncate(s, maxRunes)
}

// ── FilePanel ─────────────────────────────────────────────────────────────────

const (
	fileRowH = 16 // px per row
	fileRows = suiContentH / fileRowH // = 7 rows
)

type filePanelEntry struct {
	name  string
	path  string
	isDir bool
	isUSB bool
}

// FilePanel is a hierarchical file browser panel.
type FilePanel struct {
	mu            sync.Mutex
	stack         []string // dir navigation stack; empty = roots view
	entries       []filePanelEntry
	cursor        int
	scroll        int
	clipboard     string    // path of copied item; "" = nothing
	status        string    // transient status message shown in breadcrumb
	statusTime    time.Time // when status was set; cleared after 3s
	deleteConfirm bool      // waiting for second DELETE press to confirm
}

func newFilePanel() *FilePanel {
	fp := &FilePanel{}
	fp.loadRoots()
	return fp
}

func (fp *FilePanel) Label() string { return "FILES" }

// loadRoots populates entries with the configured allowed roots + USB drives.
func (fp *FilePanel) loadRoots() {
	fp.stack = nil
	fp.entries = nil
	fp.cursor = 0
	fp.scroll = 0

	seen := map[string]bool{}
	for _, root := range fileOps.allowedRoots {
		if seen[root] {
			continue
		}
		seen[root] = true

		if root == "/run/media" {
			// Expand to mounted drives — same filtering as handleRoots in main.go:
			// skip "swap*" dirs and dirs on the same device as /run/media (not actually mounted).
			var parentStat syscall.Stat_t
			if err := syscall.Stat(root, &parentStat); err != nil {
				continue
			}
			dirs, err := os.ReadDir(root)
			if err != nil {
				continue
			}
			for _, d := range dirs {
				if !d.IsDir() || strings.HasPrefix(d.Name(), "swap") {
					continue
				}
				p := root + "/" + d.Name()
				var childStat syscall.Stat_t
				if err := syscall.Stat(p, &childStat); err != nil {
					continue
				}
				if childStat.Dev == parentStat.Dev {
					continue // same filesystem — not actually mounted
				}
				fp.entries = append(fp.entries, filePanelEntry{
					name:  d.Name(),
					path:  p,
					isDir: true,
					isUSB: true,
				})
			}
		} else {
			label := root
			if root == "/data/Music/Ableton" || root == "/data/Music" {
				label = "Ableton Library"
			}
			fp.entries = append(fp.entries, filePanelEntry{
				name:  label,
				path:  root,
				isDir: true,
			})
		}
	}
}

// loadDir populates entries from a directory path via fileOps.List.
func (fp *FilePanel) loadDir(path string) {
	entries, err := fileOps.List(path)
	if err != nil {
		log.Printf("shadow_ui FilePanel: list %q: %v", path, err)
		fp.entries = nil
		fp.cursor = 0
		fp.scroll = 0
		return
	}
	fp.entries = make([]filePanelEntry, 0, len(entries))
	fp.cursor = 0
	fp.scroll = 0
	for _, e := range entries {
		fp.entries = append(fp.entries, filePanelEntry{
			name:  e.Name,
			path:  e.Path,
			isDir: e.IsDir,
		})
	}
}

func (fp *FilePanel) HandleCC(cc, val uint8) {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	if val != 127 {
		return // only handle press events
	}

	switch cc {
	case CCJogWheel:
		return // handled below with val check
	// ── Navigation ──────────────────────────────────────────────────────────
	case CCDPadDown:
		fp.deleteConfirm = false
		fp.moveCursor(1)
	case CCDPadUp:
		fp.deleteConfirm = false
		fp.moveCursor(-1)
	case CCJogPress, CCDPadCenter, CCDPadRight:
		fp.deleteConfirm = false
		fp.enter()
	case CCJogClickLeft, CCDPadLeft:
		fp.deleteConfirm = false
		fp.back()
	// ── Actions (bottom buttons) ─────────────────────────────────────────────
	case CCScreenBot1: // COPY
		fp.copyEntry()
	case CCScreenBot2: // PASTE
		fp.pasteEntry()
	case CCScreenBot3: // DELETE (two-press confirm)
		fp.deleteEntry()
	case CCScreenBot4: // EJECT
		fp.ejectUSB()
	}
}

func (fp *FilePanel) handleJog(val uint8) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	fp.deleteConfirm = false
	switch val {
	case 127: // CW → move up
		fp.moveCursor(-1)
	case 1: // CCW → move down
		fp.moveCursor(1)
	}
}

func (fp *FilePanel) moveCursor(delta int) {
	if len(fp.entries) == 0 {
		return
	}
	fp.cursor += delta
	if fp.cursor < 0 {
		fp.cursor = 0
	}
	if fp.cursor >= len(fp.entries) {
		fp.cursor = len(fp.entries) - 1
	}
	// Keep cursor in visible window
	if fp.cursor < fp.scroll {
		fp.scroll = fp.cursor
	}
	if fp.cursor >= fp.scroll+fileRows {
		fp.scroll = fp.cursor - fileRows + 1
	}
}

func (fp *FilePanel) enter() {
	if len(fp.entries) == 0 || fp.cursor >= len(fp.entries) {
		return
	}
	e := fp.entries[fp.cursor]
	if !e.isDir {
		return // files: no action yet
	}
	fp.stack = append(fp.stack, e.path)
	fp.loadDir(e.path)
}

func (fp *FilePanel) back() {
	if len(fp.stack) == 0 {
		return // already at roots
	}
	fp.stack = fp.stack[:len(fp.stack)-1]
	if len(fp.stack) == 0 {
		fp.loadRoots()
	} else {
		fp.loadDir(fp.stack[len(fp.stack)-1])
	}
}

// setStatus sets a transient status message (call under fp.mu held).
func (fp *FilePanel) setStatus(msg string) {
	fp.status = msg
	fp.statusTime = time.Now()
}

// BotStrip returns context-sensitive bottom button labels and nav hint.
func (fp *FilePanel) BotStrip() ([4]string, string) {
	fp.mu.Lock()
	clipboard := fp.clipboard
	deleteConfirm := fp.deleteConfirm
	isAtRoots := len(fp.stack) == 0
	var isUSBCursor bool
	if fp.cursor < len(fp.entries) {
		isUSBCursor = fp.entries[fp.cursor].isUSB && isAtRoots
	}
	fp.mu.Unlock()

	var labels [4]string
	labels[0] = "COPY"
	if clipboard != "" {
		labels[1] = "PASTE"
	}
	if deleteConfirm {
		labels[2] = "CONFIRM?"
	} else {
		labels[2] = "DELETE"
	}
	if isUSBCursor {
		labels[3] = "EJECT"
	}
	return labels, ""
}

// BotLEDColors returns LED colors for the 4 bottom buttons reflecting current state.
// Bot1 (COPY) always lit; Bot2 (PASTE) lit when clipboard non-empty;
// Bot3 (DELETE) always lit; Bot4 (EJECT) lit only when USB drive is under cursor.
func (fp *FilePanel) BotLEDColors() [4]uint8 {
	fp.mu.Lock()
	clipboard := fp.clipboard
	isAtRoots := len(fp.stack) == 0
	var isUSBCursor bool
	if fp.cursor < len(fp.entries) {
		isUSBCursor = fp.entries[fp.cursor].isUSB && isAtRoots
	}
	fp.mu.Unlock()

	const white = uint8(122) // WHITE_MIDI_VALUE
	const red = uint8(1)     // palette index 1 = red
	var colors [4]uint8
	colors[0] = white // COPY — always available
	if clipboard != "" {
		colors[1] = white // PASTE — only when clipboard has something
	}
	colors[2] = red // DELETE — always available, red to signal destructive
	if isUSBCursor {
		colors[3] = white // EJECT — only for USB drives at root
	}
	return colors
}

// copyEntry copies the cursor entry path to clipboard.
// Call under fp.mu held.
func (fp *FilePanel) copyEntry() {
	if fp.cursor >= len(fp.entries) {
		return
	}
	e := fp.entries[fp.cursor]
	fp.clipboard = e.path
	fp.deleteConfirm = false
	fp.setStatus("Copied: " + e.name)
	go updateBotLEDs(fp) // Bot2 (PASTE) should light up
}

// pasteEntry copies clipboard item into current directory.
// Spawns goroutine; call under fp.mu held (releases before goroutine starts).
func (fp *FilePanel) pasteEntry() {
	if fp.clipboard == "" {
		fp.setStatus("Nothing to paste")
		return
	}
	if len(fp.stack) == 0 {
		fp.setStatus("Navigate into a folder first")
		return
	}
	src := fp.clipboard
	dstDir := fp.stack[len(fp.stack)-1]
	dstPath := filepath.Join(dstDir, filepath.Base(src))
	fp.setStatus("Copying...")
	go func() {
		err := fileOps.Copy(src, dstPath)
		fp.mu.Lock()
		if err != nil {
			fp.setStatus("Copy error: " + err.Error())
			fp.mu.Unlock()
			return
		}
		fp.clipboard = ""
		fp.setStatus("Pasted!")
		fp.loadDir(dstDir)
		fp.mu.Unlock()
		updateBotLEDs(fp) // Bot2 (PASTE) should go dark — clipboard cleared
	}()
}

// deleteEntry deletes cursor entry with two-press confirmation.
// Call under fp.mu held.
func (fp *FilePanel) deleteEntry() {
	if fp.cursor >= len(fp.entries) {
		return
	}
	e := fp.entries[fp.cursor]
	if !fp.deleteConfirm {
		fp.deleteConfirm = true
		fp.setStatus("Delete \"" + truncate(e.name, 30) + "\"? Press DELETE again")
		return
	}
	// Confirmed
	fp.deleteConfirm = false
	path := e.path
	// Determine reload target before entry disappears
	var reloadPath string
	if len(fp.stack) > 0 {
		reloadPath = fp.stack[len(fp.stack)-1]
	}
	fp.setStatus("Deleting...")
	go func() {
		err := fileOps.DeleteAll(path)
		fp.mu.Lock()
		defer fp.mu.Unlock()
		if err != nil {
			fp.setStatus("Delete error: " + err.Error())
			return
		}
		fp.setStatus("Deleted!")
		if reloadPath != "" {
			fp.loadDir(reloadPath)
		} else {
			fp.loadRoots()
		}
	}()
}

// ejectUSB unmounts the USB drive under cursor (only valid at roots view).
// Call under fp.mu held.
func (fp *FilePanel) ejectUSB() {
	if len(fp.stack) != 0 || fp.cursor >= len(fp.entries) {
		return
	}
	e := fp.entries[fp.cursor]
	if !e.isUSB {
		fp.setStatus("Not a USB drive")
		return
	}
	path := e.path
	if err := syscall.Unmount(path, 0); err != nil {
		fp.setStatus("Eject failed: " + err.Error())
		return
	}
	// Remove automount lock so drive can re-mount on replug
	os.Remove("/tmp/.automount-" + filepath.Base(path))
	fp.setStatus("Ejected: " + e.name)
	fp.loadRoots()
	go updateBotLEDs(fp) // Bot4 (EJECT) should go dark — no USB cursor anymore
}

// currentPath returns a display string for the breadcrumb.
func (fp *FilePanel) currentPath() string {
	if len(fp.stack) == 0 {
		return "/ roots"
	}
	p := fp.stack[len(fp.stack)-1]
	if len(p) > 50 {
		p = "…" + p[len(p)-49:]
	}
	return p
}

func (fp *FilePanel) Render(img *image.NRGBA) {
	fp.mu.Lock()
	entries := fp.entries
	cursor := fp.cursor
	scroll := fp.scroll
	breadcrumb := fp.currentPath()
	// Show status for 3s, then fall back to breadcrumb
	statusText := ""
	if fp.status != "" && time.Since(fp.statusTime) < 3*time.Second {
		statusText = fp.status
	} else if fp.status != "" {
		fp.status = ""
	}
	fp.mu.Unlock()

	// Breadcrumb / status bar just below top strip
	const crumbH = 13
	crumbY := suiContentY
	crumbBg := color.NRGBA{20, 20, 20, 255}
	crumbCol := color.NRGBA{200, 200, 200, 255}
	crumbText := truncate(breadcrumb, 100)
	if statusText != "" {
		crumbBg = color.NRGBA{0, 60, 30, 255} // dark green tint for status
		crumbCol = color.NRGBA{100, 255, 150, 255}
		crumbText = statusText
	}
	fillRect(img, 0, crumbY, suiW, crumbH, crumbBg)
	drawText(img, 4, crumbY+crumbH-2, truncate(crumbText, 120), crumbCol)

	// File list rows below breadcrumb
	listY := crumbY + crumbH
	const rowH = fileRowH
	visRows := (suiContentBot - listY) / rowH

	if len(entries) == 0 {
		drawText(img, 8, listY+20, "(empty)", suiGray)
		return
	}

	for i := 0; i < visRows; i++ {
		idx := scroll + i
		if idx >= len(entries) {
			break
		}
		e := entries[idx]
		y := listY + i*rowH

		var bg, textCol color.NRGBA
		if idx == cursor {
			bg = suiSelect
			textCol = suiWhite
		} else if e.isDir {
			bg = suiBlack
			textCol = suiDirColor
		} else {
			bg = suiBlack
			textCol = suiWhite
		}
		fillRect(img, 0, y, suiW-6, rowH, bg)
		textX := 8
		if iconName := iconNameForEntry(e); iconName != "" {
			if icon := loadSuiIcon(iconName); icon != nil {
				iconY := y + (rowH-suiIconH)/2
				drawIcon(img, icon, 2, iconY)
				textX = 2 + icon.Bounds().Dx() + 3
			}
		}
		drawText(img, textX, y+rowH-3, truncate(e.name, 110), textCol)
	}

	// Scrollbar
	total := len(entries)
	if total > visRows {
		barH := suiContentH * visRows / total
		if barH < 4 {
			barH = 4
		}
		barY := listY + (suiContentBot-listY)*scroll/total
		fillRect(img, suiW-4, listY, 4, suiContentBot-listY, suiDarkGray)
		fillRect(img, suiW-4, barY, 4, barH, suiGray)
	}
}

// ── StatsPanel ────────────────────────────────────────────────────────────────

// StatsPanel shows system statistics. Stats are refreshed at most every 3s
// (collectStats samples CPU over 250ms, so we avoid calling it every frame).
type StatsPanel struct {
	mu        sync.Mutex
	cache     SystemStats
	lastFetch time.Time
}

func newStatsPanel() *StatsPanel { return &StatsPanel{} }
func (sp *StatsPanel) Label() string { return "STATS" }
func (sp *StatsPanel) HandleCC(cc, val uint8) {} // no navigation
func (sp *StatsPanel) BotStrip() ([4]string, string) {
	return [4]string{}, ""
}
func (sp *StatsPanel) BotLEDColors() [4]uint8 { return [4]uint8{} } // no actions on Stats

func (sp *StatsPanel) getStats() SystemStats {
	sp.mu.Lock()
	if time.Since(sp.lastFetch) < 3*time.Second {
		s := sp.cache
		sp.mu.Unlock()
		return s
	}
	sp.mu.Unlock()
	// Fetch outside lock (250ms blocking call)
	fresh := collectStats("/data")
	sp.mu.Lock()
	sp.cache = fresh
	sp.lastFetch = time.Now()
	sp.mu.Unlock()
	return fresh
}

func (sp *StatsPanel) Render(img *image.NRGBA) {
	stats := sp.getStats()

	type line struct {
		label string
		value string
	}
	lines := []line{
		{"CPU", fmt.Sprintf("%.1f%%", stats.CPUPercent)},
	}
	if stats.Memory != nil {
		usedMB := float64(stats.Memory.Used) / 1024 / 1024
		totalMB := float64(stats.Memory.Total) / 1024 / 1024
		lines = append(lines, line{"RAM", fmt.Sprintf("%.0f / %.0f MB", usedMB, totalMB)})
	}
	if stats.Disk != nil {
		usedGB := float64(stats.Disk.Used) / 1024 / 1024 / 1024
		totalGB := float64(stats.Disk.Total) / 1024 / 1024 / 1024
		lines = append(lines, line{"Disk", fmt.Sprintf("%.1f / %.1f GB", usedGB, totalGB)})
	}
	uptime := time.Duration(stats.UptimeSeconds * float64(time.Second))
	h := int(uptime.Hours())
	m := int(uptime.Minutes()) % 60
	lines = append(lines, line{"Uptime", fmt.Sprintf("%dh %02dm", h, m)})
	if len(stats.IPAddresses) > 0 {
		lines = append(lines, line{"IP", stats.IPAddresses[0]})
	}
	if stats.HotspotPassword != "" {
		lines = append(lines, line{"Hotspot", stats.HotspotPassword})
	}

	rows := make([]widgets.KVRow, len(lines))
	for i, l := range lines {
		rows[i] = widgets.KVRow{Label: l.label, Value: l.value, ValueCol: widgets.Default.White}
	}
	widgets.DrawKVRows(img, widgets.Default, suiContentY+6, suiW, 20, 80, suiContentBot, rows)
}

// ── MidiPanel ─────────────────────────────────────────────────────────────────

// MidiPanel shows MIDI routing state and lets the user toggle intercept and
// forward directly from Push hardware buttons. It has two views:
//   - main view: Intercept / Forward toggles (Bot1/Bot2), MONITOR enter (Bot3)
//   - monitor sub-view: live MIDI event log; Bot1-4 toggle the 4 display-filter
//     categories (matching the web UI), Encoder 1 selects the input port.
type MidiPanel struct {
	monitor      bool // sub-view active
	hideSens     bool // hide Active Sensing (default true — matches web)
	hideSysex    bool // hide SysEx          (default true — matches web)
	hideCC       bool // hide CC             (default false)
	hideNote     bool // hide Note On/Off    (default false)
	hideChanPres bool // hide Channel Pressure (default true — aftertouch noise)
}

func newMidiPanel() *MidiPanel {
	return &MidiPanel{hideSens: true, hideSysex: true, hideChanPres: true}
}
func (mp *MidiPanel) Label() string { return "MIDI" }

func (mp *MidiPanel) HandleCC(cc, val uint8) {
	if mp.monitor {
		// Filter toggles fire on press.
		if val != 127 {
			return
		}
		switch cc {
		case CCScreenBot1:
			mp.hideSens = !mp.hideSens
		case CCScreenBot2:
			mp.hideSysex = !mp.hideSysex
		case CCScreenBot3:
			mp.hideCC = !mp.hideCC
		case CCScreenBot4:
			mp.hideNote = !mp.hideNote
		case CCScreenBot5:
			mp.hideChanPres = !mp.hideChanPres
		default:
			return
		}
		go updateBotLEDs(mp)
		return
	}

	if val != 127 {
		return
	}
	switch cc {
	case CCScreenBot1: // INTERCEPT toggle
		mp.toggleIntercept()
	case CCScreenBot2: // FORWARD toggle
		mp.toggleForward()
	case CCScreenBot3: // enter MONITOR sub-view
		mp.monitor = true
		go updateBotLEDs(mp)
	}
}

// handleTabReenter exits the monitor sub-view when the MIDI tab is pressed again.
func (mp *MidiPanel) handleTabReenter() {
	if mp.monitor {
		mp.monitor = false
		go updateBotLEDs(mp)
	}
}

// midiInterceptEnabled reads the current intercept state from shm.
func midiInterceptEnabled() bool {
	data := ensureMidiFilt()
	return data != nil && data[4] == 1
}

func (mp *MidiPanel) toggleIntercept() {
	data := ensureMidiFilt()
	if data == nil {
		return
	}
	if data[4] != 1 {
		data[4] = 1
	} else {
		data[4] = 0
	}
	go updateBotLEDs(mp)
}

func (mp *MidiPanel) toggleForward() {
	midiForwardMu.Lock()
	midiForwardEnabled = !midiForwardEnabled
	midiForwardMu.Unlock()
	go updateBotLEDs(mp)
}

func (mp *MidiPanel) BotStrip() ([4]string, string) {
	if mp.monitor {
		// CHPRES (button 5) is drawn via extraBots; MIDI tab re-press exits.
		return [4]string{"SENS", "SYSEX", "CC", "NOTE"}, ""
	}
	interceptOn := midiInterceptEnabled()
	midiForwardMu.RLock()
	forwardOn := midiForwardEnabled
	midiForwardMu.RUnlock()

	interceptLabel := "INTERCEPT"
	if interceptOn {
		interceptLabel = "INTRCPT ON"
	} else {
		interceptLabel = "INTRCPT OFF"
	}
	forwardLabel := "FORWARD"
	if forwardOn {
		forwardLabel = "FWRD ON"
	} else {
		forwardLabel = "FWRD OFF"
	}
	return [4]string{interceptLabel, forwardLabel, "MONITOR", ""}, ""
}

// BotLEDColors:
//   - main view: Bot1/Bot2 green(11) if intercept/forward on, red(1) if off;
//     Bot3 white to show MONITOR is actionable.
//   - monitor view: each button green(11) if that filter category is shown,
//     red(1) if hidden.
func (mp *MidiPanel) BotLEDColors() [4]uint8 {
	const green = uint8(11)
	const red = uint8(1)
	shownLED := func(hidden bool) uint8 {
		if hidden {
			return red
		}
		return green
	}
	if mp.monitor {
		return [4]uint8{
			shownLED(mp.hideSens),
			shownLED(mp.hideSysex),
			shownLED(mp.hideCC),
			shownLED(mp.hideNote),
		}
	}
	interceptOn := midiInterceptEnabled()
	midiForwardMu.RLock()
	forwardOn := midiForwardEnabled
	midiForwardMu.RUnlock()

	var colors [4]uint8
	if interceptOn {
		colors[0] = green
	} else {
		colors[0] = red
	}
	if forwardOn {
		colors[1] = green
	} else {
		colors[1] = red
	}
	colors[2] = suiBotWhite // MONITOR
	return colors
}

// midiEventHidden reports whether ev should be dropped given the active filters.
// Classification matches the web UI (by decoded-string prefix).
func (mp *MidiPanel) midiEventHidden(dec string) bool {
	switch {
	case mp.hideSens && dec == "Active Sensing":
		return true
	case mp.hideSysex && strings.HasPrefix(dec, "SysEx"):
		return true
	case mp.hideCC && strings.HasPrefix(dec, "CC "):
		return true
	case mp.hideNote && (strings.HasPrefix(dec, "Note On") || strings.HasPrefix(dec, "Note Off")):
		return true
	case mp.hideChanPres && strings.HasPrefix(dec, "Chan Pres"):
		return true
	}
	return false
}

// extraBotStrip / extraBotLEDs expose a 5th soft-button (CHPRES) that toggles
// the Channel Pressure filter, only while the monitor sub-view is active.
func (mp *MidiPanel) extraBotStrip() []string {
	if !mp.monitor {
		return nil
	}
	return []string{"CHPRES"}
}

func (mp *MidiPanel) extraBotLEDs() []uint8 {
	if !mp.monitor {
		return nil
	}
	if mp.hideChanPres {
		return []uint8{1} // red = hidden
	}
	return []uint8{11} // green = shown
}

func (mp *MidiPanel) renderMonitor(img *image.NRGBA) {
	// Snapshot recent events newest-first (same ring walk as handleMidiEvents).
	const maxRows = 10
	midiRingMu.RLock()
	writeIdx := midiWriteIdx
	total := midiTotal
	available := int(total)
	if available > midiRingSize {
		available = midiRingSize
	}
	events := make([]midiEventJSON, 0, available)
	for i := 0; i < available; i++ {
		slot := (uint32(int(writeIdx)-1-i) + midiRingSize*4) % midiRingSize
		events = append(events, midiRing[slot])
	}
	midiRingMu.RUnlock()

	y := suiContentY + 12
	const rowPitch = 12
	shown := 0
	for _, ev := range events {
		if shown >= maxRows {
			break
		}
		if mp.midiEventHidden(ev.Decoded) {
			continue
		}
		hex := make([]string, 0, len(ev.Data))
		for _, b := range ev.Data {
			hex = append(hex, fmt.Sprintf("%02X", b&0xFF))
		}
		line := fmt.Sprintf("%-5s %-48s %s", ev.Dir, truncate(ev.Decoded, 48), strings.Join(hex, " "))
		drawText(img, 8, y, truncate(line, 135), suiWhite)
		y += rowPitch
		shown++
	}
	if shown == 0 {
		drawText(img, 8, y, "(no events)", suiGray)
	}
}

func (mp *MidiPanel) Render(img *image.NRGBA) {
	if mp.monitor {
		mp.renderMonitor(img)
		return
	}
	interceptOn := midiInterceptEnabled()
	midiForwardMu.RLock()
	forwardOn := midiForwardEnabled
	midiForwardMu.RUnlock()

	stateRow := func(label string, on bool) widgets.KVRow {
		val, col := "OFF", widgets.Default.OffColor
		if on {
			val, col = "ON", widgets.Default.OnColor
		}
		return widgets.KVRow{Label: label, Value: val, ValueCol: col}
	}
	rows := []widgets.KVRow{
		stateRow("Intercept", interceptOn),
		stateRow("Forward", forwardOn),
	}
	widgets.DrawKVRows(img, widgets.Default, suiContentY+10, suiW, 28, 120, suiContentBot, rows)
}

// ── BrowserPanel ────────────────────────────────────────────────────────────
//
// Preset browser for the Browser Bridge. Browse/search/filter are served from
// the filesystem-scanned index (presets.go) — no Live access. LOAD sends the
// preset name+category to the PushHackBrowser Remote Script (live_bridge.go),
// which instantiates it onto Live's selected track.

// browserFilter is one entry in the Bot3 (FILTER) cycle.
type browserFilter struct {
	Label      string
	Cat        PresetCategory
	Fav        bool
	TypeFilter string // "preset", "sample", "" = all
	RackOnly   bool   // .adg racks only within the category
}

// filterCycle is the order Bot3 (FILTER) cycles through. Rack-only entries follow
// their category so the user can land directly on Instrument/Audio/MIDI racks.
var filterCycle = []browserFilter{
	{"All", "", false, "", false},
	{"Instruments", CatInstruments, false, "preset", false},
	{"Instrument Racks", CatInstruments, false, "preset", true},
	{"Audio FX", CatAudioFX, false, "preset", false},
	{"Audio FX Racks", CatAudioFX, false, "preset", true},
	{"MIDI FX", CatMidiFX, false, "preset", false},
	{"MIDI FX Racks", CatMidiFX, false, "preset", true},
	{"Drums", CatDrums, false, "preset", false},
	{"Samples", "", false, "sample", false},
	{"Favourites", "", true, "", false},
}

// kbKeys is the on-screen keyboard grid (10 columns). basicfont is ASCII-only,
// so special keys use short words rather than glyphs.
var kbKeys = buildKbKeys()

const kbCols = 10

func buildKbKeys() []string {
	var k []string
	for c := 'A'; c <= 'Z'; c++ {
		k = append(k, string(c))
	}
	for c := '0'; c <= '9'; c++ {
		k = append(k, string(c))
	}
	return append(k, "SP", "DEL", "OK")
}

type BrowserPanel struct {
	mu         sync.Mutex
	entries    []PresetEntry
	cursor     int
	scroll     int
	filterIdx  int
	search     bool   // keyboard view active
	query      string // current search text
	kbCursor   int
	status     string
	statusTime time.Time
}

func newBrowserPanel() *BrowserPanel {
	bp := &BrowserPanel{}
	bp.reload()
	return bp
}

func (bp *BrowserPanel) Label() string { return "BROWSER" }

// reload re-queries the index into entries (call under bp.mu held).
func (bp *BrowserPanel) reload() {
	fc := filterCycle[bp.filterIdx]
	bp.entries = QueryPresets(PresetFilter{
		Category:   fc.Cat,
		Q:          bp.query,
		FavOnly:    fc.Fav,
		TypeFilter: fc.TypeFilter,
		RackOnly:   fc.RackOnly,
	})
	if bp.cursor >= len(bp.entries) {
		bp.cursor = 0
	}
	bp.scroll = 0
}

func (bp *BrowserPanel) setStatus(msg string) {
	bp.status = msg
	bp.statusTime = time.Now()
}

func (bp *BrowserPanel) HandleCC(cc, val uint8) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	// FILTER/REFRESH are momentary in list mode: lit green while held, back to
	// white on release. The generic LED toggle is suppressed for these buttons
	// while the Shadow UI is active (see midi.go), so we drive them directly.
	if !bp.search && (cc == CCScreenBot3 || cc == CCScreenBot4) {
		if val == 127 {
			sendSeqCC(0, cc, int32(suiBotGreen)) //nolint:errcheck
		} else if val == 0 {
			sendSeqCC(0, cc, int32(suiBotWhite)) //nolint:errcheck
		}
	}
	if val != 127 {
		return
	}
	if bp.search {
		bp.handleKeyboardCC(cc)
		return
	}
	switch cc {
	case CCDPadDown:
		bp.moveCursor(1)
	case CCDPadUp:
		bp.moveCursor(-1)
	case CCJogPress, CCDPadCenter, CCDPadRight:
		bp.loadCursor()
	case CCJogClickLeft, CCDPadLeft:
		if bp.query != "" {
			bp.query = ""
			bp.reload()
			bp.setStatus("Search cleared")
		}
	case CCScreenBot1: // LOAD
		bp.loadCursor()
	case CCScreenBot2: // SEARCH
		bp.search = true
		bp.kbCursor = 0
		go updateBotLEDs(bp)
	case CCScreenBot3: // FILTER
		bp.filterIdx = (bp.filterIdx + 1) % len(filterCycle)
		bp.reload()
	case CCScreenBot4: // REFRESH
		bp.setStatus("Refreshing…")
		go func() {
			RefreshPresets()
			bp.mu.Lock()
			bp.reload()
			bp.setStatus(fmt.Sprintf("Refreshed: %d presets", presetCount()))
			bp.mu.Unlock()
		}()
	}
}

func (bp *BrowserPanel) handleKeyboardCC(cc uint8) {
	switch cc {
	case CCDPadDown:
		bp.kbMove(kbCols)
	case CCDPadUp:
		bp.kbMove(-kbCols)
	case CCDPadRight:
		bp.kbMove(1)
	case CCDPadLeft:
		bp.kbMove(-1)
	case CCJogPress, CCDPadCenter:
		bp.kbActivate()
	case CCScreenBot2: // SEARCH again = done
		bp.search = false
		go updateBotLEDs(bp)
	}
}

func (bp *BrowserPanel) kbMove(delta int) {
	n := len(kbKeys)
	bp.kbCursor = (bp.kbCursor + delta + n) % n
}

func (bp *BrowserPanel) kbActivate() {
	if bp.kbCursor >= len(kbKeys) {
		return
	}
	switch key := kbKeys[bp.kbCursor]; key {
	case "OK":
		bp.search = false
		go updateBotLEDs(bp)
	case "DEL":
		if bp.query != "" {
			bp.query = bp.query[:len(bp.query)-1]
			bp.reload()
		}
	case "SP":
		bp.query += " "
		bp.reload()
	default:
		bp.query += key
		bp.reload()
	}
}

func (bp *BrowserPanel) handleJog(val uint8) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	if bp.search {
		switch val {
		case 127:
			bp.kbMove(-1)
		case 1:
			bp.kbMove(1)
		}
		return
	}
	switch val {
	case 127:
		bp.moveCursor(-1)
	case 1:
		bp.moveCursor(1)
	}
}

func (bp *BrowserPanel) moveCursor(delta int) {
	if len(bp.entries) == 0 {
		return
	}
	bp.cursor += delta
	if bp.cursor < 0 {
		bp.cursor = 0
	}
	if bp.cursor >= len(bp.entries) {
		bp.cursor = len(bp.entries) - 1
	}
	const visRows = (suiContentBot - suiContentY - 13) / fileRowH
	if bp.cursor < bp.scroll {
		bp.scroll = bp.cursor
	}
	if bp.cursor >= bp.scroll+visRows {
		bp.scroll = bp.cursor - visRows + 1
	}
}

// loadCursor sends the cursor entry to Live (async; call under bp.mu held).
func (bp *BrowserPanel) loadCursor() {
	if bp.cursor >= len(bp.entries) {
		return
	}
	e := bp.entries[bp.cursor]
	bp.setStatus("Loading: " + e.Name)
	go func() {
		var err error
		if e.EntryType == "sample" {
			err = liveSampleLoad(e.Name)
		} else {
			err = liveLoad(e.Name, e.Category)
		}
		bp.mu.Lock()
		if err != nil {
			bp.setStatus("Load failed: " + err.Error())
			bp.mu.Unlock()
			return
		}
		// The socket OK is only an enqueue ack — Live loads asynchronously.
		bp.setStatus("Sent: " + e.Name)
		bp.mu.Unlock()
		// Auto-exit the Shadow UI so the user can play the loaded device.
		time.Sleep(500 * time.Millisecond)
		shadowUIExitAfterLoad(e.Name)
	}()
}

func (bp *BrowserPanel) filterLabel() string {
	return filterCycle[bp.filterIdx].Label
}

func (bp *BrowserPanel) BotStrip() ([4]string, string) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	if bp.search {
		return [4]string{"", "DONE", "", ""}, "L/R move - CTR pick"
	}
	hint := fmt.Sprintf("%s - %d", bp.filterLabel(), len(bp.entries))
	return [4]string{"LOAD", "SEARCH", "FILTER", "REFRESH"}, hint
}

func (bp *BrowserPanel) BotLEDColors() [4]uint8 {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	if bp.search {
		// Keyboard mode: only DONE (Bot2) lit, green.
		return [4]uint8{0, suiBotGreen, 0, 0}
	}
	return [4]uint8{suiBotWhite, suiBotWhite, suiBotWhite, suiBotWhite}
}

// iconNameForPreset maps a preset/sample to a Push Browser PNG (under suiIconBase).
func iconNameForPreset(e PresetEntry) string {
	if e.EntryType == "sample" {
		return "Audio.png"
	}
	switch e.Category {
	case CatDrums:
		return "Browser_DrumRack.png"
	case CatAudioFX:
		if e.IsRack {
			return "Browser_AudioEffectRack.png"
		}
		return "Browser_AudioEffectPreset.png"
	case CatMidiFX:
		if e.IsRack {
			return "Browser_MidiEffectRack.png"
		}
		return "Browser_MidiEffectPreset.png"
	case CatInstruments:
		if e.IsRack {
			return "Browser_InstrumentRack.png"
		}
		return "Browser_InstrumentPreset.png"
	}
	return "PresetDevice.png"
}

func (bp *BrowserPanel) Render(img *image.NRGBA) {
	bp.mu.Lock()
	search := bp.search
	query := bp.query
	entries := bp.entries
	cursor := bp.cursor
	scroll := bp.scroll
	kbCursor := bp.kbCursor
	filterLabel := bp.filterLabel()
	statusText := ""
	if bp.status != "" && time.Since(bp.statusTime) < 3*time.Second {
		statusText = bp.status
	} else if bp.status != "" {
		bp.status = ""
	}
	bp.mu.Unlock()

	if search {
		bp.renderKeyboard(img, query, len(entries), kbCursor)
		return
	}

	// Breadcrumb / status
	const crumbH = 13
	crumbY := suiContentY
	crumbBg := color.NRGBA{20, 20, 20, 255}
	crumbCol := color.NRGBA{200, 200, 200, 255}
	crumbText := fmt.Sprintf("[%s]  %d items", filterLabel, len(entries))
	if query != "" {
		crumbText = fmt.Sprintf("[%s]  q=\"%s\"  %d", filterLabel, query, len(entries))
	}
	if statusText != "" {
		crumbBg = color.NRGBA{0, 60, 30, 255}
		crumbCol = color.NRGBA{100, 255, 150, 255}
		crumbText = statusText
	}
	fillRect(img, 0, crumbY, suiW, crumbH, crumbBg)
	drawText(img, 4, crumbY+crumbH-2, truncate(crumbText, 120), crumbCol)

	listY := crumbY + crumbH
	const rowH = fileRowH
	visRows := (suiContentBot - listY) / rowH

	if len(entries) == 0 {
		drawText(img, 8, listY+20, "(no items - press REFRESH)", suiGray)
		return
	}

	for i := 0; i < visRows; i++ {
		idx := scroll + i
		if idx >= len(entries) {
			break
		}
		e := entries[idx]
		y := listY + i*rowH
		var bg, textCol color.NRGBA
		if idx == cursor {
			bg = suiSelect
			textCol = suiWhite
		} else {
			bg = suiBlack
			textCol = suiWhite
		}
		fillRect(img, 0, y, suiW-6, rowH, bg)
		textX := 8
		if icon := loadSuiIcon(iconNameForPreset(e)); icon != nil {
			iconY := y + (rowH-suiIconH)/2
			drawIcon(img, icon, 2, iconY)
			textX = 2 + icon.Bounds().Dx() + 3
		}
		drawText(img, textX, y+rowH-3, truncate(e.Name, 108), textCol)
	}

	// Scrollbar
	total := len(entries)
	if total > visRows {
		barH := (suiContentBot - listY) * visRows / total
		if barH < 4 {
			barH = 4
		}
		barY := listY + (suiContentBot-listY)*scroll/total
		fillRect(img, suiW-4, listY, 4, suiContentBot-listY, suiDarkGray)
		fillRect(img, suiW-4, barY, 4, barH, suiGray)
	}
}

func (bp *BrowserPanel) renderKeyboard(img *image.NRGBA, query string, matches, kbCursor int) {
	// Header: query + match count
	const headH = 16
	y0 := suiContentY
	fillRect(img, 0, y0, suiW, headH, color.NRGBA{20, 20, 20, 255})
	drawText(img, 4, y0+headH-3, truncate(fmt.Sprintf("Search: %s_   (%d)", query, matches), 130), suiWhite)

	// Key grid
	gridY := y0 + headH + 2
	rows := (len(kbKeys) + kbCols - 1) / kbCols
	cellW := suiW / kbCols
	cellH := (suiContentBot - gridY) / rows
	for i, key := range kbKeys {
		r := i / kbCols
		c := i % kbCols
		x := c * cellW
		y := gridY + r*cellH
		bg := suiDarkGray
		col := suiWhite
		if i == kbCursor {
			bg = suiSelect
		}
		fillRect(img, x+1, y+1, cellW-2, cellH-2, bg)
		lx := x + (cellW-textWidth(key))/2
		drawText(img, lx, y+(cellH+10)/2, key, col)
	}
}
