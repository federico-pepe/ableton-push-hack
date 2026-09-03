package main

// display.go — draws the current parameter page (8 encoder-slot cells,
// each either a knob for a plain 0-1 float or a text readout for the
// "engine" enum) and pushes it to push-manager's display API. Same
// pattern as hacks/keyboard-visualizer/src/render.go: an HTTP client of
// push-manager only, never touching the shared-memory framebuffer
// directly — see CLAUDE.md's "Display-owning hacks" section for why that
// discipline matters.
//
// Off by default: the UI only takes the screen (and enables push-manager's
// MIDI intercept, so pad hits drive Braids instead of also reaching Live)
// while toggled on via Shift+Device — see chord.go and toggleUI below.

import (
	"fmt"
	"image"
	"log"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
	"github.com/federico-pepe/ableton-push-hack/core/pmclient"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

const (
	screenW = push3.VisW
	screenH = push3.VisH

	cellW  = screenW / 8
	knobCX = 60 // knob center within its cell (cellW/2)
	knobCY = 88
	knobR  = 30
)

var (
	uiMu sync.Mutex
	uiOn bool
)

// renderParamPage draws the header plus one cell per encoder slot (0-7) on
// the current page: a knob for a plain 0-1 float param, or a plain
// centered readout for an enum ("engine") — DrawKnob's numeric center
// doesn't fit a shape name, so that one slot draws differently.
func renderParamPage(st *paramState) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))
	gfx.FillRect(img, 0, 0, screenW, screenH, widgets.Default.Black)

	st.mu.Lock()
	page := paramPages[st.page]
	title := fmt.Sprintf("BRAIDS - PAGE %d/%d - %s", st.page+1, len(paramPages), pageNames[st.page])
	type cell struct {
		slot *paramSlot
	}
	cells := make([]cell, len(page))
	for i, key := range page {
		cells[i] = cell{slot: st.slots[key]}
	}
	st.mu.Unlock()

	text.Draw(img, 8, 16, title, widgets.Default.Gray)

	for i, c := range cells {
		if c.slot == nil {
			continue
		}
		cx := i*cellW + knobCX
		if c.slot.meta.Type == "enum" {
			name := c.slot.meta.Name
			text.Draw(img, cx-text.Width(name)/2, knobCY-knobR-4, name, widgets.Default.Gray)
			val := formatValue(c.slot)
			text.DrawScaled(img, cx-text.WidthScaled(val, 2)/2, knobCY+8, 2, val, widgets.Default.White)
			continue
		}
		widgets.DrawKnobArc(img, widgets.Default, cx, knobCY, knobR, widgets.Knob{
			Label: c.slot.meta.Name,
			Value: c.slot.value * 100,
			Min:   c.slot.meta.Min * 100,
			Max:   c.slot.meta.Max * 100,
		})
	}
	return img
}

// toggleUI flips the on-screen param UI: entering takeover mode, enabling
// push-manager's MIDI intercept (so pad hits stop reaching Live while this
// UI reads them as controls, not notes), and forcing an immediate frame —
// or releasing both back to the native Push UI / normal Live routing.
func toggleUI(pmURL string, st *paramState) {
	uiMu.Lock()
	uiOn = !uiOn
	on := uiOn
	uiMu.Unlock()

	client := pmclient.New(pmURL)
	if on {
		if err := client.SetMode(2); err != nil {
			log.Printf("display: enable takeover: %v", err)
		}
		if err := client.SetMidiFilter(true); err != nil {
			log.Printf("display: enable midi filter: %v", err)
		}
		if err := client.PushImage(renderParamPage(st)); err != nil {
			log.Printf("display: push frame: %v", err)
		}
		log.Printf("push-braids-host: UI ON (Shift+Device) — MIDI intercept enabled")
	} else {
		if err := client.SetMode(0); err != nil {
			log.Printf("display: disable takeover: %v", err)
		}
		if err := client.SetMidiFilter(false); err != nil {
			log.Printf("display: disable midi filter: %v", err)
		}
		log.Printf("push-braids-host: UI OFF (Shift+Device) — MIDI intercept disabled")
	}
}

// shutdownUI best-effort releases the display and MIDI intercept on exit,
// regardless of the UI's last toggled state — leaving push-manager's
// global MIDI intercept stuck on after this process exits would silently
// block all pad input to Live with no process left to blame.
func shutdownUI(pmURL string) {
	client := pmclient.New(pmURL)
	_ = client.SetMode(0)
	_ = client.SetMidiFilter(false)
}

// runDisplayLoop redraws only when the UI is on and an encoder turn or
// page flip marked the state dirty — polled at ~30fps, the same rate
// keyboard-visualizer's own render loop uses.
func runDisplayLoop(pmURL string, st *paramState) {
	client := pmclient.New(pmURL)
	ticker := time.NewTicker(33 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		st.mu.Lock()
		dirty := st.dirty
		st.dirty = false
		st.mu.Unlock()

		uiMu.Lock()
		on := uiOn
		uiMu.Unlock()

		if !on || !dirty {
			continue
		}
		if err := client.PushImage(renderParamPage(st)); err != nil {
			log.Printf("display: push frame: %v", err)
		}
	}
}
