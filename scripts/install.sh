#!/usr/bin/env bash
set -euo pipefail

# rIOt Agent Install Script
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh | sudo bash -s -- https://server:7331
#
# Options:
#   $1                   — rIOt server URL (required for first install, optional for reinstall)
#   --fingerprint SHA256:xxx  — verify server cert fingerprint on first connect
#   --key mykey          — registration key (if server requires one)
#   --bootstrap-key KEY  — mTLS bootstrap key for certificate enrollment
#   --version 1.2.3      — install a specific version (default: latest)
#   -y, --yes            — non-interactive: enable all remote features
#   --non-interactive    — non-interactive: use defaults (all remote features disabled)

# ── Parse arguments ──────────────────────────────────────────────────
RIOT_SERVER=""
RIOT_KEY=""
RIOT_FINGERPRINT=""
RIOT_BOOTSTRAP_KEY=""
RIOT_VERSION="latest"
RIOT_INTERACTIVE="auto"  # auto, yes-all, defaults

while [ $# -gt 0 ]; do
    case "$1" in
        --fingerprint)
            RIOT_FINGERPRINT="${2:-}"
            shift 2
            ;;
        --key)
            RIOT_KEY="${2:-}"
            shift 2
            ;;
        --bootstrap-key)
            RIOT_BOOTSTRAP_KEY="${2:-}"
            shift 2
            ;;
        --version)
            RIOT_VERSION="${2:-latest}"
            shift 2
            ;;
        -y|--yes)
            RIOT_INTERACTIVE="yes-all"
            shift
            ;;
        --non-interactive)
            RIOT_INTERACTIVE="defaults"
            shift
            ;;
        -*)
            echo "ERROR: Unknown flag: $1"
            exit 1
            ;;
        *)
            if [ -z "$RIOT_SERVER" ]; then
                RIOT_SERVER="$1"
            else
                echo "ERROR: Unexpected argument: $1"
                exit 1
            fi
            shift
            ;;
    esac
done

# Allow env var overrides
RIOT_SERVER="${RIOT_SERVER_URL:-${RIOT_SERVER:-}}"
RIOT_VERSION="${RIOT_VERSION_OVERRIDE:-${RIOT_VERSION}}"

if [ -z "$RIOT_SERVER" ]; then
    if [ -f "/etc/riot/agent.yaml" ]; then
        echo "==> No server URL provided — reinstalling with existing config"
    else
        echo "ERROR: Server URL is required for first-time install."
        echo "Usage: curl -sSL .../install.sh | sudo bash -s -- https://server:7331"
        exit 1
    fi
fi

RIOT_REPO="rbretschneider/rIOt"
RIOT_USER="riot"
RIOT_CONFIG_DIR="/etc/riot"
RIOT_DATA_DIR="/var/lib/riot"
RIOT_BIN="/usr/local/bin/riot-agent"

echo "==> rIOt Agent Installer"

# ── Detect if server URL points to this machine, use 127.0.0.1 ──────
# Only for http:// — HTTPS certs are issued to the real hostname/IP,
# so rewriting to 127.0.0.1 would break TLS verification.
resolve_server_url() {
    local url="$1"

    # Skip for HTTPS — cert SANs won't match 127.0.0.1
    case "$url" in
        https://*) return ;;
    esac

    # Extract host from URL (strip protocol and port/path)
    local host
    host=$(echo "$url" | sed -E 's|https?://||; s|[:/].*||')

    # Skip if already localhost/127.x
    case "$host" in
        localhost|127.*) return ;;
    esac

    # Check if this host's IP matches the server URL
    local local_ips
    local_ips=$(hostname -I 2>/dev/null || ip -4 addr show 2>/dev/null | grep -oP 'inet \K[\d.]+' || true)

    for ip in $local_ips; do
        if [ "$ip" = "$host" ]; then
            RIOT_SERVER=$(echo "$url" | sed "s|${host}|127.0.0.1|")
            echo "==> Detected local server, using 127.0.0.1 instead of ${host}"
            return
        fi
    done
}
resolve_server_url "$RIOT_SERVER"

