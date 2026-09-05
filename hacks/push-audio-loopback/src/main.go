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

	// The Loopback driver always creates exactly two PCM devices per
	// card, cross-wired to each other (device 0's playback arrives on
	// device 1's capture and vice versa) — that pairing is the whole
	// mechanism, not a redundant duplicate, so it can't be reduced to
	// one device without losing the loopback function itself. Only one
	// side is meant for a human to pick in Live's own audio device list;
	// the other (feedDevice) is meant to be opened directly, by hw:
	// address, by whatever process feeds it (push-braids-host, or
	// loopback_feed).
	//
	// Both devices still appear in Live's own device list regardless —
	// that list comes from the driver's ALSA-level hint/enumeration data
	// (what aloop-rename.patch's per-device pcm->name naming addresses),
	// not from filesystem permissions, so locking feedDevice's raw
	// device nodes to root-only does NOT hide it from that list. It
	// still earns its keep as a second, independent layer: if someone
	// picks the wrong ("do not select") entry in Live by mistake anyway,
	// Live gets a clean permission failure opening it instead of two
	// processes fighting over the same device.
	feedDevice = 1
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

	if idx, ok := cardIndex(cardID); ok {
		// Already loaded and correctly named — re-apply the permission
		// lock every tick anyway (cheap, idempotent), in case something
		// external reset it (e.g. a udev rule re-triggering on a device
		// re-enumeration).
		restrictFeedDevice(idx)
		return
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

	if idx, ok := cardIndex(cardID); ok {
		restrictFeedDevice(idx)
	}
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
	_, ok := cardIndex(id)
	return ok
}

func cardIndex(id string) (int, bool) {
	cards, err := alsapcm.EnumCards()
	if err != nil {
		return 0, false
	}
	for _, c := range cards {
		if c.ID == id {
			return c.Index, true
		}
	}
	return 0, false
}

// restrictFeedDevice locks feedDevice's raw device nodes to root-only, so
// only this hack's own writer (which runs as root, like every
// catalog-installed hack) can open it — a human picking an audio device
// in Live's own preferences (running as the unprivileged `ableton` user)
// won't see it as available. See feedDevice's doc comment above.
func restrictFeedDevice(cardIdx int) {
	for _, suffix := range []string{"p", "c"} {
		path := fmt.Sprintf("/dev/snd/pcmC%dD%d%s", cardIdx, feedDevice, suffix)
		if err := os.Chmod(path, 0o600); err != nil && !os.IsNotExist(err) {
			log.Printf("restricting %s: %v", path, err)
		}
	}
}

func moduleLoaded(name string) bool {
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(name+" "))
}
