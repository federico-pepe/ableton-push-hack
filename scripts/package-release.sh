#!/usr/bin/env bash
# scripts/package-release.sh -- build the Push Hack release files into dist/.
#
# Makes, from VERSION and the Makefiles:
#   dist/push-hack-<version>.tar.gz   push-manager, push-catalog, push_hook.so, the PushHack
#                                     Remote Script (remote-script/PushHack/), manifest.json
#   dist/manifest.json                the manifest plus the tarball's name, size and
#                                     SHA-256, readable without the tarball
#
# push_hook.so is taken from the commit as it is: it builds through Docker
# and is not rebuilt here. Nothing is signed; the SHA-256 only catches a
# broken download. Trust is HTTPS and the GitHub repo, as for the catalog.
#
# Usage: scripts/package-release.sh [expected-tag]
#   expected-tag  e.g. v0.1.10-alpha. The script stops if it differs from VERSION.

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# PUSH_HACK_VERSION overrides VERSION for a bench build only (never for a release).
VERSION="${PUSH_HACK_VERSION:-$(tr -d '[:space:]' < VERSION)}"
export PUSH_HACK_VERSION="$VERSION"
if [[ -n "${1:-}" && "$1" != "v${VERSION}" ]]; then
    echo "error: tag $1 does not match VERSION $VERSION" >&2
    exit 1
fi

sha256() { if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }

make -C hacks/push-manager
make -C hacks/push-catalog
HOOK=hacks/push-display/push_hook.so
[[ -f "$HOOK" ]] || { echo "error: $HOOK missing" >&2; exit 1; }

NAME="push-hack-${VERSION}"
rm -rf dist
mkdir -p "dist/${NAME}/push-manager" "dist/${NAME}/push-catalog" "dist/${NAME}/push-display"
cp hacks/push-manager/build/push-manager "dist/${NAME}/push-manager/push-manager"
cp hacks/push-catalog/build/push-catalog "dist/${NAME}/push-catalog/push-catalog"
cp "$HOOK" "dist/${NAME}/push-display/push_hook.so"
chmod +x "dist/${NAME}/push-manager/push-manager" "dist/${NAME}/push-catalog/push-catalog"

# The PushHack Remote Script, without Python caches and Mac metadata. The
# on-device update swaps this whole folder.
mkdir -p "dist/${NAME}/remote-script"
(cd remote-script && tar -cf - --exclude='__pycache__' --exclude='*.pyc' --exclude='.DS_Store' --exclude='._*' PushHack) | tar -xf - -C "dist/${NAME}/remote-script"
[[ -f "dist/${NAME}/remote-script/PushHack/__init__.py" ]] || { echo "error: remote-script/PushHack/__init__.py missing" >&2; exit 1; }
SCRIPT_VERSION="$(sed -n 's/^SCRIPT_VERSION = "\(.*\)"/\1/p' remote-script/PushHack/protocol.py)"
[[ -n "$SCRIPT_VERSION" ]] || { echo "error: SCRIPT_VERSION not found in protocol.py" >&2; exit 1; }

# The hook's shared-memory version. push-manager compares it with the hook
# already loaded in Push3 and refuses an update that would not match.
SHM_PROTOCOL="$(sed -n 's/^#define SHM_VERSION \([0-9]*\)u.*/\1/p' hacks/push-display/src/push_hook.c)"
[[ -n "$SHM_PROTOCOL" ]] || { echo "error: SHM_VERSION not found in push_hook.c" >&2; exit 1; }

# One line per file: "<sha256>  <path>". Python turns them into JSON.
(cd "dist/${NAME}" && sha256 push-manager/push-manager push-catalog/push-catalog push-display/push_hook.so \
    $(find remote-script -type f | LC_ALL=C sort)) > dist/files.txt
python3 - "$VERSION" "$SHM_PROTOCOL" "dist/${NAME}" "$SCRIPT_VERSION" <<'PY'
import json, os, sys, datetime
version, shm, root, script = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4]
meta = json.load(open("releasemeta.json"))
files = []
for line in open("dist/files.txt"):
    digest, path = line.split(None, 1)
    path = path.strip()
    files.append({"path": path, "sha256": digest, "size": os.path.getsize(os.path.join(root, path))})
manifest = {
    "version": version,
    "built_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "shm_protocol": shm,
    "needs_reinstall": bool(meta.get("needs_reinstall", False)),
    "remote_script_version": script,
    "files": files,
}
json.dump(manifest, open(os.path.join(root, "manifest.json"), "w"), indent=2)
PY
rm dist/files.txt
cp "dist/${NAME}/manifest.json" dist/manifest.json

COPYFILE_DISABLE=1 tar -czf "dist/${NAME}.tar.gz" -C dist "${NAME}"
rm -rf "dist/${NAME}"

# The manifest in the tarball cannot hold the tarball's own hash, so the loose
# copy gets it added.
TAR_SHA="$(cd dist && sha256 "${NAME}.tar.gz" | cut -d' ' -f1)"
python3 - "${NAME}.tar.gz" "$TAR_SHA" <<'PY'
import json, os, sys
name, digest = sys.argv[1], sys.argv[2]
m = json.load(open("dist/manifest.json"))
m["tarball"] = {"name": name, "sha256": digest, "size": os.path.getsize(os.path.join("dist", name))}
json.dump(m, open("dist/manifest.json", "w"), indent=2)
PY

echo "Built:"
ls -l dist