# ── Detect architecture ──────────────────────────────────────────────
ARCH=$(uname -m)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')

case "$ARCH" in
    x86_64)  SUFFIX="linux-amd64" ;;
    aarch64) SUFFIX="linux-arm64" ;;
    armv7l)  SUFFIX="linux-armv7" ;;
    armv6l)  SUFFIX="linux-armv6" ;;
    i686)    SUFFIX="linux-386" ;;
    *)
        echo "ERROR: Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

if [ "$OS" = "darwin" ]; then
    case "$ARCH" in
        x86_64)  SUFFIX="darwin-amd64" ;;
        arm64)   SUFFIX="darwin-arm64" ;;
        *)
            echo "ERROR: Unsupported macOS architecture: $ARCH"
            exit 1
            ;;
    esac
fi

BINARY_NAME="riot-agent-${SUFFIX}"
echo "==> Platform: ${OS}/${ARCH} (${BINARY_NAME})"

# ── Resolve download URL ─────────────────────────────────────────────
if [ "$RIOT_VERSION" = "latest" ]; then
    DOWNLOAD_URL="https://github.com/${RIOT_REPO}/releases/latest/download/${BINARY_NAME}"
else
    DOWNLOAD_URL="https://github.com/${RIOT_REPO}/releases/download/v${RIOT_VERSION}/${BINARY_NAME}"
fi

echo "==> Downloading from: ${DOWNLOAD_URL}"

# ── Create system user (Linux only) ──────────────────────────────────
if [ "$OS" = "linux" ]; then
    if ! id -u "$RIOT_USER" >/dev/null 2>&1; then
        echo "==> Creating system user: $RIOT_USER"
        useradd --system --no-create-home --shell /usr/sbin/nologin "$RIOT_USER"
    fi

    # Add riot user to docker group if Docker is installed
    if getent group docker >/dev/null 2>&1; then
        if ! id -nG "$RIOT_USER" | grep -qw docker; then
            echo "==> Adding $RIOT_USER to docker group (for container monitoring)"
            usermod -aG docker "$RIOT_USER"
        fi
    fi

    # Add riot user to systemd-journal group for log collection
    if getent group systemd-journal >/dev/null 2>&1; then
        if ! id -nG "$RIOT_USER" | grep -qw systemd-journal; then
            echo "==> Adding $RIOT_USER to systemd-journal group (for log collection)"
            usermod -aG systemd-journal "$RIOT_USER"
        fi
    fi
fi

# ── Install smartmontools for SMART disk health monitoring ────────────
if [ "$OS" = "linux" ]; then
    if ! command -v smartctl >/dev/null 2>&1; then
        echo "==> Installing smartmontools (for SMART disk health monitoring)"
        if command -v apt-get >/dev/null 2>&1; then
            apt-get update -qq && apt-get install -y -qq smartmontools >/dev/null 2>&1
        elif command -v dnf >/dev/null 2>&1; then
            dnf install -y -q smartmontools >/dev/null 2>&1
        elif command -v yum >/dev/null 2>&1; then
            yum install -y -q smartmontools >/dev/null 2>&1
        elif command -v pacman >/dev/null 2>&1; then
            pacman -S --noconfirm --quiet smartmontools >/dev/null 2>&1
        elif command -v apk >/dev/null 2>&1; then
            apk add --quiet smartmontools >/dev/null 2>&1
        else
            echo "    WARN: Could not install smartmontools — SMART monitoring will be unavailable"
        fi
    fi
fi

# ── Create directories ───────────────────────────────────────────────
echo "==> Creating directories"
mkdir -p "$RIOT_CONFIG_DIR" "$RIOT_DATA_DIR"
if [ "$OS" = "linux" ]; then
    chown "$RIOT_USER:$RIOT_USER" "$RIOT_CONFIG_DIR" "$RIOT_DATA_DIR"
fi

# ── Stop existing agent if running ────────────────────────────────────
if [ "$OS" = "linux" ] && systemctl is-active riot-agent >/dev/null 2>&1; then
    echo "==> Stopping running agent"
    systemctl stop riot-agent
fi

