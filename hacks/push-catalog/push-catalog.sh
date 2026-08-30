#!/usr/bin/env bash
# push-catalog — install & manage push-hack hacks from the Push itself.
#
#   push-catalog list                 # show catalog
#   push-catalog install <id>         # fetch release, extract, register, start
#   push-catalog remove  <id>         # stop, disable, delete
#   push-catalog installed            # what's installed now
#   push-catalog --self-test          # offline checks (runs anywhere)
#
# Mirrors the framework's install.sh, but runs ON the Push. Needs root for the
# service bits (init.d, rc symlinks) — re-exec via sudo if not already root.
#
# Trust model: no sha256 pin, no signing. Each catalog entry points at an
# owner's own GitHub repo (github_repo); the store fetches that repo's
# release.json live for the current version + download_url, same trust
# boundary as `go get` or a Homebrew tap. See catalog/ARCHITECTURE.md.
set -euo pipefail

# Registry URL has ONE source of truth: hack.json's settings.registry (the
# daemon passes it here as PUSH_CATALOG_REGISTRY). No URL is baked into
# this script. Standalone CLI use: export PUSH_CATALOG_REGISTRY yourself.
REGISTRY_URL="${PUSH_CATALOG_REGISTRY:-}"
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
    # Author/name/description come straight from the catalog entry; version
    # and released_at are always fetched live from the hack's own
    # release.json (never cached in the catalog itself, same as install) —
    # a slow/unreachable hack repo degrades to "?" rather than failing the
    # whole listing. Optional a[0] is hacks_dir: when given, each entry is
    # also enriched with the *installed* copy's own hack.json version, so
    # callers can flag update_available without a second round-trip.
    import urllib.request, os
    hacks_dir=a[0] if a else None
    out=[]
    for h in D["hacks"]:
        e={k:h.get(k) for k in ("id","name","description","author","homepage","requires")}
        rel_url=h.get("release_url") or (
            f"https://raw.githubusercontent.com/{h['github_repo']}/{h.get('default_branch','main')}/release.json"
            if h.get("github_repo") else None)
        version=None; released_at=None
        if rel_url:
            try:
                with urllib.request.urlopen(rel_url, timeout=10) as r:
                    rel=json.load(r)
                version=rel.get("version")
                released_at=rel.get("released_at")
            except Exception:
                pass
        e["version"]=version
        e["released_at"]=released_at
        installed_version=None
        if hacks_dir:
            hp=os.path.join(hacks_dir,h["id"],"hack.json")
            if os.path.isfile(hp):
                try:
                    installed_version=json.load(open(hp)).get("version")
                except Exception:
                    pass
        e["installed_version"]=installed_version
        e["update_available"]=bool(installed_version and version and installed_version!=version)
        out.append(e)
    print(json.dumps(out))
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
  [ -n "$REGISTRY_URL" ] || die "no registry configured — set settings.registry in hack.json (or export PUSH_CATALOG_REGISTRY)"
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
  local reg; reg="$(load_registry)"; q "$reg" catalog "$PUSH_HACK_DIR/hacks"; rm -f "$reg"
}

cmd_installed() {
  [ -d "$PUSH_HACK_DIR/hacks" ] || return 0
  ls -1 "$PUSH_HACK_DIR/hacks" 2>/dev/null || true
}

cmd_install() {
  local id="$1"; [ -n "$id" ] || die "usage: push-catalog install <id>"
  local reg; reg="$(load_registry)"
  install_with_deps "$id" "$reg" " "
  rm -f "$reg"
}

# push-manager/push-display/push-catalog are the framework's own base
# install, never entries in the catalog — a `requires` naming one of them
# just means "the base install", not something this daemon could fetch.
is_core_hack() { case "$1" in push-manager|push-display|push-catalog) return 0 ;; *) return 1 ;; esac; }

# Resolves $1's `requires` (installing any that are themselves catalog
# entries and aren't installed yet, recursively) before installing $1
# itself. $3 is a space-padded " id1 id2 " chain guard — dedupes repeated
# deps in one run and stops a cycle between catalog entries from recursing
# forever.
install_with_deps() {
  local id="$1" reg="$2" chain="$3"
  case "$chain" in *" $id "*) return 0 ;; esac
  chain="${chain}${id} "

  q "$reg" has "$id" || die "no such hack: $id"

  local reqn; reqn="$(q "$reg" len "$id" 'requires' 2>/dev/null || echo 0)"
  if [ "$reqn" -gt 0 ]; then
    local installed_now; installed_now="$(cmd_installed)"
    local i dep
    for ((i=0; i<reqn; i++)); do
      dep="$(q "$reg" field "$id" "requires[$i]")"
      echo "$installed_now" | grep -qx "$dep" && continue
      if q "$reg" has "$dep" 2>/dev/null; then
        info "installing dependency: $dep (required by $id)"
        install_with_deps "$dep" "$reg" "$chain"
      elif is_core_hack "$dep"; then
        info "WARNING: '$id' requires '$dep', part of the base install — deploy it with ./scripts/install.sh if missing"
      else
        info "WARNING: '$id' requires '$dep', which isn't installed and isn't in the catalog — install it separately"
      fi
    done
  fi

  install_one "$id" "$reg"
}

