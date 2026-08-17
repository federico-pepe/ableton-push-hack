// Package sse holds a generic Server-Sent-Events broadcaster: register a
// channel per connected client, broadcast a value to all of them. Extracted
// from automation's engine.go and keyboard-visualizer's web.go, which had
// the identical registerSSEClient/unregisterSSEClient pattern (the latter's
// own comment says it was copied from the former) differing only in the
// payload type and in whether Broadcast prunes a client whose channel is
// full — automation prunes (a truly stuck client should stop consuming
// send slots), keyboard-visualizer does not (see NewBroker's pruneDropped
// parameter, preserved as a deliberate per-hack choice, not unified).
package sse

import "sync"

// Broker fans out values of type T to any number of registered clients.
type Broker[T any] struct {
	mu           sync.Mutex
	clients      []chan T
	bufSize      int
	pruneDropped bool
}

// NewBroker creates a Broker. bufSize is each client channel's buffer size.
// pruneDropped, when true, removes a client from the broadcast list the
// moment a send to it would block (automation's behavior); when false, a
// slow client just misses that update and stays registered (keyboard-
// visualizer's behavior).
func NewBroker[T any](bufSize int, pruneDropped bool) *Broker[T] {
	return &Broker[T]{bufSize: bufSize, pruneDropped: pruneDropped}
}

// Register creates and returns a new client channel.
func (b *Broker[T]) Register() chan T {
	ch := make(chan T, b.bufSize)
	b.mu.Lock()
	b.clients = append(b.clients, ch)
	b.mu.Unlock()
	return ch
}

// Unregister removes ch from the broadcast list.
func (b *Broker[T]) Unregister(ch chan T) {
	b.mu.Lock()
	out := b.clients[:0]
	for _, c := range b.clients {
		if c != ch {
			out = append(out, c)
		}
	}
	b.clients = out
	b.mu.Unlock()
}

// Broadcast sends v to every registered client without blocking.
func (b *Broker[T]) Broadcast(v T) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.pruneDropped {
		for _, ch := range b.clients {
			select {
			case ch <- v:
			default:
			}
		}
		return
	}
	active := make([]chan T, 0, len(b.clients))
	for _, ch := range b.clients {
		select {
		case ch <- v:
			active = append(active, ch)
		default:
			// Slow or disconnected client — drop.
		}
	}
	b.clients = active
}
