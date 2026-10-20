package update

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// API serves the update routes for the web page. It only reports and checks.
type API struct {
	C *Checker
	S *Store
}

// Register adds the routes to mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/update/status", a.handleStatus)
	mux.HandleFunc("/api/update/check", a.handleCheck)
	mux.HandleFunc("/api/update/dismiss", a.handleDismiss)
	mux.HandleFunc("/api/update/settings", a.handleSettings)
}

type releaseJSON struct {
	Version    string `json:"version"`
	Tag        string `json:"tag"`
	URL        string `json:"url"`
	Prerelease bool   `json:"prerelease"`
}

type statusJSON struct {
	Installed         string       `json:"installed"`
	Latest            *releaseJSON `json:"latest"`
	Available         bool         `json:"available"`
	Visible           bool         `json:"visible"` // show the bar: available and not dismissed
	Dismissed         string       `json:"dismissed"`
	LastCheck         *time.Time   `json:"last_check"`
	NextCheck         *time.Time   `json:"next_check"`
	LastError         string       `json:"last_error"`
	AutoCheck         bool         `json:"auto_check"`
	IncludePrerelease bool         `json:"include_prerelease"`
}

func (a *API) snapshot() statusJSON {
	s := a.C.Status()
	set := a.S.Get()
	out := statusJSON{
		Installed: s.Installed, Available: s.Available, Visible: s.Visible(),
		Dismissed: s.Dismissed, LastError: s.LastError,
		AutoCheck: set.AutoCheck, IncludePrerelease: set.IncludePrerelease,
	}
	if s.Latest != nil {
		out.Latest = &releaseJSON{Version: s.Latest.Version, Tag: s.Latest.Tag, URL: s.Latest.URL, Prerelease: s.Latest.Prerelease}
	}
	if !s.LastCheck.IsZero() {
		out.LastCheck = &s.LastCheck
	}
	if !s.NextCheck.IsZero() && set.AutoCheck {
		out.NextCheck = &s.NextCheck
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, 200, a.snapshot())
}

// handleCheck runs a check now and answers with the new status. A failed
// request is not an HTTP error: the status carries last_error.
func (a *API) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := a.C.Check(ctx, true); errors.Is(err, ErrTooSoon) {
		http.Error(w, "checked less than a minute ago", http.StatusTooManyRequests)
		return
	}
	writeJSON(w, 200, a.snapshot())
}

func (a *API) handleDismiss(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body) != nil || body.Version == "" {
		http.Error(w, `body: {"version":"..."}`, http.StatusBadRequest)
		return
	}
	a.C.Dismiss(body.Version)
	writeJSON(w, 200, a.snapshot())
}

func (a *API) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var body struct {
			AutoCheck         *bool `json:"auto_check"`
			IncludePrerelease *bool `json:"include_prerelease"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body) != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		cur := a.S.Get()
		if body.AutoCheck != nil {
			cur.AutoCheck = *body.AutoCheck
		}
		if body.IncludePrerelease != nil {
			cur.IncludePrerelease = *body.IncludePrerelease
		}
		if err := a.S.Set(cur.AutoCheck, cur.IncludePrerelease); err != nil {
			http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, 200, a.snapshot())
}
