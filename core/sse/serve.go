package sse

import (
	"fmt"
	"net/http"
)

// Serve sets the standard SSE response headers and streams values from ch,
// JSON-encoded via encode, until the request context is done or ch closes.
// Callers wanting an initial "current state" event before the stream starts
// (as keyboard-visualizer's handler does) should write it before calling
// Serve — it needs the same http.Flusher this function requires from w.
func Serve[T any](w http.ResponseWriter, r *http.Request, ch <-chan T, encode func(T) ([]byte, error)) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return fmt.Errorf("sse: ResponseWriter does not support flushing")
	}

	for {
		select {
		case <-r.Context().Done():
			return nil
		case payload, open := <-ch:
			if !open {
				return nil
			}
			data, err := encode(payload)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
