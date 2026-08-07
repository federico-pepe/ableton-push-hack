package sse

import "testing"

// TestBrokerPruneDropped pins automation's behavior: a client whose buffer
// is full gets dropped from the broadcast list on the next Broadcast.
func TestBrokerPruneDropped(t *testing.T) {
	b := NewBroker[int](1, true)
	ch := b.Register()
	b.Broadcast(1) // fills the buffer (size 1)
	b.Broadcast(2) // channel full -> send would block -> pruned

	b.mu.Lock()
	n := len(b.clients)
	b.mu.Unlock()
	if n != 0 {
		t.Fatalf("client count = %d, want 0 (should have been pruned)", n)
	}
	_ = ch
}

// TestBrokerNonPruning pins keyboard-visualizer's behavior: a client whose
// buffer is full just misses that update and stays registered.
func TestBrokerNonPruning(t *testing.T) {
	b := NewBroker[int](1, false)
	ch := b.Register()
	b.Broadcast(1) // fills the buffer
	b.Broadcast(2) // dropped silently, client NOT removed

	b.mu.Lock()
	n := len(b.clients)
	b.mu.Unlock()
	if n != 1 {
		t.Fatalf("client count = %d, want 1 (must stay registered)", n)
	}
	_ = ch
}

func TestBrokerUnregister(t *testing.T) {
	b := NewBroker[int](4, true)
	ch1 := b.Register()
	ch2 := b.Register()
	b.Unregister(ch1)

	b.mu.Lock()
	n := len(b.clients)
	b.mu.Unlock()
	if n != 1 {
		t.Fatalf("client count = %d, want 1", n)
	}
	_ = ch2
}
