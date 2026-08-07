package main

// render.go — draws the keyboard as a plain image.NRGBA and pushes it to
// push-manager's display API (POST /api/display/image, takeover mode).
// This hack never touches the shared-memory framebuffer directly — it's an
// HTTP client of push-manager for display purposes only, so it stays free
// of shm/BGR565 concerns.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"mime/multipart"
	"net/http"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

const (
	screenW = push3.VisW
	screenH = push3.VisH

	// Fixed 49-key window (4 octaves) centered on middle C (60): notes 36-84.
	// Simpler than an auto-ranging window; revisit if a fixed span proves too
	// limiting (see discovery/live-note-keyboard-viz.md open questions).
	noteLo = 36
	noteHi = 84
)

var (
	colWhite   = color.NRGBA{230, 230, 235, 255}
	colBlack   = color.NRGBA{20, 20, 24, 255}
	colPressed = color.NRGBA{0, 200, 130, 255}
	colBg      = color.NRGBA{0, 0, 0, 255}
)

func isBlackKey(note int) bool {
	switch note % 12 {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

func fillRect(img *image.NRGBA, x, y, w, h int, c color.NRGBA) {
	gfx.FillRect(img, x, y, w, h, c)
}

// renderKeyboard draws the current held-note state into a fresh 960x160 image.
func renderKeyboard(held [128]bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))
	fillRect(img, 0, 0, screenW, screenH, colBg)

	whiteCount := 0
	for n := noteLo; n <= noteHi; n++ {
		if !isBlackKey(n) {
			whiteCount++
		}
	}
	if whiteCount == 0 {
		return img
	}
	whiteW := screenW / whiteCount
	blackW := whiteW * 6 / 10
	blackH := screenH * 6 / 10

	// Pass 1: white keys.
	whiteX := map[int]int{}
	idx := 0
	for n := noteLo; n <= noteHi; n++ {
		if isBlackKey(n) {
			continue
		}
		x := idx * whiteW
		whiteX[n] = x
		col := colWhite
		if held[n] {
			col = colPressed
		}
		fillRect(img, x, 0, whiteW-1, screenH, col)
		idx++
	}

	// Pass 2: black keys, drawn on top, anchored to the preceding white key.
	for n := noteLo; n <= noteHi; n++ {
		if !isBlackKey(n) {
			continue
		}
		anchor, ok := whiteX[n-1]
		if !ok {
			anchor, ok = whiteX[n+1]
			if !ok {
				continue
			}
		}
		x := anchor + whiteW - blackW/2
		col := colBlack
		if held[n] {
			col = colPressed
		}
		fillRect(img, x, 0, blackW, blackH, col)
	}

	return img
}

// pushFrame POSTs a rendered frame to push-manager's display image endpoint.
func pushFrame(pushManagerURL string, img *image.NRGBA) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("encode png: %w", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", "frame.png")
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write image: %w", err)
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, pushManagerURL+"/api/display/image", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("display/image: unexpected status %s", resp.Status)
	}
	return nil
}

// setDisplayMode sets push-manager's display mode (0=passthrough, 2=takeover).
func setDisplayMode(pushManagerURL string, mode int) error {
	req, err := http.NewRequest(http.MethodPost, pushManagerURL+"/api/display/mode",
		bytes.NewBufferString(fmt.Sprintf(`{"mode":%d}`, mode)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("display/mode: unexpected status %s", resp.Status)
	}
	return nil
}

var httpClient = &http.Client{Timeout: 3 * time.Second}

// takeoverState tracks whether this hack currently owns the display, toggled
// by the Shift+Note hardware chord (see chord.go) — not on by default, so
// the native Push UI is undisturbed until the user asks for the keyboard.
var (
	takeoverMu    sync.Mutex
	takeoverOn    bool
	lastHeld      [128]bool
	lastHeldValid bool
)

// toggleTakeover flips display ownership: entering takeover mode and forcing
// an immediate frame, or releasing back to passthrough so the native Push UI
// resumes.
func toggleTakeover(pushManagerURL string) {
	takeoverMu.Lock()
	takeoverOn = !takeoverOn
	on := takeoverOn
	takeoverMu.Unlock()

	held := snapshotHeldNotes()
	takeoverMu.Lock()
	lastHeld = held
	lastHeldValid = true
	takeoverMu.Unlock()

	if on {
		if err := setDisplayMode(pushManagerURL, 2); err != nil {
			log.Printf("render: enable takeover: %v", err)
		}
		img := renderKeyboard(held)
		if err := pushFrame(pushManagerURL, img); err != nil {
			log.Printf("render: push frame: %v", err)
		}
		log.Printf("keyboard-visualizer: takeover ON (Shift+Note)")
	} else {
		if err := setDisplayMode(pushManagerURL, 0); err != nil {
			log.Printf("render: disable takeover: %v", err)
		}
		log.Printf("keyboard-visualizer: takeover OFF (Shift+Note)")
	}
}

// checkHeldChanged snapshots held-note state and reports whether it changed
// since the last check, updating the stored snapshot either way.
func checkHeldChanged() (held [128]bool, changed bool) {
	held = snapshotHeldNotes()
	takeoverMu.Lock()
	changed = !lastHeldValid || held != lastHeld
	lastHeld = held
	lastHeldValid = true
	takeoverMu.Unlock()
	return held, changed
}

// runRenderLoop polls held-note state at 10fps. On every change it
// broadcasts to the web view's SSE clients (always, regardless of on-device
// takeover state) and, only while takeover is on (toggled via the Shift+Note
// chord), also pushes an updated frame to Push's own screen.
func runRenderLoop(pushManagerURL string) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		held, changed := checkHeldChanged()
		if !changed {
			continue
		}
		broadcastNotes(held)

		takeoverMu.Lock()
		on := takeoverOn
		takeoverMu.Unlock()
		if !on {
			continue
		}
		img := renderKeyboard(held)
		if err := pushFrame(pushManagerURL, img); err != nil {
			log.Printf("render: push frame: %v", err)
		}
	}
}
