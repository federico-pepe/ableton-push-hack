package main

// midi.go — creates an ALSA seq port ("Random Preset In") and self-subscribes
// it to Push 3's hardware port to WATCH the CC stream for the Shift+Add chord.
// An ALSA port can receive from multiple senders, so this never disturbs
// push-manager's own subscription. Port-creation + boot-settle + subscription
// pattern lifted from keyboard-visualizer/src/midi.go.

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

const bootSettleSecs = 30.0

// waitForBootSettle defers ALSA seq access until uptime >= 30s — opening
// /dev/snd during the cold-boot USB-A enumeration window can wedge the port
// until a power cycle. Same fix as every other MIDI-touching hack.
func waitForBootSettle() {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return
	}
	var up float64
	if _, err := fmt.Sscanf(string(data), "%f", &up); err != nil {
		return
	}
	if up < bootSettleSecs {
		wait := time.Duration((bootSettleSecs - up) * float64(time.Second))
		log.Printf("midi: deferring ALSA init %.1fs (uptime %.1fs < %.0fs boot-settle)", wait.Seconds(), up, bootSettleSecs)
		time.Sleep(wait)
	}
}

func initMidiIn(pmURL string) error {
	c, err := alsaseq.Open()
	if err != nil {
		return err
	}
	defer c.Close()

	if _, err := c.CreatePort("Random Preset In", alsaseq.CapWrite|alsaseq.CapSubsWrite, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
		return err
	}
	log.Printf("midi: \"Random Preset In\" ready at %d:%d", c.Addr().Client, c.Addr().Port)

	go maintainPush3Subscription(c)
	return c.ReadLoop(rpHandler{pmURL: pmURL})
}

// maintainPush3Subscription (re)subscribes to Push 3's hardware port every 30s,
// logging only on change — handles Push 3 not enumerated yet, or its client
// number shifting later.
func maintainPush3Subscription(c *alsaseq.Client) {
	var lastClient, lastPort byte
	var lastFound bool
	for {
		client, port, ok := detectPush3Port()
		if ok && (!lastFound || client != lastClient || port != lastPort) {
			if err := c.Subscribe(alsaseq.Addr{Client: client, Port: port}); err != nil {
				log.Printf("midi: subscribe to Push 3 (%d:%d): %v", client, port, err)
			} else {
				log.Printf("midi: watching Push 3 (%d:%d) for Shift+Add / Shift+Swap", client, port)
				lastClient, lastPort, lastFound = client, port, true
			}
		}
		time.Sleep(30 * time.Second)
	}
}

// rpHandler feeds every CC49/CC32 event to the chord detector. Everything else
// (notes, SysEx) is ignored — this hack only cares about the two chord buttons.
type rpHandler struct{ pmURL string }

func (h rpHandler) Fixed(evType uint8, src alsaseq.Addr, data []byte) {
	if evType != alsaseq.EvController {
		return
	}
	cc := byte(binary.LittleEndian.Uint32(data[4:]))
	val := byte(binary.LittleEndian.Uint32(data[8:]))
	if triggers[cc] {
		onChordCC(cc, val, h.pmURL)
	}
}

func (h rpHandler) VarLen(evType uint8, src alsaseq.Addr, payload []byte) {}

// runMidiIn retries forever so a transient ALSA error doesn't kill the process.
func runMidiIn(pmURL string) {
	for {
		if err := initMidiIn(pmURL); err != nil {
			log.Printf("midi: %v — retrying in 5s", err)
		}
		time.Sleep(5 * time.Second)
	}
}
