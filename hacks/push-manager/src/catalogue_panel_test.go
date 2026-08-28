// Covers the two things that actually break silently: the cursor/scroll
// window math, and the catalog JSON tags (the wire contract with the
// push-catalogue daemon).
package main

import (
	"encoding/json"
	"testing"
)

func TestCatalogueCursorScrollWindow(t *testing.T) {
	p := &CataloguePanel{installed: map[string]bool{}}
	p.hacks = make([]catalogueHack, 20)

	for i := 0; i < 8; i++ {
		p.moveCursor(1)
	}
	if p.cursor != 8 {
		t.Fatalf("cursor=%d want 8", p.cursor)
	}
	if want := 8 - catalogueVisibleRows + 1; p.scroll != want {
		t.Fatalf("scroll=%d want %d (cursor must stay in the visible window)", p.scroll, want)
	}

	for i := 0; i < 50; i++ {
		p.moveCursor(-1)
	}
	if p.cursor != 0 || p.scroll != 0 {
		t.Fatalf("top clamp failed: cursor=%d scroll=%d", p.cursor, p.scroll)
	}

	for i := 0; i < 50; i++ {
		p.moveCursor(1)
	}
	if p.cursor != 19 {
		t.Fatalf("bottom clamp: cursor=%d want 19", p.cursor)
	}
	if want := 19 - catalogueVisibleRows + 1; p.scroll != want {
		t.Fatalf("bottom scroll=%d want %d", p.scroll, want)
	}
}

func TestCatalogueCatalogParse(t *testing.T) {
	const j = `[{"id":"kv","name":"KV","version":"0.1.0","description":"d","author":"a","requires":["push-manager"]}]`
	var got []catalogueHack
	if err := json.Unmarshal([]byte(j), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "kv" || got[0].Version != "0.1.0" || len(got[0].Requires) != 1 {
		t.Fatalf("json tags out of sync with daemon: %+v", got)
	}
}
