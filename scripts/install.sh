#!/usr/bin/env bash
set -euo pipefail

# rIOt Agent Install Script
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh | sudo bash -s -- https://server:7331
#
# Options:
#   $1                   — rIOt server URL (required)
#   --fingerprint SHA256:xxx  — verify server cert fingerprint on first connect
#   --key mykey          — registration key (if server requires one)
#   --version 1.2.3      — install a specific version (default: latest)

# ── Parse arguments ──────────────────────────────────────────────────
RIOT_SERVER=""
RIOT_KEY=""
RIOT_FINGERPRINT=""
RIOT_VERSION="latest"

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
        --version)
            RIOT_VERSION="${2:-latest}"
            shift 2
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
    echo "ERROR: Server URL is required."
    echo "Usage: curl -sSL .../install.sh | sudo bash -s -- https://server:7331"
    exit 1
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

# ── Detect Docker for config ─────────────────────────────────────────
DOCKER_SECTION=""
if command -v docker >/dev/null 2>&1; then
    echo "==> Docker detected, enabling container monitoring"
    DOCKER_SECTION="
docker:
  enabled: auto
  collect_stats: true
  terminal_enabled: false"
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

# ── Write config (skip if already exists) ─────────────────────────────
if [ ! -f "$RIOT_CONFIG_DIR/agent.yaml" ]; then
    echo "==> Writing default config to ${RIOT_CONFIG_DIR}/agent.yaml"
    cat > "$RIOT_CONFIG_DIR/agent.yaml" <<EOF
server:
  url: "${RIOT_SERVER}"
  tls_verify: true${API_KEY_LINE}${CERT_PIN_LINE}

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
SUPPLEMENTARY_GROUPS=""
if getent group docker >/dev/null 2>&1; then
    SUPPLEMENTARY_GROUPS="SupplementaryGroups=docker"
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
riot ALL=(root) NOPASSWD: /usr/bin/install -m 755 ${RIOT_DATA_DIR}/riot-agent.update ${RIOT_BIN}
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
