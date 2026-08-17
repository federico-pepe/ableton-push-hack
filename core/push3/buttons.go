package push3

// buttons.go — Push 3 MIDI button / encoder map
//
// All messages on MIDI channel 1 (0-indexed: channel 0).
// CC buttons: value 127 = pressed, 0 = released.
// Encoder rotation: relative two's-complement — CC value 1 = one click
// CLOCKWISE, 127 = one click counter-clockwise. Decode with DecodeRel.
// Encoder/wheel touch: Note On vel 127 = contact, vel 0 / Note Off = release.
//
// Encoders ACCELERATE: a fast turn sends larger deltas (±11 observed), so never
// treat one message as one click — always use DecodeRel's signed value.
//
// See docs/push3-button-map.md for the full annotated map.
//
// Verified on tethered Push 3 hardware 2026-08-16 (87/87 CCs, 13/13 touch
// notes). Earlier revisions of this file and the map doc stated the encoder
// direction backwards ("CW=127, CCW=1"); DecodeRel's implementation was always
// correct and the prose was wrong.

const (
	// ── Screen buttons ────────────────────────────────────────────────────────
	// Top row (above display), left → right
	CCScreenTop1 = 102
	CCScreenTop2 = 103
	CCScreenTop3 = 104
	CCScreenTop4 = 105
	CCScreenTop5 = 106
	CCScreenTop6 = 107
	CCScreenTop7 = 108
	CCScreenTop8 = 109

	// Bottom row (below display), left → right
	CCScreenBot1 = 20
	CCScreenBot2 = 21
	CCScreenBot3 = 22
	CCScreenBot4 = 23
	CCScreenBot5 = 24
	CCScreenBot6 = 25
	CCScreenBot7 = 26
	CCScreenBot8 = 27

	// ── Encoders (above top screen buttons) ───────────────────────────────────
	// Rotation (relative delta: 1=CW, 127=CCW)
	CCEncoder1 = 71
	CCEncoder2 = 72
	CCEncoder3 = 73
	CCEncoder4 = 74
	CCEncoder5 = 75
	CCEncoder6 = 76
	CCEncoder7 = 77
	CCEncoder8 = 78
	CCVolume   = 79
	CCTempo    = 14

	// Encoder push-buttons (CC, 127 = pressed, 0 = released)
	CCTempoPress  = 15  // pressing the tempo encoder in
	CCVolumePress = 111 // pressing the volume encoder in

	// Touch (Note On vel 127 = contact, vel 0 = release)
	//
	// CORRECTED 2026-08-16 — these were previously off by one (encoders listed
	// as 1-8, volume as 9). Measured by touching each sensor in isolation, on
	// BOTH a tethered Push 3 and a Push 2, which agree.
	//
	// Note 9 is UNUSED on Push 3. It is the Swing encoder on Push 2, whose touch
	// notes run contiguously 0-10. Push 3 dropped that encoder and left the gap
	// — which is what made the old contiguous 1-10 numbering look plausible.
	NoteEncoder1Touch = 0
	NoteEncoder2Touch = 1
	NoteEncoder3Touch = 2
	NoteEncoder4Touch = 3
	NoteEncoder5Touch = 4
	NoteEncoder6Touch = 5
	NoteEncoder7Touch = 6
	NoteEncoder8Touch = 7
	NoteVolumeTouch   = 8
	// note 9: unused on Push 3 (Swing encoder touch on Push 2)
	NoteTempoTouch = 10

	// ── Jog wheel (main) ──────────────────────────────────────────────────────
	CCJogWheel       = 70  // 1=CW, 127=CCW
	NoteJogTouch     = 11  // Note On vel 127 on contact
	CCJogPress       = 94  // 127 = pressed
	CCJogClickLeft   = 93  // 127 = clicked left
	CCJogClickRight  = 95  // 127 = clicked right

	// ── D-Pad ─────────────────────────────────────────────────────────────────
	CCDPadUp     = 46
	CCDPadRight  = 45
	CCDPadDown   = 47
	CCDPadLeft   = 44
	CCDPadCenter = 91
	NoteDPadCenterTouch = 13

	// ── Touch strip ───────────────────────────────────────────────────────────
	// Touch is a Note On; position arrives as pitch bend on channel 1.
	NoteTouchStrip = 12

	// ── Top-right cluster ─────────────────────────────────────────────────────
	CCSet      = 80
	CCSettings = 30
	CCHelp     = 81
	CCUserMode = 59

	// ── View buttons (right side) ─────────────────────────────────────────────
	CCDeviceView  = 110
	CCMixerView   = 112
	CCClipView    = 113
	CCSessionView = 34

	// ── Modifiers ─────────────────────────────────────────────────────────────
	CCShift  = 49
	CCSelect = 48

	// ── Edit ──────────────────────────────────────────────────────────────────
	CCUndo = 119
	CCSave = 82
	CCAdd  = 32
	CCSwap = 33

	// ── Track ─────────────────────────────────────────────────────────────────
	CCLock         = 83
	CCStopClips    = 29
	CCMute         = 60
	CCSolo         = 61
	CCSelectMain   = 28

	// ── Transport / global ────────────────────────────────────────────────────
	CCTapTempo    = 3
	CCMetronome   = 9
	CCQuantize    = 116
	CCFixedLength = 90
	CCAutomate    = 89
	CCNew         = 92
	CCCapture     = 65
	CCRecord      = 86
	CCPlay        = 85

	// ── Scene / step resolution (left column, bottom → top) ──────────────────
	CCScene14   = 36 // 1/4
	CCScene14t  = 37 // 1/4t
	CCScene18   = 38 // 1/8
	CCScene18t  = 39 // 1/8t
	CCScene116  = 40 // 1/16
	CCScene116t = 41 // 1/16t
	CCScene132  = 42 // 1/32
	CCScene132t = 43 // 1/32t

	// ── Mode buttons ──────────────────────────────────────────────────────────
	CCRepeat  = 56
	CCAccent  = 57
	CCScale   = 58
	CCLayout  = 31
	CCNote    = 50
	CCSession = 51

	// ── Loop / clip actions ───────────────────────────────────────────────────
	CCDoubleLoop = 117
	CCDuplicate  = 88
	CCConvert    = 35
	CCDelete     = 118

	// ── Navigation ────────────────────────────────────────────────────────────
	CCOctaveUp   = 55
	CCOctaveDown = 54
	CCPageLeft   = 62
	CCPageRight  = 63

	// ── Pad grid (8×8) ────────────────────────────────────────────────────────
	// Note On/Off, channel 0. Bottom-left = 36, top-right = 99.
	// Row bottom→top, column left→right: note = 36 + row*8 + col
	PadNoteMin = 36
	PadNoteMax = 99
)

