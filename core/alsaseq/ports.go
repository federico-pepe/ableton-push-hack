package alsaseq

// ports.go — enumerate ALSA sequencer ports from /proc/asound/seq/clients.
// Replaces enumMidiPorts (push-manager:1671, automation:473, near-identical
// text-format parsers), automation's third internal copy detectLivePort
// (automation:177), and all three detectPush3Port helpers (push-manager:753,
// automation:443, keyboard-visualizer/chord.go:61).
//
// keyboard-visualizer's divergence is preserved deliberately, not
// unified: it calls FindByName with requireCaps=0, matching on port name
// alone exactly as its own chord.go:61 did — push-manager and automation
// pass the R/W capability mask they actually need.

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

// Port is one ALSA sequencer port as listed in /proc/asound/seq/clients.
type Port struct {
	Addr       Addr
	ClientName string
	PortName   string
	Caps       uint32 // CapRead / CapWrite bits parsed from the "(RWe)" field
}

// ParseClients parses the /proc/asound/seq/clients text format. Split out
// from EnumPorts as a testable seam — see ports_test.go's fixtures, one
// captured bare and one with an external USB MIDI device attached (client
// number shifted from 16 to 20).
func ParseClients(r io.Reader) ([]Port, error) {
	var ports []Port
	curClient := -1
	curClientName := ""

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())

		// Client line: `Client  16 : "Ableton Push 3 Live Port" [Kernel]`
		if strings.HasPrefix(trimmed, "Client ") && !strings.HasPrefix(trimmed, "Client info") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "Client "))
			colonIdx := strings.Index(rest, ":")
			if colonIdx < 0 {
				curClient = -1
				continue
			}
			id, err := strconv.Atoi(strings.TrimSpace(rest[:colonIdx]))
			if err != nil {
				curClient = -1
				continue
			}
			curClient = id
			curClientName = quotedField(rest[colonIdx+1:])
			continue
		}

		// Port line: `  Port   0 : "Ableton Push 3 Live Port" (RWe)`
		if strings.HasPrefix(trimmed, "Port ") && curClient >= 0 {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "Port "))
			colonIdx := strings.Index(rest, ":")
			if colonIdx < 0 {
				continue
			}
			portID, err := strconv.Atoi(strings.TrimSpace(rest[:colonIdx]))
			if err != nil {
				continue
			}
			after := rest[colonIdx+1:]

			parenOpen := strings.LastIndex(after, "(")
			parenClose := strings.LastIndex(after, ")")
			if parenOpen < 0 || parenClose <= parenOpen {
				continue
			}
			capsStr := after[parenOpen+1 : parenClose]
			var caps uint32
			if strings.Contains(capsStr, "R") {
				caps |= CapRead
			}
			if strings.Contains(capsStr, "W") {
				caps |= CapWrite
			}

			ports = append(ports, Port{
				Addr:       Addr{Client: byte(curClient), Port: byte(portID)},
				ClientName: curClientName,
				PortName:   quotedField(after),
				Caps:       caps,
			})
		}
	}
	return ports, scanner.Err()
}

// quotedField returns the contents of the first "..." quoted string in s.
func quotedField(s string) string {
	q1 := strings.Index(s, `"`)
	if q1 < 0 {
		return ""
	}
	q2 := strings.Index(s[q1+1:], `"`)
	if q2 < 0 {
		return ""
	}
	return s[q1+1 : q1+1+q2]
}

// EnumPorts reads /proc/asound/seq/clients and returns every port whose
// capability bits are a superset of requireCaps. requireCaps=0 matches any
// port regardless of capability (see keyboard-visualizer's usage above).
func EnumPorts(requireCaps uint32) ([]Port, error) {
	f, err := os.Open("/proc/asound/seq/clients")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	all, err := ParseClients(f)
	if err != nil {
		return nil, err
	}
	if requireCaps == 0 {
		return all, nil
	}
	var filtered []Port
	for _, p := range all {
		if p.Caps&requireCaps == requireCaps {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// FindByName returns the first port (in /proc/asound/seq/clients order)
// whose port name contains substr and whose capability bits are a superset
// of requireCaps.
func FindByName(substr string, requireCaps uint32) (Port, bool) {
	ports, err := EnumPorts(requireCaps)
	if err != nil {
		return Port{}, false
	}
	for _, p := range ports {
		if strings.Contains(p.PortName, substr) {
			return p, true
		}
	}
	return Port{}, false
}
