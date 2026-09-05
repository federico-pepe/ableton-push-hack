//go:build linux

package main

import (
	"bytes"

	"golang.org/x/sys/unix"
)

func loadModule(koBytes []byte, args string) error {
	return unix.InitModule(koBytes, args)
}

func deleteModule(name string) error {
	return unix.DeleteModule(name, unix.O_NONBLOCK)
}

func unameRelease() (string, error) {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return "", err
	}
	return cString(uts.Release[:]), nil
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
