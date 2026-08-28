#!/usr/bin/env bash
# push-store — install & manage push-hack hacks from the Push itself.
#
#   push-store list                 # show catalog
#   push-store install <id>         # fetch release, extract, register, start
#   push-store remove  <id>         # stop, disable, delete
#   push-store installed            # what's installed now
#   push-store --self-test          # offline checks (runs anywhere)
#
# Mirrors the framework's install.sh, but runs ON the Push. Needs root for the
# service bits (init.d, rc symlinks) — re-exec via sudo if not already root.
#
# Trust model: no sha256 pin, no signing. Each catalog entry points at an
# owner's own GitHub repo (github_repo); the store fetches that repo's
# release.json live for the current version + download_url, same trust
# boundary as `go get` or a Homebrew tap. See catalogue/ARCHITECTURE.md.
set -euo pipefail

# Registry URL has ONE source of truth: hack.json's settings.registry (the
# daemon passes it here as PUSH_STORE_REGISTRY). No URL is baked into this
# script. Standalone CLI use: export PUSH_STORE_REGISTRY yourself.
REGISTRY_URL="${PUSH_STORE_REGISTRY:-}"
PUSH_HACK_DIR="${PUSH_HACK_DIR:-/data/push-hack}"

# ── tiny helpers ──────────────────────────────────────────────────────────────
die() { echo "error: $*" >&2; exit 1; }
info() { echo ">> $*"; }

# One JSON reader, python3-only (single code path — the framework's installer
# pointedly avoids jq, so it may not be on the Push; python3 is the safer bet).
#   q <file> schema                 -> catalog_version number
#   q <file> list                   -> "id\tname — desc" per hack
#   q <file> has <id>               -> exit 0/1
#   q <file> field <id> <dotpath>   -> scalar, or JSON for dict/list; [i] indexes
#   q <file> len <id> <dotpath>     -> list length
q() {
  command -v python3 >/dev/null 2>&1 || die "python3 required to read the catalog"
  python3 - "$@" <<'PY'
import json,sys,re
f,op,*a=sys.argv[1:]
D=json.load(open(f))
def entry(i): return next((h for h in D["hacks"] if h["id"]==i),None)
def walk(v,path):
    for seg in [s for s in path.split(".") if s]:
        m=re.fullmatch(r"(\w+)?\[(\d+)\]",seg)
        if m:
            if m.group(1): v=v[m.group(1)]
            v=v[int(m.group(2))]
        else: v=v[seg]
    return v
if op=="schema": print(D.get("catalog_version"))
elif op=="list":
    for h in D["hacks"]: print(f'{h["id"]}\t{h["name"]} — {h["description"]}')
elif op=="catalog":
    print(json.dumps([{k:h.get(k) for k in ("id","name","description","author","homepage","requires")} for h in D["hacks"]]))
elif op=="has": sys.exit(0 if entry(a[0]) else 1)
elif op=="field":
    e=entry(a[0]); sys.exit(1) if e is None else None
    v=walk(e,a[1]); print(json.dumps(v) if isinstance(v,(dict,list,bool)) else v)
elif op=="len": e=entry(a[0]); print(len(walk(e,a[1])))
else: sys.exit(2)
PY
}

# Reader for a bare {version, download_url} document (a hack's release.json) —
# no "hacks" array, no entry lookup, just top-level fields.
#   rq <file> field <key>   -> scalar
rq() {
  command -v python3 >/dev/null 2>&1 || die "python3 required to read release.json"
  python3 - "$@" <<'PY'
import json,sys
f,op,key=sys.argv[1:]
D=json.load(open(f))
if op=="field": print(D.get(key,""))
else: sys.exit(2)
PY
}

# Fetch via python3 urllib — the Push has no curl, and this one path handles
# http/https AND file:// (used by --self-test), so no per-scheme branching.
fetch() {
  python3 - "$1" "$2" <<'PY' || die "download failed: $1"
import sys,urllib.request,shutil
with urllib.request.urlopen(sys.argv[1], timeout=60) as r, open(sys.argv[2],"wb") as f:
    shutil.copyfileobj(r, f)
PY
}

# Fetch a hack's release.json (from its own repo, always live — never
# cached/pinned in the catalog) and print "<version>\t<download_url>".
# Third arg is an optional override for the release.json URL itself (used by
# entries that aren't backed by a github_repo yet, and by --self-test's
# offline fixture) — real catalog entries omit it and get the constructed
# raw.githubusercontent.com URL.
fetch_release() {
  local github_repo="$1" branch="$2" override="${3:-}"
  local url="${override:-https://raw.githubusercontent.com/${github_repo}/${branch}/release.json}"
  local tmp; tmp="$(mktemp)"
  fetch "$url" "$tmp"
  local version url_out
  version="$(rq "$tmp" field version)"
  url_out="$(rq "$tmp" field download_url)"
  rm -f "$tmp"
  [ -n "$version" ] && [ -n "$url_out" ] || die "invalid release.json at $url"
  printf '%s\t%s\n' "$version" "$url_out"
}

