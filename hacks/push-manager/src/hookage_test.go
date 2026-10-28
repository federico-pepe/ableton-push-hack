package main

import "testing"

func TestStartTicks(t *testing.T) {
	// Command name with a space and a bracket: field 2 must not split the line.
	stat := "2592 (Push3 (x) y) S 1 2592 2592 0 -1 4194560 100 0 0 0 5 6 0 0 20 0 30 0 123456 1000 2000 0"
	got, ok := startTicks(stat)
	if !ok || got != 123456 {
		t.Fatalf("startTicks = %d, %v; want 123456, true", got, ok)
	}
	if _, ok := startTicks("garbage"); ok {
		t.Fatal("accepted a line with no command name")
	}
	if _, ok := startTicks("1 (a) S 1 2"); ok {
		t.Fatal("accepted a short line")
	}
}
