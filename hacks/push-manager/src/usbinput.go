package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
)

// usbhidLoaded reports whether the usbhid kernel module is currently
// loaded, by checking for its /sys/module entry — cheaper and simpler
// than parsing lsmod/modules output, and this is exactly what "is it
// loaded" means to the kernel.
func usbhidLoaded() bool {
	_, err := os.Stat("/sys/module/usbhid")
	return err == nil
}

// POST /api/input/usbhid — load or unload the usbhid kernel module, so a
// USB mouse/keyboard plugged into the USB-A port becomes usable (or stops
// being usable). Push3's own onboard app never reads keyboard/mouse input
// itself either way (see docs/push3-internals.md) — this only affects
// whether Xorg/Live can see it, confirmed live 2026-08-25.
func handleUsbHid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}

	var cmd *exec.Cmd
	if body.Enabled {
		cmd = exec.Command("modprobe", "usbhid")
	} else {
		// modprobe -r rather than rmmod: also drops usbhid's dependents
		// (usbkbd/usbmouse legacy drivers, if loaded) and is a no-op
		// error, not a crash, if a device is still actively attached —
		// surfaced to the caller as a normal HTTP error either way.
		cmd = exec.Command("modprobe", "-r", "usbhid")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		http.Error(w, string(out)+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"enabled": usbhidLoaded()})
}

// GET /api/input/usbhid — current usbhid load state.
func handleUsbHidStatus(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{"enabled": usbhidLoaded()})
}
