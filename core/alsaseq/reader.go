package alsaseq

// reader.go — decode a buffer of snd_seq_event records and read them off a
// Client's fd in a loop. Replaces processSeqBuf (push-manager:787,
// keyboard-visualizer:227) and readMidiEvents (automation:363).
//
// This is where automation's SysEx desync gets fixed by construction:
// automation's own event walker had no varlen branch, so a SysEx byte
// stream from Push 3 (it subscribes to Push3 16:0 for clock) would desync
// its fixed-28-byte-stride decode until a buffer boundary happened to
// re-align it. push-manager's and keyboard-visualizer's walkers already
// handled it correctly; Walk is their (correct) logic, shared.

import "syscall"

// Handler receives decoded events from Walk/ReadLoop. src is the sending
// client:port — keyboard-visualizer needs it to tell Live's routed notes
// apart from Push 3's own raw pad stream sharing the same port (see
// isPush3Client in its midi.go); push-manager and automation ignore it.
type Handler interface {
	// Fixed is called for a fixed-length (28-byte) event. data is the
	// 12-byte data union starting at EventOffData.
	Fixed(evType uint8, src Addr, data []byte)
	// VarLen is called for a variable-length event (e.g. SysEx). payload is
	// the raw bytes after the 28-byte header, length EXT.len.
	VarLen(evType uint8, src Addr, payload []byte)
}

// Walk decodes zero or more snd_seq_event records from buf, calling the
// appropriate Handler method for each. An incomplete trailing variable-
// length event (rare; shouldn't happen with proper seq reads) is dropped.
func Walk(buf []byte, h Handler) {
	for off := 0; off+EventSize <= len(buf); {
		evType := buf[off+EventOffType]
		evFlags := buf[off+EventOffFlags]
		src := Addr{Client: buf[off+EventOffSrcClient], Port: buf[off+EventOffSrcPort]}
		data := buf[off+EventOffData : off+EventSize]

		if evFlags&FlagVarLen != 0 {
			// Variable-length: ext.len (uint32 LE) at data[0], bytes follow header.
			varLen := int(uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24)
			end := off + EventSize + varLen
			if end > len(buf) {
				break // incomplete; shouldn't happen with proper seq reads
			}
			h.VarLen(evType, src, buf[off+EventSize:end])
			off = end
		} else {
			h.Fixed(evType, src, data)
			off += EventSize
		}
	}
}

// ReadLoop blocks reading c's fd in a loop, calling Walk(buf, h) on each
// read, until Read returns an error (e.g. the fd was closed to interrupt a
// subscription change).
func (c *Client) ReadLoop(h Handler) error {
	buf := make([]byte, 8192)
	for {
		n, err := syscall.Read(c.fd, buf)
		if err != nil {
			return err
		}
		Walk(buf[:n], h)
	}
}
