package main

// midi.go — creates a single writable ALSA sequencer port ("Keyboard Viz In")
// that a Live track's MIDI Out can be routed to (from Push's own on-screen
// routing UI). This delivers Live's actual sounding notes — after any
// octave-shift / Scale-mode transform — as opposed to the pad grid's raw
// pre-transform MIDI. Live initiates that connection when the user picks
// this port as a routing destination — no subscription needed on our side.
//
// The same port is ALSO used to watch Push 3's own hardware CC stream for
// the Shift+Note display-takeover chord (see chord.go): an ALSA seq port can
// receive from multiple senders at once, so rather than creating a second
// visible ALSA client just to watch two CCs, we self-subscribe this port to
// Push 3's hardware port too. Only one "Keyboard Viz" interface shows up in
// Live's MIDI routing as a result.
//
// Port-creation pattern copied from push-manager/src/midi.go (its own
// "Push Manager In" port, midi.go:838-848) and automation/src/midi.go.

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ── ALSA sequencer constants (identical layout to push-manager/automation) ──

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

	capWrite     = uint32(0x02)
	capSubsWrite = uint32(0x40)

	portTypeMidi = uint32(1 << 1)
	portTypeApp  = uint32(1 << 20)

	seqEvOffType      = 0
	seqEvOffFlags     = 1
	seqEvOffSrcClient = 12
	seqEvOffSrcPort   = 13
	seqEvOffData      = 16
	seqEventSize      = 28

	seqFlagVarLen = uint8(1 << 2)

	seqEvNoteOn  = uint8(6)
	seqEvNoteOff = uint8(7)

	seqEvController = uint8(10)

	ioctlSubscribePort = uintptr(0x40505330) // _IOW('S', 0x30, subscribe=80)
	subSize            = 80
	subOffSenderClient = 0
	subOffSenderPort   = 1
	subOffDestClient   = 2
	subOffDestPort     = 3
)

const bootSettleSecs = 30.0

// waitForBootSettle defers ALSA seq access until uptime >= 30s — opening
// /dev/snd during the cold-boot USB-A enumeration window can wedge the port
// permanently until power-cycle. Same fix as push-manager/automation.
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

// heldNotes tracks which of the 128 MIDI notes are currently "on" — set on
// Note On, cleared on Note Off. A note is held from its Note-On until its
// matching Note-Off; no other state machine is needed. Guarded by its own
// mutex since ALSA delivery (readAlsaSeq's goroutine) and rendering (the
// render loop's goroutine) run concurrently.
var (
	heldNotesMu sync.Mutex
	heldNotes   [128]bool
)

func setNoteHeld(note byte, on bool) {
	if note > 127 {
		return
	}
	heldNotesMu.Lock()
	heldNotes[note] = on
	heldNotesMu.Unlock()
}

// snapshotHeldNotes returns a copy safe to read without holding the lock.
func snapshotHeldNotes() [128]bool {
	heldNotesMu.Lock()
	defer heldNotesMu.Unlock()
	return heldNotes
}

// push3Client tracks the ALSA client currently identified as Push 3's own
// hardware port. Note On/Off arriving from this client are the pad grid's
// raw pre-transform MIDI — the whole reason this hack exists is to show
// Live's post-transform notes instead, so those are filtered out. Only
// CC49/50 from this client are of interest (the takeover chord).
var (
	push3Mu    sync.Mutex
	push3ID    byte
	push3Known bool
)

func setPush3Client(client byte) {
	push3Mu.Lock()
	push3ID = client
	push3Known = true
	push3Mu.Unlock()
}

func isPush3Client(client byte) bool {
	push3Mu.Lock()
	defer push3Mu.Unlock()
	return push3Known && client == push3ID
}

