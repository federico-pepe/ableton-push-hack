package alsaseq

// event.go — send fixed-length and variable-length snd_seq_event structs.
// Replaces writeSeqEvent (push-manager:409 ≡ automation:284), sendSeqCC
// (push-manager:242 ≡ automation:264), sendSeqNote (push-manager:265),
// sendSeqSysEx (push-manager:288), and sendSeqCCTo/sendSeqNoteTo
// (push-manager's remap.go:108,126 — previously misfiled there since they
// were really the explicit-destination variant of sendSeqCC/sendSeqNote).
//
// The implicit-dest (push3Dest()) and explicit-dest variants collapse into
// one method taking dst Addr; callers that previously relied on a package
// global for the destination now pass it explicitly.
//
// writeSeqEvent used to take an fd parameter and then re-read the global
// midiOutFd inside to check it hadn't been invalidated by a reconnect. As a
// Client method there is no global left to consult — that check is gone by
// construction, not by omission.

import (
	"encoding/binary"
	"syscall"
)

// WriteEvent writes a single fixed-length 28-byte snd_seq_event to dst, with
// QUEUE_DIRECT for immediate delivery. data is the 12-byte data union.
func (c *Client) WriteEvent(evType uint8, dst Addr, data []byte) error {
	ev := make([]byte, EventSize)
	ev[0] = evType
	ev[1] = 0           // flags: tick timestamp, absolute
	ev[2] = 0           // tag
	ev[3] = QueueDirect // deliver immediately, no queue
	// bytes 4–11: timestamp = 0 (ignored for QUEUE_DIRECT)
	ev[EventOffSrcClient] = c.id
	ev[EventOffSrcPort] = c.port
	ev[14] = dst.Client
	ev[15] = dst.Port
	copy(ev[16:], data)

	_, err := syscall.Write(c.fd, ev)
	return err
}

// SendCC sends a MIDI Control Change event to dst. channel is 0-indexed
// (0 = MIDI ch 1); cc and value are 0-127.
func (c *Client) SendCC(dst Addr, channel, cc byte, value int32) error {
	// snd_seq_ev_ctrl: channel(1B) + unused(3B) + param(uint32 LE) + value(int32 LE)
	data := make([]byte, 12)
	data[0] = channel
	binary.LittleEndian.PutUint32(data[4:], uint32(cc))
	binary.LittleEndian.PutUint32(data[8:], uint32(value))
	return c.WriteEvent(EvController, dst, data)
}

// SendNote sends a MIDI Note On event to dst. channel is 0-indexed; note and
// velocity are 0-127. velocity=0 acts as Note Off.
func (c *Client) SendNote(dst Addr, channel, note, velocity byte) error {
	// snd_seq_ev_note: channel(1B) + note(1B) + vel(1B) + off_vel(1B) + duration(uint32 LE)
	data := make([]byte, 12)
	data[0] = channel
	data[1] = note
	data[2] = velocity
	return c.WriteEvent(EvNoteOn, dst, data)
}

// SendSysEx sends a variable-length SysEx event to dst. payload must
// include the leading F0 and trailing F7.
func (c *Client) SendSysEx(dst Addr, payload []byte) error {
	// Variable-length snd_seq_event: 28-byte header + inline SysEx bytes.
	// flags |= FlagVarLen; data[0..3] = ext.len (uint32 LE); data follows.
	ev := make([]byte, EventSize+len(payload))
	ev[EventOffType] = EvSysEx
	ev[EventOffFlags] = FlagVarLen
	ev[2] = 0           // tag
	ev[3] = QueueDirect // deliver immediately
	// bytes 4–11: timestamp = 0 (ignored for QUEUE_DIRECT)
	ev[EventOffSrcClient] = c.id
	ev[EventOffSrcPort] = c.port
	ev[14] = dst.Client
	ev[15] = dst.Port
	binary.LittleEndian.PutUint32(ev[EventOffData:], uint32(len(payload))) // ext.len
	copy(ev[EventSize:], payload)

	_, err := syscall.Write(c.fd, ev)
	return err
}
