package harmony

// sysex.go -- the wire format (docs/harmony-sync.md).
//
//   F0 7D 50 48 01 <type> <body> F7
//
// 7D is MIDI's manufacturer ID for non-commercial use, 50 48 is "PH" (Push
// Hack), 01 the version. Every body byte is 7-bit. A reader ignores types
// it does not know and bytes past the fields it knows, so the format can
// grow without breaking older followers.

// Message types.
const (
	// TypeHarmony: key, scale, chord, bass, when, flags.
	TypeHarmony = 0x01
	// TypeAlive: "still here", sent every second or so by a sender with
	// nothing new to say, so a follower can tell a quiet sender from a
	// gone one.
	TypeAlive = 0x02
	// TypeBye: the sender stops (it quits, or stops sending harmony).
	TypeBye = 0x03
)

const (
	version    = 0x01
	none7      = 0x7F
	flagPlay   = 0x01
	flagArmed  = 0x02
	harmonyLen = 10 // key, scale(2), root, chord(2), bass, delta(2), flags
)

var prefix = []byte{0xF0, 0x7D, 0x50, 0x48, version}

// MaxDelta is the furthest ahead a change can be announced, in pulses.
const MaxDelta = 1<<14 - 1

// Message is a decoded message.
type Message struct {
	Type    int
	Harmony Harmony // TypeHarmony only
	// Delta is when the harmony applies: this many clock pulses (24 a
	// quarter, Live's MIDI clock) after the message; 0 now.
	Delta int
}

// Encode is a HARMONY message: h applies delta pulses after it arrives.
func Encode(h Harmony, delta int) []byte {
	delta = min(max(delta, 0), MaxDelta)
	b := append([]byte(nil), prefix...)
	b = append(b, TypeHarmony,
		pc7(h.Key), byte(h.Scale>>7)&0x1F, byte(h.Scale)&0x7F,
		pc7(h.Root), byte(h.Chord>>7)&0x1F, byte(h.Chord)&0x7F,
		pc7(h.Bass), byte(delta>>7)&0x7F, byte(delta)&0x7F, flags(h), 0xF7)
	return b
}

// EncodeType is a message with no body: TypeAlive or TypeBye.
func EncodeType(t int) []byte {
	return append(append([]byte(nil), prefix...), byte(t)&0x7F, 0xF7)
}

// Decode reads a message. ok is false for anything that is not Push Hack
// Harmony, a newer version, or a HARMONY too short to hold its fields; a
// type this reader does not know decodes with only Type set.
func Decode(b []byte) (m Message, ok bool) {
	if len(b) < len(prefix)+2 || b[len(b)-1] != 0xF7 {
		return m, false
	}
	for i, c := range prefix {
		if b[i] != c {
			return m, false
		}
	}
	body := b[len(prefix) : len(b)-1]
	for _, c := range body {
		if c > 0x7F {
			return m, false
		}
	}
	m.Type = int(body[0])
	if m.Type != TypeHarmony {
		return m, true
	}
	f := body[1:]
	if len(f) < harmonyLen {
		return m, false
	}
	m.Harmony = Harmony{
		Key:     pcOf(f[0]),
		Scale:   int(f[1])<<7 | int(f[2]),
		Root:    pcOf(f[3]),
		Chord:   int(f[4])<<7 | int(f[5]),
		Bass:    pcOf(f[6]),
		Playing: f[9]&flagPlay != 0,
		Armed:   f[9]&flagArmed != 0,
	}
	m.Harmony.Scale &= 0xFFF
	m.Harmony.Chord &= 0xFFF
	m.Delta = int(f[7])<<7 | int(f[8])
	return m, true
}

func pc7(pc int) byte {
	if pc < 0 || pc > 11 {
		return none7
	}
	return byte(pc)
}

func pcOf(b byte) int {
	if b > 11 {
		return None
	}
	return int(b)
}

func flags(h Harmony) byte {
	var f byte
	if h.Playing {
		f |= flagPlay
	}
	if h.Armed {
		f |= flagArmed
	}
	return f
}
