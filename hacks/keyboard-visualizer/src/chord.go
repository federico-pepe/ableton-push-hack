package main

// chord.go — Shift+Note (CC49+CC50, docs/push3-button-map.md) chord detection
// that toggles display takeover on/off. The CC events themselves arrive via
// the "Keyboard Viz In" port's self-subscription to Push 3's hardware port
// (see maintainPush3Subscription/processSeqBuf in midi.go) — this file only
// holds the chord state machine and the Push 3 port-detection helper.
// Pattern mirrors push-manager's existing hardware-chord detector
// (hacks/push-manager/src/midi.go: chordCCPressed/chordCCReleased) but
// scoped to this single hardcoded chord.

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ccShift = uint8(49)
	ccNote  = uint8(50)

	chordDebounce = 500 * time.Millisecond
)

var (
	chordMu       sync.Mutex
	chordHeld     = map[uint8]bool{}
	chordLastFire time.Time
)

// onChordCC is called for every CC49/CC50 event seen on the shared "Keyboard
// Viz In" port. When both are held together (debounced 500ms), toggles
// display takeover.
func onChordCC(cc, val byte, pushManagerURL string) {
	chordMu.Lock()
	if val > 0 {
		chordHeld[cc] = true
	} else {
		delete(chordHeld, cc)
	}
	fire := chordHeld[ccShift] && chordHeld[ccNote]
	if fire {
		now := time.Now()
		if now.Sub(chordLastFire) < chordDebounce {
			fire = false
		} else {
			chordLastFire = now
		}
	}
	chordMu.Unlock()

	if fire {
		toggleTakeover(pushManagerURL)
	}
}

// detectPush3Port scans /proc/asound/seq/clients for "Ableton Push 3 Live Port".
func detectPush3Port() (client, port byte, ok bool) {
	const push3PortName = "Ableton Push 3 Live Port"

	f, err := os.Open("/proc/asound/seq/clients")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	curClient := -1
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(trimmed, "Client ") && !strings.HasPrefix(trimmed, "Client info") {
			rest := strings.TrimPrefix(trimmed, "Client ")
			colonIdx := strings.Index(rest, ":")
			if colonIdx < 0 {
				curClient = -1
				continue
			}
			id, err2 := strconv.Atoi(strings.TrimSpace(rest[:colonIdx]))
			if err2 != nil {
				curClient = -1
				continue
			}
			curClient = id
			continue
		}

		if strings.HasPrefix(trimmed, "Port ") && curClient >= 0 {
			rest := strings.TrimPrefix(trimmed, "Port ")
			colonIdx := strings.Index(rest, ":")
			if colonIdx < 0 {
				continue
			}
			portID, err2 := strconv.Atoi(strings.TrimSpace(rest[:colonIdx]))
			if err2 != nil {
				continue
			}
			after := rest[colonIdx+1:]
			q1 := strings.Index(after, `"`)
			if q1 < 0 {
				continue
			}
			q2 := strings.Index(after[q1+1:], `"`)
			if q2 < 0 {
				continue
			}
			name := after[q1+1 : q1+1+q2]
			if name == push3PortName {
				return byte(curClient), byte(portID), true
			}
		}
	}
	return 0, 0, false
}
