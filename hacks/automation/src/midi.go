package main

// midi.go — minimal ALSA sequencer CC output for the automation engine.
// Sends MIDI CC events directly to "Ableton Push 3 Live Port" via /dev/snd/seq.
// No subscription, no LED control, no ring buffer — CC output only.
//
// Patterns copied from push-manager/src/midi.go.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// ── ALSA sequencer constants ───────────────────────────────────────────────

const (
	seqDev = "/dev/snd/seq"

	ioctlClientID   = uintptr(0x80045301) // _IOR('S',0x01, int32=4)
	ioctlCreatePort = uintptr(0xC0A85320) // _IOWR('S',0x20, portInfo=168)

	portOffAddrClient = 0
	portOffAddrPort   = 1
	portOffName       = 2
	portOffCapability = 68
	portOffType       = 72
	portInfoSize      = 168

	capRead      = uint32(0x01)
	capSubsRead  = uint32(0x20)
	capWrite     = uint32(0x02)
	capSubsWrite = uint32(0x40)

	seqQueueDirect = byte(253)

	portTypeMidi = uint32(1 << 1)
	portTypeApp  = uint32(1 << 20)

	seqEvOffType    = 0
	seqEvOffData    = 16
	seqEventSize    = 28
	seqEvController = uint8(10)

	// Clock / transport event types
	seqEvStart    = uint8(30)
	seqEvContinue = uint8(32)
	seqEvStop     = uint8(31)
	seqEvClock    = uint8(36)

	// Subscribe struct constants (identical to push-manager)
	ioctlSubscribePort = uintptr(0x40505330) // _IOW('S', 0x30, subscribe=80)
	subSize            = 80
	subOffSenderClient = 0
	subOffSenderPort   = 1
	subOffDestClient   = 2
	subOffDestPort     = 3

	push3ClientDefault = byte(16)
	push3PortDefault   = byte(0)
)

// ── Global MIDI output state ───────────────────────────────────────────────

var (
	midiTargetMu     sync.Mutex
	midiTargetClient = push3ClientDefault
	midiTargetPort   = push3PortDefault

	liveDestMu     sync.Mutex
	liveDestClient = byte(128) // Ableton Live — may shift, updated by detectLivePort
	liveDestPort   = byte(2)
	liveDestFound  bool

	midiOutMu     sync.Mutex
	midiOutFd     = -1
	midiOutClient = byte(0)
	midiOutPort   = byte(0)

	clockMu    sync.Mutex
	clockRing  [24]int64 // nanosecond timestamps, ring indexed by clockTotal%24
	clockTotal int64     // total MIDI clock ticks received

	midiInMu sync.Mutex
	midiInFd = -1
)

// ── Boot-settle gate (USB-A safety) ───────────────────────────────────────

const bootSettleSecs = 30.0

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
		log.Printf("midi: deferring ALSA init %.1fs (uptime %.1fs < %.0fs boot-settle, USB-A safety)",
			wait.Seconds(), up, bootSettleSecs)
		time.Sleep(wait)
	}
}

func initMidiOut() {
	detectLivePort()

	fd, err := syscall.Open(seqDev, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		log.Printf("midi_out: open %s: %v (CC output disabled)", seqDev, err)
		return
	}

	var clientID int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		ioctlClientID, uintptr(unsafe.Pointer(&clientID))); errno != 0 {
		syscall.Close(fd)
		log.Printf("midi_out: CLIENT_ID ioctl: %v (CC output disabled)", errno)
		return
	}

	portInfo := make([]byte, portInfoSize)
	portInfo[portOffAddrClient] = byte(clientID)
	copy(portInfo[portOffName:], "Push Hack Automation\x00")
	binary.LittleEndian.PutUint32(portInfo[portOffCapability:], capRead|capSubsRead)
	binary.LittleEndian.PutUint32(portInfo[portOffType:], portTypeMidi|portTypeApp)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		ioctlCreatePort, uintptr(unsafe.Pointer(&portInfo[0]))); errno != 0 {
		syscall.Close(fd)
		log.Printf("midi_out: CREATE_PORT ioctl: %v (CC output disabled)", errno)
		return
	}
	ourPort := portInfo[portOffAddrPort]

	midiOutMu.Lock()
	midiOutFd = fd
	midiOutClient = byte(clientID)
	midiOutPort = ourPort
	midiOutMu.Unlock()

	liveDestMu.Lock()
	lc, lp := liveDestClient, liveDestPort
	liveDestMu.Unlock()
	log.Printf("midi_out: ready (client %d port %d → Live at %d:%d)", clientID, ourPort, lc, lp)
}

