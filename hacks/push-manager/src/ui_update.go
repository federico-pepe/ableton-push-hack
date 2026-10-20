package main

// ui_update.go -- the Push Hack update bar and its OK / CANCEL prompt on the
// Shadow UI. Plan: plans/2026-10-20-push-hack-self-update.md.
//
// A yellow bar covers the last 14 px of the content area while a newer
// release is known and not dismissed. The 8th under-screen button is UPDATE.
// It opens a prompt: button 7 is OK, button 8 is CANCEL. The opening button
// cancels, so a double tap never installs. The prompt shuts itself after 15 s.
//
// Text here reaches the screen: ASCII only (see CLAUDE.md).

import (
	"image"
	"image/color"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
	"github.com/federico-pepe/ableton-push-hack/core/push3"

	"push-manager/update"
)

const (
	updBarH       = 14
	updPromptWait = 15 * time.Second
	updNoticeFor  = 4 * time.Second
)

var (
	updateChecker *update.Checker // set by startUpdateChecker
	updatePrompt  = &update.Prompt{Open: CCScreenBot8, OK: CCScreenBot7, Cancel: CCScreenBot8, Timeout: updPromptWait}

	updMu     sync.Mutex
	updNotice string
	updUntil  time.Time
)

var (
	updYellow = push3.ColorForIndex(colorIdx("yellow")).RGB
	updDark   = color.NRGBA{22, 22, 24, 255}
)

func colorIdx(name string) uint8 { i, _ := push3.ColorByName(name); return i }

// updatePending is the version the bar offers, if any.
func updatePending() (string, bool) {
	if updateChecker == nil {
		return "", false
	}
	return updateChecker.Pending()
}

// updateInstall runs when the user presses OK. Installing is not built yet;
// say so on the bar instead of doing nothing.
func updateInstall(version string) {
	updMu.Lock()
	updNotice = "Install is not available yet"
	updUntil = time.Now().Add(updNoticeFor)
	updMu.Unlock()
}

// updateCC offers a button event to the update bar. It reports whether the
// event was used, so the panel must not see it.
func updateCC(cc, val uint8) bool {
	v, pending := updatePending()
	consumed, install := updatePrompt.Press(cc, val, pending, time.Now())
	if install {
		updateInstall(v)
	}
	if consumed && val == 127 {
		go shadowUIRefreshBotLEDs() // the prompt changed which buttons are lit
	}
	return consumed
}

// updateStrip puts UPDATE, or OK and CANCEL, on the bottom strip.
func updateStrip(buttons [8]widgets.SoftButton, hint string) ([8]widgets.SoftButton, string) {
	_, pending := updatePending()
	switch {
	case updatePrompt.Active(time.Now(), pending):
		buttons = [8]widgets.SoftButton{}
		buttons[6] = widgets.SoftButton{Label: "OK", State: widgets.SoftOn}
		buttons[7] = widgets.SoftButton{Label: "CANCEL", State: widgets.SoftOff}
		return buttons, ""
	case pending:
		buttons[7] = widgets.SoftButton{Label: "UPDATE", Color: updYellow}
	}
	return buttons, hint
}

// updateOverrideLEDs overrides the under-screen LEDs the update uses.
func updateOverrideLEDs(colors *[8]uint8) {
	_, pending := updatePending()
	switch {
	case updatePrompt.Active(time.Now(), pending):
		*colors = [8]uint8{}
		colors[6] = colorIdx("green_vv")
		colors[7] = colorIdx("red")
	case pending:
		colors[7] = colorIdx("yellow")
	}
}

// updateBarShown is true while the bar or the prompt is up; the render loop
// watches it to refresh the LEDs when it changes.
func updateBarShown() bool {
	_, pending := updatePending()
	return pending
}

func centerX(s string) int { return (suiW - textWidth(s)) / 2 }

// drawUpdate draws the bar, and the prompt over it when open.
func drawUpdate(img *image.NRGBA) {
	v, pending := updatePending()
	if !pending {
		return
	}
	now := time.Now()
	if updatePrompt.Active(now, pending) {
		const w, h = 600, 76
		x, y := (suiW-w)/2, suiContentY+(suiContentH-h)/2
		fillRect(img, x-2, y-2, w+4, h+4, updYellow)
		fillRect(img, x, y, w, h, updDark)
		l1 := "Install Push Hack " + v + "?"
		l2 := "Apps go back to Live. The screen goes dark for a moment."
		l3 := "Press OK to install, or CANCEL."
		drawText(img, centerX(l1), y+22, l1, updYellow)
		drawText(img, centerX(l2), y+44, l2, suiWhite)
		drawText(img, centerX(l3), y+64, l3, suiWhite)
		return
	}
	y := suiContentBot - updBarH
	fillRect(img, 0, y, suiW, updBarH, updYellow)
	msg := "Push Hack " + v + " is available"
	updMu.Lock()
	if now.Before(updUntil) {
		msg = updNotice
	}
	updMu.Unlock()
	drawText(img, 8, y+updBarH-3, msg, updDark)
}
