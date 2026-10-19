package push3

import "testing"

func TestLEDButtonsNoDuplicatesAndSceneColumn(t *testing.T) {
	seen := map[uint8]bool{}
	for _, cc := range LEDButtons {
		if seen[cc] {
			t.Errorf("CC %d listed twice", cc)
		}
		seen[cc] = true
	}
	for cc := uint8(CCScene14); cc <= CCScene132t; cc++ {
		if !seen[cc] || !IsRGBButton(cc) {
			t.Errorf("scene button CC %d missing or not RGB", cc)
		}
	}
	if IsRGBButton(CCUndo) || IsRGBButton(CCPlay) {
		t.Error("Undo and Play are not palette-indexed screen or scene buttons")
	}
}
