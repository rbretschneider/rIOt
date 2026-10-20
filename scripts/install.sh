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
    aarch64|arm64) SUFFIX="linux-arm64" ;;
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

# Authenticate every asset before changing users, configuration, policy or services.
[ "$(id -u)" = 0 ] || { echo "ERROR: Run this installer as root"; exit 1; }
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077
RELEASE_PUBLIC_KEY='31W+lKF8LVms4ar/Qb9DI0//nj8CUT3zEx2aRhVbBjY='
RIOT_APPLY_DIR=/usr/local/lib/riot
RIOT_APPLY_BIN=${RIOT_APPLY_DIR}/riot-agent-apply
MAINT_BIN=${RIOT_APPLY_DIR}/riot-maintenance
STAGE=$(mktemp -d /tmp/riot_install.XXXXXXXX)
chmod 0700 "$STAGE"
NATIVE_LOCK_PID=""
cleanup() {
    if [ -n "$NATIVE_LOCK_PID" ]; then
        kill "$NATIVE_LOCK_PID" 2>/dev/null || true
        wait "$NATIVE_LOCK_PID" 2>/dev/null || true
    fi
    rm -rf -- "$STAGE"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
fail() { echo "ERROR: $*" >&2; exit 1; }
atomic_install() {
    local source="$1" target="$2" mode="$3" fresh
    fresh=$(mktemp "$(dirname "$target")/.riot_install.XXXXXXXX")
    install -m "$mode" -o root -g root "$source" "$fresh"
    mv -fT -- "$fresh" "$target"
}
restore_policy() {
    if [ -f "$BACKUP/etc/sudoers.d/riot-agent" ]; then
        atomic_install "$BACKUP/etc/sudoers.d/riot-agent" /etc/sudoers.d/riot-agent 0440
    else
        rm -f -- /etc/sudoers.d/riot-agent
    fi
}

download_file() {
    if command -v curl >/dev/null 2>&1; then
        curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL -o "$2" "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget --https-only -qO "$2" "$1"
    else
        fail "curl or wget is required"
    fi
}
if [ "$RIOT_VERSION" = latest ]; then
    RELEASE_URL="https://github.com/${RIOT_REPO}/releases/latest/download"
else
    [[ "$RIOT_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || fail "Invalid release version"
    RELEASE_URL="https://github.com/${RIOT_REPO}/releases/download/v${RIOT_VERSION}"
fi
for metadata in checksums.txt checksums.txt.sig; do
    download_file "$RELEASE_URL/$metadata" "$STAGE/$metadata"
done
[ "$(wc -c < "$STAGE/checksums.txt")" -le 1048576 ] || fail "Release manifest is too large"
[ "$(wc -c < "$STAGE/checksums.txt.sig")" -le 4096 ] || fail "Release signature is too large"
# OpenSSL 3 supports Ed25519 raw verification. Do not install a verifier through apt.
if command -v openssl >/dev/null 2>&1 && [[ "$(openssl version)" =~ ^OpenSSL\ [3-9]\. ]]; then
    { printf '\x30\x2a\x30\x05\x06\x03\x2b\x65\x70\x03\x21\x00'; printf '%s' "$RELEASE_PUBLIC_KEY" | openssl base64 -d -A; } > "$STAGE/release.der"
    tr -d '[:space:]' < "$STAGE/checksums.txt.sig" | openssl base64 -d -A > "$STAGE/signature.raw"
    [ "$(wc -c < "$STAGE/signature.raw")" -eq 64 ] || fail "Invalid Ed25519 signature encoding"
    openssl pkeyutl -verify -pubin -inkey "$STAGE/release.der" -keyform DER -rawin -in "$STAGE/checksums.txt" -sigfile "$STAGE/signature.raw" >/dev/null 2>&1 || fail "Release signature verification failed"
elif command -v python3 >/dev/null 2>&1; then
    python3 - "$STAGE" "$RELEASE_PUBLIC_KEY" <<'PY'
import base64, pathlib, sys
try:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    stage = pathlib.Path(sys.argv[1])
    key = base64.b64decode(sys.argv[2], validate=True)
    sig = base64.b64decode((stage / 'checksums.txt.sig').read_bytes().strip(), validate=True)
    Ed25519PublicKey.from_public_bytes(key).verify(sig, (stage / 'checksums.txt').read_bytes())
except Exception:
    sys.exit('ERROR: preinstalled Python cryptography and a valid release signature are required')
PY
else
    fail "Install OpenSSL 3 or Python3 cryptography through your normal administrative process first"
fi
verify_asset() {
    local asset="$1" digest actual count
    count=$(awk -v name="$asset" '$2 == name || $2 == "*"name {n++} END {print n+0}' "$STAGE/checksums.txt")
    [ "$count" -eq 1 ] || fail "Missing or duplicate signed asset: $asset"
    digest=$(awk -v name="$asset" '$2 == name || $2 == "*"name {print $1}' "$STAGE/checksums.txt")
    [[ "$digest" =~ ^[0-9a-fA-F]{64}$ ]] || fail "Invalid checksum for $asset"
    download_file "$RELEASE_URL/$asset" "$STAGE/$asset"
    [ "$(wc -c < "$STAGE/$asset")" -le 134217728 ] || fail "Oversize asset: $asset"
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$STAGE/$asset" | awk '{print $1}')
    else
        actual=$(shasum -a 256 "$STAGE/$asset" | awk '{print $1}')
    fi
    [ "$(printf '%s' "$actual" | tr A-F a-f)" = "$(printf '%s' "$digest" | tr A-F a-f)" ] || fail "Signed checksum mismatch: $asset"
}
verify_asset "$BINARY_NAME"
if [ "$OS" = linux ]; then
    verify_asset "riot-agent-apply-${SUFFIX}"
    verify_asset "riot-maintenance-${SUFFIX}"
    verify_asset riot-maintenance.service
fi

# Validate privileged path parents before using them as root write destinations.
secure_dir() {
    local path="$1" mode owner
    [ -d "$path" ] && [ ! -L "$path" ] || fail "Unsafe directory: $path"
    owner=$(stat -c %u "$path"); mode=$(stat -c %a "$path")
    [ "$owner" -eq 0 ] && (( (8#$mode & 0022) == 0 )) || fail "Directory must be root-owned and not group/world writable: $path"
}
secure_file() {
    local path="$1" mode
    [ -f "$path" ] && [ ! -L "$path" ] && [ "$(stat -c %u "$path")" -eq 0 ] || fail "Unsafe privileged file: $path"
    mode=$(stat -c %a "$path")
    (( (8#$mode & 0022) == 0 )) || fail "Writable privileged file: $path"
}
MAINT_SUPPORTED=false
if [ "$OS" = linux ]; then
    for tool in systemctl systemd-run systemd-analyze visudo flock install; do
        command -v "$tool" >/dev/null 2>&1 || fail "$tool is required; install prerequisites separately"
        secure_file "$(readlink -f "$(command -v "$tool")")"
    done
    SYSTEMCTL_PATH=$(command -v systemctl)
    SYSTEMD_RUN_PATH=$(command -v systemd-run)
    for directory in /usr /usr/local /usr/local/bin /usr/local/lib /etc /etc/systemd /etc/systemd/system /etc/sudoers.d /var /var/lib /var/backups /run; do secure_dir "$directory"; done
    if [ -e "$RIOT_APPLY_DIR" ] || [ -L "$RIOT_APPLY_DIR" ]; then secure_dir "$RIOT_APPLY_DIR"; fi
    if [ -e /etc/systemd/system/riot-agent.service.d ] || [ -L /etc/systemd/system/riot-agent.service.d ]; then
        secure_dir /etc/systemd/system/riot-agent.service.d
    fi
    for directory in "$RIOT_CONFIG_DIR" "$RIOT_DATA_DIR"; do
        [ ! -L "$directory" ] || fail "Refusing symlink directory: $directory"
        if [ -e "$directory" ]; then [ -d "$directory" ] || fail "Expected directory: $directory"; fi
    done
    if command -v apt-get >/dev/null 2>&1; then
        apt_major=$(/usr/bin/apt-get --version | awk 'NR==1 {split($2,v,"."); print v[1]}')
        systemd_major=$(systemctl --version | awk 'NR==1 {print $2}')
        [[ "$apt_major" =~ ^[0-9]+$ && "$systemd_major" =~ ^[0-9]+$ ]] || fail "Cannot determine apt/systemd versions"
        [ "$apt_major" -ge 2 ] && [ "$systemd_major" -ge 241 ] || fail "Detached apt requires apt2+/systemd241+"
        for tool in /usr/bin/apt-get /usr/bin/dpkg /usr/bin/dpkg-query /usr/bin/apt-mark; do secure_file "$tool"; done
        command -v python3 >/dev/null 2>&1 || fail "Preinstalled Python3 is required for native package-lock-safe migration"
        secure_file "$(readlink -f "$(command -v python3)")"
        MAINT_SUPPORTED=true
    fi
    # The lock's root-owned inode lives in the normal sticky root-owned lock dir.
    [ -d /run/lock ] && [ ! -L /run/lock ] && [ "$(stat -c %u /run/lock)" -eq 0 ] || fail "Unsafe lock directory"
    lock_mode=$(stat -c %a /run/lock)
    (( (8#$lock_mode & 0022) == 0 || (8#$lock_mode & 01000) != 0 )) || fail "Unsafe lock directory permissions"
    if [ ! -e /run/lock/riot-maintenance.lock ] && [ ! -L /run/lock/riot-maintenance.lock ]; then
        (set -o noclobber; : > /run/lock/riot-maintenance.lock) || fail "Cannot create exclusion lock"
    fi
    secure_file /run/lock/riot-maintenance.lock
    [ "$(stat -c %h /run/lock/riot-maintenance.lock)" -eq 1 ] || fail "Exclusion lock must have one link"
    [ "$(stat -c %a /run/lock/riot-maintenance.lock)" = 600 ] || fail "Exclusion lock must be0600"
    exec 9>>/run/lock/riot-maintenance.lock
    flock -n 9 || fail "Maintenance/apply/installer is busy; no files changed"
    state=$(systemctl show riot-maintenance.service -p ActiveState --value) || fail "Cannot inspect maintenance service"
    case "$state" in inactive|failed) ;; *) fail "Maintenance service is $state; refusing replacement" ;; esac
    if [ -e /var/lib/riot-maintenance ] || [ -L /var/lib/riot-maintenance ]; then
        secure_dir /var/lib/riot-maintenance
        if [ -e /var/lib/riot-maintenance/current.json ] || [ -L /var/lib/riot-maintenance/current.json ]; then
            secure_file /var/lib/riot-maintenance/current.json
            command -v python3 >/dev/null 2>&1 || fail "Python3 is required to validate existing maintenance state"
            python3 - <<'PY'
import datetime, json, os, sys, uuid
p = '/var/lib/riot-maintenance/current.json'
if os.stat(p).st_size > 2*1024*1024: sys.exit('ERROR: oversized maintenance state')
def unique(pairs):
    d = {}
    for k, v in pairs:
        if k in d: raise ValueError('duplicate field')
        d[k] = v
    return d
try:
    d = json.load(open(p), object_pairs_hook=unique)
    assert type(d['version']) is int and d['version'] == 1
    assert str(uuid.UUID(d['command_id'])) == d['command_id'] and uuid.UUID(d['command_id']).int != 0
    assert d['mode'] in ('full','security','repair') and type(d['include_reboot_class']) is bool
    assert d['mode'] != 'repair' or d['include_reboot_class'] is False
    assert d['outcome'] in ('success','error','interrupted') and d['phase'] == 'finished'
    times = [datetime.datetime.fromisoformat(d[k].replace('Z', '+00:00')) for k in ('started_at','updated_at','finished_at')]
    assert all(t.tzinfo is not None for t in times) and times[0] <= times[1] <= times[2]
    assert d['holds_released'] is False and d['hold_cleanup_failed'] is False
    assert type(d['interrupted']) is bool and type(d['exit_code']) is int
    assert d.get('hold_restore_error', '') == ''
    assert isinstance(d.get('output',''), str) and len(d.get('output','').encode()) <= 256*1024
    assert isinstance(d.get('summary',''), str) and len(d.get('summary','').encode()) <= 4096
    if d['outcome'] == 'success': assert d['exit_code'] == 0 and d['interrupted'] is False
except Exception:
    sys.exit('ERROR: unfinished or invalid maintenance state; recover it before installing')
PY
        fi
    fi
    if [ "$MAINT_SUPPORTED" = true ]; then
        # flock does not see apt/dpkg's POSIX record locks. Hold all four native
        # locks in an independent, bounded helper through stop and replacement.
        python3 - "$STAGE" "$$" <<'PY' &
import fcntl, os, pathlib, signal, stat, sys, time
stage, parent = pathlib.Path(sys.argv[1]), int(sys.argv[2])
fds = []
try:
    for path in ('/var/lib/dpkg/lock-frontend', '/var/lib/dpkg/lock', '/var/lib/apt/lists/lock', '/var/cache/apt/archives/lock'):
        fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
        try:
            parts = pathlib.PurePosixPath(path).parts[1:]
            for part in parts[:-1]:
                nxt = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
                os.close(fd); fd = nxt
                s = os.fstat(fd)
                if s.st_uid != 0 or s.st_mode & 0o022: raise ValueError('unsafe native lock parent')
            lock = os.open(parts[-1], os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600, dir_fd=fd)
        finally:
            os.close(fd)
        fds.append(lock)
        s = os.fstat(lock)
        if not stat.S_ISREG(s.st_mode) or s.st_uid != 0 or s.st_nlink != 1 or s.st_mode & 0o022:
            raise ValueError('unsafe native package lock')
        fcntl.lockf(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    (stage / 'native-lock-ready').write_text('ok')
    deadline = time.monotonic() + 15*60
    while os.getppid() == parent:
        if time.monotonic() >= deadline:
            # Never quietly drop exclusion while an installer could still replace files.
            os.kill(parent, signal.SIGTERM)
            time.sleep(3)
            if os.getppid() == parent: os.kill(parent, signal.SIGKILL)
            break
        time.sleep(0.2)
except Exception:
    (stage / 'native-lock-ready').write_text('error: package manager busy or unsafe native lock; no replacement allowed')
finally:
    for fd in fds: os.close(fd)
PY
        NATIVE_LOCK_PID=$!
        for _ in {1..100}; do
            [ ! -f "$STAGE/native-lock-ready" ] || break
            kill -0 "$NATIVE_LOCK_PID" 2>/dev/null || fail "Native package lock holder failed"
            sleep 0.05
        done
        [ -f "$STAGE/native-lock-ready" ] && [ "$(cat "$STAGE/native-lock-ready")" = ok ] || fail "Native package locks are busy, unsafe or unavailable"
    fi
    # Validate the entire existing policy first; unrelated invalid files are never edited.
    visudo -cf /etc/sudoers >/dev/null || fail "Existing sudo policy is invalid; resolve it before installing"
    {
        echo '# rIOt fixed privileges; package transactions run only in the detached worker.'
        if [ "$MAINT_SUPPORTED" = true ]; then
            echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} start --no-block riot-maintenance.service"
            echo 'riot ALL=(root) NOPASSWD: /usr/bin/apt-mark hold *'
            echo 'riot ALL=(root) NOPASSWD: /usr/bin/apt-mark unhold *'
        elif command -v dnf >/dev/null 2>&1; then
            DNF_PATH=$(command -v dnf)
            echo "riot ALL=(root) NOPASSWD: ${DNF_PATH} makecache"
            echo "riot ALL=(root) NOPASSWD: ${DNF_PATH} -y update"
            echo "riot ALL=(root) NOPASSWD: ${DNF_PATH} -y --security update"
            echo 'riot ALL=(root) NOPASSWD: /usr/bin/install -m 0644 -o root -g root /var/lib/riot/dnf-holds.staged /etc/dnf/libdnf5.conf.d/60-riot-holds.conf'
            echo 'riot ALL=(root) NOPASSWD: /usr/bin/rm -f /etc/dnf/libdnf5.conf.d/60-riot-holds.conf'
        fi
        echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} reboot"
        echo "riot ALL=(root) NOPASSWD: ${SYSTEMD_RUN_PATH} --unit=riot-agent-update ${RIOT_APPLY_BIN}"
        echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} reset-failed riot-agent-update"
        if command -v nginx >/dev/null 2>&1; then
            NGINX_PATH=$(command -v nginx)
            echo "riot ALL=(root) NOPASSWD: ${NGINX_PATH} -t"
            echo "riot ALL=(root) NOPASSWD: ${NGINX_PATH} -T"
        fi
        if command -v smartctl >/dev/null 2>&1; then
            SMARTCTL_PATH=$(command -v smartctl)
            echo "riot ALL=(root) NOPASSWD: ${SMARTCTL_PATH} --version"
            for disk in /sys/block/*; do
                [ -e "$disk" ] || continue
                disk=${disk##*/}
                [[ "$disk" =~ ^(sd[a-z]+|hd[a-z]+|vd[a-z]+|xvd[a-z]+|nvme[0-9]+n[0-9]+|mmcblk[0-9]+)$ ]] || continue
                echo "riot ALL=(root) NOPASSWD: ${SMARTCTL_PATH} --json=c --all /dev/${disk}"
            done
        else
            echo '# smartmontools absent: install separately if SMART monitoring is wanted.'
        fi
    } > "$STAGE/riot-agent.sudoers"
    chmod 0440 "$STAGE/riot-agent.sudoers"
    visudo -cf "$STAGE/riot-agent.sudoers" >/dev/null || fail "Candidate sudo policy is invalid"
    cat > "$STAGE/riot-agent.service" <<EOF
[Unit]
Description=rIOt Agent - Infrastructure Monitoring
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
ExecStartPre=-+${RIOT_APPLY_BIN} --boot
ExecStart=${RIOT_BIN} -config ${RIOT_CONFIG_DIR}/agent.yaml
Restart=always
RestartSec=5
User=riot
Group=riot
SupplementaryGroups=$(for group in docker systemd-journal; do getent group "$group" >/dev/null && printf '%s ' "$group"; done)
LimitNOFILE=65536
MemoryMax=50%
Environment=GOMEMLIMIT=200MiB
OOMPolicy=continue
ProtectSystem=false
ReadWritePaths=${RIOT_DATA_DIR} ${RIOT_CONFIG_DIR}
PrivateTmp=true
[Install]
WantedBy=multi-user.target
EOF
    mkdir "$STAGE/verify"
    chmod 0755 "$STAGE/$BINARY_NAME" "$STAGE/riot-agent-apply-${SUFFIX}" "$STAGE/riot-maintenance-${SUFFIX}"
    # Verify syntax using authenticated executable snapshots before replacing live files.
    sed -e "s|${RIOT_APPLY_BIN}|$STAGE/riot-agent-apply-${SUFFIX}|g" -e "s|${RIOT_BIN}|$STAGE/$BINARY_NAME|g" "$STAGE/riot-agent.service" > "$STAGE/verify/riot-agent.service"
    sed "s|${MAINT_BIN}|$STAGE/riot-maintenance-${SUFFIX}|g" "$STAGE/riot-maintenance.service" > "$STAGE/verify/riot-maintenance.service"
    systemd-analyze verify "$STAGE/verify/riot-agent.service" "$STAGE/verify/riot-maintenance.service" || fail "Candidate units did not validate"
    # Preserve current config, binaries and reviewed policy before the first mutation.
    BACKUP=$(mktemp -d /var/backups/riot_install.XXXXXXXX)
    chmod 0700 "$BACKUP"
    for path in "$RIOT_BIN" "$RIOT_APPLY_DIR" /etc/riot /etc/sudoers.d/riot-agent /etc/systemd/system/riot-agent.service /etc/systemd/system/riot-agent.service.d /etc/systemd/system/riot-maintenance.service; do
        if [ -e "$path" ] || [ -L "$path" ]; then
            mkdir -p "$BACKUP$(dirname "$path")"
            cp -a -- "$path" "$BACKUP$path"
        fi
    done
    echo "==> Existing files preserved in $BACKUP"
    # Validate the effective policy before stopping the old agent or replacing
    # any executable/unit. A failed merged policy restores the working drop-in.
    atomic_install "$STAGE/riot-agent.sudoers" /etc/sudoers.d/riot-agent 0440
    if ! visudo -cf /etc/sudoers >/dev/null; then
        restore_policy
        fail "Effective candidate policy failed; previous managed policy restored"
    fi
    if ! id -u riot >/dev/null 2>&1; then
        useradd --system --no-create-home --shell "$(command -v nologin)" riot
    fi
    for group in docker systemd-journal; do
        if getent group "$group" >/dev/null; then usermod -aG "$group" riot; fi
    done
    # Stop only after lock, signature and candidate validation. No package operation runs.
    if [ "$(systemctl show riot-agent.service -p LoadState --value)" != not-found ]; then
        systemctl stop riot-agent
    fi
    for directory in "$RIOT_CONFIG_DIR" "$RIOT_DATA_DIR"; do
        [ ! -L "$directory" ] || fail "Refusing symlink directory: $directory"
    done
    mkdir -p "$RIOT_CONFIG_DIR" "$RIOT_DATA_DIR"
    chown root:root "$RIOT_CONFIG_DIR"
    chmod 0700 "$RIOT_CONFIG_DIR"
    chown riot:riot "$RIOT_DATA_DIR"
    mkdir -p "$RIOT_APPLY_DIR" /var/lib/riot-maintenance
    chmod 0755 "$RIOT_APPLY_DIR"
    chown root:root "$RIOT_APPLY_DIR"
    chown root:riot /var/lib/riot-maintenance
    chmod 0750 /var/lib/riot-maintenance
else
    mkdir -p "$RIOT_CONFIG_DIR" "$RIOT_DATA_DIR"
fi
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
  terminal_enabled: ${ALLOW_DOCKER_TERMINAL}
  # compose_roots: []      # Directories compose updates may run in, e.g. [/opt/stacks] (required on Windows)"
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

# Fresh temporary inodes prevent retained write descriptors from modifying an installed binary.
if [ "$OS" = linux ]; then
    atomic_install "$STAGE/$BINARY_NAME" "$RIOT_BIN" 0755
    atomic_install "$STAGE/riot-agent-apply-${SUFFIX}" "$RIOT_APPLY_BIN" 0755
    atomic_install "$STAGE/riot-maintenance-${SUFFIX}" "$MAINT_BIN" 0755
    atomic_install "$STAGE/riot-maintenance.service" /etc/systemd/system/riot-maintenance.service 0644
    atomic_install "$STAGE/riot-agent.service" /etc/systemd/system/riot-agent.service 0644
    mkdir -p /etc/systemd/system/riot-agent.service.d
    secure_dir /etc/systemd/system/riot-agent.service.d
    cat > "$STAGE/zzzz-riot-signed-boot.conf" <<EOF
[Service]
ExecStartPre=
ExecStartPre=-+${RIOT_APPLY_BIN} --boot
EOF
    atomic_install "$STAGE/zzzz-riot-signed-boot.conf" /etc/systemd/system/riot-agent.service.d/zzzz-riot-signed-boot.conf 0644
    chown riot:riot "$RIOT_CONFIG_DIR"
    chmod 0700 "$RIOT_CONFIG_DIR"
    if ! visudo -cf /etc/sudoers >/dev/null; then
        restore_policy
        fail "Effective policy changed during installation; previous managed policy restored; backup: $BACKUP"
    fi
    systemctl daemon-reload
    pre=$(systemctl show riot-agent.service -p ExecStartPre --value) || fail "Cannot inspect effective agent precommand"
    [[ "$pre" == *"path=${RIOT_APPLY_BIN} ;"* && "$pre" == *"argv[]=${RIOT_APPLY_BIN} --boot ;"* ]] || fail "Unexpected effective boot hook; review preserved drop-ins before starting"
    [ "$(printf '%s' "$pre" | grep -o 'path=' | wc -l)" -eq 1 ] || fail "Unexpected additional boot hooks; review preserved drop-ins before starting"
    systemctl enable riot-agent
    # The installer owns exclusion here, so boot verification safely defers staged updates.
    systemctl start riot-agent
    echo "==> rIOt installed; detached apt maintenance: $MAINT_SUPPORTED"
    echo "    Config: ${RIOT_CONFIG_DIR}/agent.yaml"
    echo "    Backup: $BACKUP"
    echo '    Run riot-agent doctor to inspect readiness without package changes.'
else
    install -m 0755 "$STAGE/$BINARY_NAME" "$RIOT_BIN"
    echo "==> rIOt agent installed at $RIOT_BIN; run manually with -config ${RIOT_CONFIG_DIR}/agent.yaml"
fi
