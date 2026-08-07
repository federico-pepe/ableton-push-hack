// Package alsaseq talks to /dev/snd/seq directly via ioctls (no cgo, no
// subprocess) — the ALSA sequencer layer push-manager, automation and
// keyboard-visualizer each hand-rolled independently. This file is a
// verbatim, single-commit move of the const block from
// hacks/push-manager/src/midi.go:42-126 — diff it against that file's
// git history (and against automation's/keyboard-visualizer's own copies)
// before they're deleted. One wrong digit here (e.g. in the ioctl numbers)
// produces an EPERM at CREATE_PORT that looks like an unrelated permissions
// problem.
package alsaseq

const (
	Dev = "/dev/snd/seq"

	// ioctl codes: _IOR/_IOW/_IOWR('S', nr, sizeof_struct)
	//   _IOR  = (2<<30) | (size<<16) | ('S'<<8) | nr
	//   _IOW  = (1<<30) | (size<<16) | ('S'<<8) | nr
	//   _IOWR = (3<<30) | (size<<16) | ('S'<<8) | nr
	IoctlClientID      = uintptr(0x80045301) // _IOR('S',0x01, int32=4)
	IoctlCreatePort    = uintptr(0xC0A85320) // _IOWR('S',0x20, portInfo=168)
	IoctlSubscribePort = uintptr(0x40505330) // _IOW('S', 0x30, subscribe=80)

	// snd_seq_port_info byte offsets (168 bytes total, x86-64):
	//   0:  addr.client (1B), addr.port (1B)
	//   2:  name[64]
	//  66:  (2 bytes padding for uint32 alignment)
	//  68:  capability (uint32)
	//  72:  type (uint32)
	//  76–95: midi_channels/voices/synth_voices/read_use/write_use (5×int32)
	//  96:  kernel ptr (8B, 8-byte aligned)
	// 104:  flags (uint32)
	// 108:  time_queue (1B)
	// 109:  reserved[59]
	PortOffAddrClient = 0
	PortOffAddrPort   = 1
	PortOffName       = 2
	PortOffCapability = 68
	PortOffType       = 72
	PortInfoSize      = 168

	// Port capability bits (snd_seq_port_info.capability)
	CapRead      = uint32(0x01) // can send events out (readable by others)
	CapWrite     = uint32(0x02) // can receive events (writable)
	CapSubsRead  = uint32(0x20) // allow read subscriptions
	CapSubsWrite = uint32(0x40) // allow write subscriptions

	// Direct-delivery queue: bypass sequencer queue, deliver immediately.
	QueueDirect = byte(253) // SNDRV_SEQ_QUEUE_DIRECT

	// Port type bits
	PortTypeMidi = uint32(1 << 1)  // MIDI generic
	PortTypeApp  = uint32(1 << 20) // application

	// snd_seq_port_subscribe byte offsets (80 bytes):
	//   0: sender.client (1B), sender.port (1B)
	//   2: dest.client (1B), dest.port (1B)
	//   4: voices (uint32)
	//   8: flags (uint32)
	//  12: queue (1B), pad[3]
	//  16: reserved[64]
	SubOffSenderClient = 0
	SubOffSenderPort   = 1
	SubOffDestClient   = 2
	SubOffDestPort     = 3
	SubSize            = 80

	// snd_seq_event layout (28 bytes):
	//   0: type (1B), flags (1B), tag (1B), queue (1B)
	//   4: timestamp union (8B)
	//  12: src.client (1B), src.port (1B), dst.client (1B), dst.port (1B)
	//  16: data union (12B)
	EventOffType      = 0
	EventOffFlags     = 1
	EventOffSrcClient = 12
	EventOffSrcPort   = 13
	EventOffData      = 16
	EventSize         = 28

	FlagVarLen = uint8(1 << 2) // variable-length event

	// ALSA sequencer event types — union of all three hacks' subsets.
	EvNoteOn     = uint8(6)
	EvNoteOff    = uint8(7)
	EvKeyPress   = uint8(8)   // poly aftertouch
	EvController = uint8(10)  // CC, pitch bend (all control events)
	EvPgmChange  = uint8(11)
	EvChanPress  = uint8(12)  // channel aftertouch
	EvPitchBend  = uint8(13)
	EvStart      = uint8(30)
	EvStop       = uint8(31)
	EvContinue   = uint8(32)
	EvClock      = uint8(36)
	EvSensing    = uint8(40)  // active sensing
	EvSysEx      = uint8(130) // variable length

	// Push 3 ALSA seq address defaults (kernel client 16 = first sound card).
	// Shift if external MIDI devices are connected at boot — use port selector.
	Push3ClientDefault = byte(16)
	Push3PortDefault    = byte(0) // "Ableton Push 3 Live Port"
)
