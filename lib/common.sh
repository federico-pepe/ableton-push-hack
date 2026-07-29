#!/usr/bin/env bash
# lib/common.sh — shared SSH helpers for push-hack scripts
# Source this file: source "$(dirname "$0")/../lib/common.sh"

set -euo pipefail

# ── Defaults ──────────────────────────────────────────────────────────────────
PUSH_HOST="${PUSH_HOST:-push.local}"
PUSH_USER="${PUSH_USER:-ableton}"
PUSH_SSH_KEY="${PUSH_SSH_KEY:-}"
PUSH_HACK_REMOTE_DIR="${PUSH_HACK_REMOTE_DIR:-/data/UserData/push-hack}"
PUSH_SYSTEMD_DIR="${PUSH_SYSTEMD_DIR:-/etc/systemd/system}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ── Banner ────────────────────────────────────────────────────────────────────
print_banner() {
    clear
    echo ""
    echo -e "${BLUE}"
    cat << 'EOF'
  ____            _       _   _            _    
 |  _ \ _   _ ___| |__   | | | | __ _  ___| | __
 | |_) | | | / __| '_ \  | |_| |/ _` |/ __| |/ /
 |  __/| |_| \__ \ | | | |  _  | (_| | (__|   < 
 |_|    \__,_|___/_| |_| |_| |_|\__,_|\___|_|\_\                                                                                            
EOF
    echo -e "${NC}"
    echo " An UNOFFICIAL extensible hack framework for Ableton Push 3 Standalone"
}

# ── Logging ───────────────────────────────────────────────────────────────────
info()    { echo -e "${BLUE}[push-hack]${NC} $*"; }
success() { echo -e "${GREEN}[push-hack]${NC} $*"; }
warn()    { echo -e "${YELLOW}[push-hack]${NC} $*"; }
error()   { echo -e "${RED}[push-hack]${NC} $*" >&2; }
die()     { error "$*"; exit 1; }

# ── SSH helpers ───────────────────────────────────────────────────────────────

# Build SSH options array
ssh_opts() {
    local opts=(
        -o StrictHostKeyChecking=no
        -o ConnectTimeout=10
        -o BatchMode=yes
    )
    if [[ -n "${PUSH_SSH_KEY}" ]]; then
        opts+=(-i "${PUSH_SSH_KEY}")
    fi
    echo "${opts[@]}"
}

# Run command on Push as ableton user
push_exec() {
    # shellcheck disable=SC2046
    # -n: redirect stdin from /dev/null — prevents SSH from consuming stdin when
    # called inside a `while read` loop (e.g. uninstall.sh service loop).
    ssh -n $(ssh_opts) "${PUSH_USER}@${PUSH_HOST}" "$@"
}

# Run command on Push as root
push_exec_root() {
    # shellcheck disable=SC2046
    ssh -n $(ssh_opts) "root@${PUSH_HOST}" "$@"
}

# Copy file/dir TO Push (as ableton user)
push_copy() {
    local src="$1"
    local dst="$2"
    # shellcheck disable=SC2046
    scp $(ssh_opts) -r "${src}" "${PUSH_USER}@${PUSH_HOST}:${dst}"
}

# Copy file/dir TO Push (as root)
push_copy_root() {
    local src="$1"
    local dst="$2"
    # shellcheck disable=SC2046
    scp $(ssh_opts) -r "${src}" "root@${PUSH_HOST}:${dst}"
}

# Copy file FROM Push
push_fetch() {
    local src="$1"
    local dst="$2"
    # shellcheck disable=SC2046
    scp $(ssh_opts) "${PUSH_USER}@${PUSH_HOST}:${src}" "${dst}"
}

# Remove a stale/changed host key for PUSH_HOST from known_hosts.
# Called when SSH reports "REMOTE HOST IDENTIFICATION HAS CHANGED" — happens
# after a Push OS update regenerates the device host key.
clear_host_key() {
    warn "Host key for ${PUSH_HOST} changed (Push likely updated). Removing stale key..."
    ssh-keygen -R "${PUSH_HOST}" &>/dev/null || true
}

# Check if Push is reachable
check_connection() {
    info "Checking connection to ${PUSH_HOST}..."
    local out rc
    # shellcheck disable=SC2046
    out=$(ssh -n $(ssh_opts) "${PUSH_USER}@${PUSH_HOST}" "echo ok" 2>&1) && rc=0 || rc=$?
    # A changed host key (e.g. after a Push OS update) prints this warning on
    # stderr and pollutes every later op — clear it and retry, whether or not
    # this probe itself happened to connect.
    if grep -q "REMOTE HOST IDENTIFICATION HAS CHANGED\|Host key verification failed" <<<"${out}"; then
        clear_host_key
        # shellcheck disable=SC2046
        out=$(ssh -n $(ssh_opts) "${PUSH_USER}@${PUSH_HOST}" "echo ok" 2>&1) && rc=0 || rc=$?
    fi
    if [[ "${rc}" -ne 0 ]]; then
        die "Cannot connect to Push at ${PUSH_HOST}. Check SSH access and hostname."
    fi
    success "Connected to ${PUSH_HOST}"
}

# Check if root SSH is available
check_root_connection() {
    if ! push_exec_root "echo ok" &>/dev/null; then
        die "Cannot connect as root to ${PUSH_HOST}. Root SSH required for service installation."
    fi
}

# ── Push OS detection ─────────────────────────────────────────────────────────

# Detect init system: prints "systemd", "openrc", or "sysvinit"
detect_init_system() {
    local init_name
    init_name=$(push_exec "cat /proc/1/comm 2>/dev/null || basename \$(readlink -f /proc/1/exe) 2>/dev/null || echo unknown")
    case "${init_name}" in
        systemd) echo "systemd" ;;
        openrc*) echo "openrc" ;;
        *)
            # Try detecting via systemctl
            if push_exec "systemctl --version" &>/dev/null; then
                echo "systemd"
            else
                echo "sysvinit"
            fi
            ;;
    esac
}