# Push has NO sudo — privileged runs use the separate `root@push.local` login,
# and the daemon already runs as root via init.d. Fall back to sudo only for
# off-device dev machines that have it.
as_root() {
  if [ "$(id -u)" = 0 ]; then "$@"
  elif command -v sudo >/dev/null 2>&1; then sudo "$@"
  else die "must run as root (ssh root@push.local) — Push has no sudo"; fi
}

# ── registry access ───────────────────────────────────────────────────────────
load_registry() { # -> path to a temp copy of index.json
  [ -n "$REGISTRY_URL" ] || die "no registry configured — set settings.registry in hack.json (or export PUSH_STORE_REGISTRY)"
  local f; f="$(mktemp)"
  fetch "$REGISTRY_URL" "$f"   # urllib handles http(s):// and file://
  [ "$(q "$f" schema)" = "2" ] || die "unsupported catalog_version"
  echo "$f"
}

# ── commands ──────────────────────────────────────────────────────────────────
cmd_list() {
  local reg; reg="$(load_registry)"
  q "$reg" list
  rm -f "$reg"
}

cmd_catalog() { # machine-readable catalog for the web/screen UI
  local reg; reg="$(load_registry)"; q "$reg" catalog; rm -f "$reg"
}

cmd_installed() {
  [ -d "$PUSH_HACK_DIR/hacks" ] || return 0
  ls -1 "$PUSH_HACK_DIR/hacks" 2>/dev/null || true
}

cmd_install() {
  local id="$1"; [ -n "$id" ] || die "usage: push-store install <id>"
  local reg; reg="$(load_registry)"
  q "$reg" has "$id" || die "no such hack: $id"

  local github_repo branch override
  github_repo="$(q "$reg" field "$id" 'github_repo' 2>/dev/null || echo "")"
  branch="$(q "$reg" field "$id" 'default_branch' 2>/dev/null || echo "main")"
  override="$(q "$reg" field "$id" 'release_url' 2>/dev/null || echo "")"
  [ -n "$github_repo" ] || [ -n "$override" ] || die "catalog entry '$id' has no github_repo or release_url"

  info "checking release for $id"
  local version dl_url
  IFS=$'\t' read -r version dl_url < <(fetch_release "$github_repo" "$branch" "$override")

  local hacks_dir="$PUSH_HACK_DIR/hacks" dir="$PUSH_HACK_DIR/hacks/$id"
  as_root mkdir -p "$hacks_dir" "$PUSH_HACK_DIR/logs"

  info "fetching $id v$version"
  local tmp; tmp="$(mktemp)"
  fetch "$dl_url" "$tmp"
  # tarball's own top-level "<id>/" entry lands correctly under hacks_dir
  as_root tar -xzf "$tmp" -C "$hacks_dir"
  rm -f "$tmp"
  [ -f "$dir/hack.json" ] || die "tarball for $id did not contain hack.json"

  install_service "$id" "$dir"
  rm -f "$reg"
  info "installed $id v$version"
}

# Generate + enable + start an init.d service — same shell-backgrounding
# pattern as the framework's own generate_initd_script() (lib/common.sh),
# not start-stop-daemon: on this device's busybox, `start-stop-daemon -b`
# silently drops the invoking shell's stdout/stderr redirection once it
# detaches, so a store-installed hack would run correctly but write nothing
# to its log file — found by an on-device install, not by reading the code.
# Reads the hack's own binary name from the hack.json the tarball just
# extracted — the catalog no longer carries hack metadata.
install_service() {
  local id="$1" dir="$2"
  local bin; bin="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('binary',''))" "$dir/hack.json" 2>/dev/null || echo "")"
  [ -n "$bin" ] || { info "no binary — nothing to run"; return 0; }
  local svc="push-hack-$id" log="$PUSH_HACK_DIR/logs/$id.log"
  local script; script="$(mktemp)"
  cat > "$script" <<EOF
#!/bin/sh
### BEGIN INIT INFO
# Provides:          $svc
# Required-Start:    \$local_fs \$network
# Required-Stop:     \$local_fs
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: push-hack: $id (via push-store)
### END INIT INFO
BIN="$dir/$bin"; CFG="$dir/hack.json"; LOG="$log"; PIDF="/var/run/$svc.pid"
start() { echo "starting $svc"; mkdir -p "\$(dirname "\$LOG")"
  nice -n 19 "\$BIN" -config "\$CFG" >>"\$LOG" 2>&1 &
  echo \$! >"\$PIDF"; }
