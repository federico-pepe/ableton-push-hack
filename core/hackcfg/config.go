// Package hackcfg loads a hack.json config file. Covers the minimal shape
// (id/name/version/port) shared by automation and keyboard-visualizer.
// push-manager's config is a strict superset (allowed_roots, settings, ~
// expansion — file-browser policy) and stays out of scope; it embeds
// hackcfg.Config to decode the shared fields in the same pass.
package hackcfg

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config mirrors the common subset of hack.json.
type Config struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Port    int    `json:"port"`
}

// Load reads path into a Config, defaulting Port to defaultPort if unset.
func Load(path string, defaultPort int) (Config, error) {
	var cfg Config
	f, err := os.Open(path)
	if err != nil {
		return cfg, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	return cfg, nil
}
