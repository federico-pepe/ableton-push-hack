package main

// script_status.go -- routes for the PushHack Remote Script on the System page.
//
//	GET  /api/live/script             state (rscript.State)
//	POST /api/live/script/remove-old  {"name": "PushHackBrowser"}
//
// Removing an old script is the user's call: a hack that was not updated yet
// may still need it, so nothing removes one by itself. Logic: rscript package.

import (
	"encoding/json"
	"errors"
	"net/http"

	"push-manager/rscript"
)

// bridgeHello asks the running script who it is, over the bridge socket.
func bridgeHello() (string, int, error) {
	reply, err := bridgeSend(`{"id":1,"cmd":"hello"}` + "\n")
	if err != nil {
		return "", 0, err
	}
	var r struct {
		OK   bool `json:"ok"`
		Data struct {
			Protocol      int    `json:"protocol"`
			ScriptVersion string `json:"script_version"`
		} `json:"data"`
	}
	// An old PushHackBrowser answers "OK", not JSON: that is not PushHack.
	if err := json.Unmarshal([]byte(reply), &r); err != nil || !r.OK {
		return "", 0, errors.New("not PushHack")
	}
	return r.Data.ScriptVersion, r.Data.Protocol, nil
}

func handleLiveScriptStatus(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, rscript.Read(remoteScriptsRoot, bridgeHello))
}

func handleLiveScriptRemoveOld(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	switch err := rscript.RemoveOld(remoteScriptsRoot, req.Name); {
	case err == nil:
		jsonResponse(w, rscript.Read(remoteScriptsRoot, bridgeHello))
	case errors.Is(err, rscript.ErrUnknownName):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, rscript.ErrNoPushHack):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, rscript.ErrNotThere):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
