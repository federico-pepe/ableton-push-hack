package plugintree

import (
	"os"
	"testing"
)

func TestGeneratedAirwindowsFileIsValid(t *testing.T) {
	const f = "../../../../resources/VST/Airwindows/plugin-folders.json"
	if _, err := os.Stat(f); err != nil {
		t.Skip("generated file is not in this checkout (resources/VST is git-ignored)")
	}
	c, err := Load(f)
	if err != nil || len(c.Folders) != 1 || len(c.Folders[0].Folders) < 20 {
		t.Fatalf("err=%v folders=%d", err, len(c.Folders))
	}
}
