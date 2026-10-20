package update

import (
	"sync"
	"time"
)

// Prompt is the two-step confirm on the Push: one button opens it, then OK
// or CANCEL. It knows button numbers, not the hardware, so it tests on a Mac.
//
// OK and CANCEL are different buttons, and CANCEL is the one that opened the
// prompt. A double tap on the opening button cancels. It can never install.
type Prompt struct {
	Open, OK, Cancel byte
	Timeout          time.Duration

	mu       sync.Mutex
	open     bool
	deadline time.Time
}

// Active reports whether the prompt is up. It closes itself after Timeout, and
// when the update it asked about is gone (pending false).
func (p *Prompt) Active(now time.Time, pending bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activeLocked(now, pending)
}

func (p *Prompt) activeLocked(now time.Time, pending bool) bool {
	if p.open && (!pending || !now.Before(p.deadline)) {
		p.open = false
	}
	return p.open
}

// Press feeds one button event (val 127 is a press, 0 a release). consumed
// says the caller must not pass it on. install says OK was pressed.
func (p *Prompt) Press(cc, val byte, pending bool, now time.Time) (consumed, install bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	press := val == 127
	if p.activeLocked(now, pending) {
		switch cc {
		case p.OK:
			if press {
				p.open = false
				return true, true
			}
		case p.Cancel:
			if press {
				p.open = false
			}
		}
		return true, false // every other button is ignored while it is up
	}
	if pending && cc == p.Open {
		if press {
			p.open = true
			p.deadline = now.Add(p.Timeout)
		}
		return true, false
	}
	return false, false
}
