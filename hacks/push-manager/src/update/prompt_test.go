package update

import (
	"context"
	"testing"
	"time"
)

const (
	ccOpen   = 27 // far right under the screen
	ccOK     = 26
	ccCancel = 27
	ccOther  = 20
)

func newPrompt() *Prompt {
	return &Prompt{Open: ccOpen, OK: ccOK, Cancel: ccCancel, Timeout: 15 * time.Second}
}

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func TestPromptIgnoresEverythingWithoutUpdate(t *testing.T) {
	p := newPrompt()
	for _, cc := range []byte{ccOpen, ccOK, ccOther} {
		if c, i := p.Press(cc, 127, false, t0); c || i {
			t.Fatalf("cc %d consumed without an update: %v %v", cc, c, i)
		}
	}
}

func TestPromptOpenThenOK(t *testing.T) {
	p := newPrompt()
	if c, i := p.Press(ccOpen, 127, true, t0); !c || i {
		t.Fatalf("open: consumed=%v install=%v", c, i)
	}
	if !p.Active(t0, true) {
		t.Fatal("not active after open")
	}
	if c, i := p.Press(ccOK, 127, true, t0.Add(time.Second)); !c || !i {
		t.Fatalf("ok: consumed=%v install=%v", c, i)
	}
	if p.Active(t0.Add(time.Second), true) {
		t.Fatal("still active after OK")
	}
}

func TestPromptDoubleTapOpenButtonCancels(t *testing.T) {
	p := newPrompt()
	p.Press(ccOpen, 127, true, t0)
	p.Press(ccOpen, 0, true, t0) // release of the opening press
	if !p.Active(t0, true) {
		t.Fatal("release of the opening press closed the prompt")
	}
	if c, i := p.Press(ccOpen, 127, true, t0.Add(100*time.Millisecond)); !c || i {
		t.Fatalf("second tap: consumed=%v install=%v", c, i)
	}
	if p.Active(t0.Add(100*time.Millisecond), true) {
		t.Fatal("second tap did not cancel")
	}
}

func TestPromptSwallowsOtherButtonsWhileOpen(t *testing.T) {
	p := newPrompt()
	p.Press(ccOpen, 127, true, t0)
	if c, i := p.Press(ccOther, 127, true, t0); !c || i {
		t.Fatalf("other button: consumed=%v install=%v", c, i)
	}
	if !p.Active(t0, true) {
		t.Fatal("other button closed the prompt")
	}
}

func TestPromptTimesOut(t *testing.T) {
	p := newPrompt()
	p.Press(ccOpen, 127, true, t0)
	late := t0.Add(16 * time.Second)
	if p.Active(late, true) {
		t.Fatal("still active after the timeout")
	}
	if c, _ := p.Press(ccOther, 127, true, late); c {
		t.Fatal("swallowed a press after the timeout")
	}
}

func TestPromptClosesWhenUpdateGoesAway(t *testing.T) {
	p := newPrompt()
	p.Press(ccOpen, 127, true, t0)
	if p.Active(t0, false) { // dismissed on the web page meanwhile
		t.Fatal("active with no update pending")
	}
	if c, i := p.Press(ccOK, 127, false, t0); c || i {
		t.Fatal("OK acted with no update pending")
	}
}

func TestPromptOKNeedsAnOpenPrompt(t *testing.T) {
	p := newPrompt()
	if c, i := p.Press(ccOK, 127, true, t0); c || i {
		t.Fatalf("OK without opening: consumed=%v install=%v", c, i)
	}
}

func TestPending(t *testing.T) {
	f := newFixture(t, "0.1.10-alpha")
	if _, ok := f.c.Pending(); ok {
		t.Fatal("pending before any check")
	}
	f.c.Check(context.Background(), true)
	if v, ok := f.c.Pending(); !ok || v != "0.1.11-alpha" {
		t.Fatalf("pending = %q %v", v, ok)
	}
	f.c.Dismiss("0.1.11-alpha")
	if _, ok := f.c.Pending(); ok {
		t.Fatal("pending after dismiss")
	}
}