# ── Download binary ──────────────────────────────────────────────────
echo "==> Downloading agent binary"
if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$RIOT_BIN" "$DOWNLOAD_URL"
elif command -v wget >/dev/null 2>&1; then
    wget -qO "$RIOT_BIN" "$DOWNLOAD_URL"
else
    echo "ERROR: curl or wget is required"
    exit 1
fi
chmod +x "$RIOT_BIN"
if [ "$OS" = "linux" ]; then
    chown "$RIOT_USER:$RIOT_USER" "$RIOT_BIN"
fi

echo "==> Installed: $($RIOT_BIN --version 2>/dev/null || echo "$RIOT_BIN")"

# ── Helper: ask yes/no question ──────────────────────────────────────
# Reads from /dev/tty so it works even when piped (curl | bash).
# Usage: ask_yn "prompt" default_value
#   default_value: "y" or "n"
#   Returns 0 for yes, 1 for no
ask_yn() {
    local prompt="$1" default="$2"
    local hint="[y/N]"
    [ "$default" = "y" ] && hint="[Y/n]"

    # Non-interactive modes
    if [ "$RIOT_INTERACTIVE" = "yes-all" ]; then
        echo "    ${prompt} ${hint}: y (auto)"
        return 0
    elif [ "$RIOT_INTERACTIVE" = "defaults" ]; then
        [ "$default" = "y" ] && return 0 || return 1
    fi

    # Interactive: read from /dev/tty
    if [ -t 0 ] || [ -e /dev/tty ]; then
        printf "    %s %s: " "$prompt" "$hint"
        local answer
        read -r answer < /dev/tty 2>/dev/null || answer=""
        answer=$(echo "$answer" | tr '[:upper:]' '[:lower:]')
        case "$answer" in
            y|yes) return 0 ;;
            n|no)  return 1 ;;
            "")    [ "$default" = "y" ] && return 0 || return 1 ;;
        esac
        [ "$default" = "y" ] && return 0 || return 1
    else
        # No tty available, use defaults
        [ "$default" = "y" ] && return 0 || return 1
    fi
}

# ── Detect Docker for config ─────────────────────────────────────────
HAS_DOCKER="false"
if command -v docker >/dev/null 2>&1; then
    HAS_DOCKER="true"
    echo "==> Docker detected, enabling container monitoring"
fi

# ── Build optional config sections ────────────────────────────────────
API_KEY_LINE=""
if [ -n "$RIOT_KEY" ]; then
    API_KEY_LINE="
  api_key: \"${RIOT_KEY}\""
fi

CERT_PIN_LINE=""
if [ -n "$RIOT_FINGERPRINT" ]; then
    CERT_PIN_LINE="
  server_cert_pin: \"${RIOT_FINGERPRINT}\""
fi

BOOTSTRAP_KEY_LINE=""
if [ -n "$RIOT_BOOTSTRAP_KEY" ]; then
    BOOTSTRAP_KEY_LINE="
  bootstrap_key: \"${RIOT_BOOTSTRAP_KEY}\""
fi

# ── Interactive feature configuration ────────────────────────────────
ALLOW_REBOOT="false"
ALLOW_PATCHING="false"
ALLOW_TERMINAL="false"
ALLOW_DOCKER_TERMINAL="false"

# Only ask on fresh install (no existing config)
if [ ! -f "$RIOT_CONFIG_DIR/agent.yaml" ]; then
    echo ""
    echo "==> Configure remote management features"
    echo "    These can be changed later in ${RIOT_CONFIG_DIR}/agent.yaml"
    echo ""

    if ask_yn "Allow remote reboot from dashboard?" "n"; then
        ALLOW_REBOOT="true"
    fi

    if ask_yn "Allow remote OS patching from dashboard?" "n"; then
        ALLOW_PATCHING="true"
    fi

    if ask_yn "Allow remote terminal access to this host?" "n"; then
        ALLOW_TERMINAL="true"
    fi

    if [ "$HAS_DOCKER" = "true" ]; then
        if ask_yn "Allow remote exec into Docker containers?" "n"; then
            ALLOW_DOCKER_TERMINAL="true"
        fi
    fi

    echo ""
fi

