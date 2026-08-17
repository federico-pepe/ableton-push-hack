package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Pins the "allowMethods is per-hack" contract: push-manager, automation
// and keyboard-visualizer each pass their own Allow-Methods string, and a
// future edit collapsing WithCORS to a fixed string would silently change
// all three hacks' CORS behavior.
func TestWithCORSAllowMethodsIsPerCall(t *testing.T) {
	cases := []string{
		"GET, POST, DELETE, OPTIONS",
		"GET, POST, PUT, DELETE, OPTIONS",
		"GET, POST, OPTIONS",
	}
	for _, methods := range cases {
		h := WithCORS(methods, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		h.ServeHTTP(rr, req)
		if got := rr.Header().Get("Access-Control-Allow-Methods"); got != methods {
			t.Errorf("Allow-Methods = %q, want %q", got, methods)
		}
	}
}

func TestWithCORSOptionsShortCircuits(t *testing.T) {
	called := false
	h := WithCORS("GET, OPTIONS", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	h.ServeHTTP(rr, req)
	if called {
		t.Error("next handler was called for an OPTIONS request")
	}
	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}
