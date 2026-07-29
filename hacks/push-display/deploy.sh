#!/usr/bin/env bash
# hacks/push-display/deploy.sh
# Deploy push_hook.so and inject LD_PRELOAD into /etc/init.d/push3.
#
# Usage:
#   ./deploy.sh           — build + deploy + restart Push3
#   ./deploy.sh --remove  — remove hook + restart Push3 cleanly
#   ./deploy.sh --no-build — skip build step
#
# WARNING: restarts Push3 (and therefore Live). Takes ~5s.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${REPO_ROOT}/lib/common.sh"

REMOTE_DIR="/data/push-hack/hacks/push-display"
HOOK_SO="${SCRIPT_DIR}/push_hook.so"
REMOTE_SO="${REMOTE_DIR}/push_hook.so"
PUSH3_INITD="/etc/init.d/push3"
PUSH3_BACKUP="/etc/init.d/push3.push-hack-bak"
PRELOAD_MARKER="# push-hack: push-display"

REMOVE=false
NO_BUILD=false

for arg in "$@"; do
    case "$arg" in
        --remove)   REMOVE=true ;;
        --no-build) NO_BUILD=true ;;
    esac
done

# ── Remove mode ──────────────────────────────────────────────────────────────
if [[ "${REMOVE}" == true ]]; then
    info "Removing push-display hook..."
    push_exec_root "
        if [ -f '${PUSH3_BACKUP}' ]; then
            cp '${PUSH3_BACKUP}' '${PUSH3_INITD}'
            rm -f '${PUSH3_BACKUP}'
            echo 'Restored /etc/init.d/push3 from backup'
        else
            # Fallback: strip the LD_PRELOAD line if backup missing
            sed -i '/${PRELOAD_MARKER}/d' '${PUSH3_INITD}'
            sed -i '/export LD_PRELOAD.*push_hook/d' '${PUSH3_INITD}'
            echo 'Stripped LD_PRELOAD from /etc/init.d/push3'
        fi
        /etc/init.d/push3 restart
        echo 'Push3 restarted cleanly'
    "
    success "Hook removed"
    exit 0
fi

# ── Build ─────────────────────────────────────────────────────────────────────
if [[ "${NO_BUILD}" == false ]]; then
    info "Building push_hook.so via Docker (linux/amd64)..."
    make -C "${SCRIPT_DIR}"
fi

[[ -f "${HOOK_SO}" ]] || die "push_hook.so not found — run make first"

# ── Deploy .so ────────────────────────────────────────────────────────────────
info "Deploying push_hook.so..."
push_exec_root "mkdir -p '${REMOTE_DIR}'"
push_copy_root "${HOOK_SO}" "${REMOTE_SO}"
push_exec_root "chmod 755 '${REMOTE_SO}'"
success "Deployed: ${REMOTE_SO}"

# ── Patch /etc/init.d/push3 ──────────────────────────────────────────────────
info "Patching /etc/init.d/push3..."
push_exec_root "
    # Idempotent: only patch if not already patched
    if grep -q 'push_hook' '${PUSH3_INITD}'; then
        echo 'Already patched — updating path'
        sed -i '/export LD_PRELOAD.*push_hook/d' '${PUSH3_INITD}'
        sed -i '/${PRELOAD_MARKER}/d' '${PUSH3_INITD}'
    else
        # First time: save backup
        cp '${PUSH3_INITD}' '${PUSH3_BACKUP}'
        echo 'Saved backup: ${PUSH3_BACKUP}'
    fi

    # Insert LD_PRELOAD export before the start-stop-daemon line
    sed -i \"/start-stop-daemon/i\\\\
${PRELOAD_MARKER}\\n\\
export LD_PRELOAD=${REMOTE_SO}\" '${PUSH3_INITD}'
    echo 'Patched ${PUSH3_INITD}'
    grep -A1 'push-hack' '${PUSH3_INITD}'
"

# ── Restart Push3 ────────────────────────────────────────────────────────────

echo ""
warn "About to restart Push3 (Live will restart, takes ~5s)."
read -r -p "Continue? [y/N] " confirm
[[ "${confirm}" == [yY] ]] || { info "Aborted — hook deployed but Push3 not restarted yet."; exit 0; }

info "Restarting Push3..."
push_exec_root "/etc/init.d/push3 stop; sleep 2; /etc/init.d/push3 start"
sleep 5

# ── Verify ───────────────────────────────────────────────────────────────────
info "Checking log..."
push_exec_root "tail -20 /data/push-hack/logs/push-hook.log 2>/dev/null || echo '(log empty — hook may not have loaded)'"

success "Done. Watch log with:"
echo "  ssh root@push.local 'tail -f /data/push-hack/logs/push-hook.log'"
