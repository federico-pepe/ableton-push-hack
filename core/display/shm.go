//go:build linux

// Linux-only: push_hook.so's shared-memory framebuf only ever exists on the
// Push device itself (an on-device Linux hack, deployed over SSH), and this
// file's syscall.Mmap/PROT_READ/MAP_SHARED constants do not exist on Windows.
// A cross-platform consumer of this package (e.g. push-tethered-app) must not
// pull this file in on non-Linux — ToBGR565/FromBGR565 in codec.go are the
// portable half of this package and live in a separate file for that reason.

package display

// shm.go — mmap bridge to push_hook.so's shared-memory framebuf. Moved from
// hacks/push-manager/src/display.go's package-level shmBuf/shmMu/
// shmLastAttempt globals + tryOpenShm/ensureShm/shmGetMode/shmReadFrame/
// shmSetMode/shmWritePixels functions, collapsed into a struct.
//
// The os.OpenFile call below keeps os.O_RDWR with NO os.O_CREATE, and that
// is the single most important line in this file: push_hook.c is the sole
// creator of the shm file, push-manager (via this package) is the sole
// writer, and push-manager is never the creator. See CLAUDE.md's
// "Display-owning hacks" section — that single-writer discipline is what
// keeps the shm protocol from racing. Do not add O_CREATE here.

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"
	"time"
)

const (
	File      = "/data/push-hack/hacks/push-display/framebuf"
	magic     = uint32(0x50555348)
	totalSize = 16 + TotalBytes

	// Mode values — mirror push_hook.c.
	ModePassthrough = uint32(0)
	ModeBar         = uint32(1)
	ModeTakeover    = uint32(2)

	// shm byte offsets.
	offMagic    = 0
	offVersion  = 4
	offMode     = 8
	offFrameSeq = 12
	offPixels   = 16
)

// Shm is a connection to push_hook.so's shared-memory framebuf. Zero value
// is a valid, disconnected Shm.
type Shm struct {
	mu          sync.Mutex
	buf         []byte // mmap'd bytes; nil if unavailable
	lastAttempt time.Time

	// OnConnect, if set, is called synchronously from Ensure/tryOpen after a
	// successful (re)connect, with the frame_seq read from shm (0 = a fresh
	// hook load that hasn't rendered anything yet). Lets a caller trigger a
	// startup splash or similar without this package knowing about OSD.
	OnConnect func(frameSeq uint32)
}

// tryOpen attempts to mmap File. Caller must hold mu.
func (s *Shm) tryOpen() {
	f, err := os.OpenFile(File, os.O_RDWR, 0664)
	if err != nil {
		return // hook not running yet — silent
	}
	defer f.Close()

	data, err := syscall.Mmap(int(f.Fd()), 0, totalSize,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		log.Printf("display shm: mmap: %v", err)
		return
	}

	m := binary.LittleEndian.Uint32(data[offMagic:])
	if m != magic {
		log.Printf("display shm: bad magic 0x%08X (hook may not be loaded yet)", m)
		syscall.Munmap(data) //nolint
		return
	}

	s.buf = data
	seq := binary.LittleEndian.Uint32(data[offFrameSeq:])
	if seq > 0 {
		// We were running before this connect; reset any stale takeover.
		if binary.LittleEndian.Uint32(data[offMode:]) == ModeTakeover {
			binary.LittleEndian.PutUint32(data[offMode:], ModePassthrough)
			log.Printf("display shm reconnected: cleared stale mode=2 (seq=%d)", seq)
		} else {
			log.Printf("display shm reconnected: seq=%d", seq)
		}
	} else {
		log.Printf("display shm connected: fresh hook (seq=0), startup splash pending")
	}
	if s.OnConnect != nil {
		s.OnConnect(seq)
	}
}

// Ensure connects if not already connected, rate-limited to once per 5s.
// Safe to call on every status request.
func (s *Shm) Ensure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf != nil {
		return
	}
	if time.Since(s.lastAttempt) < 5*time.Second {
		return
	}
	s.lastAttempt = time.Now()
	s.tryOpen()
}

// Connected reports whether the shm is currently mapped.
func (s *Shm) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf != nil
}

// Mode returns the current display mode, or ModePassthrough if disconnected.
func (s *Shm) Mode() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf == nil {
		return ModePassthrough
	}
	return binary.LittleEndian.Uint32(s.buf[offMode:])
}

// SetMode sets the display mode.
func (s *Shm) SetMode(mode uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf == nil {
		return fmt.Errorf("display hook not connected (is push-display deployed and Push3 running?)")
	}
	binary.LittleEndian.PutUint32(s.buf[offMode:], mode)
	return nil
}

// ReadFrame copies the current framebuf (first of the two duplicated
// halves) and the current mode out of shm. ok=false if not connected.
func (s *Shm) ReadFrame() (px []byte, mode uint32, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf == nil {
		return nil, ModePassthrough, false
	}
	px = make([]byte, FrameBytes)
	copy(px, s.buf[offPixels:offPixels+FrameBytes])
	mode = binary.LittleEndian.Uint32(s.buf[offMode:])
	return px, mode, true
}

// FrameSeq returns the current frame_seq counter, or 0 if disconnected.
func (s *Shm) FrameSeq() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(s.buf[offFrameSeq:])
}

// CompareAndSetMode sets mode to to only if it is currently from (and shm
// is connected), returning whether it did. Used to restore a prior mode
// without racing a concurrent mode change (e.g. the startup splash
// restoring passthrough only if takeover mode is still what it set).
func (s *Shm) CompareAndSetMode(from, to uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf == nil {
		return false
	}
	if binary.LittleEndian.Uint32(s.buf[offMode:]) != from {
		return false
	}
	binary.LittleEndian.PutUint32(s.buf[offMode:], to)
	return true
}

// WritePixels writes a full (both-halves-duplicated) BGR565 frame into shm
// and increments frame_seq. pixels must be TotalBytes long.
func (s *Shm) WritePixels(pixels []byte) error {
	if len(pixels) != TotalBytes {
		return fmt.Errorf("expected %d bytes, got %d", TotalBytes, len(pixels))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf == nil {
		return fmt.Errorf("display hook not connected")
	}
	copy(s.buf[offPixels:], pixels)
	seq := binary.LittleEndian.Uint32(s.buf[offFrameSeq:]) + 1
	binary.LittleEndian.PutUint32(s.buf[offFrameSeq:], seq)
	return nil
}