# Fetch + extract + register the single hack $1 (no dependency handling —
# see install_with_deps).
install_one() {
  local id="$1" reg="$2"
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

  # tar run as root restores the *original* uid/gid baked into the archive
  # (e.g. a CI runner's own uid) rather than defaulting to the current user.
  # Re-chown to match hacks_dir's owner (normally ableton:users) — same
  # convention push-manager's own runtime file writes already follow —
  # so `ableton` (uninstall.sh, manual rm; the Push has no sudo) can still
  # manage/delete what a root-run install just extracted.
  local owner; owner="$(stat -c '%u:%g' "$hacks_dir" 2>/dev/null || stat -f '%u:%g' "$hacks_dir" 2>/dev/null || echo "")"
  [ -n "$owner" ] && as_root chown -R "$owner" "$dir"

  install_service "$id" "$dir"
  install_payload "$id" "$dir"
  info "installed $id v$version"

  local post_install
  post_install="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('post_install',''))" "$dir/hack.json" 2>/dev/null || echo "")"
  [ -n "$post_install" ] && info "NEXT: $post_install"
}

# Copy a non-service payload to a hack-declared install_path, e.g. an
# Ableton Live Remote Script that must land in the User Library rather than
# hacks/<id>/. Convention (documented in catalog/schema.md): the tarball's
# remote-script/ directory is copied verbatim to install_path. A hack with
# no install_path is a no-op here — this only exists for the small set of
# hacks that don't fit the binary+service model at all.
install_payload() {
  local id="$1" dir="$2"
  local install_path; install_path="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('install_path',''))" "$dir/hack.json" 2>/dev/null || echo "")"
  [ -n "$install_path" ] || return 0
  [ -d "$dir/remote-script" ] || die "hack '$id' declares install_path but has no remote-script/ payload"
  as_root mkdir -p "$(dirname "$install_path")"
  as_root rm -rf "$install_path"
  as_root cp -r "$dir/remote-script" "$install_path"
  local owner; owner="$(stat -c '%u:%g' "$dir" 2>/dev/null || stat -f '%u:%g' "$dir" 2>/dev/null || echo "")"
  [ -n "$owner" ] && as_root chown -R "$owner" "$install_path"
  info "installed $id payload to $install_path"
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
# Short-Description: push-hack: $id (via push-catalog)
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
  local id="$1"; [ -n "$id" ] || die "usage: push-catalog remove <id>"
  local svc="push-hack-$id" dir="$PUSH_HACK_DIR/hacks/$id"
  as_root "/etc/init.d/$svc" stop 2>/dev/null || true
  command -v update-rc.d >/dev/null 2>&1 && as_root update-rc.d -f "$svc" remove >/dev/null 2>&1 || true
  as_root rm -f "/etc/init.d/$svc" /etc/rc*.d/S99"$svc"
  # Clean up a non-hacks/<id> install_path (e.g. a Remote Script in Live's
  # User Library) before the source hack.json that names it is gone.
  if [ -f "$dir/hack.json" ]; then
    local install_path; install_path="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('install_path',''))" "$dir/hack.json" 2>/dev/null || echo "")"
    [ -n "$install_path" ] && as_root rm -rf "$install_path"
  fi
  as_root rm -rf "$dir"
  info "removed $id"
}

