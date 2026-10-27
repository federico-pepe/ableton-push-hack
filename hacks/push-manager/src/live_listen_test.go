package main

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A fake Remote Script on the bridge's port: records the command, sends reply.
func fakeBridge(t *testing.T, reply string) (got chan string, stop func()) {
	ln, err := net.Listen("tcp", liveBridgeAddr)
	if err != nil {
		t.Skipf("bridge port busy: %v", err)
	}
	got = make(chan string, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 512)
			n, _ := c.Read(buf)
			got <- string(buf[:n])
			c.Write([]byte(reply + "\n"))
			c.Close()
		}
	}()
	return got, func() { ln.Close() }
}

func TestLiveListenRelaysTheBridge(t *testing.T) {
	got, stop := fakeBridge(t, `{"ok":true,"on":true,"track":true,"input":"1-Piano","channel":"Post FX","output":"Stacks Listen","monitoring":0,"source":"Piano"}`)
	defer stop()

	w := httptest.NewRecorder()
	handleLiveListen(w, httptest.NewRequest(http.MethodPost, "/api/live/listen", strings.NewReader(`{"on":true,"port":"Stacks Listen"}`)))
	if cmd := <-got; cmd != "listen_on:Stacks Listen" {
		t.Errorf("sent %q", cmd)
	}
	var st map[string]any
	json.NewDecoder(bufio.NewReader(w.Body)).Decode(&st)
	if st["input"] != "1-Piano" || st["on"] != true {
		t.Errorf("reply %v", st)
	}

	w = httptest.NewRecorder()
	handleLiveListen(w, httptest.NewRequest(http.MethodPost, "/api/live/listen", strings.NewReader(`{"on":false}`)))
	if cmd := <-got; cmd != "listen_off" {
		t.Errorf("off sent %q", cmd)
	}
	w = httptest.NewRecorder()
	handleLiveListen(w, httptest.NewRequest(http.MethodGet, "/api/live/listen", nil))
	if cmd := <-got; cmd != "get_listen" {
		t.Errorf("GET sent %q", cmd)
	}
	w = httptest.NewRecorder()
	handleLiveListen(w, httptest.NewRequest(http.MethodPost, "/api/live/listen", strings.NewReader(`{"on":true,"port":"a\nb"}`)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a port with a newline: %d, want 400", w.Code)
	}
}

func TestLiveListenOlderBridge(t *testing.T) {
	_, stop := fakeBridge(t, "ERROR")
	defer stop()
	w := httptest.NewRecorder()
	handleLiveListen(w, httptest.NewRequest(http.MethodGet, "/api/live/listen", nil))
	if !strings.Contains(w.Body.String(), "install PushHack, or update it") {
		t.Errorf("older script: %s", w.Body.String())
	}
}
