//go:build linux

package display

// usb.go — drive push 3 screen straight over usbfs, no push3, no hook.
// only valid while push3 is stopped: push3 claims iface 0 itself, and
// whoever claims first wins. caller must Close() before push3 starts again.
// protocol same as push3 sends (push-display hook log): 16-byte header,
// then two full-frame chunks, all to bulk OUT ep 0x01. screen goes dark
// after ~2s with no frame, so loop resends last frame.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	usbVendor    = "2982" // ableton
	usbIface     = 0
	usbEndpoint  = 0x01
	usbClaim     = 0x8004550F // USBDEVFS_CLAIMINTERFACE, _IOR('U', 15, uint)
	usbRelease   = 0x80045510 // USBDEVFS_RELEASEINTERFACE, _IOR('U', 16, uint)
	usbBulk      = 0xC0185502 // USBDEVFS_BULK, _IOWR('U', 2, usbdevfs_bulktransfer)
	usbFPS       = 30
	usbKeepAlive = 500 * time.Millisecond
	usbTimeoutMS = 1000
)

// usbdevfs_bulktransfer, amd64 layout.
type usbBulkXfer struct {
	ep, length, timeout uint32
	_                   uint32
	data                uintptr
}

var usbHeader = [16]byte{0xFF, 0xCC, 0xAA, 0x88}

// USB owns push 3 screen while push3 is stopped.
type USB struct {
	fd    int
	mu    sync.Mutex
	px    []byte // latest raw BGR565 frame, TotalBytes
	dirty bool
	stop  chan struct{}
	done  chan struct{}
}

// OpenUSB claims screen iface. fails (EBUSY) while push3 holds it.
func OpenUSB() (*USB, error) {
	path, err := findPushDev()
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	iface := uint32(usbIface)
	if err := usbIoctl(fd, usbClaim, unsafe.Pointer(&iface)); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("claim screen interface (is Push3 still running?): %w", err)
	}
	u := &USB{fd: fd, px: make([]byte, TotalBytes), dirty: true, stop: make(chan struct{}), done: make(chan struct{})}
	go u.loop()
	return u, nil
}

// Set queues a full raw BGR565 frame (TotalBytes, as from ToBGR565).
func (u *USB) Set(px []byte) {
	if len(px) != TotalBytes {
		return
	}
	u.mu.Lock()
	copy(u.px, px)
	u.dirty = true
	u.mu.Unlock()
}

// Close stops sending and releases iface. returns after release, so push3
// can start right after.
func (u *USB) Close() {
	close(u.stop)
	<-u.done
	iface := uint32(usbIface)
	usbIoctl(u.fd, usbRelease, unsafe.Pointer(&iface)) //nolint:errcheck
	syscall.Close(u.fd)
}

func (u *USB) loop() {
	defer close(u.done)
	wire := make([]byte, TotalBytes)
	tick := time.NewTicker(time.Second / usbFPS)
	defer tick.Stop()
	var last time.Time
	var lastErr string
	for {
		select {
		case <-u.stop:
			return
		case <-tick.C:
		}
		u.mu.Lock()
		send := u.dirty || time.Since(last) >= usbKeepAlive
		if send {
			ToWire(wire, u.px)
			u.dirty = false
		}
		u.mu.Unlock()
		if !send {
			continue
		}
		last = time.Now()
		if err := u.sendFrame(wire); err != nil {
			if err.Error() != lastErr { // log on change only
				log.Printf("display usb: %v", err)
				lastErr = err.Error()
			}
		} else {
			lastErr = ""
		}
	}
}

func (u *USB) sendFrame(wire []byte) error {
	if err := u.bulk(usbHeader[:]); err != nil {
		return fmt.Errorf("header: %w", err)
	}
	for h := 0; h < 2; h++ {
		if err := u.bulk(wire[h*FrameBytes : (h+1)*FrameBytes]); err != nil {
			return fmt.Errorf("chunk %d: %w", h, err)
		}
	}
	return nil
}

func (u *USB) bulk(data []byte) error {
	x := usbBulkXfer{ep: usbEndpoint, length: uint32(len(data)), timeout: usbTimeoutMS, data: uintptr(unsafe.Pointer(&data[0]))}
	n, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(u.fd), usbBulk, uintptr(unsafe.Pointer(&x)))
	if e != 0 {
		return e
	}
	if int(n) != len(data) {
		return fmt.Errorf("short write %d/%d", n, len(data))
	}
	return nil
}

func usbIoctl(fd int, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// findPushDev maps ableton vid in sysfs to its /dev/bus/usb node.
func findPushDev() (string, error) {
	devs, _ := filepath.Glob("/sys/bus/usb/devices/*")
	for _, d := range devs {
		v, _ := os.ReadFile(d + "/idVendor")
		if strings.TrimSpace(string(v)) != usbVendor {
			continue
		}
		var bus, dev int
		b, _ := os.ReadFile(d + "/busnum")
		n, _ := os.ReadFile(d + "/devnum")
		fmt.Sscan(string(b), &bus)
		fmt.Sscan(string(n), &dev)
		if bus > 0 && dev > 0 {
			return fmt.Sprintf("/dev/bus/usb/%03d/%03d", bus, dev), nil
		}
	}
	return "", fmt.Errorf("Push 3 not found on USB")
}
