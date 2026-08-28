#!/usr/bin/env bash
# push-store — install & manage push-hack hacks from the Push itself.
#
#   push-store list                 # show catalog
#   push-store install <id>         # download, verify, register, start
#   push-store remove  <id>         # stop, disable, delete
#   push-store installed            # what's installed now
#   push-store --self-test          # offline checks (runs anywhere)
#
# Mirrors the framework's install.sh, but runs ON the Push. Needs root for the
# service bits (init.d, rc symlinks) — re-exec via sudo if not already root.
#
# ponytail: curated-trust model — assets are sha256-pinned, not signed. Fine for
# a reviewed catalog; add minisign before opening public taps (see PLAN Phase 3).
set -euo pipefail

REGISTRY_URL="${PUSH_STORE_REGISTRY:-https://raw.githubusercontent.com/YOUR_USER/push-homebrew/main/registry/index.json}"
PUSH_HACK_DIR="${PUSH_HACK_DIR:-/data/push-hack}"

# ── tiny helpers ──────────────────────────────────────────────────────────────
die() { echo "error: $*" >&2; exit 1; }
info() { echo ">> $*"; }

# One JSON reader, python3-only (single code path — the framework's installer
# pointedly avoids jq, so it may not be on the Push; python3 is the safer bet).
#   q <file> schema                 -> schema number
#   q <file> list                   -> "id\tversion\tname — desc" per hack
#   q <file> has <id>               -> exit 0/1
#   q <file> field <id> <dotpath>   -> scalar, or JSON for dict/list; [i] indexes
#   q <file> len <id> <dotpath>     -> list length
q() {
  command -v python3 >/dev/null 2>&1 || die "python3 required to read the registry"
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
if op=="schema": print(D.get("schema"))
elif op=="list":
    for h in D["hacks"]: print(f'{h["id"]}\t{h["version"]}\t{h["name"]} — {h["description"]}')
elif op=="catalog":
    print(json.dumps([{k:h.get(k) for k in ("id","name","version","description","author","requires")} for h in D["hacks"]]))
elif op=="has": sys.exit(0 if entry(a[0]) else 1)
elif op=="field":
    e=entry(a[0]); sys.exit(1) if e is None else None
    v=walk(e,a[1]); print(json.dumps(v) if isinstance(v,(dict,list,bool)) else v)
elif op=="len": e=entry(a[0]); print(len(walk(e,a[1])))
else: sys.exit(2)
PY
}

sha256() { # portable: Linux sha256sum, macOS shasum
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1; fi
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
  local f; f="$(mktemp)"
  fetch "$REGISTRY_URL" "$f"   # urllib handles http(s):// and file://
  [ "$(q "$f" schema)" = "1" ] || die "unsupported registry schema"
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

  local dir="$PUSH_HACK_DIR/hacks/$id"
  as_root mkdir -p "$dir" "$PUSH_HACK_DIR/logs"

  # write hack.json from the entry's embedded hack object
  local hj; hj="$(mktemp)"; q "$reg" field "$id" 'hack' > "$hj"
  as_root cp "$hj" "$dir/hack.json"; rm -f "$hj"

  # download + verify each asset
  local n i url want got name exec_flag
  n="$(q "$reg" len "$id" 'assets')"
  for ((i=0; i<n; i++)); do
    name="$(q "$reg" field "$id" "assets[$i].name")"
    url="$(q  "$reg" field "$id" "assets[$i].url")"
    want="$(q "$reg" field "$id" "assets[$i].sha256")"
    exec_flag="$(q "$reg" field "$id" "assets[$i].exec" 2>/dev/null || echo false)"
    info "fetching $name"
    local tmp; tmp="$(mktemp)"; fetch "$url" "$tmp"
    got="$(sha256 "$tmp")"
    [ "$got" = "$want" ] || { rm -f "$tmp"; die "sha256 mismatch for $name: got $got want $want"; }
    as_root cp "$tmp" "$dir/$name"; rm -f "$tmp"
    [ "$exec_flag" = "true" ] && as_root chmod +x "$dir/$name"
  done

  install_service "$id" "$dir" "$reg"
  rm -f "$reg"
  info "installed $id"
}

# Generate + enable + start an init.d service (mirrors install.sh's fallback).
install_service() {
  local id="$1" dir="$2" reg="$3"
  local bin; bin="$(q "$reg" field "$id" 'hack.binary' 2>/dev/null || echo "")"
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
start() { echo "starting $svc"; mkdir -p "$(dirname "\$LOG")"
  start-stop-daemon -S -b -m -p "\$PIDF" -x "\$BIN" -- -config "\$CFG" >>"\$LOG" 2>&1 \\
    || { nohup "\$BIN" -config "\$CFG" >>"\$LOG" 2>&1 & echo \$! >"\$PIDF"; }; }
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

# ── offline self-test: parse fixture, verify sha256 logic, entry lookup ───────
self_test() {
  local here; here="$(cd "$(dirname "$0")" && pwd)"
  local fixture="$here/../registry/index.json"
  [ -f "$fixture" ] || die "self-test: fixture not found ($fixture)"
  PUSH_STORE_REGISTRY="file://$fixture"; REGISTRY_URL="$PUSH_STORE_REGISTRY"
  local reg; reg="$(load_registry)"

  # 1. catalog lists the seeded hack
  cmd_list | grep -q keyboard-visualizer || die "self-test: list missing keyboard-visualizer"

  # 2. entry lookup resolves nested fields
  [ "$(q "$reg" field keyboard-visualizer 'hack.port')" = "7702" ] \
    || die "self-test: nested field lookup broke"
  [ "$(q "$reg" len keyboard-visualizer 'assets')" = "1" ] \
    || die "self-test: asset count wrong"
  q "$reg" has keyboard-visualizer || die "self-test: has lookup broke"
  q "$reg" has nope-not-real && die "self-test: has false-positive" || true
  # booleans must render as shell-comparable "true" (else chmod +x never fires)
  [ "$(q "$reg" field keyboard-visualizer 'assets[0].exec')" = "true" ] \
    || die "self-test: bool not rendered as 'true'"

  # 3. sha256 verify: a known vector must match, a wrong one must fail
  local t; t="$(mktemp)"; printf abc > "$t"
  [ "$(sha256 "$t")" = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" ] \
    || die "self-test: sha256 of 'abc' wrong"
  [ "$(sha256 "$t")" != "deadbeef" ] || die "self-test: sha256 false-positive"
  rm -f "$t" "$reg"
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
