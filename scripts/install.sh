#!/usr/bin/env bash
# scripts/install.sh — deploy push-hack hacks to Push via SSH
# Usage: ./scripts/install.sh [options]
#
# Options:
#   --host <hostname>     Push hostname or IP (default: push.local)
#   --user <user>         SSH user (default: ableton)
#   --key  <path>         SSH key path
#   --hack <id>           Deploy specific hack only (default: all enabled)
#   --build               Build from source (default: deploy pre-built binaries)
#   --dry-run             Print actions without executing
#   --yes                 Skip disclaimer and SSH wizard (for scripted use)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${SCRIPT_DIR}/../lib/common.sh"

# ── Defaults ──────────────────────────────────────────────────────────────────
SPECIFIC_HACK=""
NO_BUILD=true
DRY_RUN=false
YES=false

# ── Arg parsing ───────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --host)      PUSH_HOST="$2"; shift 2 ;;
        --user)      PUSH_USER="$2"; shift 2 ;;
        --key)       PUSH_SSH_KEY="$2"; shift 2 ;;
        --hack)      SPECIFIC_HACK="$2"; shift 2 ;;
        --build)     NO_BUILD=false; shift ;;
        --dry-run)   DRY_RUN=true; shift ;;
        --yes)       YES=true; shift ;;
        *) die "Unknown option: $1" ;;
    esac
done

maybe() {
    if [[ "${DRY_RUN}" == true ]]; then
        echo -e "${YELLOW}[dry-run]${NC} $*"
    else
        "$@"
    fi
}

# ── Disclaimer ────────────────────────────────────────────────────────────────
check_disclaimer() {
    [[ "${YES}" == true ]] && return 0
    echo -e "${RED}${BOLD}"
    echo "  ╔════════════════════════════════════════════════════════════════╗"
    echo "  ║                                                                ║"
    echo "  ║                        !!! WARNING !!!                         ║"
    echo "  ║                                                                ║"
    echo "  ║  Push Hack is an UNOFFICIAL, UNSUPPORTED project.              ║"
    echo "  ║  It is NOT endorsed by or affiliated with Ableton.             ║"
    echo "  ║                                                                ║"
    echo "  ║  Installing this software modifies your Push 3 device.         ║"
    echo "  ║  You assume ALL risk. The authors accept NO liability for      ║"
    echo "  ║  any damage, data loss, or voided warranty that may result.    ║"
    echo "  ║                                                                ║"
    echo "  ║  Proceed only if you understand and accept these risks.        ║"
    echo "  ╚════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
    echo -n "  Type \"Yes\" to accept and continue: "
    read -r response </dev/tty
    if [[ "${response}" != "Yes" ]]; then
        echo ""
        echo "  Installation aborted."
        echo ""
        exit 1
    fi
    echo ""
}

# ── SSH wizard ────────────────────────────────────────────────────────────────
ssh_wizard() {
    echo -e "${BOLD}── SSH Key Setup ──────────────────────────────────────────────────${NC}"
    echo ""
    echo "  push-hack connects to Push 3 over SSH. Let's set that up."
    echo ""

    # Step 1: locate or generate key
    if [[ -f "${HOME}/.ssh/id_rsa.pub" ]]; then
        info "Found existing SSH key: ~/.ssh/id_rsa.pub"
    else
        info "No SSH key found — generating one now..."
        ssh-keygen -t rsa -N "" -f "${HOME}/.ssh/id_rsa"
        success "Key generated: ~/.ssh/id_rsa.pub"
    fi

    # Step 2: show key + copy to clipboard
    echo ""
    info "Your public key:"
    echo ""
    cat "${HOME}/.ssh/id_rsa.pub"
    echo ""
    if command -v pbcopy &>/dev/null; then
        pbcopy < "${HOME}/.ssh/id_rsa.pub"
        success "Public key copied to clipboard"
    fi

    # Step 3: guide user to Push web UI
    echo ""
    echo -e "  ${BOLD}Add this key to Push 3:${NC}"
    echo ""
    echo -e "  1. Open your browser and go to:  ${CYAN}http://push.local/ssh${NC}"
    echo "  2. Paste your public key and save."
    echo ""
    echo -n "  Press ENTER once the key has been added to Push... "
    read -r _ </dev/tty
    echo ""

    # Step 4: ensure ~/.ssh/config has push*.local entry
    local ssh_config="${HOME}/.ssh/config"
    local host_entry="Host push*.local
  AddKeysToAgent yes
  UseKeychain yes"

    if [[ -f "${ssh_config}" ]] && grep -q "Host push\*\.local" "${ssh_config}" 2>/dev/null; then
        info "SSH config already has push*.local entry"
    else
        touch "${ssh_config}"
        printf "\n%s\n" "${host_entry}" >> "${ssh_config}"
        chmod 600 "${ssh_config}"
        success "Added push*.local entry to ~/.ssh/config"
    fi

    echo ""
    success "SSH setup complete — retrying connection..."
    echo ""
}

