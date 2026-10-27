package main

import (
	"strings"
	"testing"
)

func TestBridgeCallReturnsDataAndSendsTheRequest(t *testing.T) {
	got := fakeBridge(t, okData(`{"playing":true,"tempo":128.5,"beat":7.25}`))
	tr, err := liveTransportGet()
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Playing || tr.Tempo != 128.5 || tr.Beat != 7.25 {
		t.Errorf("transport %+v", tr)
	}
	if req := <-got; req["ns"] != "transport" || req["cmd"] != "get" {
		t.Errorf("sent %v", req)
	}
}

func TestLiveGettersUseTheTransportSnapshot(t *testing.T) {
	fakeBridge(t, okData(`{"playing":false,"tempo":97.5,"beat":3}`))
	if p, err := liveIsPlaying(); err != nil || p {
		t.Errorf("playing %v %v", p, err)
	}
	if b, err := liveTempo(); err != nil || b != 97.5 {
		t.Errorf("tempo %v %v", b, err)
	}
	if b, err := liveBeat(); err != nil || b != 3 {
		t.Errorf("beat %v %v", b, err)
	}
}

func TestLiveLoadScopeAndError(t *testing.T) {
	got := fakeBridge(t, okData(`{"loaded":"Wobble"}`))
	if err := liveLoad("Wobble", CatInstruments); err != nil {
		t.Fatal(err)
	}
	req := <-got
	if req["ns"] != "browser" || req["cmd"] != "load" || req["name"] != "Wobble" || req["scope"] != "instruments" {
		t.Errorf("sent %v", req)
	}
	if err := liveLoad("Wobble", PresetCategory("")); err != nil {
		t.Fatal(err)
	}
	if req := <-got; req["scope"] != nil {
		t.Errorf("an unknown category must not send a scope: %v", req)
	}
}

func TestLiveLoadReportsWhyItFailed(t *testing.T) {
	fakeBridge(t, func(map[string]any) string { return `{"ok":false,"id":1,"error":"not_found"}` })
	err := liveLoad("Nope", CatInstruments)
	if err == nil || !strings.Contains(err.Error(), "no matching item") {
		t.Fatalf("err = %v", err)
	}
	if err := liveSampleLoad("nope.wav"); err == nil {
		t.Fatal("a failed sample load must be an error")
	}
}

func TestLoadSampleAndPlugin(t *testing.T) {
	got := fakeBridge(t, okData(`{"loaded":"x"}`))
	if err := liveSampleLoad("kick.wav"); err != nil {
		t.Fatal(err)
	}
	if req := <-got; req["cmd"] != "load_sample" || req["name"] != "kick.wav" {
		t.Errorf("sample sent %v", req)
	}
	// Colons are fine now: the JSON protocol has no separator to confuse.
	if err := livePluginLoad("Vendor: Synth", "A:B"); err != nil {
		t.Fatal(err)
	}
	if req := <-got; req["cmd"] != "load_plugin" || req["plugin"] != "Vendor: Synth" || req["preset"] != "A:B" {
		t.Errorf("plugin sent %v", req)
	}
	if err := livePluginLoad("Synth", ""); err != nil {
		t.Fatal(err)
	}
	if req := <-got; req["preset"] != nil {
		t.Errorf("no preset: %v", req)
	}
}

func TestLivePlugins(t *testing.T) {
	fakeBridge(t, okData(`{"plugins":[{"vendor":"Acme","name":"Synth","presets":["Warm"]}]}`))
	ps, err := livePlugins()
	if err != nil || len(ps) != 1 || ps[0].Name != "Synth" || ps[0].Presets[0] != "Warm" {
		t.Fatalf("%+v %v", ps, err)
	}
}

func TestLiveScale(t *testing.T) {
	fakeBridge(t, okData(`{"supported":true,"root":9,"name":"Minor","intervals":[0,2,3,5,7,8,10],"mode":true}`))
	sc, err := liveScale()
	if err != nil || !sc.Supported || sc.Root != 9 || sc.Name != "Minor" {
		t.Fatalf("%+v %v", sc, err)
	}
	fakeBridge(t, okData(`{"supported":true,"root":40,"name":"x","intervals":[0]}`))
	if _, err := liveScale(); err == nil {
		t.Error("an implausible root must be refused")
	}
}

func TestPlayStopAndHello(t *testing.T) {
	got := fakeBridge(t, func(req map[string]any) string {
		if req["cmd"] == "hello" {
			return `{"ok":true,"id":1,"data":{"protocol":1,"script_version":"0.2.0"}}`
		}
		return `{"ok":true,"id":1}`
	})
	if err := livePlay(); err != nil {
		t.Fatal(err)
	}
	if err := liveStop(); err != nil {
		t.Fatal(err)
	}
	if r1, r2 := <-got, <-got; r1["cmd"] != "play" || r2["cmd"] != "stop" || r1["ns"] != "transport" {
		t.Errorf("sent %v %v", r1, r2)
	}
	v, p, err := bridgeHello()
	if err != nil || v != "0.2.0" || p != 1 {
		t.Fatalf("hello %q %d %v", v, p, err)
	}
	if req := <-got; req["ns"] != nil || req["cmd"] != "hello" {
		t.Errorf("hello must have no ns: %v", req)
	}
	if !liveBridgeAlive() {
		t.Error("alive")
	}
}

func TestOldBrowserScriptIsRecognised(t *testing.T) {
	for _, reply := range []string{"OK", "ERROR", "pong", "120.0000"} {
		fakeBridge(t, func(map[string]any) string { return reply })
		if _, err := liveTempo(); err == nil || !strings.Contains(err.Error(), "install PushHack, or update it") {
			t.Errorf("reply %q: err = %v", reply, err)
		}
		if liveBridgeAlive() {
			t.Errorf("reply %q: an old script is not PushHack", reply)
		}
	}
}

func TestUnknownCommandMeansOldPushHack(t *testing.T) {
	fakeBridge(t, func(map[string]any) string { return `{"ok":false,"error":"unknown_ns"}` })
	_, err := liveScale()
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("err = %v", err)
	}
}

func TestNothingListening(t *testing.T) {
	old := liveBridgeAddr
	liveBridgeAddr = "127.0.0.1:1" // nothing there
	defer func() { liveBridgeAddr = old }()
	if _, err := liveTempo(); err == nil {
		t.Error("no script must be an error")
	}
	if liveBridgeAlive() {
		t.Error("alive with nothing listening")
	}
}