func push3Dest() (client, port byte) {
	midiTargetMu.Lock()
	c, p := midiTargetClient, midiTargetPort
	midiTargetMu.Unlock()
	return c, p
}

func liveDest() (client, port byte) {
	liveDestMu.Lock()
	c, p := liveDestClient, liveDestPort
	liveDestMu.Unlock()
	return c, p
}

// detectLivePort scans /proc/asound/seq/clients for "Ableton Live" and finds
// its first writable non-Announce port — that is Live's MIDI input.
func detectLivePort() {
	f, err := os.Open("/proc/asound/seq/clients")
	if err != nil {
		return
	}
	defer f.Close()

	curClient := -1
	inLive := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(trimmed, "Client ") && !strings.HasPrefix(trimmed, "Client info") {
			rest := strings.TrimPrefix(trimmed, "Client ")
			colonIdx := strings.Index(rest, ":")
			if colonIdx < 0 {
				curClient = -1
				inLive = false
				continue
			}
			id, err2 := strconv.Atoi(strings.TrimSpace(rest[:colonIdx]))
			if err2 != nil {
				curClient = -1
				inLive = false
				continue
			}
			curClient = id
			inLive = strings.Contains(rest[colonIdx+1:], `"Ableton Live"`)
			continue
		}

		if inLive && strings.HasPrefix(trimmed, "Port ") && curClient >= 0 {
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

			parenOpen := strings.LastIndex(after, "(")
			parenClose := strings.LastIndex(after, ")")
			if parenOpen < 0 || parenClose <= parenOpen {
				continue
			}
			caps := after[parenOpen+1 : parenClose]
			if !strings.Contains(caps, "W") {
				continue
			}

			// Skip "Announce" port
			q1 := strings.Index(after, `"`)
			if q1 >= 0 {
				q2 := strings.Index(after[q1+1:], `"`)
				if q2 >= 0 && after[q1+1:q1+1+q2] == "Announce" {
					continue
				}
			}

			liveDestMu.Lock()
			oldC, oldP, oldFound := liveDestClient, liveDestPort, liveDestFound
			liveDestClient = byte(curClient)
			liveDestPort = byte(portID)
			liveDestFound = true
			liveDestMu.Unlock()
			if !oldFound || oldC != byte(curClient) || oldP != byte(portID) {
				log.Printf("midi: detected Ableton Live MIDI input at %d:%d", curClient, portID)
			}
			return
		}
	}

	liveDestMu.Lock()
	found := liveDestFound
	liveDestMu.Unlock()
	if !found {
		log.Printf("midi: Ableton Live writable port not found — CC inactive until Live starts")
	}
}

// sendSeqCC sends a single MIDI CC event to Live's ALSA seq input port.
// channel is 0-indexed; cc and value are 0–127.
func sendSeqCC(channel, cc byte, value int32) error {
	midiOutMu.Lock()
	fd := midiOutFd
	srcClient := midiOutClient
	srcPort := midiOutPort
	midiOutMu.Unlock()
	if fd < 0 {
		return fmt.Errorf("midi_out not initialized")
	}

	dstClient, dstPort := liveDest()

	data := make([]byte, 12)
	data[0] = channel
	binary.LittleEndian.PutUint32(data[4:], uint32(cc))
	binary.LittleEndian.PutUint32(data[8:], uint32(value))

	return writeSeqEvent(fd, seqEvController, srcClient, srcPort, dstClient, dstPort, data)
}

