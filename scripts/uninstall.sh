#!/usr/bin/env bash
set -euo pipefail

# rIOt Agent Uninstall Script
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/uninstall.sh | sudo bash
#
# Options:
#   --keep-config   Don't remove /etc/riot (preserves agent.yaml and device ID)

KEEP_CONFIG=false
for arg in "$@"; do
    case "$arg" in
        --keep-config) KEEP_CONFIG=true ;;
    esac
done

RIOT_USER="riot"
RIOT_CONFIG_DIR="/etc/riot"
RIOT_DATA_DIR="/var/lib/riot"
RIOT_BIN="/usr/local/bin/riot-agent"
RIOT_SERVICE="/etc/systemd/system/riot-agent.service"

echo "==> rIOt Agent Uninstaller"

# ── Stop and disable systemd service ─────────────────────────────────
if command -v systemctl >/dev/null 2>&1; then
    if systemctl is-active riot-agent >/dev/null 2>&1; then
        echo "==> Stopping riot-agent service"
        systemctl stop riot-agent
    fi
    if systemctl is-enabled riot-agent >/dev/null 2>&1; then
        echo "==> Disabling riot-agent service"
        systemctl disable riot-agent
    fi
    if [ -f "$RIOT_SERVICE" ]; then
        echo "==> Removing systemd unit"
        rm -f "$RIOT_SERVICE"
        systemctl daemon-reload
    fi
fi

# ── Remove binary ────────────────────────────────────────────────────
if [ -f "$RIOT_BIN" ]; then
    echo "==> Removing binary: $RIOT_BIN"
    rm -f "$RIOT_BIN"
fi

# ── Remove data directory ────────────────────────────────────────────
if [ -d "$RIOT_DATA_DIR" ]; then
    echo "==> Removing data directory: $RIOT_DATA_DIR"
    rm -rf "$RIOT_DATA_DIR"
fi

# ── Remove config directory ──────────────────────────────────────────
if [ "$KEEP_CONFIG" = true ]; then
    echo "==> Keeping config directory: $RIOT_CONFIG_DIR (--keep-config)"
else
    if [ -d "$RIOT_CONFIG_DIR" ]; then
        echo "==> Removing config directory: $RIOT_CONFIG_DIR"
        rm -rf "$RIOT_CONFIG_DIR"
    fi
fi

# ── Remove system user ──────────────────────────────────────────────
if id -u "$RIOT_USER" >/dev/null 2>&1; then
    echo "==> Removing system user: $RIOT_USER"
    userdel "$RIOT_USER" 2>/dev/null || true
fi

echo ""
echo "==> rIOt agent uninstalled."
if [ "$KEEP_CONFIG" = true ]; then
    echo "    Config preserved at: $RIOT_CONFIG_DIR"
    echo "    To fully remove: rm -rf $RIOT_CONFIG_DIR"
fi