# ── offline self-test: catalog parsing + the fetch-release/extract path ───────
self_test() {
  local here; here="$(cd "$(dirname "$0")" && pwd)"
  # Real production catalog — one directory too shallow was the old bug here
  # (registry/ was a level *inside* hacks/push-catalog/, not the repo root).
  local fixture="$here/../../catalog/catalog.json"
  [ -f "$fixture" ] || die "self-test: fixture not found ($fixture)"
  PUSH_CATALOG_REGISTRY="file://$fixture"; REGISTRY_URL="$PUSH_CATALOG_REGISTRY"
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
  printf '{"version":"0.1.0","download_url":"file://%s","released_at":"2026-01-01T00:00:00Z"}\n' "$tarball" > "$rel"
  local version dl_url
  IFS=$'\t' read -r version dl_url < <(fetch_release "" "" "file://$rel")
  [ "$version" = "0.1.0" ] || die "self-test: fetch_release version wrong ($version)"
  [ "$dl_url" = "file://$tarball" ] || die "self-test: fetch_release download_url wrong"

  # 3b. catalog op: per-hack live-release enrichment (version + released_at),
  #     exercised offline via a release_url override so no network is hit.
  #     Also covers the optional hacks_dir arg: installed_version read from a
  #     local hack.json, update_available flipped since it differs from the
  #     live release version above (0.0.9 installed vs. 0.1.0 released).
  local cat; cat="$(mktemp)"
  printf '{"catalog_version":2,"hacks":[{"id":"fixture-hack","name":"Fixture Hack","description":"d","author":"tester","homepage":"https://example.invalid","release_url":"file://%s","requires":[]}]}\n' "$rel" > "$cat"
  local fake_hacks_dir; fake_hacks_dir="$(mktemp -d)"
  mkdir -p "$fake_hacks_dir/fixture-hack"
  printf '{"id":"fixture-hack","version":"0.0.9"}\n' > "$fake_hacks_dir/fixture-hack/hack.json"
  local catjson
  catjson="$(q "$cat" catalog "$fake_hacks_dir")"
  rm -f "$rel" "$cat"; rm -rf "$fake_hacks_dir"
  echo "$catjson" | grep -q '"author": "tester"' || die "self-test: catalog missing author"
  echo "$catjson" | grep -q '"version": "0.1.0"' || die "self-test: catalog missing live version"
  echo "$catjson" | grep -q '"released_at": "2026-01-01T00:00:00Z"' || die "self-test: catalog missing released_at"
  echo "$catjson" | grep -q '"installed_version": "0.0.9"' || die "self-test: catalog missing installed_version"
  echo "$catjson" | grep -q '"update_available": true' || die "self-test: update_available not flagged"

  # 3c. dependency-resolution building blocks (is_core_hack, requires
  #     parsing). A full install_with_deps run needs a privileged
  #     PUSH_HACK_DIR (init.d, as_root writes) so it's covered by the
  #     hardware check instead — this exercises the pure-logic pieces.
  is_core_hack push-manager || die "self-test: is_core_hack missed a real core hack"
  is_core_hack totally-unrelated-hack && die "self-test: is_core_hack false-positive" || true

  local cat2; cat2="$(mktemp)"
  cat > "$cat2" <<'JSON'
{"catalog_version":2,"hacks":[
  {"id":"leaf","name":"Leaf","description":"d","author":"t","requires":[]},
  {"id":"root","name":"Root","description":"d","author":"t","requires":["leaf","push-manager","not-in-catalog"]},
  {"id":"no-reqs","name":"No Reqs","description":"d","author":"t"}
]}
JSON
  [ "$(q "$cat2" len root 'requires')" = "3" ] || die "self-test: requires length wrong"
  [ "$(q "$cat2" field root 'requires[0]')" = "leaf" ] || die "self-test: requires[0] wrong"
  [ "$(q "$cat2" len leaf 'requires')" = "0" ] || die "self-test: empty requires array wrong"
  # real entries (keyboard-visualizer, automation) omit `requires` entirely
  # when they have none — install_with_deps's `2>/dev/null || echo 0`
  # fallback is what makes that safe; assert the fallback path directly.
  [ "$(q "$cat2" len no-reqs 'requires' 2>/dev/null || echo 0)" = "0" ] \
    || die "self-test: missing requires field mishandled"
  rm -f "$cat2"

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

  # 5. install_path/post_install field reads (same inline python one-liners
  #    install_one/install_payload/cmd_remove use) — no as_root/filesystem
  #    writes here, that needs real root and is covered by a hardware check.
  local fake_dir; fake_dir="$(mktemp -d)"
  cat > "$fake_dir/hack.json" <<'JSON'
{"id":"fixture-remote-script","version":"0.1.0","binary":"","install_path":"/data/Music/Ableton/User Library/Remote Scripts/Fixture","post_install":"Enable Fixture in a control-surface slot and restart Live."}
JSON
  local ip; ip="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('install_path',''))" "$fake_dir/hack.json")"
  [ "$ip" = "/data/Music/Ableton/User Library/Remote Scripts/Fixture" ] || die "self-test: install_path misread"
  local pi; pi="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('post_install',''))" "$fake_dir/hack.json")"
  [ "$pi" = "Enable Fixture in a control-surface slot and restart Live." ] || die "self-test: post_install misread"
  # A hack.json with no install_path/post_install must read back empty, not error.
  printf '{"id":"no-extras","version":"0.1.0","binary":"x"}\n' > "$fake_dir/hack.json"
  ip="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('install_path',''))" "$fake_dir/hack.json")"
  [ -z "$ip" ] || die "self-test: install_path should default empty"
  rm -rf "$fake_dir"

  rm -f "$reg"
  echo "self-test: OK"
}

# ── dispatch ──────────────────────────────────────────────────────────────────
# Skip when sourced for testing: `PUSH_CATALOG_LIB=1 source push-catalog.sh`
[ -n "${PUSH_CATALOG_LIB:-}" ] && return 0 2>/dev/null || true
case "${1:---help}" in
  list)       cmd_list ;;
  catalog)    cmd_catalog ;;
  install)    cmd_install "${2:-}" ;;
  remove)     cmd_remove  "${2:-}" ;;
  installed)  cmd_installed ;;
  --self-test) self_test ;;
  *) sed -n '2,10p' "$0" ;;
esac
