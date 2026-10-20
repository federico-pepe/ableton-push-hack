package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings are the user's switches. A missing or broken file gives the
// defaults: both on.
type Settings struct {
	AutoCheck         bool `json:"auto_check"`
	IncludePrerelease bool `json:"include_prerelease"`
	// URL replaces the GitHub release list. For tests on a bench only: point
	// it at a local server that serves a fake list. Not settable over HTTP.
	URL string `json:"url,omitempty"`
}

// Store keeps Settings in one JSON file.
type Store struct {
	path string
	mu   sync.Mutex
	s    Settings
}

// OpenStore loads the file at path.
func OpenStore(path string) *Store {
	st := &Store{path: path, s: Settings{AutoCheck: true, IncludePrerelease: true}}
	if b, err := os.ReadFile(path); err == nil {
		var s Settings
		if json.Unmarshal(b, &s) == nil {
			st.s = s
		}
	}
	return st
}

// Get returns a copy.
func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s
}

// AutoCheck and IncludePrerelease fit the Config hooks.
func (st *Store) AutoCheck() bool         { return st.Get().AutoCheck }
func (st *Store) IncludePrerelease() bool { return st.Get().IncludePrerelease }

// Set changes the two switches and saves. URL is kept as it was.
func (st *Store) Set(auto, pre bool) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	next := st.s
	next.AutoCheck, next.IncludePrerelease = auto, pre
	b, err := json.MarshalIndent(next, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(st.path), 0o755)
	}
	if err == nil {
		tmp := st.path + ".tmp"
		if err = os.WriteFile(tmp, b, 0o644); err == nil {
			err = os.Rename(tmp, st.path)
		}
	}
	if err != nil {
		return err
	}
	st.s = next
	return nil
}
