package alsaseq

// Boot-settle gate (USB-A safety) — verbatim move of
// hacks/push-manager/src/midi.go:171-190.
//
// Opening /dev/snd (ALSA seq) while a USB-Audio device plugged into the
// USB-A port is still enumerating — the ~3-15s window after a COLD power-on
// — can prevent that device from enumerating at all, wedging the USB-A port
// until a power cycle. push-manager starts early at boot (init.d S20),
// squarely inside that window. WaitForBootSettle defers the first /dev/snd
// access until the system has been up long enough for USB-A enumeration to
// finish. It is a no-op when a hack is (re)started later on an
// already-running system.

import (
	"fmt"
	"log"
	"os"
	"time"
)

const BootSettleSecs = 30.0

// WaitForBootSettle blocks until system uptime reaches BootSettleSecs.
// Keeps the "midi:" log prefix so log-grep habits survive the move.
func WaitForBootSettle() {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return // can't read uptime — proceed without delay
	}
	var up float64
	if _, err := fmt.Sscanf(string(data), "%f", &up); err != nil {
		return
	}
	if up < BootSettleSecs {
		wait := time.Duration((BootSettleSecs - up) * float64(time.Second))
		log.Printf("midi: deferring ALSA init %.1fs (uptime %.1fs < %.0fs boot-settle, USB-A safety)",
			wait.Seconds(), up, BootSettleSecs)
		time.Sleep(wait)
	}
}
