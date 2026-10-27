package main

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeBridge is a fake Remote Script on a port of its own. reply gets each
// request and returns the raw line to send back. The requests are recorded.
func fakeBridge(t *testing.T, reply func(req map[string]any) string) (got chan map[string]any) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	old := liveBridgeAddr
	liveBridgeAddr = ln.Addr().String()
	t.Cleanup(func() { ln.Close(); liveBridgeAddr = old })
	got = make(chan map[string]any, 16)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					return
				}
				var req map[string]any
				if json.Unmarshal(line, &req) != nil {
					return
				}
				got <- req
				c.Write([]byte(reply(req) + "\n"))
			}()
		}
	}()
	return got
}

func okData(data string) func(map[string]any) string {
	return func(map[string]any) string { return `{"ok":true,"id":1,"data":` + data + `}` }
}

func TestLiveListenRelaysTheBridge(t *testing.T) {
	got := fakeBridge(t, okData(`{"ok":true,"on":true,"track":true,"input":"1-Piano","channel":"Post FX","output":"Stacks Listen","monitoring":0,"source":"Piano"}`))

	w := httptest.NewRecorder()
	handleLiveListen(w, httptest.NewRequest(http.MethodPost, "/api/live/listen", strings.NewReader(`{"on":true,"port":"Stacks Listen"}`)))
	req := <-got
	if req["ns"] != "listen" || req["cmd"] != "on" || req["port"] != "Stacks Listen" {
		t.Errorf("sent %v", req)
	}
	var st map[string]any
	json.NewDecoder(w.Body).Decode(&st)
	if st["input"] != "1-Piano" || st["on"] != true {
		t.Errorf("reply %v", st)
	}

	w = httptest.NewRecorder()
	handleLiveListen(w, httptest.NewRequest(http.MethodPost, "/api/live/listen", strings.NewReader(`{"on":false}`)))
	if req := <-got; req["ns"] != "listen" || req["cmd"] != "off" {
		t.Errorf("off sent %v", req)
	}
	w = httptest.NewRecorder()
	handleLiveListen(w, httptest.NewRequest(http.MethodGet, "/api/live/listen", nil))
	if req := <-got; req["ns"] != "listen" || req["cmd"] != "get" {
		t.Errorf("GET sent %v", req)
	}
	w = httptest.NewRecorder()
	handleLiveListen(w, httptest.NewRequest(http.MethodPost, "/api/live/listen", strings.NewReader(`{"on":true,"port":"a\nb"}`)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a port with a newline: %d, want 400", w.Code)
	}
}

func TestLiveListenOlderBridge(t *testing.T) {
	fakeBridge(t, func(map[string]any) string { return "ERROR" })
	w := httptest.NewRecorder()
	handleLiveListen(w, httptest.NewRequest(http.MethodGet, "/api/live/listen", nil))
	if !strings.Contains(w.Body.String(), "install PushHack, or update it") {
		t.Errorf("older script: %s", w.Body.String())
	}
}