# Find writable user-data directory on Push.
# /data is the big NVMe partition (201GB) used by AbletonOS for all user content.
detect_user_data_dir() {
    local candidates=(
        "/data"
        "/data/UserData"
        "/home/ableton"
    )
    for dir in "${candidates[@]}"; do
        if push_exec "test -d '${dir}' && test -w '${dir}'" 2>/dev/null; then
            echo "${dir}"
            return 0
        fi
    done
    warn "Could not auto-detect writable data dir. Defaulting to /data"
    echo "/data"
}

# Get Push architecture
detect_arch() {
    push_exec "uname -m"
}

# Get Push kernel version
detect_kernel() {
    push_exec "uname -r"
}

# ── Service helpers ───────────────────────────────────────────────────────────

service_name() {
    local hack_id="$1"
    echo "push-hack-${hack_id}"
}

# Install service — dispatches to systemd or sysvinit
install_service() {
    local hack_id="$1"
    local unit_file="$2"          # systemd .service file path (local)
    local init_system="${3:-systemd}"

    case "${init_system}" in
        systemd) _install_service_systemd "${hack_id}" "${unit_file}" ;;
        *)       _install_service_initd   "${hack_id}" "${unit_file}" ;;
    esac
}

_install_service_systemd() {
    local hack_id="$1"
    local unit_file="$2"
    local svc_name
    svc_name=$(service_name "${hack_id}")

    info "Installing systemd service ${svc_name}..."
    push_copy_root "${unit_file}" "${PUSH_SYSTEMD_DIR}/${svc_name}.service"
    push_exec_root "systemctl daemon-reload"
    push_exec_root "systemctl enable ${svc_name}"
    push_exec_root "systemctl stop  ${svc_name} 2>/dev/null || true"
    push_exec_root "systemctl start ${svc_name}"
    success "Service ${svc_name} running"
}

_install_service_initd() {
    local hack_id="$1"
    local initd_script="$2"       # local path to generated init.d script
    local svc_name
    svc_name=$(service_name "${hack_id}")

    info "Installing init.d service ${svc_name}..."
    push_copy_root "${initd_script}" "/etc/init.d/${svc_name}"
    push_exec_root "chmod +x /etc/init.d/${svc_name}"

    # Enable at boot — try update-rc.d, fall back to manual symlinks
    push_exec_root "
        if command -v update-rc.d >/dev/null 2>&1; then
            update-rc.d ${svc_name} defaults 2>/dev/null || true
        else
            for n in 2 3 4 5; do
                ln -sf /etc/init.d/${svc_name} /etc/rc\${n}.d/S99${svc_name} 2>/dev/null || true
            done
            for n in 0 1 6; do
                ln -sf /etc/init.d/${svc_name} /etc/rc\${n}.d/K01${svc_name} 2>/dev/null || true
            done
        fi
    "
    # Stop if already running, then start
    push_exec_root "/etc/init.d/${svc_name} stop 2>/dev/null || true"
    push_exec_root "/etc/init.d/${svc_name} start"
    success "Service ${svc_name} started"
}

# Remove service — dispatches to systemd or sysvinit
remove_service() {
    local hack_id="$1"
    local init_system="${2:-systemd}"

    case "${init_system}" in
        systemd) _remove_service_systemd "${hack_id}" ;;
        *)       _remove_service_initd   "${hack_id}" ;;
    esac
}

