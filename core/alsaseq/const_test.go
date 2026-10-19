package alsaseq

import "testing"

// Event type numbers are kernel ABI: include/uapi/sound/asequencer.h,
// SNDRV_SEQ_EVENT_*. EvStop and EvContinue were swapped until 2026-10-16,
// so a clock-following hack read Live's Stop as Continue. Confirmed on
// device: Live's Stop arrives as type 32.
func TestEventTypesMatchKernel(t *testing.T) {
	cases := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"NOTEON", EvNoteOn, 6},
		{"NOTEOFF", EvNoteOff, 7},
		{"KEYPRESS", EvKeyPress, 8},
		{"CONTROLLER", EvController, 10},
		{"PGMCHANGE", EvPgmChange, 11},
		{"CHANPRESS", EvChanPress, 12},
		{"PITCHBEND", EvPitchBend, 13},
		{"START", EvStart, 30},
		{"CONTINUE", EvContinue, 31},
		{"STOP", EvStop, 32},
		{"SONGPOS", EvSongPos, 33},
		{"CLOCK", EvClock, 36},
		{"SENSING", EvSensing, 42},
		{"SYSEX", EvSysEx, 130},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("SNDRV_SEQ_EVENT_%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}
