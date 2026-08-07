package widgets

// kvrow.go — label:value row rendering. Generalizes push-manager's
// StatsPanel and MidiPanel, which each reimplemented this independently
// (see discovery/shadow-ui-component-framework.md). Baseline offset is
// standardized on StatsPanel's (rowH-4); MidiPanel's was rowH-6, a 2px
// cosmetic drift with no functional effect.

import (
	"image"
	"image/color"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
)

// KVRow is one label:value row. ValueCol lets a caller color-code the value
// (e.g. Theme.OnColor/OffColor for an ON/OFF state) — the label is always
// drawn in Theme.Gray.
type KVRow struct {
	Label    string
	Value    string
	ValueCol color.NRGBA
}

// DrawKVRows draws rows starting at y, rowH pixels each with a labelW-pixel
// label column, stopping once a row would fall at/past maxY (the panel's
// content bottom). w is the row width, used for the separator line.
func DrawKVRows(img *image.NRGBA, t Theme, y, w, rowH, labelW, maxY int, rows []KVRow) {
	for _, r := range rows {
		if y+rowH > maxY {
			break
		}
		text.Draw(img, 8, y+rowH-4, r.Label, t.Gray)
		text.Draw(img, 8+labelW, y+rowH-4, r.Value, r.ValueCol)
		gfx.FillRect(img, 0, y+rowH-1, w, 1, t.DarkGray)
		y += rowH
	}
}
