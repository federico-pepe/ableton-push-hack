package main

// vst3.go -- routes for the VST3 scan switch. Logic: vst3scan package.
//
//	GET  /api/vst3                 state: on | off | unknown, plugin count
//	POST /api/vst3 {"enabled":b}   change the flag, restart the Push stack

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"push-manager/vst3scan"
)

var vst3 = newVst3()

// newVst3 wires the switch. Before Live stops, MIDI intercept goes off and the
// Shadow UI closes, as /api/display/mode does when an app takes the screen:
// the restart would leave both half-dead (Shadow UI drawing on a screen that
// Push3 is about to rebuild, intercept holding the MIDI Push3 needs back).
func newVst3() *vst3scan.Ctl {
	c := vst3scan.New()
	c.BeforeStop = func() {
		if filt := ensureMidiFilt(); filt != nil {
			filt[4] = 0
		}
		shadowUIStop()
	}
	return c
}

func handleVst3(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, vst3.State())
	case http.MethodPost:
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil && err != io.EOF || req.Enabled == nil {
			http.Error(w, "bad JSON: need {\"enabled\": bool}", http.StatusBadRequest)
			return
		}
		restarting, err := vst3.Set(*req.Enabled)
		switch {
		case err == nil:
			jsonResponse(w, map[string]interface{}{"restarting": restarting})
		case errors.Is(err, vst3scan.ErrBusy):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, vst3scan.ErrLayout), errors.Is(err, vst3scan.ErrNoFile):
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	default:
		http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
	}
}