_remove_service_systemd() {
    local hack_id="$1"
    local svc_name
    svc_name=$(service_name "${hack_id}")

    info "Removing systemd service ${svc_name}..."
    push_exec_root "systemctl stop    ${svc_name} 2>/dev/null || true"
    push_exec_root "systemctl disable ${svc_name} 2>/dev/null || true"
    push_exec_root "rm -f ${PUSH_SYSTEMD_DIR}/${svc_name}.service"
    push_exec_root "systemctl daemon-reload"
    success "Service ${svc_name} removed"
}

_remove_service_initd() {
    local hack_id="$1"
    local svc_name
    svc_name=$(service_name "${hack_id}")

    info "Removing init.d service ${svc_name}..."
    push_exec_root "/etc/init.d/${svc_name} stop 2>/dev/null || true"
    push_exec_root "
        if command -v update-rc.d >/dev/null 2>&1; then
            update-rc.d -f ${svc_name} remove 2>/dev/null || true
        else
            rm -f /etc/rc*.d/*${svc_name} 2>/dev/null || true
        fi
    "
    push_exec_root "rm -f /etc/init.d/${svc_name}"
    success "Service ${svc_name} removed"
}

# Check service status — returns "active" or "inactive"
service_status() {
    local hack_id="$1"
    local init_system="${2:-systemd}"
    local svc_name
    svc_name=$(service_name "${hack_id}")

    if [[ "${init_system}" == "systemd" ]]; then
        push_exec_root "systemctl is-active ${svc_name} 2>/dev/null || echo inactive"
    else
        push_exec_root "
            if [ -f /etc/init.d/${svc_name} ]; then
                /etc/init.d/${svc_name} status 2>/dev/null | grep -qi 'running' && echo active || echo inactive
            else
                echo inactive
            fi
        "
    fi
}

# Generate an init.d script for a hack, write to tmp file, return path
generate_initd_script() {
    local hack_id="$1"
    local binary_path="$2"    # full remote path to binary
    local config_path="$3"    # full remote path to hack.json
    local log_path="$4"       # full remote path to log file
    local svc_name
    svc_name=$(service_name "${hack_id}")

    local tmp
    tmp=$(mktemp /tmp/push-hack-initd-XXXXXX)

    cat > "${tmp}" <<EOF
#!/bin/sh
### BEGIN INIT INFO
# Provides:          ${svc_name}
# Required-Start:    \$network \$local_fs
# Required-Stop:     \$network
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: push-hack: ${hack_id}
### END INIT INFO

DAEMON="${binary_path}"
PIDFILE="/var/run/${svc_name}.pid"
LOGFILE="${log_path}"
CONFIG="${config_path}"

[ -x "\$DAEMON" ] || exit 0

start() {
    echo "Starting ${svc_name}..."
    if [ -f "\$PIDFILE" ] && kill -0 \$(cat "\$PIDFILE") 2>/dev/null; then
        echo "${svc_name} already running"
        return 0
    fi
    mkdir -p "\$(dirname "\$LOGFILE")"
    # Run at lowest CPU priority (nice 19), limit to 64MB virtual memory
    nice -n 19 "\$DAEMON" -config "\$CONFIG" >> "\$LOGFILE" 2>&1 &
    echo \$! > "\$PIDFILE"
    echo "${svc_name} started (PID \$(cat \$PIDFILE))"
}

stop() {
    echo "Stopping ${svc_name}..."
    if [ -f "\$PIDFILE" ]; then
        kill \$(cat "\$PIDFILE") 2>/dev/null || true
        rm -f "\$PIDFILE"
    fi
    echo "${svc_name} stopped"
}

status() {
    if [ -f "\$PIDFILE" ] && kill -0 \$(cat "\$PIDFILE") 2>/dev/null; then
        echo "${svc_name} is running (PID \$(cat \$PIDFILE))"
        return 0
    else
        echo "${svc_name} is not running"
        return 1
    fi
}

case "\$1" in
    start)   start   ;;
    stop)    stop    ;;
    restart) stop; sleep 1; start ;;
    status)  status  ;;
    *)
        echo "Usage: \$0 {start|stop|restart|status}"
        exit 1
        ;;
esac
EOF

    echo "${tmp}"
}

# ── Hack metadata ─────────────────────────────────────────────────────────────

# Read field from hack.json (requires jq or python)
hack_json_field() {
    local json_file="$1"
    local field="$2"
    if command -v jq &>/dev/null; then
        jq -r ".${field}" "${json_file}"
    else
        python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print(d.get(sys.argv[2],''))" "${json_file}" "${field}"
    fi
}
