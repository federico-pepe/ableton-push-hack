package main

import "testing"

func TestExtractVermagic(t *testing.T) {
	data := []byte("junk\x00vermagic=5.15.48-push3 SMP mod_unload \x00more junk")
	got, ok := extractVermagic(data)
	if !ok {
		t.Fatal("expected a vermagic match")
	}
	want := "5.15.48-push3 SMP mod_unload "
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractVermagicMissing(t *testing.T) {
	if _, ok := extractVermagic([]byte("nothing here")); ok {
		t.Error("expected no match")
	}
}

func TestCString(t *testing.T) {
	got := cString([]byte("5.15.48-push3\x00\x00\x00"))
	if got != "5.15.48-push3" {
		t.Errorf("got %q", got)
	}
}
