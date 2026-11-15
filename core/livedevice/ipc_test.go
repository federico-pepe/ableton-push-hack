package livedevice

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The C header and ipc.go must give the same layout: the plugin and the hack
// read the same bytes. This evaluates the header's #defines and compares.
func TestHeaderMatchesGo(t *testing.T) {
	src, err := os.ReadFile("vst3/source/livedevice_ipc.h")
	if err != nil {
		t.Fatal(err)
	}
	defs := map[string]int{}
	re := regexp.MustCompile(`(?m)^#define (LD_\w+) (.+?)\s*(?:/\*.*)?$`)
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		defs[m[1]] = eval(t, m[2], defs)
	}
	want := map[string]int{
		"LD_IPC_MAGIC": ipcMagic, "LD_IPC_VERSION": ipcVersion, "LD_IPC_MAXF": MaxFrames,
		"LD_IPC_MAXEV": MaxEvents, "LD_IPC_MAXPAR": MaxParams,
		"LD_OFF_MAGIC": offMagic, "LD_OFF_VERSION": offVersion, "LD_OFF_REQ": offReq, "LD_OFF_DONE": offDone,
		"LD_OFF_FRAMES": offFrames, "LD_OFF_FLAGS": offFlags, "LD_OFF_BPM": offBPM, "LD_OFF_BEAT": offBeat,
		"LD_OFF_NEV": offNEv, "LD_OFF_NPAR": offNPar, "LD_OFF_LATE": offLate, "LD_OFF_ALIVE": offAlive,
		"LD_OFF_EVENTS": offEvents, "LD_OFF_PARAMS": offParams, "LD_OFF_IN_L": offInL, "LD_OFF_IN_R": offInR,
		"LD_OFF_OUT_L": offOutL, "LD_OFF_OUT_R": offOutR, "LD_OFF_ONEV": offONEv, "LD_OFF_OEVENTS": offOEvents,
		"LD_OFF_ONPAR": offONPar, "LD_OFF_OPARAMS": offOParams,
		"LD_IPC_SIZE": ipcSize,
	}
	for k, v := range want {
		got, ok := defs[k]
		if !ok {
			t.Errorf("%s missing in the header", k)
		} else if got != v {
			t.Errorf("%s: header %d, Go %d", k, got, v)
		}
	}
}

// eval handles what the header uses: numbers, names, + and *, parentheses.
func eval(t *testing.T, expr string, defs map[string]int) int {
	expr = strings.TrimSpace(strings.Trim(strings.TrimSpace(expr), "()"))
	if i := strings.LastIndex(expr, "+"); i > 0 {
		return eval(t, expr[:i], defs) + eval(t, expr[i+1:], defs)
	}
	if i := strings.LastIndex(expr, "*"); i > 0 {
		return eval(t, expr[:i], defs) * eval(t, expr[i+1:], defs)
	}
	if v, ok := defs[expr]; ok {
		return v
	}
	v, err := strconv.ParseInt(strings.TrimSuffix(expr, "u"), 0, 64)
	if err != nil {
		t.Fatalf("cannot read %q", expr)
	}
	return int(v)
}
