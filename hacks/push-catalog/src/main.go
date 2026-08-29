// push-catalog daemon — the web face of Push Hack Catalog.
//
// It does almost nothing itself: serves one page and shells out to the embedded
// push-catalog.sh for every action, so the install logic has exactly one home.
// Runs as root under init.d (installing hacks needs it); when root, the script's
// as_root calls are no-ops.
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

//go:embed push-catalog.sh
var storeScript string

//go:embed index.html
var indexHTML []byte

// Hack ids flow from HTTP straight into a shell argument — validate hard.
var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type config struct {
	Port     int `json:"port"`
	Settings struct {
		Registry string `json:"registry"`
	} `json:"settings"`
}

// runStore pipes the embedded script into bash: `bash -s -- <args>`.
func runStore(registry string, args ...string) (string, error) {
	cmd := exec.Command("bash", append([]string{"-s", "--"}, args...)...)
	cmd.Stdin = strings.NewReader(storeScript)
	cmd.Env = os.Environ()
	if registry != "" {
		cmd.Env = append(cmd.Env, "PUSH_CATALOG_REGISTRY="+registry)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func main() {
	cfgPath := flag.String("config", "hack.json", "path to hack.json")
	addr := flag.String("addr", "", "override listen address (default :<port from config>)")
	flag.Parse()

	cfg := config{Port: 7702}
	if b, err := os.ReadFile(*cfgPath); err != nil {
		log.Printf("no config at %s, using defaults", *cfgPath)
	} else if len(strings.TrimSpace(string(b))) == 0 {
		log.Printf("empty config %s, using defaults", *cfgPath)
	} else if err := json.Unmarshal(b, &cfg); err != nil {
		log.Fatalf("bad %s: %v", *cfgPath, err)
	}
	registry := cfg.Settings.Registry

	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	// catalog: proxy the script's JSON straight through.
	mux.HandleFunc("/api/catalog", func(w http.ResponseWriter, r *http.Request) {
		out, err := runStore(registry, "catalog")
		if err != nil {
			http.Error(w, out, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(out))
	})

	// installed: one id per line -> JSON array.
	mux.HandleFunc("/api/installed", func(w http.ResponseWriter, r *http.Request) {
		out, _ := runStore(registry, "installed")
		ids := []string{}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				ids = append(ids, line)
			}
		}
		writeJSON(w, ids)
	})

	// install / remove: POST, id validated, output returned for the log pane.
	action := func(verb string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			id := r.URL.Query().Get("id")
			if !idRe.MatchString(id) {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}
			out, err := runStore(registry, verb, id)
			writeJSON(w, map[string]any{"ok": err == nil, "output": out})
		}
	}
	mux.HandleFunc("/api/install", action("install"))
	mux.HandleFunc("/api/remove", action("remove"))

	listen := *addr
	if listen == "" {
		listen = ":" + strconv.Itoa(cfg.Port)
	}
	log.Printf("push-catalog listening on %s (registry: %s)", listen, registry)
	log.Fatal(http.ListenAndServe(listen, mux))
}
