//go:build !linux

// This hack only ever runs on Push 3 (linux/amd64). These stubs exist so the
// package builds and its portable logic (parsing, vermagic extraction) can
// be unit-tested from a non-Linux dev machine.
package main

import (
	"errors"
	"strings"
)

var errNotLinux = errors.New("not supported on this platform (linux only)")

func loadModule(koBytes []byte, args string) error {
	return errNotLinux
}

func deleteModule(name string) error {
	return errNotLinux
}

func unameRelease() (string, error) {
	return "", errNotLinux
}

func cString(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}