func writeSeqEvent(fd int, evType, srcClient, srcPort, dstClient, dstPort byte, data []byte) error {
	ev := make([]byte, seqEventSize)
	ev[seqEvOffType] = evType
	ev[1] = 0
	ev[2] = 0
	ev[3] = seqQueueDirect
	ev[12] = srcClient
	ev[13] = srcPort
	ev[14] = dstClient
	ev[15] = dstPort
	copy(ev[seqEvOffData:], data)

	midiOutMu.Lock()
	defer midiOutMu.Unlock()
	if midiOutFd != fd {
		return fmt.Errorf("midi_out fd invalidated")
	}
	_, err := syscall.Write(fd, ev)
	return err
}

// initMidiIn creates an ALSA seq input port and subscribes to Push 3's live port
// to receive MIDI clock, start, and stop events.
func initMidiIn() {
	midiTargetMu.Lock()
	srcClient := midiTargetClient
	srcPort := midiTargetPort
	midiTargetMu.Unlock()

	fd, err := syscall.Open(seqDev, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		log.Printf("midi_in: open: %v", err)
		return
	}

	var clientID int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		ioctlClientID, uintptr(unsafe.Pointer(&clientID))); errno != 0 {
		syscall.Close(fd)
		log.Printf("midi_in: CLIENT_ID: %v", errno)
		return
	}

	portInfo := make([]byte, portInfoSize)
	portInfo[portOffAddrClient] = byte(clientID)
	copy(portInfo[portOffName:], "Push Hack Clock\x00")
	binary.LittleEndian.PutUint32(portInfo[portOffCapability:], capWrite|capSubsWrite)
	binary.LittleEndian.PutUint32(portInfo[portOffType:], portTypeMidi|portTypeApp)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		ioctlCreatePort, uintptr(unsafe.Pointer(&portInfo[0]))); errno != 0 {
		syscall.Close(fd)
		log.Printf("midi_in: CREATE_PORT: %v", errno)
		return
	}
	ourPort := portInfo[portOffAddrPort]

	sub := make([]byte, subSize)
	sub[subOffSenderClient] = srcClient
	sub[subOffSenderPort] = srcPort
	sub[subOffDestClient] = byte(clientID)
	sub[subOffDestPort] = ourPort

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		ioctlSubscribePort, uintptr(unsafe.Pointer(&sub[0]))); errno != 0 {
		log.Printf("midi_in: SUBSCRIBE_PORT: %v — MIDI clock unavailable", errno)
	} else {
		log.Printf("midi_in: subscribed to %d:%d (client %d port %d)", srcClient, srcPort, clientID, ourPort)
	}

	midiInMu.Lock()
	midiInFd = fd
	midiInMu.Unlock()

	go readMidiEvents(fd)
}

func readMidiEvents(fd int) {
	buf := make([]byte, 8192)
	for {
		n, err := syscall.Read(fd, buf)
		if err != nil || n < seqEventSize {
			log.Printf("midi_in: read stopped: %v", err)
			return
		}
		for off := 0; off+seqEventSize <= n; off += seqEventSize {
			evType := buf[off+seqEvOffType]
			switch evType {
			case seqEvClock:
				onMidiClock()
			case seqEvStart, seqEvContinue:
				onMidiTransportStart()
			case seqEvController:
				if off+seqEvOffData+12 <= n {
					ch := buf[off+seqEvOffData]
					cc := binary.LittleEndian.Uint32(buf[off+seqEvOffData+4:])
					val := binary.LittleEndian.Uint32(buf[off+seqEvOffData+8:])
					if ch == 0 && cc == 85 && val == 127 {
						onPlayButtonPress()
					}
				}
			}
		}
	}
}

