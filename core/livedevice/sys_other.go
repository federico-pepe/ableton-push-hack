//go:build !linux

package livedevice

import (
	"errors"
	"time"
)

// Devices run on the Push (Linux) only; elsewhere the package builds and its
// pure parts (classes, presets) work.

func shmCreate(path string, size int) ([]byte, error) {
	return nil, errors.New("livedevice: devices need Linux")
}

func shmFree(mem []byte, path string)                     {}
func futexWait(addr *uint32, val uint32, d time.Duration) { time.Sleep(d) }
func futexWake(addr *uint32)                              {}
func setRealtime(priority int)                            {}
