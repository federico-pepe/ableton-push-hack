//go:build linux

package livedevice

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

func shmCreate(path string, size int) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	os.Chmod(path, 0o666) // Live runs as ableton; the umask would take the w away
	if err := f.Truncate(int64(size)); err != nil {
		os.Remove(path)
		return nil, err
	}
	mem, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	return mem, nil
}

func shmFree(mem []byte, path string) {
	syscall.Munmap(mem)
	os.Remove(path)
}

// futexWait sleeps while *addr == val, at most d. Shared futex: the plugin is
// another process.
func futexWait(addr *uint32, val uint32, d time.Duration) {
	ts := syscall.NsecToTimespec(int64(d))
	syscall.Syscall6(syscall.SYS_FUTEX, uintptr(unsafe.Pointer(addr)), 0 /* FUTEX_WAIT */, uintptr(val),
		uintptr(unsafe.Pointer(&ts)), 0, 0)
}

func futexWake(addr *uint32) {
	syscall.Syscall6(syscall.SYS_FUTEX, uintptr(unsafe.Pointer(addr)), 1 /* FUTEX_WAKE */, 1<<30, 0, 0, 0)
}

// setRealtime puts the calling thread on SCHED_FIFO at priority. Needs root
// (a hack's service runs as root); without it the thread stays normal.
func setRealtime(priority int) {
	param := struct{ priority int32 }{int32(priority)}
	syscall.RawSyscall(syscall.SYS_SCHED_SETSCHEDULER, 0, 1 /* SCHED_FIFO */, uintptr(unsafe.Pointer(&param)))
}
