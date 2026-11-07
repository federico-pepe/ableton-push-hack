package audiobus

import (
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Ring: one-writer one-reader queue of stereo float32 frames in a shared file.
// The client writes, the mixer reads. No locks, no syscalls on the hot path.
//
// Header (64 bytes): magic u32, cap u32 (frames, power of two), w u64, r u64.
// w and r count frames since open and never wrap. Data: cap frames of L,R.
const (
	ringMagic = 0x41425531
	ringHdr   = 64
)

type ring struct {
	mem  []byte
	f    *os.File
	w, r *uint64
	cap  uint64
	data []float32
}

func ringBytes(capFrames int) int { return ringHdr + capFrames*2*4 }

// pow2 returns the smallest power of two >= n.
func pow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// createRing makes and maps the file. Server side.
func createRing(path string, capFrames int) (*ring, error) {
	if capFrames&(capFrames-1) != 0 {
		return nil, fmt.Errorf("ring size %d is not a power of two", capFrames)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(int64(ringBytes(capFrames))); err != nil {
		f.Close()
		return nil, err
	}
	rg, err := mapRing(f, ringBytes(capFrames))
	if err != nil {
		f.Close()
		return nil, err
	}
	*(*uint32)(unsafe.Pointer(&rg.mem[4])) = uint32(capFrames)
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&rg.mem[0])), ringMagic)
	rg.cap = uint64(capFrames)
	return rg, nil
}

// openRing maps an existing file. Client side.
func openRing(path string) (*ring, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	rg, err := mapRing(f, int(st.Size()))
	if err != nil {
		f.Close()
		return nil, err
	}
	if atomic.LoadUint32((*uint32)(unsafe.Pointer(&rg.mem[0]))) != ringMagic {
		rg.close()
		return nil, fmt.Errorf("%s is not a ring", path)
	}
	rg.cap = uint64(*(*uint32)(unsafe.Pointer(&rg.mem[4])))
	if rg.cap == 0 || rg.cap&(rg.cap-1) != 0 || ringBytes(int(rg.cap)) != int(st.Size()) {
		rg.close()
		return nil, fmt.Errorf("%s: bad ring size", path)
	}
	return rg, nil
}

func mapRing(f *os.File, size int) (*ring, error) {
	if size < ringHdr+8 {
		return nil, fmt.Errorf("ring file too small")
	}
	mem, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	return &ring{
		mem:  mem,
		f:    f,
		w:    (*uint64)(unsafe.Pointer(&mem[8])),
		r:    (*uint64)(unsafe.Pointer(&mem[16])),
		data: unsafe.Slice((*float32)(unsafe.Pointer(&mem[ringHdr])), (size-ringHdr)/4),
	}, nil
}

func (g *ring) close() {
	_ = syscall.Munmap(g.mem)
	_ = g.f.Close()
}

// free is how many frames the writer may add.
func (g *ring) free() int {
	return int(g.cap - (atomic.LoadUint64(g.w) - atomic.LoadUint64(g.r)))
}

// avail is how many frames the reader may take.
func (g *ring) avail() int {
	return int(atomic.LoadUint64(g.w) - atomic.LoadUint64(g.r))
}

// write adds all of src (interleaved L,R) or nothing. Writer only.
func (g *ring) write(src []float32) bool {
	n := len(src) / 2
	if n > g.free() {
		return false
	}
	w := atomic.LoadUint64(g.w)
	mask := g.cap - 1
	for i := 0; i < n; i++ {
		p := ((w + uint64(i)) & mask) * 2
		g.data[p] = src[i*2]
		g.data[p+1] = src[i*2+1]
	}
	atomic.StoreUint64(g.w, w+uint64(n))
	return true
}

// read copies up to len(dst)/2 frames out (interleaved L,R) and returns how
// many. Reader only. No allocation.
func (g *ring) read(dst []float32) int {
	n := len(dst) / 2
	if a := g.avail(); n > a {
		n = a
	}
	r := atomic.LoadUint64(g.r)
	mask := g.cap - 1
	for i := 0; i < n; i++ {
		p := ((r + uint64(i)) & mask) * 2
		dst[i*2], dst[i*2+1] = g.data[p], g.data[p+1]
	}
	atomic.StoreUint64(g.r, r+uint64(n))
	return n
}

// addTo adds up to n frames into dst (interleaved with stride, first channel
// at dst[0]) and returns how many frames it took and the peak it saw. Reader
// only. No allocation.
func (g *ring) addTo(dst []float32, stride, n int) (got int, peak float32) {
	if a := g.avail(); n > a {
		n = a
	}
	r := atomic.LoadUint64(g.r)
	mask := g.cap - 1
	for i := 0; i < n; i++ {
		p := ((r + uint64(i)) & mask) * 2
		l, rr := g.data[p], g.data[p+1]
		dst[i*stride] += l
		dst[i*stride+1] += rr
		if l < 0 {
			l = -l
		}
		if rr < 0 {
			rr = -rr
		}
		if l > peak {
			peak = l
		}
		if rr > peak {
			peak = rr
		}
	}
	atomic.StoreUint64(g.r, r+uint64(n))
	return n, peak
}