// PadNote returns the MIDI note number for pad at (col, row), both 0-indexed
// from bottom-left. col 0–7 (left→right), row 0–7 (bottom→top).
func PadNote(col, row int) byte {
	return byte(PadNoteMin + row*8 + col)
}

// IsPadNote reports whether note is a grid pad note.
func IsPadNote(note byte) bool {
	return note >= PadNoteMin && note <= PadNoteMax
}

// PadCoord returns (col, row) for a pad note, both 0-indexed from bottom-left.
func PadCoord(note byte) (col, row int) {
	n := int(note) - PadNoteMin
	return n % 8, n / 8
}

// CCScreenTopN returns the CC for the n-th top screen button (n = 0–7).
func CCScreenTopN(n int) byte { return byte(CCScreenTop1 + n) }

// CCScreenBotN returns the CC for the n-th bottom screen button (n = 0–7).
func CCScreenBotN(n int) byte { return byte(CCScreenBot1 + n) }

// CCEncoderN returns the CC for the n-th encoder rotation (n = 0–7).
func CCEncoderN(n int) byte { return byte(CCEncoder1 + n) }

// NoteEncoderTouchN returns the touch note for the n-th encoder (n = 0–7).
func NoteEncoderTouchN(n int) byte { return byte(NoteEncoder1Touch + n) }
