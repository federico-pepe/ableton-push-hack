package text

import (
	"strings"
	"testing"
)

// TestTruncateResultIsASCII is the regression guard that matters.
//
// basicfont.Face7x13 has no glyph past ASCII and draws a missing-glyph box
// instead. Truncate appended U+2026 until 2026-08-17, so every cut filename in
// push-manager's browser rendered a box on the panel — invisible in logs, only
// visible on the hardware. Any non-ASCII byte in the output is a bug.
func TestTruncateResultIsASCII(t *testing.T) {
	inputs := []string{
		"a fairly long sentence that will certainly be cut somewhere",
		strings.Repeat("x", 200),
		"short",
	}
	for _, in := range inputs {
		for _, max := range []int{0, 1, 2, 3, 4, 5, 10, 50, 199, 500} {
			got := Truncate(in, max)
			for i := 0; i < len(got); i++ {
				if got[i] > 0x7E || got[i] < 0x20 {
					t.Errorf("Truncate(%q, %d) = %q: byte %d (%#x) is not printable ASCII",
						in, max, got, i, got[i])
					break
				}
			}
		}
	}
}

func TestTruncateNoOpWhenShortEnough(t *testing.T) {
	for _, tt := range []struct {
		s   string
		max int
	}{
		{"abc", 3},  // exactly at the limit
		{"abc", 10}, // under
		{"", 0},     // empty
		{"", 5},
	} {
		if got := Truncate(tt.s, tt.max); got != tt.s {
			t.Errorf("Truncate(%q, %d) = %q, want it unchanged", tt.s, tt.max, got)
		}
	}
}

func TestTruncateCuts(t *testing.T) {
	tests := []struct {
		s    string
		max  int
		want string
	}{
		{"abcdefghij", 5, "ab..."},
		{"abcdefghij", 9, "abcdef..."},
		{"abcdefghij", 4, "a..."},
		{"hello world", 8, "hello..."},
	}
	for _, tt := range tests {
		if got := Truncate(tt.s, tt.max); got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
		}
	}
}

// TestTruncateNeverExceedsMax pins the invariant callers size layouts from.
func TestTruncateNeverExceedsMax(t *testing.T) {
	const s = "the quick brown fox jumps over the lazy dog"
	for max := 0; max <= len(s)+5; max++ {
		got := Truncate(s, max)
		if n := len([]rune(got)); n > max {
			t.Errorf("Truncate(_, %d) returned %d runes (%q) — exceeds the limit",
				max, n, got)
		}
	}
}

// TestTruncateSmallLimitsDoNotPanic covers the latent crash that shipped
// alongside the ellipsis: maxRunes <= 0 evaluated runes[:maxRunes-1].
func TestTruncateSmallLimitsDoNotPanic(t *testing.T) {
	tests := []struct {
		max  int
		want string
	}{
		{-5, ""},
		{0, ""},
		{1, "."},
		{2, ".."},
		{3, "..."},
	}
	for _, tt := range tests {
		got := Truncate("something long", tt.max)
		if got != tt.want {
			t.Errorf("Truncate(_, %d) = %q, want %q", tt.max, got, tt.want)
		}
	}
}

// TestTruncateCountsRunesNotBytes — a multibyte string must not be cut mid-rune,
// which would emit invalid UTF-8. The characters still cannot be *drawn* (see
// TestTruncateResultIsASCII's rationale), but Truncate must not be the thing
// that corrupts them.
func TestTruncateCountsRunesNotBytes(t *testing.T) {
	// Six runes, twelve bytes.
	const s = "αααααα"
	if len(s) != 12 {
		t.Fatalf("test premise wrong: len = %d", len(s))
	}
	got := Truncate(s, 5)
	if n := len([]rune(got)); n != 5 {
		t.Errorf("Truncate(%q, 5) = %q (%d runes), want 5 runes", s, got, n)
	}
	if !strings.HasSuffix(got, cutMarker) {
		t.Errorf("Truncate(%q, 5) = %q, want it to end in %q", s, got, cutMarker)
	}
	if !strings.ContainsRune(got, 'α') {
		t.Errorf("Truncate(%q, 5) = %q, expected some content to survive", s, got)
	}
}

func TestWidth(t *testing.T) {
	if got := Width(""); got != 0 {
		t.Errorf("Width(\"\") = %d, want 0", got)
	}
	if got := Width("abc"); got != 21 {
		t.Errorf("Width(\"abc\") = %d, want 21", got)
	}
	// Width and Truncate agree on ASCII, which is the only case that renders.
	const s = "a long ASCII string to be cut down"
	cut := Truncate(s, 12)
	if Width(cut) != 12*7 {
		t.Errorf("Width(Truncate(_, 12)) = %d, want %d", Width(cut), 12*7)
	}
}
