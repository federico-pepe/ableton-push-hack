package semver

import "testing"

func TestCompare(t *testing.T) {
	// Each row: a < b. The reverse must give +1, and a vs a must give 0.
	ordered := [][2]string{
		{"0.1.9-alpha", "0.1.10-alpha"},
		{"0.1.9", "0.1.10"},
		{"0.1.10", "0.2.0"},
		{"0.9.0", "1.0.0"},
		{"0.1.10-alpha", "0.1.10-beta"},
		{"0.1.10-beta", "0.1.10-rc.1"},
		{"0.1.10-rc.1", "0.1.10-rc.2"},
		{"0.1.10-rc.2", "0.1.10-rc.10"},
		{"0.1.10-rc.10", "0.1.10"},
		{"0.1.10-alpha", "0.1.10"},
		{"0.1.10-alpha", "0.1.10-alpha.1"},
		{"0.1.10-alpha.1", "0.1.10-alpha.beta"}, // number < word
		{"0.1.9", "0.1.10-alpha"},               // older release < newer pre-release
		{"v0.1.9-alpha", "v0.1.10-alpha"},       // tag form
		{"0.1.9-alpha", "v0.1.10-alpha"},        // mixed
	}
	for _, r := range ordered {
		a, err := Parse(r[0])
		if err != nil {
			t.Fatalf("parse %q: %v", r[0], err)
		}
		b, err := Parse(r[1])
		if err != nil {
			t.Fatalf("parse %q: %v", r[1], err)
		}
		if got := a.Compare(b); got != -1 {
			t.Errorf("%s vs %s = %d, want -1", r[0], r[1], got)
		}
		if got := b.Compare(a); got != 1 {
			t.Errorf("%s vs %s = %d, want 1", r[1], r[0], got)
		}
		if got := a.Compare(a); got != 0 {
			t.Errorf("%s vs itself = %d, want 0", r[0], got)
		}
	}
}

func TestEqualIgnoresVAndBuild(t *testing.T) {
	a, _ := Parse("v0.1.10-alpha")
	b, _ := Parse("0.1.10-alpha+abc123")
	if a.Compare(b) != 0 {
		t.Fatalf("v-prefix and build metadata must not change the order")
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{
		"", "dev", "1", "1.2", "1.2.3.4", "a.b.c", "1.2.x", "-1.2.3",
		"1.2.3-", "1.2.3-alpha..1", "01.2.3", "1.2.3-01", "1.2.3 ", "v",
	} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) accepted, want error", s)
		}
	}
}

func TestString(t *testing.T) {
	v, err := Parse("v0.1.10-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "0.1.10-alpha" {
		t.Fatalf("String() = %q", v.String())
	}
	if !v.Prerelease() {
		t.Fatal("Prerelease() = false")
	}
	r, _ := Parse("1.0.0")
	if r.Prerelease() {
		t.Fatal("Prerelease() = true for a release")
	}
}