# ── Build config sections from answers ───────────────────────────────
COMMANDS_SECTION=""
if [ "$ALLOW_REBOOT" = "true" ] || [ "$ALLOW_PATCHING" = "true" ]; then
    COMMANDS_SECTION="
commands:
  allow_reboot: ${ALLOW_REBOOT}
  allow_patching: ${ALLOW_PATCHING}"
fi

TERMINAL_SECTION=""
if [ "$ALLOW_TERMINAL" = "true" ]; then
    TERMINAL_SECTION="
host_terminal:
  enabled: true"
fi

DOCKER_SECTION=""
if [ "$HAS_DOCKER" = "true" ]; then
    DOCKER_SECTION="
docker:
  enabled: auto
  collect_stats: true
  terminal_enabled: ${ALLOW_DOCKER_TERMINAL}"
fi

# ── Handle re-install with bootstrap key ──────────────────────────────
# If a bootstrap key is provided and a config already exists, the user is
# re-enrolling. Wipe old enrollment state so the agent starts fresh.
if [ -n "$RIOT_BOOTSTRAP_KEY" ] && [ -f "$RIOT_CONFIG_DIR/agent.yaml" ]; then
    echo "==> Bootstrap key provided — resetting agent for fresh enrollment"
    rm -f "$RIOT_CONFIG_DIR/agent.yaml"
    rm -f "$RIOT_CONFIG_DIR/client.crt" "$RIOT_CONFIG_DIR/client.key" "$RIOT_CONFIG_DIR/ca.crt"
    rm -f "$RIOT_CONFIG_DIR/server.crt"
    rm -f "$RIOT_DATA_DIR/device-id"
fi

# ── Write config (skip if already exists) ─────────────────────────────
if [ ! -f "$RIOT_CONFIG_DIR/agent.yaml" ]; then
    echo "==> Writing config to ${RIOT_CONFIG_DIR}/agent.yaml"
    cat > "$RIOT_CONFIG_DIR/agent.yaml" <<EOF
server:
  url: "${RIOT_SERVER}"
  tls_verify: true${API_KEY_LINE}${CERT_PIN_LINE}${BOOTSTRAP_KEY_LINE}

agent:
  poll_interval: 60
  heartbeat_interval: 15

collectors:
  enabled:
    - system
    - cpu
    - memory
    - disk
    - network
    - os_info
    - updates
    - services
    - processes
    - docker
    - security
    - logs
    - ups
    - webservers
    - usb
${COMMANDS_SECTION}
${TERMINAL_SECTION}
${DOCKER_SECTION}
EOF
    if [ "$OS" = "linux" ]; then
        chown "$RIOT_USER:$RIOT_USER" "$RIOT_CONFIG_DIR/agent.yaml"
    fi
    chmod 600 "$RIOT_CONFIG_DIR/agent.yaml"
else
    echo "==> Config already exists, skipping (${RIOT_CONFIG_DIR}/agent.yaml)"
fi

# ── Build supplementary groups for systemd ────────────────────────────
SUPP_GROUPS=""
if getent group docker >/dev/null 2>&1; then
    SUPP_GROUPS="docker"
fi
if getent group systemd-journal >/dev/null 2>&1; then
    SUPP_GROUPS="${SUPP_GROUPS:+$SUPP_GROUPS }systemd-journal"
fi
SUPPLEMENTARY_GROUPS=""
if [ -n "$SUPP_GROUPS" ]; then
    SUPPLEMENTARY_GROUPS="SupplementaryGroups=$SUPP_GROUPS"
fi

# ── Install sudoers drop-in for privilege escalation ──────────────────
PROTECT_SYSTEM="ProtectSystem=strict"
if [ "$OS" = "linux" ]; then
    SUDOERS_FILE="/etc/sudoers.d/riot-agent"
    echo "==> Installing sudoers rules for fleet management"
    cat > "$SUDOERS_FILE" <<SUDOEOF
