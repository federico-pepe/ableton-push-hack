package main

// web.go — SSE broadcast of held-note state to the mobile-friendly web view
// (src/ui/index.html). Independent of the on-device display takeover: the
// web view always reflects current held notes regardless of whether the
// Shift+Note chord has toggled the Push screen on. Broadcaster pattern
// copied from hacks/automation/src/engine.go (registerSSEClient/
// unregisterSSEClient).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
)

type notesPayload struct {
	Notes []int `json:"notes"`
}

var (
	sseMu      sync.Mutex
	sseClients []chan notesPayload
)

func registerSSEClient() chan notesPayload {
	ch := make(chan notesPayload, 4)
	sseMu.Lock()
	sseClients = append(sseClients, ch)
	sseMu.Unlock()
	return ch
}

func unregisterSSEClient(ch chan notesPayload) {
	sseMu.Lock()
	out := sseClients[:0]
	for _, c := range sseClients {
		if c != ch {
			out = append(out, c)
		}
	}
	sseClients = out
	sseMu.Unlock()
}

// broadcastNotes sends the current held-note set to every connected web
// client. Non-blocking: a slow/stuck client is dropped rather than stalling
// the render loop that calls this.
func broadcastNotes(held [128]bool) {
	var notes []int
	for n, on := range held {
		if on {
			notes = append(notes, n)
		}
	}
	sort.Ints(notes)
	payload := notesPayload{Notes: notes}

	sseMu.Lock()
	defer sseMu.Unlock()
	for _, ch := range sseClients {
		select {
		case ch <- payload:
		default:
		}
	}
}

// handleNotesStream is GET /api/notes/stream — an SSE feed of currently
// held MIDI notes, e.g. `data: {"notes":[60,64,67]}\n\n` on every change.
func handleNotesStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := registerSSEClient()
	defer unregisterSSEClient(ch)

	// Send current state immediately so a freshly-opened page doesn't wait
	// for the next change to show anything.
	initial := notesPayload{}
	held := snapshotHeldNotes()
	for n, on := range held {
		if on {
			initial.Notes = append(initial.Notes, n)
		}
	}
	if data, err := json.Marshal(initial); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			data, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