stop()  { echo "stopping $svc"; [ -f "\$PIDF" ] && kill "\$(cat "\$PIDF")" 2>/dev/null; rm -f "\$PIDF"; }
case "\$1" in start) start;; stop) stop;; restart) stop; sleep 1; start;;
  status) [ -f "\$PIDF" ] && kill -0 "\$(cat "\$PIDF")" 2>/dev/null && echo active || echo inactive;;
  *) echo "usage: \$0 {start|stop|restart|status}"; exit 1;; esac
EOF
  as_root cp "$script" "/etc/init.d/$svc"; rm -f "$script"
  as_root chmod +x "/etc/init.d/$svc"
  if command -v update-rc.d >/dev/null 2>&1; then as_root update-rc.d "$svc" defaults >/dev/null 2>&1 || true
  else for r in 2 3 4 5; do as_root ln -sf "/etc/init.d/$svc" "/etc/rc$r.d/S99$svc" 2>/dev/null || true; done; fi
  as_root "/etc/init.d/$svc" restart || true
}

cmd_remove() {
  local id="$1"; [ -n "$id" ] || die "usage: push-store remove <id>"
  local svc="push-hack-$id"
  as_root "/etc/init.d/$svc" stop 2>/dev/null || true
  command -v update-rc.d >/dev/null 2>&1 && as_root update-rc.d -f "$svc" remove >/dev/null 2>&1 || true
  as_root rm -f "/etc/init.d/$svc" /etc/rc*.d/S99"$svc"
  as_root rm -rf "$PUSH_HACK_DIR/hacks/$id"
  info "removed $id"
}

# ── offline self-test: catalog parsing + the fetch-release/extract path ───────
self_test() {
  local here; here="$(cd "$(dirname "$0")" && pwd)"
  # Real production catalog — one directory too shallow was the old bug here
  # (registry/ was a level *inside* hacks/push-store/, not the repo root).
  local fixture="$here/../../catalogue/catalog.json"
  [ -f "$fixture" ] || die "self-test: fixture not found ($fixture)"
  PUSH_STORE_REGISTRY="file://$fixture"; REGISTRY_URL="$PUSH_STORE_REGISTRY"
  local reg; reg="$(load_registry)"

  # 1. catalog lists the seeded hack
  cmd_list | grep -q keyboard-visualizer || die "self-test: list missing keyboard-visualizer"

  # 2. entry lookup resolves flat fields
  [ "$(q "$reg" field keyboard-visualizer 'default_branch')" = "main" ] \
    || die "self-test: field lookup broke"
  q "$reg" has keyboard-visualizer || die "self-test: has lookup broke"
  q "$reg" has nope-not-real && die "self-test: has false-positive" || true

  # 3. fetch_release: resolve a release.json (built in-memory, pointing at
  #    the checked-in fixture tarball) via the same file:// override path
  #    catalog entries can use for local/dev testing.
  local tarball="$here/testdata/fixture-hack.tar.gz"
  [ -f "$tarball" ] || die "self-test: fixture tarball not found ($tarball)"
  local rel; rel="$(mktemp)"
  printf '{"version":"0.1.0","download_url":"file://%s"}\n' "$tarball" > "$rel"
  local version dl_url
  IFS=$'\t' read -r version dl_url < <(fetch_release "" "" "file://$rel")
  rm -f "$rel"
  [ "$version" = "0.1.0" ] || die "self-test: fetch_release version wrong ($version)"
  [ "$dl_url" = "file://$tarball" ] || die "self-test: fetch_release download_url wrong"

  # 4. extraction: same `tar -xzf ... -C hacks_dir` cmd_install uses, into a
  #    scratch dir (no as_root/root/service registration — this only proves
  #    the tarball layout the store expects actually unpacks and parses).
  local extract_dir; extract_dir="$(mktemp -d)"
  tar -xzf "$tarball" -C "$extract_dir"
  [ -f "$extract_dir/fixture-hack/hack.json" ] || die "self-test: extracted tarball missing hack.json"
  [ -x "$extract_dir/fixture-hack/fixture-hack" ] || die "self-test: extracted binary lost its exec bit"
  local bin; bin="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('binary',''))" "$extract_dir/fixture-hack/hack.json")"
  [ "$bin" = "fixture-hack" ] || die "self-test: hack.json binary field misread ($bin)"
  rm -rf "$extract_dir"

  rm -f "$reg"
  echo "self-test: OK"
}

# ── dispatch ──────────────────────────────────────────────────────────────────
# Skip when sourced for testing: `PUSH_STORE_LIB=1 source push-store.sh`
[ -n "${PUSH_STORE_LIB:-}" ] && return 0 2>/dev/null || true
case "${1:---help}" in
  list)       cmd_list ;;
  catalog)    cmd_catalog ;;
  install)    cmd_install "${2:-}" ;;
  remove)     cmd_remove  "${2:-}" ;;
  installed)  cmd_installed ;;
  --self-test) self_test ;;
  *) sed -n '2,10p' "$0" ;;
esac