# rIOt Agent — least-privilege escalation for fleet management
riot ALL=(root) NOPASSWD: /usr/bin/apt-get update
riot ALL=(root) NOPASSWD: /usr/bin/apt-get -y dist-upgrade -o Dpkg\:\:Options\:\:=--force-confold -o Dpkg\:\:Options\:\:=--force-confdef
riot ALL=(root) NOPASSWD: /usr/bin/apt-get -y upgrade -o Dpkg\:\:Options\:\:=--force-confold -o Dpkg\:\:Options\:\:=--force-confdef
riot ALL=(root) NOPASSWD: /usr/bin/dnf makecache
riot ALL=(root) NOPASSWD: /usr/bin/dnf -y update
riot ALL=(root) NOPASSWD: /usr/bin/dnf -y --security update
riot ALL=(root) NOPASSWD: /usr/bin/systemctl reboot
riot ALL=(root) NOPASSWD: /bin/sh -c mv -f ${RIOT_BIN} ${RIOT_BIN}.old && cp ${RIOT_DATA_DIR}/riot-agent.update ${RIOT_BIN} && chmod 755 ${RIOT_BIN} && rm -f ${RIOT_BIN}.old
riot ALL=(root) NOPASSWD: /usr/bin/systemd-run --unit=riot-agent-update sh -c *
riot ALL=(root) NOPASSWD: /usr/bin/systemctl reset-failed riot-agent-update
# Web server config inspection (read-only)
riot ALL=(root) NOPASSWD: /usr/sbin/nginx -t
riot ALL=(root) NOPASSWD: /usr/sbin/nginx -T
# SMART disk health monitoring
riot ALL=(root) NOPASSWD: /usr/sbin/smartctl
SUDOEOF
    chmod 0440 "$SUDOERS_FILE"
    if visudo -cf "$SUDOERS_FILE" >/dev/null 2>&1; then
        echo "==> Sudoers rules validated OK"
        # With sudoers installed, relax ProtectSystem so sudo children
        # (package managers, reboot) can write to /usr, /var/lib/dpkg, etc.
        PROTECT_SYSTEM="ProtectSystem=false"
    else
        echo "WARN: Sudoers validation failed, removing ${SUDOERS_FILE}"
        rm -f "$SUDOERS_FILE"
    fi
fi

# ── Install systemd service (Linux only) ──────────────────────────────
if [ "$OS" = "linux" ] && command -v systemctl >/dev/null 2>&1; then
    echo "==> Installing systemd service"
    cat > /etc/systemd/system/riot-agent.service <<EOF
[Unit]
Description=rIOt Agent - Infrastructure Monitoring
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStartPre=+/bin/sh -c 'test -f ${RIOT_DATA_DIR}/riot-agent.update && cp -f ${RIOT_DATA_DIR}/riot-agent.update ${RIOT_BIN} && chmod 755 ${RIOT_BIN} && rm -f ${RIOT_DATA_DIR}/riot-agent.update || true'
ExecStart=${RIOT_BIN} -config ${RIOT_CONFIG_DIR}/agent.yaml
Restart=always
RestartSec=5
User=${RIOT_USER}
Group=${RIOT_USER}
${SUPPLEMENTARY_GROUPS}
LimitNOFILE=65536
${PROTECT_SYSTEM}
ReadWritePaths=${RIOT_DATA_DIR} ${RIOT_CONFIG_DIR} ${RIOT_BIN}
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable riot-agent
    systemctl restart riot-agent

    echo ""
    echo "==> rIOt agent installed and running!"
    echo "    Config:  ${RIOT_CONFIG_DIR}/agent.yaml"
    echo "    Status:  systemctl status riot-agent"
    echo "    Logs:    journalctl -u riot-agent -f"
elif [ "$OS" = "darwin" ]; then
    echo ""
    echo "==> rIOt agent installed at ${RIOT_BIN}"
    echo "    Run manually:  riot-agent -config ${RIOT_CONFIG_DIR}/agent.yaml"
    echo "    Config:        ${RIOT_CONFIG_DIR}/agent.yaml"
    echo ""
    echo "    To run as a launchd service, create a plist in ~/Library/LaunchAgents/"
else
    echo ""
    echo "==> rIOt agent installed at ${RIOT_BIN}"
    echo "    Run manually:  riot-agent -config ${RIOT_CONFIG_DIR}/agent.yaml"
fi
