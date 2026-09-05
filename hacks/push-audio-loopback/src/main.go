// push-audio-loopback keeps the "Push Hack Virtual Audio" ALSA Loopback
// card loaded across reboots. It replaces the manual insmod steps in this
// hack's README with a small service that self-heals: load the kernel
// module, watch that it stays loaded, and retry if it goes away.
//
// It is a catalog-installed hack: the store always runs it as
// "push-audio-loopback -config hack.json" with no other arguments, so all
// of its logic lives here rather than in shell.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsapcm"
	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

const (
	cardID     = "PHVAudio"
	moduleName = "snd_aloop"
	insmodArgs = "id=" + cardID + " timer_source=A3.0.0"
	pollEvery  = 30 * time.Second
	refModule  = "snd-usb-audio.ko" // known-loaded system module, for vermagic comparison
)

func main() {
	configPath := flag.String("config", "hack.json", "path to hack.json config file")
	flag.Parse()
	hackDir := filepath.Dir(*configPath)

	alsaseq.WaitForBootSettle()

	for {
		runOnce(hackDir)
		time.Sleep(pollEvery)
	}
}

// runOnce makes one attempt to reach "PHVAudio card loaded", logging state
// transitions only. It never lets a panic escape — a bad read on some
// unexpected /proc format must degrade to "log and retry next tick", not
// crash the one process responsible for healing the audio chain.
func runOnce(hackDir string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered from panic in module-load attempt: %v", r)
		}
	}()

	if cardPresent(cardID) {
		return // already loaded and correctly named — nothing to do
	}

	if moduleLoaded(moduleName) {
		// Loaded, but not under our card name — could be a stock Loopback
		// someone else loaded. Try to remove it; never force past a real
		// user (EBUSY).
		if err := deleteModule(moduleName); err != nil {
			log.Printf("%s loaded under an unexpected name and busy, leaving it alone: %v", moduleName, err)
			return
		}
		log.Printf("removed stray %s not matching card id %s", moduleName, cardID)
	}

	koPath, err := koPathForRunningKernel(hackDir)
	if err != nil {
		log.Printf("no bundled kernel module for this kernel, cannot load %s: %v", cardID, err)
		return
	}

	koBytes, err := os.ReadFile(koPath)
	if err != nil {
		log.Printf("reading %s: %v", koPath, err)
		return
	}

	if ok, mismatchMsg := vermagicMatches(koBytes); !ok {
		log.Printf("refusing to load %s: %s", koPath, mismatchMsg)
		return
	}

	if err := loadModule(koBytes, insmodArgs); err != nil {
		log.Printf("loading %s: %v", koPath, err)
		return
	}

	log.Printf("loaded %s (%s)", koPath, insmodArgs)
}

func koPathForRunningKernel(hackDir string) (string, error) {
	release, err := unameRelease()
	if err != nil {
		return "", fmt.Errorf("reading running kernel version: %w", err)
	}
	path := filepath.Join(hackDir, "ko", release, "snd-aloop.ko")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return path, nil
}

// vermagicMatches extracts the bundled module's vermagic string and checks
// it against a module the running kernel has already loaded successfully
// (refModule) — the same check the README's manual "strings ... | grep
// ^vermagic=" recipe does by hand.
func vermagicMatches(koBytes []byte) (ok bool, reason string) {
	bundled, ok1 := extractVermagic(koBytes)
	if !ok1 {
		return false, "bundled .ko has no vermagic string"
	}

	refPath, err := runningSystemModulePath(refModule)
	if err != nil {
		return false, fmt.Sprintf("cannot locate reference module %s: %v", refModule, err)
	}
	refBytes, err := os.ReadFile(refPath)
	if err != nil {
		return false, fmt.Sprintf("reading reference module %s: %v", refPath, err)
	}
	running, ok2 := extractVermagic(refBytes)
	if !ok2 {
		return false, fmt.Sprintf("reference module %s has no vermagic string", refPath)
	}

	if bundled != running {
		return false, fmt.Sprintf("bundled module vermagic %q does not match running kernel's %q", bundled, running)
	}
	return true, ""
}

func runningSystemModulePath(name string) (string, error) {
	release, err := unameRelease()
	if err != nil {
		return "", err
	}
	return filepath.Join("/lib/modules", release, "kernel/sound/usb", name), nil
}

// extractVermagic finds "vermagic=<value>" inside a kernel module's raw
// bytes and returns <value> up to the next NUL byte.
func extractVermagic(data []byte) (string, bool) {
	const marker = "vermagic="
	idx := bytes.Index(data, []byte(marker))
	if idx < 0 {
		return "", false
	}
	rest := data[idx+len(marker):]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		end = len(rest)
	}
	return string(rest[:end]), true
}

func cardPresent(id string) bool {
	cards, err := alsapcm.EnumCards()
	if err != nil {
		return false
	}
	for _, c := range cards {
		if c.ID == id {
			return true
		}
	}
	return false
}

func moduleLoaded(name string) bool {
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(name+" "))
}
