package push3

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"testing"
)

// Every named Palette entry must match the hardware table.
func TestPaletteMatchesHardwareRGB(t *testing.T) {
	for _, e := range Palette {
		if e.RGB != hardwareRGB[e.Index] {
			t.Errorf("Palette[%d] %q = %v, hardwareRGB = %v", e.Index, e.Name, e.RGB, hardwareRGB[e.Index])
		}
	}
}

func TestColorForIndexIsExact(t *testing.T) {
	for i := 0; i < 128; i++ {
		got := ColorForIndex(uint8(i))
		if got.Index != uint8(i) || got.RGB != hardwareRGB[i] {
			t.Errorf("ColorForIndex(%d) = %+v", i, got)
		}
	}
	// 67 has no name. It used to return the color of 66.
	if a, b := ColorForIndex(66).RGB, ColorForIndex(67).RGB; a == b {
		t.Errorf("index 66 and 67 have the same color %v", a)
	}
	if got := ColorForIndex(200); got.Index != 127 {
		t.Errorf("ColorForIndex(200).Index = %d, want 127", got.Index)
	}
	if got := ColorForIndex(1); got.Name != "red" {
		t.Errorf("ColorForIndex(1).Name = %q, want red", got.Name)
	}
}

// hardwareRGB must stay equal to the table in docs/push3-led-colors.md.
func TestHardwareRGBMatchesDoc(t *testing.T) {
	f, err := os.Open("../../docs/push3-led-colors.md")
	if err != nil {
		t.Skip("doc not available:", err)
	}
	defer f.Close()
	row := regexp.MustCompile("^\\|\\s*(\\d+)\\s*\\|\\s*`#[0-9A-Fa-f]{6}`\\s*\\|\\s*(\\d+)\\s*\\|\\s*(\\d+)\\s*\\|\\s*(\\d+)\\s*\\|")
	seen := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := row.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		i, _ := strconv.Atoi(m[1])
		r, _ := strconv.Atoi(m[2])
		g, _ := strconv.Atoi(m[3])
		b, _ := strconv.Atoi(m[4])
		if i > 127 {
			continue
		}
		seen++
		if hw := hardwareRGB[i]; int(hw.R) != r || int(hw.G) != g || int(hw.B) != b {
			t.Errorf("index %d: doc %d,%d,%d, code %d,%d,%d", i, r, g, b, hw.R, hw.G, hw.B)
		}
	}
	if seen != 128 {
		t.Errorf("found %d table rows in the doc, want 128", seen)
	}
}
