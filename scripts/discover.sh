#!/usr/bin/env bash
# scripts/discover.sh — probe Push OS internals and print a filesystem/service map
# Usage: ./scripts/discover.sh [--host push.local] [--user ableton] [--key ~/.ssh/id_rsa]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../lib/common.sh"

# ── Arg parsing ───────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --host) PUSH_HOST="$2"; shift 2 ;;
        --user) PUSH_USER="$2"; shift 2 ;;
        --key)  PUSH_SSH_KEY="$2"; shift 2 ;;
        *) die "Unknown option: $1" ;;
    esac
done

# ── Main ──────────────────────────────────────────────────────────────────────
print_banner
echo -e "${BOLD}=== Discovering: ${PUSH_HOST} ===${NC}"
echo ""

check_connection

# Basic system info
echo -e "${BOLD}── System ──────────────────────────────────────────${NC}"
ARCH=$(push_exec "uname -m")
KERNEL=$(push_exec "uname -r")
HOSTNAME=$(push_exec "hostname")
echo "  Hostname  : ${HOSTNAME}"
echo "  Arch      : ${ARCH}"
echo "  Kernel    : ${KERNEL}"
echo ""

# Init system
echo -e "${BOLD}── Init system ─────────────────────────────────────${NC}"
INIT=$(detect_init_system)
echo "  Init      : ${INIT}"
if [[ "${INIT}" == "systemd" ]]; then
    SYSTEMD_VER=$(push_exec "systemctl --version | head -1")
    echo "  Version   : ${SYSTEMD_VER}"
fi
echo ""

# OS / distro
echo -e "${BOLD}── OS ──────────────────────────────────────────────${NC}"
OS_RELEASE=$(push_exec "cat /etc/os-release 2>/dev/null || cat /etc/issue 2>/dev/null || echo unknown")
echo "${OS_RELEASE}" | sed 's/^/  /'
echo ""

# Filesystem layout
echo -e "${BOLD}── Filesystem (mounts) ─────────────────────────────${NC}"
push_exec "cat /proc/mounts" | awk '{print "  "$0}'
echo ""

# Writable dirs
echo -e "${BOLD}── Writable directories ────────────────────────────${NC}"
WRITABLE_DIRS=$(push_exec "
    for d in /data /data/UserData /home/ableton /opt /tmp /var; do
        if [ -d \"\$d\" ]; then
            if [ -w \"\$d\" ]; then
                echo \"  [writable] \$d\"
            else
                echo \"  [readonly] \$d\"
            fi
        fi
    done
")
echo "${WRITABLE_DIRS}"
echo ""

# User data dir detection
echo -e "${BOLD}── User data dir ───────────────────────────────────${NC}"
USER_DATA=$(detect_user_data_dir)
echo "  Detected  : ${USER_DATA}"
if push_exec "test -d '${USER_DATA}'" 2>/dev/null; then
    push_exec "du -sh '${USER_DATA}' 2>/dev/null | awk '{print \"  Usage     : \"\$1}'"
    echo "  Contents  :"
    push_exec "ls -la '${USER_DATA}' 2>/dev/null" | sed 's/^/    /'
fi
echo ""

# Live / Ableton processes
echo -e "${BOLD}── Running Ableton processes ───────────────────────${NC}"
push_exec "ps aux 2>/dev/null | grep -i -E '(live|push|ableton|move)' | grep -v grep || echo '  none found'"
echo ""

# All processes (abbreviated)
echo -e "${BOLD}── All processes (top 30) ──────────────────────────${NC}"
push_exec "ps aux --sort=-%cpu 2>/dev/null | head -31" | sed 's/^/  /'
echo ""

# Network interfaces
echo -e "${BOLD}── Network interfaces ──────────────────────────────${NC}"
push_exec "ip addr show 2>/dev/null || ifconfig 2>/dev/null || echo 'ip/ifconfig not available'"  | sed 's/^/  /'
echo ""

# Listening ports
echo -e "${BOLD}── Listening ports ─────────────────────────────────${NC}"
push_exec "ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null || echo 'ss/netstat not available'" | sed 's/^/  /'
echo ""

# Systemd services (if available)
if [[ "${INIT}" == "systemd" ]]; then
    echo -e "${BOLD}── Systemd services (active) ────────────────────────${NC}"
    push_exec "systemctl list-units --type=service --state=active --no-pager 2>/dev/null | head -40" | sed 's/^/  /'
    echo ""
fi

# Available runtimes
echo -e "${BOLD}── Available runtimes ──────────────────────────────${NC}"
for bin in python python3 python2 node nodejs go bash sh; do
    VER=$(push_exec "command -v ${bin} &>/dev/null && ${bin} --version 2>&1 | head -1 || echo 'not found'")
    printf "  %-10s: %s\n" "${bin}" "${VER}"
done
echo ""

# Disk space
echo -e "${BOLD}── Disk space ──────────────────────────────────────${NC}"
push_exec "df -h 2>/dev/null" | sed 's/^/  /'
echo ""

# Memory
echo -e "${BOLD}── Memory ──────────────────────────────────────────${NC}"
push_exec "free -h 2>/dev/null || cat /proc/meminfo | head -10" | sed 's/^/  /'
echo ""

# Summary
echo -e "${BOLD}=== Summary ===${NC}"
echo ""
echo "  Connect   : ssh ${PUSH_USER}@${PUSH_HOST}"
echo "  Arch      : ${ARCH}"
echo "  Init      : ${INIT}"
echo "  User data : ${USER_DATA}"
echo ""
echo -e "${GREEN}Recommendation:${NC}"
echo "  Install dir  : ${USER_DATA}/push-hack"
echo "  Service type : ${INIT}"
if [[ "${ARCH}" != "x86_64" ]]; then
    warn "  Non-x86_64 arch detected (${ARCH}). Adjust Go build target: GOARCH=arm64 or GOARCH=arm"
fi
echo ""
echo -e "${YELLOW}Next step:${NC} run ./scripts/install.sh"
echo ""
