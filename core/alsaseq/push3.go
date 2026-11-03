package alsaseq

// push3.go — follow Push 3's Live Port when its client number moves.
//
// The kernel numbers sequencer clients by sound card (16 + 4 × card index).
// Push 3 is client 16 only while it is ALSA card 0; when the loopback card
// (PHVAudio) or a USB MIDI device is card 0, Push 3 is client 20 or more.
// push-manager already looks it up by name (detectPush3Port); this gives the
// hacks the same lookup so they stop pinning Push3ClientDefault.

import "sync/atomic"

// Push3LivePortName is the name of Push 3's control port.
const Push3LivePortName = "Ableton Push 3 Live Port"

// Push3 follows where Push 3's Live Port is. The zero value points at
// Push3ClientDefault:Push3PortDefault until Refresh finds it. Safe for
// concurrent use: Refresh runs on a supervisor goroutine, Is on the read loop.
type Push3 struct {
	addr atomic.Uint32 // client<<8 | port, plus 1<<16 once set
}

const push3Known = 1 << 16

// Refresh looks Push 3 up in /proc/asound/seq/clients and remembers where it
// is. When it is not listed, the last known address stays. It returns the
// address now in use.
func (p *Push3) Refresh() Addr {
	ports, err := EnumPorts(CapRead)
	if err == nil {
		p.refreshFrom(ports)
	}
	return p.Addr()
}

func (p *Push3) refreshFrom(ports []Port) {
	for _, pt := range ports {
		if pt.PortName == Push3LivePortName {
			p.addr.Store(push3Known | uint32(pt.Addr.Client)<<8 | uint32(pt.Addr.Port))
			return
		}
	}
}

// Addr is Push 3's Live Port: the one found by Refresh, else the defaults.
func (p *Push3) Addr() Addr {
	v := p.addr.Load()
	if v&push3Known == 0 {
		return Addr{Client: Push3ClientDefault, Port: Push3PortDefault}
	}
	return Addr{Client: byte(v >> 8), Port: byte(v)}
}

// Is says whether src is a port of Push 3's client (the Live Port, the User
// Port or the External Port).
func (p *Push3) Is(src Addr) bool { return src.Client == p.Addr().Client }

// IsLivePort says whether src is exactly Push 3's Live Port.
func (p *Push3) IsLivePort(src Addr) bool { return src == p.Addr() }
