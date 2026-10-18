package alsaseq

// midi.go — raw MIDI bytes <-> snd_seq_event. Ported tools (Schwung/Move
// Anything modules, a DSP's MIDI FX surface) speak raw 1-3 byte messages;
// the sequencer speaks typed events. EncodeMIDI/SendMIDI go out, DecodeMIDI
// comes back in, so a host never hand-builds the 12-byte data union.
//
// Covered: channel voice messages (80-E0), clock F8, start FA, continue FB,
// stop FC, sensing FE, song position F2. SysEx goes through SendSysEx.

import "encoding/binary"

// Subscribers is the destination for "everyone subscribed to my port" —
// what a hack's output port sends to once Live has connected to it.
var Subscribers = Addr{Client: AddrSubscribers, Port: AddrUnknown}

// EncodeMIDI maps one raw MIDI message to an event type and 12-byte data
// union. ok is false for a message this file does not cover or one that is
// too short for its status byte.
func EncodeMIDI(msg []byte) (evType uint8, data []byte, ok bool) {
	if len(msg) == 0 {
		return 0, nil, false
	}
	data = make([]byte, 12)
	status := msg[0]
	switch status {
	case 0xF8:
		return EvClock, data, true
	case 0xFA:
		return EvStart, data, true
	case 0xFB:
		return EvContinue, data, true
	case 0xFC:
		return EvStop, data, true
	case 0xFE:
		return EvSensing, data, true
	case 0xF2:
		if len(msg) < 3 {
			return 0, nil, false
		}
		putCtrl(data, 0, 0, int32(msg[1]&0x7F)|int32(msg[2]&0x7F)<<7)
		return EvSongPos, data, true
	}
	if status < 0x80 || status >= 0xF0 {
		return 0, nil, false
	}
	ch := status & 0x0F
	need := 3
	if k := status & 0xF0; k == 0xC0 || k == 0xD0 {
		need = 2
	}
	if len(msg) < need {
		return 0, nil, false
	}
	d1 := msg[1] & 0x7F
	switch status & 0xF0 {
	case 0x80, 0x90, 0xA0:
		// snd_seq_ev_note: channel, note, velocity, off_velocity, duration
		data[0], data[1], data[2] = ch, d1, msg[2]&0x7F
		switch status & 0xF0 {
		case 0x80:
			evType = EvNoteOff
		case 0x90:
			evType = EvNoteOn
		default:
			evType = EvKeyPress
		}
	case 0xB0:
		putCtrl(data, ch, uint32(d1), int32(msg[2]&0x7F))
		evType = EvController
	case 0xC0:
		putCtrl(data, ch, 0, int32(d1))
		evType = EvPgmChange
	case 0xD0:
		putCtrl(data, ch, 0, int32(d1))
		evType = EvChanPress
	case 0xE0:
		putCtrl(data, ch, 0, (int32(d1)|int32(msg[2]&0x7F)<<7)-8192)
		evType = EvPitchBend
	}
	return evType, data, true
}

// DecodeMIDI is EncodeMIDI's inverse for a received fixed-length event
// (data = the 12-byte union, as Handler.Fixed gets it). Returns nil for an
// event type that has no raw MIDI form here.
func DecodeMIDI(evType uint8, data []byte) []byte {
	if len(data) < 12 {
		return nil
	}
	ch := data[0] & 0x0F
	param := byte(binary.LittleEndian.Uint32(data[4:]) & 0x7F)
	value := int32(binary.LittleEndian.Uint32(data[8:]))
	switch evType {
	case EvClock:
		return []byte{0xF8}
	case EvStart:
		return []byte{0xFA}
	case EvContinue:
		return []byte{0xFB}
	case EvStop:
		return []byte{0xFC}
	case EvSensing:
		return []byte{0xFE}
	case EvSongPos:
		return []byte{0xF2, byte(value & 0x7F), byte(value >> 7 & 0x7F)}
	case EvNoteOn:
		return []byte{0x90 | ch, data[1] & 0x7F, data[2] & 0x7F}
	case EvNoteOff:
		return []byte{0x80 | ch, data[1] & 0x7F, data[2] & 0x7F}
	case EvKeyPress:
		return []byte{0xA0 | ch, data[1] & 0x7F, data[2] & 0x7F}
	case EvController:
		return []byte{0xB0 | ch, param, byte(value & 0x7F)}
	case EvPgmChange:
		return []byte{0xC0 | ch, byte(value & 0x7F)}
	case EvChanPress:
		return []byte{0xD0 | ch, byte(value & 0x7F)}
	case EvPitchBend:
		v := value + 8192
		return []byte{0xE0 | ch, byte(v & 0x7F), byte(v >> 7 & 0x7F)}
	}
	return nil
}

// SendMIDI sends one raw MIDI message to dst (often Subscribers). A
// message EncodeMIDI does not cover is dropped and reported as false.
func (c *Client) SendMIDI(dst Addr, msg []byte) (bool, error) {
	evType, data, ok := EncodeMIDI(msg)
	if !ok {
		return false, nil
	}
	return true, c.WriteEvent(evType, dst, data)
}

// putCtrl fills snd_seq_ev_ctrl: channel(1B) + pad(3B) + param(u32) + value(s32).
func putCtrl(data []byte, ch byte, param uint32, value int32) {
	data[0] = ch
	binary.LittleEndian.PutUint32(data[4:], param)
	binary.LittleEndian.PutUint32(data[8:], uint32(value))
}
