package main

import (
	"image"
	"testing"

	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
)

type stubPanel struct{ label string }

func (p *stubPanel) Render(*image.NRGBA)    {}
func (p *stubPanel) HandleCC(cc, val uint8) {}
func (p *stubPanel) Label() string          { return p.label }
func (p *stubPanel) SoftBotStrip() ([8]widgets.SoftButton, string) {
	return [8]widgets.SoftButton{}, ""
}
func (p *stubPanel) BotLEDColors() [8]uint8 { return [8]uint8{} }

// drawPanelTabs must work on a list that buildTabs has since replaced, and
// the replaced list must never hold a nil Panel. renderFrame passes it a copy
// taken under the lock.
func TestDrawPanelTabsOnCopy(t *testing.T) {
	s := &ShadowUI{active: true}
	s.panels = []Panel{&stubPanel{"A"}, &stubPanel{"B"}, &stubPanel{"C"}}
	s.panelIdx = 1
	s.mu.Lock()
	panels := append([]Panel(nil), s.panels...)
	// what buildTabs used to expose for a moment: a fresh slice with nils
	s.panels = make([]Panel, 6)
	s.mu.Unlock()

	img := image.NewNRGBA(image.Rect(0, 0, suiW, suiH))
	drawPanelTabs(img, panels, 1) // would panic on the nil entries of s.panels
	for i, p := range panels {
		if p == nil {
			t.Fatalf("copy has a nil panel at %d", i)
		}
	}
}
