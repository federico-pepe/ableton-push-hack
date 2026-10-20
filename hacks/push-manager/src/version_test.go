package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestStatusReportsHackVersion(t *testing.T) {
	old := buildVersion
	buildVersion = "9.9.9-test"
	defer func() { buildVersion = old }()

	w := httptest.NewRecorder()
	handleStatus(w, httptest.NewRequest("GET", "/api/status", nil))

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["hack_version"] != "9.9.9-test" {
		t.Fatalf("hack_version = %v, want 9.9.9-test", got["hack_version"])
	}
}
