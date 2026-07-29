#!/usr/bin/env bash
# scripts/uninstall.sh — remove all push-hack hacks from Push
# Usage: ./scripts/uninstall.sh [options]
#
# Options:
#   --host <hostname>   Push hostname or IP (default: push.local)
#   --user <user>       SSH user (default: ableton)
#   --key  <path>       SSH key path
#   --hack <id>         Remove specific hack only
#   --purge             Also delete push-hack data directory (irreversible!)
#   --yes               Skip confirmation prompt

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${SCRIPT_DIR}/../lib/common.sh"

# ── Defaults ──────────────────────────────────────────────────────────────────
SPECIFIC_HACK=""
PURGE=false
YES=false

# ── Arg parsing ───────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --host)   PUSH_HOST="$2"; shift 2 ;;
        --user)   PUSH_USER="$2"; shift 2 ;;
        --key)    PUSH_SSH_KEY="$2"; shift 2 ;;
        --hack)   SPECIFIC_HACK="$2"; shift 2 ;;
        --purge)  PURGE=true; shift ;;
        --yes)    YES=true; shift ;;
        *) die "Unknown option: $1" ;;
    esac
done

# ── Confirm ───────────────────────────────────────────────────────────────────
confirm() {
    if [[ "${YES}" == true ]]; then
        return 0
    fi
    echo ""
    if [[ "${PURGE}" == true ]]; then
        echo -e "${RED}${BOLD}"
        echo "  ╔════════════════════════════════════════════════════════════════╗"
        echo "  ║                    !!! --purge WARNING !!!                     ║"
        echo "  ║                                                                ║"
        echo "  ║  This will permanently delete ALL push-hack data on Push:      ║"
        echo "  ║  binaries, configs, logs, and any hack-specific files.         ║"
        echo "  ║  This action CANNOT be undone.                                 ║"
        echo "  ╚════════════════════════════════════════════════════════════════╝"
        echo -e "${NC}"
    fi
    read -rp "  Remove push-hack from ${PUSH_HOST}? [y/N] " ans
    [[ "${ans}" =~ ^[Yy]$ ]] || { info "Aborted."; exit 0; }
}

# ── Remove single hack ────────────────────────────────────────────────────────
remove_hack() {
    local hack_id="$1"
    local remote_hack_dir="${PUSH_HACK_REMOTE_DIR}/hacks/${hack_id}"

    info "Removing hack: ${hack_id}"

    # Stop + remove service
    remove_service "${hack_id}" "${INIT:-sysvinit}"

    # Remove hack directory from Push
    push_exec "rm -rf '${remote_hack_dir}'" 2>/dev/null || true
    success "Removed: ${hack_id}"
}

# ── Entry point ───────────────────────────────────────────────────────────────
main() {
    print_banner
    echo -e "${BOLD}=== push-hack uninstaller ===${NC}"
    echo ""

    check_connection
    check_root_connection

    # Detect init system + user data dir
    INIT=$(detect_init_system)
    [[ "${INIT}" != "systemd" ]] && INIT="sysvinit"
    USER_DATA=$(detect_user_data_dir)
    PUSH_HACK_REMOTE_DIR="${USER_DATA}/push-hack"

    # Check push-hack is installed
    if ! push_exec "test -d '${PUSH_HACK_REMOTE_DIR}'" 2>/dev/null; then
        warn "push-hack does not appear to be installed (${PUSH_HACK_REMOTE_DIR} not found)"
        exit 0
    fi

    confirm

    if [[ -n "${SPECIFIC_HACK}" ]]; then
        # Remove specific hack
        remove_hack "${SPECIFIC_HACK}"
    else
        # Remove all push-hack services
        info "Scanning for installed push-hack services..."
        local services
        if [[ "${INIT}" == "systemd" ]]; then
            services=$(push_exec_root "systemctl list-units --type=service --all --no-pager 2>/dev/null | grep push-hack | awk '{print \$1}' | sed 's/\.service//'") || true
        else
            services=$(push_exec_root "ls /etc/init.d/ 2>/dev/null | grep '^push-hack-'") || true
        fi

        if [[ -n "${services}" ]]; then
            while IFS= read -r svc; do
                [[ -z "${svc}" ]] && continue
                local hack_id="${svc#push-hack-}"
                remove_hack "${hack_id}"
            done <<< "${services}"
        else
            warn "No push-hack services found on Push"
        fi

        # Clean up deploy.sh legacy backup (not a service, just a file)
        push_exec_root "rm -f /etc/init.d/push3.push-hack-bak" 2>/dev/null || true

        # Remove top-level dirs (but not UserData content)
        if [[ "${PURGE}" == true ]]; then
            echo ""
            warn "Purging ${PUSH_HACK_REMOTE_DIR}..."
            push_exec "rm -rf '${PUSH_HACK_REMOTE_DIR}'"
            success "Purged"
        else
            info "Leaving ${PUSH_HACK_REMOTE_DIR} intact (use --purge to delete)"
            info "Removing binaries only..."
            push_exec "find '${PUSH_HACK_REMOTE_DIR}/hacks' -maxdepth 2 -type f -not -name 'hack.json' -delete" 2>/dev/null || true
        fi
    fi

    echo ""
    success "Uninstall complete. Push is clean."
    echo ""
    info "Verify: SSH into Push and run: ls /etc/init.d/ | grep '^push-hack-'"
    echo ""
}

main "$@"
