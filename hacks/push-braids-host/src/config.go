package main

// config.go — this hack's own persisted settings (MIDI input port, audio
// output device), stored as a sibling of hack.json so it survives a
// catalog install/update the same way push-manager's midi.json does.
// Catalog-installed hacks only ever get "-config <hack.json path>" as an
// argument (see hacks/push-catalog/push-catalog.sh's install_service), so
// there is no other way to point this hack at a non-default value.

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

const configFileName = "braids-config.json"

// persistedConfig is user-selectable state that must survive a restart —
// currently just the two on-screen-picker choices. Defaults match this
// hack's historical hardcoded values, so an upgrading install behaves
// identically until the user actually touches the picker.
type persistedConfig struct {
	MidiClient byte   `json:"midi_client"`
	MidiPort   byte   `json:"midi_port"`
	PCMDevice  string `json:"pcm_device"`
	// ChannelOffset is the 0-indexed channel the stereo signal is written
	// to within PCMDevice's negotiated channel count (e.g. 2 means
	// channels 3-4). Defaults to 0 (channels 1-2), this hack's original
	// hardcoded behavior.
	ChannelOffset int `json:"channel_offset"`
	// PushManagerURL overrides the default http://localhost:7701 — for
	// development only; never set by the on-screen picker.
	PushManagerURL string `json:"push_manager_url,omitempty"`
}

func defaultConfig() persistedConfig {
	return persistedConfig{
		MidiClient:    alsaseq.Push3ClientDefault,
		MidiPort:      alsaseq.Push3PortDefault,
		PCMDevice:     "hw:PHVAudio,1,0",
		ChannelOffset: 0,
	}
}

// loadConfig reads braids-config.json next to hackDir's hack.json. A
// missing file is not an error — it just means "use the defaults," the
// normal state for a fresh install that has never had its picker touched.
func loadConfig(hackDir string) (persistedConfig, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(filepath.Join(hackDir, configFileName))
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// saveConfig writes cfg atomically (temp file + rename), so a crash or
// power loss mid-write can never leave a half-written, unparsable config
// behind.
func saveConfig(hackDir string, cfg persistedConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(hackDir, configFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
