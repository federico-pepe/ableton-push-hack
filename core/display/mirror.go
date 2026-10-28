//go:build linux

package display

// mirror.go — read-only view of push_hook.c's mirror file: the frame that
// really goes out to the screen, in every mode. The hook is the only writer
// and creates the file; this side maps it PROT_READ and never creates it,
// so it cannot race the hook (see shm.go for the framebuf rule).

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	MirrorFile = "/data/push-hack/hacks/push-display/mirror"

	mirrorMagic   = uint32(0x4D495252)
	mirrorVersion = uint32(1)
	mirrorSize    = 16 + FrameBytes
	offMirrorSeq  = 8
	offMirrorPix  = 16
)

// Mirror is a connection to the hook's mirror file. Zero value is a valid,
// disconnected Mirror.
type Mirror struct {
	Path string // default MirrorFile; a test points it at a temp file

	mu          sync.Mutex
	buf         []byte
	lastAttempt time.Time
}

// Ensure maps the file if it is not mapped yet, at most once per 5 s. A hook
// older than the mirror feature never creates the file: Ensure stays quiet.
func (m *Mirror) Ensure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.buf != nil || time.Since(m.lastAttempt) < 5*time.Second {
		return
	}
	m.lastAttempt = time.Now()
	path := m.Path
	if path == "" {
		path = MirrorFile
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.Size() < mirrorSize {
		return
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, mirrorSize, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return
	}
	if binary.LittleEndian.Uint32(data) != mirrorMagic || binary.LittleEndian.Uint32(data[4:]) != mirrorVersion {
		syscall.Munmap(data) //nolint
		return
	}
	m.buf = data
}

// Connected reports whether the file is mapped.
func (m *Mirror) Connected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf != nil
}

func (m *Mirror) seqPtr() *uint32 {
	return (*uint32)(unsafe.Pointer(&m.buf[offMirrorSeq])) // page aligned, so 4-byte aligned
}

// Seq returns the sequence number of the last whole frame, or 0 when not
// connected. It changes every time the hook finishes a frame.
func (m *Mirror) Seq() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.buf == nil {
		return 0
	}
	return atomic.LoadUint32(m.seqPtr()) &^ 1
}

// Read copies the last whole frame into dst (FrameBytes long) as raw BGR565,
// XOR removed, and returns its sequence number. ok is false when not
// connected, or when the hook kept writing for every one of a few tries.
func (m *Mirror) Read(dst []byte) (seq uint32, ok bool) {
	if len(dst) != FrameBytes {
		panic(fmt.Sprintf("display: Mirror.Read needs %d bytes, got %d", FrameBytes, len(dst)))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.buf == nil {
		return 0, false
	}
	p := m.seqPtr()
	for try := 0; try < 4; try++ {
		s1 := atomic.LoadUint32(p)
		if s1&1 != 0 { // hook is mid-frame
			runtime.Gosched()
			time.Sleep(time.Millisecond)
			continue
		}
		copy(dst, m.buf[offMirrorPix:])
		if atomic.LoadUint32(p) == s1 {
			ToWire(dst, dst) // XOR is its own inverse
			return s1, true
		}
	}
	return 0, false
}