// initMidiIn creates the "Keyboard Viz In" port, self-subscribes it to Push
// 3's hardware port (for the Shift+Note chord watch), and blocks reading
// incoming events. Runs forever (or until the fd errors); callers should run
// it in a goroutine and are free to restart it if it returns.
func initMidiIn(pushManagerURL string) error {
	fd, err := syscall.Open(seqDev, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", seqDev, err)
	}
	defer syscall.Close(fd)

	var clientID int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		ioctlClientID, uintptr(unsafe.Pointer(&clientID))); errno != 0 {
		return fmt.Errorf("CLIENT_ID ioctl: %w", errno)
	}

	portInfo := make([]byte, portInfoSize)
	portInfo[portOffAddrClient] = byte(clientID)
	copy(portInfo[portOffName:], "Keyboard Viz In\x00")
	binary.LittleEndian.PutUint32(portInfo[portOffCapability:], capWrite|capSubsWrite)
	binary.LittleEndian.PutUint32(portInfo[portOffType:], portTypeMidi|portTypeApp)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		ioctlCreatePort, uintptr(unsafe.Pointer(&portInfo[0]))); errno != 0 {
		return fmt.Errorf("CREATE_PORT ioctl: %w", errno)
	}
	ourPort := portInfo[portOffAddrPort]

	log.Printf("midi: \"Keyboard Viz In\" ready at client %d port %d — route a Live track's MIDI Out here",
		clientID, ourPort)

	// Self-subscribe to Push 3's hardware port so this same port also
	// receives CC49/50 (Shift+Note) for the takeover-toggle chord. Retried
	// in the background in case Push 3 isn't enumerated yet or its client
	// number shifts later.
	go maintainPush3Subscription(fd, byte(clientID), ourPort)

	buf := make([]byte, 8192)
	for {
		n, err := syscall.Read(fd, buf)
		if err != nil {
			return fmt.Errorf("read seq: %w", err)
		}
		processSeqBuf(buf[:n], pushManagerURL)
	}
}

// maintainPush3Subscription (re)subscribes fd's port to Push 3's hardware
// port every 30s, logging only on change — handles Push 3 not being
// enumerated yet at hack startup, and its client number shifting later.
func maintainPush3Subscription(fd int, clientID, ourPort byte) {
	var lastClient, lastPort byte
	var lastFound bool
	for {
		client, port, ok := detectPush3Port()
		if ok && (!lastFound || client != lastClient || port != lastPort) {
			sub := make([]byte, subSize)
			sub[subOffSenderClient] = client
			sub[subOffSenderPort] = port
			sub[subOffDestClient] = clientID
			sub[subOffDestPort] = ourPort
			if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
				ioctlSubscribePort, uintptr(unsafe.Pointer(&sub[0]))); errno != 0 {
				log.Printf("chord: subscribe to Push 3 (%d:%d): %v", client, port, errno)
			} else {
				log.Printf("chord: watching Push 3 (%d:%d) for Shift+Note (CC%d+CC%d) → toggle takeover",
					client, port, ccShift, ccNote)
				lastClient, lastPort, lastFound = client, port, true
				setPush3Client(client)
			}
		}
		time.Sleep(30 * time.Second)
	}
}

// processSeqBuf decodes fixed-length snd_seq_event records, ignoring
// variable-length ones (sysex etc.). Handles Note On/Off (Live's sounding
// notes only — Push 3's own raw pad Note On/Off is filtered out by source,
// see isPush3Client) and CC (Push 3 hardware, for the Shift+Note takeover
// chord).
func processSeqBuf(buf []byte, pushManagerURL string) {
	for off := 0; off+seqEventSize <= len(buf); {
		evType := buf[off+seqEvOffType]
		evFlags := buf[off+seqEvOffFlags]
		srcClient := buf[off+seqEvOffSrcClient]
		data := buf[off+seqEvOffData : off+seqEventSize]

		if evFlags&seqFlagVarLen != 0 {
			varLen := int(binary.LittleEndian.Uint32(data[0:]))
			end := off + seqEventSize + varLen
			if end > len(buf) {
				break
			}
			off = end
			continue
		}

		switch evType {
		case seqEvNoteOn:
			if !isPush3Client(srcClient) {
				note, vel := data[1], data[2]
				setNoteHeld(note, vel > 0) // vel=0 Note-On is a Note-Off
			}
		case seqEvNoteOff:
			if !isPush3Client(srcClient) {
				note := data[1]
				setNoteHeld(note, false)
			}
		case seqEvController:
			cc := byte(binary.LittleEndian.Uint32(data[4:]))
			val := byte(binary.LittleEndian.Uint32(data[8:]))
			if cc == ccShift || cc == ccNote {
				onChordCC(cc, val, pushManagerURL)
			}
		}
		off += seqEventSize
	}
}

// runMidiIn retries initMidiIn forever with a backoff, so a transient ALSA
// error (or Live not routed yet) doesn't kill the process.
func runMidiIn(pushManagerURL string) {
	for {
		if err := initMidiIn(pushManagerURL); err != nil {
			log.Printf("midi: %v — retrying in 5s", err)
		}
		time.Sleep(5 * time.Second)
	}
}