// onPlayButtonPress handles CC85 val=127 (Push Play button press).
// When TransportSync is on this is the SOLE authority over Running — Push has
// no stop button, so each press toggles play/stop. MIDI Start/Stop events and
// the tempo poll deliberately do NOT touch Running (they used to fight this
// toggle and desynced the WebUI).
func onPlayButtonPress() {
	autoMu.Lock()
	if autoState.TransportSync {
		autoState.Running = !autoState.Running
		running := autoState.Running
		if !running {
			atomic.StoreUint32(&resetPhasesRequested, 1)
		}
		log.Printf("midi: Play button (CC85) → automation %v", running)
	}
	autoMu.Unlock()
}

func onMidiClock() {
	now := time.Now().UnixNano()
	clockMu.Lock()
	i := int(clockTotal % 24)
	prev := clockRing[i]
	clockRing[i] = now
	clockTotal++
	clockMu.Unlock()

	if prev > 0 {
		elapsed := float64(now-prev) * 1e-9
		if elapsed > 0.04 && elapsed < 10.0 { // sanity: ~6–1500 BPM
			bpm := 60.0 / elapsed
			liveBPMMu.Lock()
			liveBPM = bpm
			liveBPMMu.Unlock()
		}
	}
}

// onMidiTransportStart resets the BPM clock ring so tempo measurement restarts
// cleanly on a fresh transport start. It does NOT touch Running — the Push Play
// button (CC85) is the sole transport authority (see onPlayButtonPress).
func onMidiTransportStart() {
	clockMu.Lock()
	clockTotal = 0
	for i := range clockRing {
		clockRing[i] = 0
	}
	clockMu.Unlock()
}

// detectPush3Port scans /proc/asound/seq/clients for "Ableton Push 3 Live Port"
// and updates midiTargetClient/Port. Handles shifted client numbers when extra
// USB MIDI devices are connected at boot.
func detectPush3Port() {
	const push3PortName = "Ableton Push 3 Live Port"
	ports, err := enumMidiPorts()
	if err != nil {
		return
	}
	for _, p := range ports {
		if p.Name == push3PortName {
			c, port := byte(p.Client), byte(p.Port)
			midiTargetMu.Lock()
			if c != midiTargetClient || port != midiTargetPort {
				log.Printf("midi: auto-detected Push 3 at %d:%d (was %d:%d)",
					c, port, midiTargetClient, midiTargetPort)
				midiTargetClient = c
				midiTargetPort = port
			}
			midiTargetMu.Unlock()
			return
		}
	}
	log.Printf("midi: Push 3 Live Port not found — using %d:%d",
		midiTargetClient, midiTargetPort)
}

type midiPort struct {
	Client int
	Port   int
	Name   string
}

func enumMidiPorts() ([]midiPort, error) {
	f, err := os.Open("/proc/asound/seq/clients")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var ports []midiPort
	curClient := -1

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Client ") && !strings.HasPrefix(trimmed, "Client info") {
			rest := strings.TrimPrefix(trimmed, "Client ")
			colonIdx := strings.Index(rest, ":")
			if colonIdx < 0 {
				continue
			}
			id, err2 := strconv.Atoi(strings.TrimSpace(rest[:colonIdx]))
			if err2 != nil {
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
			parenOpen := strings.LastIndex(after, "(")
			parenClose := strings.LastIndex(after, ")")
			if parenOpen < 0 || parenClose <= parenOpen {
				continue
			}
			caps := after[parenOpen+1 : parenClose]
			if !strings.Contains(caps, "R") {
				continue
			}
			q1 := strings.Index(after, `"`)
			if q1 < 0 {
				continue
			}
			q2 := strings.Index(after[q1+1:], `"`)
			if q2 < 0 {
				continue
			}
			portName := after[q1+1 : q1+1+q2]
			ports = append(ports, midiPort{Client: curClient, Port: portID, Name: portName})
		}
	}
	return ports, scanner.Err()
}
