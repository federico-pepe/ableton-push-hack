package main

// web.go — SSE broadcast of held-note state to the mobile-friendly web view
// (src/ui/index.html). Independent of the on-device display takeover: the
// web view always reflects current held notes regardless of whether the
// Shift+Note chord has toggled the Push screen on.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/federico-pepe/ableton-push-hack/core/sse"
)

type notesPayload struct {
	Notes []int `json:"notes"`
}

// broker is unbuffered-pruning=false: a slow/stuck web client just misses an
// update rather than being dropped from the broadcast list (unlike
// automation's broker) — see core/sse.NewBroker's pruneDropped doc.
var broker = sse.NewBroker[notesPayload](4, false)

func registerSSEClient() chan notesPayload { return broker.Register() }

func unregisterSSEClient(ch chan notesPayload) { broker.Unregister(ch) }

// broadcastNotes sends the current held-note set to every connected web
// client. Non-blocking: a slow/stuck client is skipped rather than stalling
// the render loop that calls this.
func broadcastNotes(held [128]bool) {
	var notes []int
	for n, on := range held {
		if on {
			notes = append(notes, n)
		}
	}
	sort.Ints(notes)
	broker.Broadcast(notesPayload{Notes: notes})
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

	sse.Serve(w, r, ch, func(p notesPayload) ([]byte, error) { return json.Marshal(p) })
}