# ── Build hack ────────────────────────────────────────────────────────────────
build_hack() {
    local hack_id="$1"
    local hack_dir="${REPO_ROOT}/hacks/${hack_id}"

    if [[ ! -f "${hack_dir}/Makefile" ]]; then
        warn "No Makefile found for hack '${hack_id}', skipping build"
        return 0
    fi

    info "Building ${hack_id}..."
    if ! make -C "${hack_dir}" 2>&1; then
        die "Build failed for hack '${hack_id}'"
    fi
    success "Built ${hack_id}"
}

# ── Deploy hack ───────────────────────────────────────────────────────────────
deploy_hack() {
    local hack_id="$1"
    local hack_dir="${REPO_ROOT}/hacks/${hack_id}"
    local hack_json="${hack_dir}/hack.json"
    local remote_hack_dir="${PUSH_HACK_REMOTE_DIR}/hacks/${hack_id}"
    local log_dir="${PUSH_HACK_REMOTE_DIR}/logs"

    # Validate hack dir
    [[ -d "${hack_dir}" ]] || die "Hack directory not found: ${hack_dir}"
    [[ -f "${hack_json}" ]] || die "hack.json not found: ${hack_json}"

    # Check enabled
    local enabled
    enabled=$(hack_json_field "${hack_json}" "enabled")
    if [[ "${enabled}" == "false" ]]; then
        warn "Hack '${hack_id}' is disabled in hack.json, skipping"
        return 0
    fi

    # Get metadata
    local port
    port=$(hack_json_field "${hack_json}" "port")
    local binary_name
    binary_name=$(hack_json_field "${hack_json}" "binary")

    local port_display=""
    [[ -n "${port}" && "${port}" != "0" && "${port}" != "null" ]] && port_display=" (port ${port})"
    info "Deploying hack: ${hack_id}${port_display}"

    # Find binary (skipped when binary field is empty — e.g. udev-only hacks)
    local binary_path=""
    if [[ -n "${binary_name}" ]]; then
        binary_path=$(find "${hack_dir}" -maxdepth 2 -name "${binary_name}" -type f 2>/dev/null | head -1)
        if [[ -z "${binary_path}" ]]; then
            die "Binary '${binary_name}' not found in ${hack_dir}. Run make first."
        fi
    fi

    # Create remote directories
    maybe push_exec "mkdir -p '${remote_hack_dir}' '${log_dir}'"

    # Ensure the hack dir is owned by the deploy user. A prior root-owned `.so`
    # copy (via push_copy_root) or a standalone deploy.sh run can leave the dir
    # as root:root; the mkdir above is then a no-op and the non-root hack.json
    # copy below fails with "Permission denied". Normalise ownership as root.
    maybe push_exec_root "chown '${PUSH_USER:-ableton}:users' '${remote_hack_dir}'"

    # Stop service first so running binary isn't locked during copy
    local svc_name
    svc_name=$(service_name "${hack_id}")
    if [[ "${INIT}" == "systemd" ]]; then
        push_exec_root "systemctl stop '${svc_name}'" 2>/dev/null || true
    else
        push_exec_root "/etc/init.d/${svc_name} stop" 2>/dev/null || true
    fi

    # Copy binary (skipped when not applicable)
    # Use root SCP for .so files — they are owned by root on the target
    if [[ -n "${binary_path}" ]]; then
        info "  Copying binary..."
        if [[ "${binary_name}" == *.so ]]; then
            maybe push_copy_root "${binary_path}" "${remote_hack_dir}/${binary_name}"
        else
            maybe push_copy "${binary_path}" "${remote_hack_dir}/${binary_name}"
        fi
        maybe push_exec_root "chmod +x '${remote_hack_dir}/${binary_name}'"
    fi

    # Copy extra files (e.g. udev .rules files)
    for extra_file in "${hack_dir}"/*.rules; do
        [[ -f "${extra_file}" ]] || continue
        info "  Copying $(basename "${extra_file}")..."
        maybe push_copy "${extra_file}" "${remote_hack_dir}/$(basename "${extra_file}")"
    done

    # Copy remote-script payload dir (e.g. an Ableton Live MIDI Remote Script).
    # The hack's service.initd is responsible for installing it into Live's
    # User Library on start. push_copy uses scp -r, so directories work.
    if [[ -d "${hack_dir}/remote-script" ]]; then
        info "  Copying remote-script payload..."
        # Remove the remote dir first — scp -r into an existing dir nests the
        # source as <dst>/remote-script/remote-script and leaves stale top-level
        # files behind on re-deploy.
        maybe push_exec "rm -rf '${remote_hack_dir}/remote-script'"
        maybe push_copy "${hack_dir}/remote-script" "${remote_hack_dir}/remote-script"
    fi

    # Copy hack.json (with resolved paths)
    info "  Copying config..."
    local tmp_json
    tmp_json=$(mktemp)
    # Inject resolved remote paths into hack.json
    local py
    if py=$(find_python); then
        "${py}" - "${hack_json}" "${PUSH_HACK_REMOTE_DIR}" "${remote_hack_dir}" > "${tmp_json}" <<'PYEOF'
import json, sys
with open(sys.argv[1]) as f:
    d = json.load(f)
d['_push_hack_dir'] = sys.argv[2]
d['_hack_dir'] = sys.argv[3]
# Resolve allowed_roots: replace placeholder with detected user data dir
roots = d.get('allowed_roots', [])
resolved = []
for r in roots:
    resolved.append(r.replace('${USER_DATA}', sys.argv[2].replace('/push-hack', '')))
d['allowed_roots'] = resolved
print(json.dumps(d, indent=2))
PYEOF
    else
        cp "${hack_json}" "${tmp_json}"
    fi
    maybe push_copy "${tmp_json}" "${remote_hack_dir}/hack.json"
    rm -f "${tmp_json}"

    # Generate and install service (systemd or init.d)
    install_hack_service "${hack_id}" "${hack_dir}" "${remote_hack_dir}" "${binary_name}" "${port}"

    success "Deployed: ${hack_id}"
}

# ── Service installation (systemd or init.d) ─────────────────────────────────
install_hack_service() {
    local hack_id="$1"
    local hack_dir="$2"
    local remote_hack_dir="$3"
    local binary_name="$4"
    local port="$5"
    local log_path="${PUSH_HACK_REMOTE_DIR}/logs/${hack_id}.log"

    if [[ "${INIT}" == "systemd" ]]; then
        # ── systemd path ──────────────────────────────────────────────────
        local svc_name
        svc_name=$(service_name "${hack_id}")
        local tmp_service
        tmp_service=$(mktemp)
        local service_template="${hack_dir}/${hack_id}.service"

        if [[ -f "${service_template}" ]]; then
            sed \
                -e "s|{{HACK_DIR}}|${remote_hack_dir}|g" \
                -e "s|{{BINARY}}|${binary_name}|g" \
                -e "s|{{LOG_DIR}}|${PUSH_HACK_REMOTE_DIR}/logs|g" \
                -e "s|{{PORT}}|${port}|g" \
                "${service_template}" > "${tmp_service}"
        else
            cat > "${tmp_service}" <<EOF
[Unit]
Description=push-hack: ${hack_id}
After=network.target

[Service]
Type=simple
ExecStart=${remote_hack_dir}/${binary_name} -config ${remote_hack_dir}/hack.json
Restart=on-failure
RestartSec=10
Nice=19
MemoryMax=64M
StandardOutput=append:${log_path}
StandardError=append:${log_path}
WorkingDirectory=${remote_hack_dir}

[Install]
WantedBy=multi-user.target
EOF
        fi

        info "  Installing systemd unit..."
        maybe push_copy_root "${tmp_service}" "${PUSH_SYSTEMD_DIR}/${svc_name}.service"
        rm -f "${tmp_service}"
        maybe push_exec_root "systemctl daemon-reload"
        maybe push_exec_root "systemctl enable '${svc_name}'"
        push_exec_root "systemctl stop '${svc_name}'" 2>/dev/null || true
        maybe push_exec_root "systemctl start '${svc_name}'"

    else
        # ── init.d path ───────────────────────────────────────────────────
        local initd_script
        local custom_initd="${hack_dir}/service.initd"
        if [[ -f "${custom_initd}" ]]; then
            # Hack provides its own init.d template — substitute placeholders
            local tmp_custom
            tmp_custom=$(mktemp)
            sed \
                -e "s|{{HACK_ID}}|${hack_id}|g" \
                -e "s|{{SVC_NAME}}|$(service_name "${hack_id}")|g" \
                -e "s|{{HACK_DIR}}|${remote_hack_dir}|g" \
                -e "s|{{LOG_DIR}}|${PUSH_HACK_REMOTE_DIR}/logs|g" \
                -e "s|{{PORT}}|${port}|g" \
                "${custom_initd}" > "${tmp_custom}"
            initd_script="${tmp_custom}"
        else
            initd_script=$(generate_initd_script \
                "${hack_id}" \
                "${remote_hack_dir}/${binary_name}" \
                "${remote_hack_dir}/hack.json" \
                "${log_path}")
        fi

        info "  Installing init.d script..."
        maybe push_copy_root "${initd_script}" "/etc/init.d/$(service_name "${hack_id}")"
        rm -f "${initd_script}"
        maybe push_exec_root "chmod +x /etc/init.d/$(service_name "${hack_id}")"

        # Enable at boot
        maybe push_exec_root "
            if command -v update-rc.d >/dev/null 2>&1; then
                update-rc.d $(service_name "${hack_id}") defaults 2>/dev/null || true
            else
                for n in 2 3 4 5; do
                    ln -sf /etc/init.d/$(service_name "${hack_id}") /etc/rc\${n}.d/S99$(service_name "${hack_id}") 2>/dev/null || true
                done
            fi
        "

        push_exec_root "/etc/init.d/$(service_name "${hack_id}") stop 2>/dev/null || true"
        maybe push_exec_root "/etc/init.d/$(service_name "${hack_id}") start"
    fi

    # Verify
    if [[ "${DRY_RUN}" == false ]]; then
        sleep 2
        local status
        status=$(service_status "${hack_id}" "${INIT}")
        if [[ "${status}" == "active" ]]; then
            success "  Service $(service_name "${hack_id}"): active ✓"
        else
            warn "  Service $(service_name "${hack_id}"): ${status} (check: ${log_path})"
        fi
    fi
}

# ── Entry point ───────────────────────────────────────────────────────────────
main() {
    print_banner
    check_disclaimer

    # If SSH isn't working yet, walk the user through key setup
    if [[ "${DRY_RUN}" == false && "${YES}" == false ]]; then
        if ! ssh -o ConnectTimeout=5 -o BatchMode=yes \
               ${PUSH_SSH_KEY:+-i "${PUSH_SSH_KEY}"} \
               "${PUSH_USER:-ableton}@${PUSH_HOST:-push.local}" exit 0 2>/dev/null; then
            ssh_wizard
        fi
    fi

    echo ""
    echo -e "${BOLD}=== push-hack installer ===${NC}"
    echo ""

    check_connection
    check_root_connection

    # Detect init system
    INIT=$(detect_init_system)
    info "Init system: ${INIT}"
    if [[ "${INIT}" != "systemd" && "${INIT}" != "sysvinit" && "${INIT}" != "openrc" ]]; then
        warn "Unknown init system '${INIT}' — will attempt init.d fallback"
        INIT="sysvinit"
    fi

    # Detect user data dir (update remote dir if needed)
    USER_DATA=$(detect_user_data_dir)
    PUSH_HACK_REMOTE_DIR="${USER_DATA}/push-hack"
    info "Install target: ${PUSH_HACK_REMOTE_DIR}"

    # Create top-level structure
    maybe push_exec "mkdir -p '${PUSH_HACK_REMOTE_DIR}/hacks' '${PUSH_HACK_REMOTE_DIR}/logs'"

    # Collect hacks to deploy
    local hacks_to_deploy=()
    if [[ -n "${SPECIFIC_HACK}" ]]; then
        hacks_to_deploy=("${SPECIFIC_HACK}")
    else
        while IFS= read -r -d '' hack_json; do
            hack_id=$(basename "$(dirname "${hack_json}")")
            hacks_to_deploy+=("${hack_id}")
        done < <(find "${REPO_ROOT}/hacks" -maxdepth 2 -name "hack.json" -print0)
    fi

    if [[ ${#hacks_to_deploy[@]} -eq 0 ]]; then
        warn "No hacks found in ${REPO_ROOT}/hacks/"
        exit 0
    fi

    # Build then deploy each hack
    for hack_id in "${hacks_to_deploy[@]}"; do
        echo ""
        echo -e "${BOLD}── Hack: ${hack_id} ─────────────────────────────────${NC}"
        if [[ "${NO_BUILD}" == false ]]; then
            build_hack "${hack_id}"
        fi
        deploy_hack "${hack_id}"
    done

    echo ""
    success "Installation complete!"
    echo ""
    info "Access Push Manager at: http://${PUSH_HOST}:$(hack_json_field "${REPO_ROOT}/hacks/push-manager/hack.json" "port")"
    echo ""
}

main "$@"
