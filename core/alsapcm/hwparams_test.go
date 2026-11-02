package alsapcm

import (
	"os"
	"testing"
)

func parseFile(t *testing.T, p string) (HWParams, bool) {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, ok, err := ParseHWParams(f)
	if err != nil {
		t.Fatal(err)
	}
	return got, ok
}

func TestHWParamsClosed(t *testing.T) {
	if _, ok := parseFile(t, "testdata/closed.txt"); ok {
		t.Fatal("closed stream reported open")
	}
}

func TestHWParamsOpen(t *testing.T) {
	p, ok := parseFile(t, "testdata/open.txt")
	if !ok || p != (HWParams{32, 44100, 128, 384}) {
		t.Fatalf("got %+v ok=%v", p, ok)
	}
}

func TestHWParamsMissingCard(t *testing.T) {
	if _, ok, err := ReadHWParams("NoSuchCard987"); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
