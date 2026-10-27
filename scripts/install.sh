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
# True from the moment this run installs a drop-in carrying the poweroff grant
# until the config and the grant are confirmed to agree. While it is true, any
# exit restores the previous managed policy (SHUTDOWN-SUDO-001 AD-006).
GRANT_PENDING=false
cleanup() {
    if [ "$GRANT_PENDING" = true ]; then
        GRANT_PENDING=false
        restore_policy || echo "WARNING: could not restore the previous rIOt sudo policy" >&2
    fi
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

# The agent config is writable by the unprivileged riot user, so the installer
# reads it as untrusted input. The reader accepts exactly one spelling and
# rejects the whole file on anything it cannot classify. It runs inside "if"
# conditions, where set -e is off, so every step ends in "|| return 1".
# EXTRA-UPD-001: the same single pass also reads commands.allow_extra_updaters.
# It prints one fact per line: "shutdown" when allow_shutdown is a plain true,
# and "extra ID" for each identifier of a list of unquoted names directly under
# commands. A list in any other shape yields no identifiers and does not reject
# the file; every structural problem still rejects the whole file, so both
# facts fail closed together.
# BEGIN shutdown-config-classifier
config_facts() {
    local p="$1" snap="$STAGE/shutdown-config.snapshot" size plain out
    [ -f "$p" ] && [ ! -L "$p" ] || return 1
    timeout 5 head -c 65537 -- "$p" > "$snap" 2>/dev/null || return 1
    size=$(wc -c < "$snap") || return 1
    [ "$size" -le 65536 ] || return 1
    plain=$(LC_ALL=C tr -d '\000' < "$snap" | wc -c) || return 1
    [ "$plain" -eq "$size" ] || return 1
    out=$(LC_ALL=C awk '
function reject() { bad = 1; exit 0 }
function cnt(s, ch,    n, i) {
    n = 0
    for (i = 1; i <= length(s); i++) if (substr(s, i, 1) == ch) n++
    return n
}
function chk(t,    c, u, q) {
    c = substr(t, 1, 1)
    if (c == "|" || c == ">" || c == "&" || c == "*" || c == "!" || c == "%" || c == "@" || c == bt) reject()
    if (c == dq) {
        u = substr(t, 2)
        gsub(/\\\\/, "", u)
        gsub(/\\"/, "", u)
        q = index(u, dq)
        if (q == 0) reject()
        u = substr(u, q + 1)
        if (u !~ /^ *$/ && u !~ /^ +#/ && u !~ /^ *:/) reject()
    } else if (c == sq) {
        u = substr(t, 2)
        gsub(sq sq, "", u)
        q = index(u, sq)
        if (q == 0) reject()
        u = substr(u, q + 1)
        if (u !~ /^ *$/ && u !~ /^ +#/ && u !~ /^ *:/) reject()
    } else if (c == "[") {
        if (cnt(t, "[") != cnt(t, "]")) reject()
    } else if (c == "{") {
        if (cnt(t, "{") != cnt(t, "}")) reject()
    }
}
function addid(id) {
    if (length(id) <= 32 && !(id in idseen)) { idseen[id] = 1; nids++; ids[nids] = id }
}
# Each name is checked on its own: mawk 1.3.4-20200120 (Debian 12, DietPi on
# bookworm) fails to match a repeated group such as "( *, *name)*", which
# silently dropped every flow list.
function listshape(val,    t, inner, n, parts, i, id) {
    t = val
    sub(/ +#.*$/, "", t)
    sub(/ +$/, "", t)
    if (substr(t, 1, 1) != "[" || substr(t, length(t)) != "]") return
    inner = substr(t, 2, length(t) - 2)
    if (inner ~ /^ *$/) return
    n = split(inner, parts, ",")
    for (i = 1; i <= n; i++) {
        id = parts[i]
        sub(/^ +/, "", id)
        sub(/ +$/, "", id)
        if (id !~ /^[a-z][a-z0-9_-]*$/) return
        parts[i] = id
    }
    for (i = 1; i <= n; i++) addid(parts[i])
}
BEGIN {
    sq = sprintf("%c", 39); dq = sprintf("%c", 34); bt = sprintf("%c", 96)
    tab = sprintf("%c", 9); cr = sprintf("%c", 13)
    bad = 0; seen = 0; docstart = 0; incmd = 0; ncmd = 0; cmdindent = -1; nshut = 0; shutval = ""
    inlist = 0; nids = 0
}
{
    line = $0
    if (index(line, cr) > 0) reject()
    if (line ~ /^ *$/) next
    if (line ~ /^ *#/) next
    match(line, /^ */)
    ind = RLENGTH
    rest = substr(line, ind + 1)
    if (substr(rest, 1, 1) == tab) reject()
    if (seen == 0 && ind > 0) reject()
    if (ind == 0) {
        if (line == "---" || substr(line, 1, 4) == "--- ") {
            if (seen > 0 || docstart > 0) reject()
            if (line != "---" && line !~ /^--- *$/ && line !~ /^--- +#/) reject()
            docstart = 1
            next
        }
        if (line == "..." || substr(line, 1, 4) == "... ") reject()
        if (substr(line, 1, 1) == "%") reject()
    }
    seen++
    s = rest
    isseq = 0
    while (substr(s, 1, 2) == "- ") { s = substr(s, 3); sub(/^ +/, "", s); isseq = 1 }
    v = s
    p = index(s, ": ")
    if (p > 0) v = substr(s, p + 2)
    else if (substr(s, length(s)) == ":") v = ""
    sub(/^ +/, "", v)
    chk(v)
    if (isseq) chk(s)
    if (ind == 0) {
        key = line
        sub(/:.*$/, "", key)
        if (key !~ /^[A-Za-z_][A-Za-z0-9_]*$/) reject()
        after = substr(line, length(key) + 2, 1)
        if (after != "" && after != " ") reject()
        if (key in top) reject()
        top[key] = 1
        incmd = 0
        if (key == "commands") {
            ncmd++
            if (line !~ /^commands: *$/ && line !~ /^commands: +#/) reject()
            incmd = 1
        }
        next
    }
    if (incmd) {
        if (inlist && ind > cmdindent) {
            if (line ~ /^ +- +[a-z][a-z0-9_-]* *$/ || line ~ /^ +- +[a-z][a-z0-9_-]* +#/) {
                id = rest
                sub(/^- +/, "", id)
                sub(/ .*$/, "", id)
                addid(id)
                next
            }
            reject()
        }
        inlist = 0
        if (rest ~ /^allow_extra_updaters:( |$)/) {
            if (cmdindent < 0) cmdindent = ind
            else if (ind != cmdindent) reject()
            if ("allow_extra_updaters" in ck) reject()
            ck["allow_extra_updaters"] = 1
            val = rest
            sub(/^allow_extra_updaters: */, "", val)
            if (val == "" || val ~ /^#/) inlist = 1
            else listshape(val)
            next
        }
        if (line !~ /^ +[a-z_]+: +[A-Za-z0-9_.-]+ *$/ && line !~ /^ +[a-z_]+: +[A-Za-z0-9_.-]+ +#/) reject()
        if (cmdindent < 0) cmdindent = ind
        else if (ind != cmdindent) reject()
        k = rest
        sub(/:.*$/, "", k)
        if (k in ck) reject()
        ck[k] = 1
        val = rest
        sub(/^[a-z_]+: +/, "", val)
        sub(/ .*$/, "", val)
        if (k == "allow_shutdown") { nshut++; shutval = val }
    }
}
END {
    if (!bad && ncmd == 1) {
        if (nshut == 1 && shutval == "true") print "shutdown"
        for (i = 1; i <= nids; i++) print "extra " ids[i]
    }
}
' "$snap") || return 1
    printf '%s\n' "$out" || return 1
    return 0
}
# True when the facts of the config at $1 include the exact line $2.
config_has_fact() {
    local facts
    facts=$(config_facts "$1") || return 1
    case "
$facts
" in
        *"
$2
"*) return 0 ;;
    esac
    return 1
}
config_enables_shutdown() { config_has_fact "$1" shutdown || return 1; }
# True when commands.allow_extra_updaters lists the identifier $2 (a plain
# unquoted name in a flow list or a block list under commands).
config_lists_extra_updater() { config_has_fact "$1" "extra $2" || return 1; }
# END shutdown-config-classifier

# The agent config must be a regular file with a single link: a symlink,
# directory, FIFO or extra hard link is never read or written through.
refuse_unsafe_config_path() {
    local p="${RIOT_CONFIG_DIR}/agent.yaml" links
    if [ -e "$p" ] || [ -L "$p" ]; then
        [ -f "$p" ] || fail "${p} $1 is not a regular file; $2"
        links=$(stat -c %h -- "$p") || fail "Cannot inspect ${p}; $2"
        [ "$links" = 1 ] || fail "${p} $1 has ${links} links; $2"
    fi
}

# The drop-in was built from an earlier decision. Re-read the config and
# require the decision to still hold; otherwise put the old policy back. The
# DietPi opt-in (EXTRA-UPD-001) is one more decision made from the same file,
# so it is re-checked here too; nothing root-side for it is written before this
# check passes.
require_shutdown_agreement() {
    local final=false final_extra=false changed=""
    if config_enables_shutdown "${RIOT_CONFIG_DIR}/agent.yaml"; then final=true; fi
    if [ "$DIETPI_PRESENT" = true ] && config_lists_extra_updater "${RIOT_CONFIG_DIR}/agent.yaml" dietpi; then final_extra=true; fi
    [ "$final" = "$SHUTDOWN_GRANT" ] || changed="commands.allow_shutdown"
    [ "$final_extra" = "$EXTRA_GRANT" ] || changed="${changed:+$changed and }commands.allow_extra_updaters"
    if [ -n "$changed" ]; then
        GRANT_PENDING=false
        restore_policy
        fail "agent.yaml ${changed} changed during installation; previous managed policy restored; the agent is left stopped; re-run the installer"
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

# EXPECT-008: on a reinstall the installer stops a running agent, which the
# server would otherwise page as a device-offline outage. Just before that stop
# it sends one notice, authenticated with the device key, so the server expects
# the restart. It is best effort: it reads no installer input, prints exactly
# one line, takes at most 10 seconds and can never change the install result.
# agent.yaml is writable by the unprivileged riot user, so it is untrusted input:
# every value is validated before use, and the key reaches curl only through the
# printf builtin into curl's own config pipe, never on any command line.
# BEGIN reinstall-notice
# Copy a root-readable file into $2 from the descriptor that passed the checks:
# open first, then require the opened inode to equal the path's lstat, a regular
# file with one link. A path swapped before or after the open is refused.
notice_snapshot() {
    local src="$1" dst="$2" size plain
    [ -f "$src" ] && [ ! -L "$src" ] || return 1
    LC_ALL=C timeout 1 bash -c '
        exec 3<"$1" || exit 1
        a=$(stat -c "%d:%i:%h:%F" -- "$1") || exit 1
        b=$(stat -L -c "%d:%i:%h:%F" /proc/self/fd/3) || exit 1
        [ "$a" = "$b" ] || exit 1
        case "$a" in
            *:1:regular\ file | *:1:regular\ empty\ file) ;;
            *) exit 1 ;;
        esac
        head -c 65537 <&3
    ' notice-snapshot "$src" > "$dst" 2>/dev/null </dev/null || { rm -f -- "$dst"; return 1; }
    size=$(wc -c < "$dst") || return 1
    [ "$size" -le 65536 ] || return 1
    plain=$(LC_ALL=C tr -d '\000' < "$dst" | wc -c) || return 1
    [ "$plain" -eq "$size" ] || return 1
}
# One awk pass over a snapshot of agent.yaml. Prints name=value lines for the
# fields the notice needs, or exits non-zero to reject the whole file. mawk 1.3.4
# (Debian 12) has no interval expressions and no repeated groups, so none appear
# in any awk regex; the strict value checks are done in bash by the caller.
notice_facts() {
    LC_ALL=C awk '
function reject() { bad = 1; exit 1 }
function scalar(v,    c, rest, q, tail, t) {
    sub(/^ +/, "", v)
    if (v == "" || substr(v, 1, 1) == "#") return ""
    c = substr(v, 1, 1)
    if (c == dq || c == sq) {
        rest = substr(v, 2)
        q = index(rest, c)
        if (q == 0) reject()
        t = substr(rest, 1, q - 1)
        tail = substr(rest, q + 1)
        if (c == dq && index(t, bs) > 0) reject()
        if (tail !~ /^ *$/ && tail !~ /^ +#/) reject()
        return t
    }
    t = v
    sub(/ +#.*$/, "", t)
    sub(/ +$/, "", t)
    if (t !~ plainre) reject()
    return t
}
BEGIN {
    sq = sprintf("%c", 39); dq = sprintf("%c", 34); bs = sprintf("%c", 92)
    tab = sprintf("%c", 9); cr = sprintf("%c", 13)
    plainre = "^[-A-Za-z0-9._:/@+=~%_]+$"
    name["server.url"] = "url"; name["server.api_key"] = "key"; name["server.tls_verify"] = "verify"
    name["server.ca_cert_file"] = "ca"; name["server.client_cert"] = "cert"; name["server.client_key"] = "ckey"
    name["server.server_cert_pin"] = "pin"; name["agent.device_id"] = "id"
    order = "url key verify ca cert ckey pin id"
    bad = 0; seen = 0; docstart = 0; sec = ""; cind = -1; prevread = 0
}
{
    line = $0
    if (index(line, cr) > 0) reject()
    if (line ~ /^ *$/) next
    if (line ~ /^ *#/) next
    match(line, /^ */)
    ind = RLENGTH
    rest = substr(line, ind + 1)
    if (substr(rest, 1, 1) == tab) reject()
    if (ind == 0) {
        if (line == "---" || substr(line, 1, 4) == "--- ") {
            if (seen > 0 || docstart > 0) reject()
            if (line !~ /^--- *$/ && line !~ /^--- +#/) reject()
            docstart = 1
            next
        }
        if (line == "..." || substr(line, 1, 4) == "... ") reject()
        if (substr(line, 1, 1) == "%") reject()
    }
    seen++
    if (ind == 0) {
        k = line
        sub(/:.*$/, "", k)
        if (k !~ /^[A-Za-z_][A-Za-z0-9_]*$/) reject()
        after = substr(line, length(k) + 2, 1)
        if (after != "" && after != " ") reject()
        if (k in top) reject()
        top[k] = 1
        sec = ""; cind = -1; prevread = 0
        if (k == "server" || k == "agent") {
            v = substr(line, length(k) + 2)
            sub(/^ +/, "", v)
            if (v != "" && substr(v, 1, 1) != "#") reject()
            sec = k
        }
        next
    }
    if (sec == "") next
    if (cind < 0) cind = ind
    if (ind < cind) reject()
    if (ind > cind) {
        if (prevread) reject()
        next
    }
    prevread = 0
    if (substr(rest, 1, 2) == "- " || rest == "-") reject()
    k = rest
    sub(/:.*$/, "", k)
    if (k !~ /^[A-Za-z_][A-Za-z0-9_]*$/) reject()
    after = substr(rest, length(k) + 2, 1)
    if (after != "" && after != " ") reject()
    full = sec "." k
    if (full in name) {
        if (full in got) reject()
        got[full] = 1
        val[name[full]] = scalar(substr(rest, length(k) + 2))
        prevread = 1
    }
}
END {
    if (bad) exit 1
    n = split(order, names, " ")
    for (i = 1; i <= n; i++) if (names[i] in val) print names[i] "=" val[names[i]]
}
' "$1"
}
# The value of fact $2 in the notice_facts output $1; empty when absent.
notice_fact() {
    local f=$'\n'"$1" t
    case "$f" in
        *$'\n'"$2="*) t=${f#*$'\n'"$2="}; printf '%s' "${t%%$'\n'*}" ;;
    esac
    return 0
}
# The fixed text of a failure id, and nothing else, ever reaches the terminal.
notice_reason() {
    case "$1" in
        curl_missing) printf '%s' 'curl not available' ;;
        config) printf '%s' 'agent.yaml not readable' ;;
        verify_off) printf '%s' 'TLS verification is off in agent.yaml' ;;
        trust) printf '%s' 'trust settings not supported' ;;
        paused) printf '%s' 'reinstall tracking paused on server' ;;
        offline) printf '%s' 'device already offline on server' ;;
        unavailable) printf '%s' 'server could not record it' ;;
        unexpected) printf '%s' 'unexpected server response' ;;
        unsupported) printf '%s' 'server does not support reinstall notices' ;;
        key_rejected) printf '%s' 'device key rejected' ;;
        busy) printf '%s' 'server busy' ;;
        server_error) printf '%s' 'server error' ;;
        resolve) printf '%s' 'server name not resolved' ;;
        unreachable) printf '%s' 'server unreachable' ;;
        timeout) printf '%s' 'timed out' ;;
        tls) printf '%s' 'TLS verification failed' ;;
        internal) printf '%s' 'internal error' ;;
        conn) printf '%s' 'connection failed' ;;
        *) return 1 ;;
    esac
}
# The worker. Prints exactly one token and nothing else: skip, ok or fail:<id>.
# Every file it writes lives in $STAGE/notice. Nothing here reads stdin: awk,
# head, stat, wc and tr always get a file operand or the verified descriptor,
# and curl reads only its own config pipe.
notice_attempt() {
    local NS="$STAGE/notice" facts nid nkey url verify pin ca cert ckey scheme target
    local re_id='^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    local re_key='^riot_[0-9a-f]{64}$'
    local re_url='^(https?)://([A-Za-z0-9.-]+|\[[0-9A-Fa-f:.]+\])(:[0-9]{1,5})?(/[A-Za-z0-9._~%/-]*)?$'
    local re_pin='^SHA256:[0-9a-fA-F]{64}$'
    local re_disp='"reinstall":"([a-z_]+)"'
    local -a TLS_ARGS=() st
    local rc code left size body disp
    SECONDS=0
    command -v curl >/dev/null 2>&1 || { echo fail:curl_missing; return 0; }
    mkdir -m 0700 -- "$NS" "$NS/no-ca" || { echo fail:internal; return 0; }
    notice_snapshot "${RIOT_CONFIG_DIR}/agent.yaml" "$NS/agent.yaml" || { echo fail:config; return 0; }
    facts=$(notice_facts "$NS/agent.yaml") || { echo fail:config; return 0; }
    nid=$(notice_fact "$facts" id)
    nkey=$(notice_fact "$facts" key)
    if [ -z "$nid" ] || [ -z "$nkey" ]; then echo skip; return 0; fi
    [[ $nid =~ $re_id ]] || { echo fail:config; return 0; }
    [[ $nkey =~ $re_key ]] || { echo fail:config; return 0; }
    url=$(notice_fact "$facts" url)
    [[ $url =~ $re_url ]] || { echo fail:config; return 0; }
    scheme=${BASH_REMATCH[1]}
    verify=$(notice_fact "$facts" verify)
    case "$verify" in
        ''|true) verify=true ;;
        false) ;;
        *) echo fail:trust; return 0 ;;
    esac
    pin=$(notice_fact "$facts" pin)
    if [ -n "$pin" ]; then [[ $pin =~ $re_pin ]] || { echo fail:config; return 0; }; fi
    if [ "$scheme" = https ]; then
        TLS_ARGS=(--tlsv1.2)
        # The anchor set is exactly the snapshot: --cacert replaces only the CA
        # file, and curl also loads its compile-time default CA directory unless
        # --capath names another, so an empty root-only directory is always given.
        if [ -e "${RIOT_CONFIG_DIR}/server.crt" ] || [ -L "${RIOT_CONFIG_DIR}/server.crt" ]; then
            notice_snapshot "${RIOT_CONFIG_DIR}/server.crt" "$NS/ca.pem" || { echo fail:trust; return 0; }
            TLS_ARGS+=(--cacert "$NS/ca.pem" --capath "$NS/no-ca")
        else
            if [ "$verify" = false ]; then echo fail:verify_off; return 0; fi
            if [ -n "$pin" ]; then echo fail:trust; return 0; fi
            ca=$(notice_fact "$facts" ca)
            if [ -n "$ca" ]; then
                case "$ca" in
                    "${RIOT_CONFIG_DIR}/ca.crt"|"${RIOT_CONFIG_DIR}/server.crt") ;;
                    *) echo fail:trust; return 0 ;;
                esac
                notice_snapshot "$ca" "$NS/ca.pem" || { echo fail:trust; return 0; }
                TLS_ARGS+=(--cacert "$NS/ca.pem" --capath "$NS/no-ca")
            fi
        fi
        cert=$(notice_fact "$facts" cert)
        ckey=$(notice_fact "$facts" ckey)
        if [ -n "$cert" ] && [ -n "$ckey" ]; then
            [ "$cert" = "${RIOT_CONFIG_DIR}/client.crt" ] && [ "$ckey" = "${RIOT_CONFIG_DIR}/client.key" ] || { echo fail:trust; return 0; }
            notice_snapshot "$cert" "$NS/client.crt" || { echo fail:trust; return 0; }
            notice_snapshot "$ckey" "$NS/client.key" || { echo fail:trust; return 0; }
            # A fixed dummy passphrase keeps curl from ever asking for one. It is not
            # a secret, and curl 8.x rejects an empty --pass argument.
            TLS_ARGS+=(--cert "$NS/client.crt" --key "$NS/client.key" --pass riot-no-passphrase)
        fi
    fi
    target="${url}/api/v1/devices/${nid}/events"
    left=$((8 - SECONDS))
    [ "$left" -ge 3 ] || { echo fail:timeout; return 0; }
    printf 'header = "X-rIOt-Key: %s"\n' "$nkey" |
      env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C \
        timeout -k 1 "$left" curl -q --config - \
          --silent --noproxy '*' --proto "=$scheme" --max-redirs 0 \
          --connect-timeout 5 --max-time "$((left - 1))" \
          "${TLS_ARGS[@]}" \
          --user-agent riot-installer \
          --header 'Content-Type: application/json' \
          --data-binary '{"type":"agent_reinstall_started","severity":"info","message":"Agent reinstall started by installer"}' \
          --max-filesize 4096 \
          --write-out '%{stderr}%{http_code}' \
          --url "$target" 2>"$NS/status" |
      head -c 4096 >"$NS/response"
    st=("${PIPESTATUS[@]}")
    rc=${st[1]}
    code=$(head -c 16 "$NS/status")
    [[ $code =~ ^[0-9][0-9][0-9]$ ]] || code=000
    size=$(wc -c < "$NS/response")
    if [ "$size" -ge 4096 ]; then echo fail:unexpected; return 0; fi
    case "$rc" in
        0|23|141)
            case "$code" in
                200)
                    body=$(LC_ALL=C tr -d '\000' < "$NS/response")
                    disp=""
                    if [[ $body =~ $re_disp ]]; then disp=${BASH_REMATCH[1]}; fi
                    case "$disp" in
                        created|open_record) echo ok ;;
                        capped) echo fail:paused ;;
                        device_offline) echo fail:offline ;;
                        unavailable) echo fail:unavailable ;;
                        *) echo fail:unexpected ;;
                    esac ;;
                400|404|405) echo fail:unsupported ;;
                401|403) echo fail:key_rejected ;;
                429) echo fail:busy ;;
                5??) echo fail:server_error ;;
                *) echo fail:unexpected ;;
            esac ;;
        63) echo fail:unexpected ;;
        6) echo fail:resolve ;;
        7) echo fail:unreachable ;;
        28|124|137) echo fail:timeout ;;
        35|51|53|54|58|59|60|64|66|77|80|82|83|90|91) echo fail:tls ;;
        126|127) echo fail:internal ;;
        *) echo fail:conn ;;
    esac
    return 0
}
# The wrapper. A subshell: it does not inherit the EXIT trap, turns off errexit,
# nounset, pipefail and xtrace, discards stderr, and prints one line chosen from
# fixed text by the worker's token. Anything it cannot recognise, including a
# crashed or noisy worker, is reported as an internal error.
reinstall_notice() (
    { set +x; } 2>/dev/null
    exec </dev/null 2>/dev/null
    set +e +u +o pipefail
    LC_ALL=C
    export LC_ALL
    umask 077
    tok=$(notice_attempt | head -c 128)
    case "$tok" in
        skip) ;;
        ok) printf '%s\n' '==> Server notified: agent reinstall (offline alerts paused up to 10m)' ;;
        fail:*)
            reason=$(notice_reason "${tok#fail:}") || reason=$(notice_reason internal)
            printf '==> Server not notified (%s); a device-offline alert may follow\n' "$reason" ;;
        *) printf '%s\n' '==> Server not notified (internal error); a device-offline alert may follow' ;;
    esac
    rm -rf -- "$STAGE/notice"
    exit 0
)
# END reinstall-notice
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

# ── Refuse an unsafe agent.yaml before anything is changed ───────────
if [ "$OS" = linux ]; then
    refuse_unsafe_config_path "exists but" "move it aside and re-run; no files changed"
fi
# A reinstall keeps the existing config; a fresh install asks the questions.
# The questions come first, before any lock is taken or file changed, because
# the sudoers policy built below depends on the shutdown answer.
CONFIG_EXISTED=true
[ -f "$RIOT_CONFIG_DIR/agent.yaml" ] || CONFIG_EXISTED=false

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

# ── Detect DietPi (Linux) ────────────────────────────────────────────
# Present means both files are regular files and not links, the rule the
# agent detector uses too (EXTRA-UPD-001 FR-008). It only decides whether the
# DietPi question is asked and whether a root-side opt-in may be recorded.
DIETPI_PRESENT=false
if [ "$OS" = "linux" ] && [ -f /boot/dietpi/.version ] && [ ! -L /boot/dietpi/.version ] && [ -f /boot/dietpi/dietpi-update ] && [ ! -L /boot/dietpi/dietpi-update ]; then
    DIETPI_PRESENT=true
fi

# ── Detect Docker for config ─────────────────────────────────────────
HAS_DOCKER="false"
if command -v docker >/dev/null 2>&1; then
    HAS_DOCKER="true"
    echo "==> Docker detected, enabling container monitoring"
fi

# ── Interactive feature configuration ────────────────────────────────
ALLOW_REBOOT="false"
ALLOW_SHUTDOWN="false"
ALLOW_PATCHING="false"
ALLOW_EXTRA_DIETPI="false"
ALLOW_TERMINAL="false"
ALLOW_DOCKER_TERMINAL="false"

# Only ask on fresh install (no existing config)
if [ "$CONFIG_EXISTED" = false ]; then
    echo ""
    echo "==> Configure remote management features"
    echo "    These can be changed later in ${RIOT_CONFIG_DIR}/agent.yaml"
    echo ""

    if ask_yn "Allow remote reboot from dashboard?" "n"; then
        ALLOW_REBOOT="true"
    fi

    if [ "$OS" = "linux" ]; then
        if ask_yn "Allow remote shutdown from dashboard?" "n"; then
            ALLOW_SHUTDOWN="true"
        fi
    fi

    if ask_yn "Allow remote OS patching from dashboard?" "n"; then
        ALLOW_PATCHING="true"
    fi

    if [ "$DIETPI_PRESENT" = true ]; then
        if ask_yn "Allow rIOt to apply DietPi updates during OS patching?" "n"; then
            ALLOW_EXTRA_DIETPI="true"
        fi
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

# ── Decide the poweroff grant (Linux) ────────────────────────────────
# One decision drives both the sudoers line and the config line, so the two
# cannot disagree. The config is the source of truth on a reinstall.
SHUTDOWN_GRANT=false
if [ "$OS" = "linux" ]; then
    if [ "$CONFIG_EXISTED" = false ]; then
        SHUTDOWN_GRANT="$ALLOW_SHUTDOWN"
    elif [ -n "$RIOT_BOOTSTRAP_KEY" ]; then
        echo "==> Remote shutdown: re-enrolment resets remote features; no poweroff grant"
    elif config_enables_shutdown "$RIOT_CONFIG_DIR/agent.yaml"; then
        SHUTDOWN_GRANT=true
        echo "==> Remote shutdown: commands.allow_shutdown is true in ${RIOT_CONFIG_DIR}/agent.yaml; granting sudo systemctl poweroff"
    else
        echo "==> Remote shutdown: not enabled in ${RIOT_CONFIG_DIR}/agent.yaml; no poweroff grant (only a plain \"allow_shutdown: true\" under \"commands:\" in a regular file enables it)"
    fi
fi

# ── Decide the DietPi opt-in (Linux, EXTRA-UPD-001) ──────────────────
# One decision drives both the config line and the root-side files. The config
# is the source of truth on a reinstall, and the answer only counts on a host
# where DietPi is present. Nothing is granted through sudo: the opt-in is the
# root-owned policy file and the unit drop-in written further down.
EXTRA_GRANT=false
if [ "$OS" = "linux" ]; then
    if [ "$CONFIG_EXISTED" = false ]; then
        if [ "$DIETPI_PRESENT" = true ]; then EXTRA_GRANT="$ALLOW_EXTRA_DIETPI"; fi
    elif [ -n "$RIOT_BOOTSTRAP_KEY" ]; then
        if [ "$DIETPI_PRESENT" = true ]; then
            echo "==> DietPi updates: re-enrolment resets remote features; no root-side opt-in"
        fi
    elif config_lists_extra_updater "$RIOT_CONFIG_DIR/agent.yaml" dietpi; then
        if [ "$DIETPI_PRESENT" = true ]; then
            EXTRA_GRANT=true
            echo "==> DietPi updates: commands.allow_extra_updaters lists dietpi in ${RIOT_CONFIG_DIR}/agent.yaml; recording root-side opt-in"
        else
            echo "==> DietPi updates: commands.allow_extra_updaters lists dietpi in ${RIOT_CONFIG_DIR}/agent.yaml but DietPi is not present; nothing recorded"
        fi
    elif [ "$DIETPI_PRESENT" = true ]; then
        echo "==> DietPi updates: commands.allow_extra_updaters does not list dietpi in ${RIOT_CONFIG_DIR}/agent.yaml; no root-side opt-in"
    fi
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

# ── DietPi opt-in, root side (EXTRA-UPD-001 AD-003) ──────────────────
# The opt-in is two small root-owned files written from fixed literals only,
# never from config text: a policy the maintenance worker enforces, and a unit
# drop-in that lengthens the service backstop on opted-in hosts. Order matters
# (SEC-002): the backstop is lengthened, reloaded and verified before the
# policy lists an updater, and on revocation the policy is emptied before the
# backstop is lowered. The files are written only when an updater is recorded
# or they already exist, and they are rewritten, never deleted.
EXTRA_POLICY_DIR=/etc/riot-maintenance
EXTRA_POLICY=${EXTRA_POLICY_DIR}/extra-updaters.json
EXTRA_DROPIN_DIR=/etc/systemd/system/riot-maintenance.service.d
EXTRA_DROPIN=${EXTRA_DROPIN_DIR}/50-riot-extra-updaters.conf
extra_files_exist() {
    [ -e "$EXTRA_POLICY" ] || [ -L "$EXTRA_POLICY" ] || [ -e "$EXTRA_DROPIN" ] || [ -L "$EXTRA_DROPIN" ]
}
prepare_extra_dir() {
    local directory="$1"
    [ ! -L "$directory" ] || fail "Refusing symlink directory: $directory"
    mkdir -p "$directory"
    chown root:root "$directory"
    chmod 0755 "$directory"
    secure_dir "$directory"
}
write_extra_policy() {
    prepare_extra_dir "$EXTRA_POLICY_DIR"
    if [ "$1" = optin ]; then
        printf '{"version":1,"extra_updaters":["dietpi"]}\n' > "$STAGE/extra-updaters.json"
    else
        printf '{"version":1,"extra_updaters":[]}\n' > "$STAGE/extra-updaters.json"
    fi
    atomic_install "$STAGE/extra-updaters.json" "$EXTRA_POLICY" 0644
}
write_extra_dropin() {
    prepare_extra_dir "$EXTRA_DROPIN_DIR"
    printf '[Service]\nRuntimeMaxSec=%s\n' "$1" > "$STAGE/50-riot-extra-updaters.conf"
    atomic_install "$STAGE/50-riot-extra-updaters.conf" "$EXTRA_DROPIN" 0644
}
# After daemon-reload: the unit that will really run must have the expected
# backstop and no mount sandbox, or DietPi mounts would stay hidden from the host.
verify_extra_unit() {
    local value
    value=$(systemctl show riot-maintenance.service -p RuntimeMaxUSec --value) || fail "Cannot inspect the effective maintenance backstop"
    case "$1" in
        85min) [ "$value" = "1h 25min" ] || [ "$value" = 5100000000 ] || fail "The effective maintenance backstop is '${value}', not 1h 25min; no DietPi opt-in was recorded" ;;
        *) [ "$value" = 35min ] || [ "$value" = 2100000000 ] || fail "The effective maintenance backstop is '${value}', not 35min" ;;
    esac
    value=$(systemctl show riot-maintenance.service -p PrivateTmp --value) || fail "Cannot inspect the effective maintenance sandbox"
    [ "$value" = no ] || fail "The maintenance service has PrivateTmp='${value}'; DietPi mounts would not reach the host"
    value=$(systemctl show riot-maintenance.service -p MountFlags --value) || fail "Cannot inspect the effective maintenance mount flags"
    [ -z "$value" ] || fail "The maintenance service has MountFlags='${value}'; DietPi mounts would not reach the host"
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
    for directory in "$EXTRA_POLICY_DIR" "$EXTRA_DROPIN_DIR"; do
        if [ -e "$directory" ] || [ -L "$directory" ]; then secure_dir "$directory"; fi
    done
    for file in "$EXTRA_POLICY" "$EXTRA_DROPIN"; do
        if [ -e "$file" ] || [ -L "$file" ]; then secure_file "$file"; fi
    done
    for directory in "$RIOT_CONFIG_DIR" "$RIOT_DATA_DIR"; do
        [ ! -L "$directory" ] || fail "Refusing symlink directory: $directory"
        if [ -e "$directory" ]; then [ -d "$directory" ] || fail "Expected directory: $directory"; fi
    done
    if command -v apt-get >/dev/null 2>&1; then
        command -v sudo >/dev/null 2>&1 || fail "Preinstalled sudo1.9.10+ is required for safe package-hold rules"
        secure_file "$(readlink -f "$(command -v sudo)")"
        sudo_version=$(sudo --version | awk 'NR==1 {print $3}')
        [[ "$sudo_version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)(p[0-9]+)?$ ]] || fail "Cannot determine supported sudo version"
        sudo_major=${BASH_REMATCH[1]}; sudo_minor=${BASH_REMATCH[2]}; sudo_patch=${BASH_REMATCH[3]}
        (( sudo_major > 1 || (sudo_major == 1 && (sudo_minor > 9 || (sudo_minor == 9 && sudo_patch >= 10))) )) || fail "Preinstalled sudo1.9.10+ is required; wildcard package-hold rules are unsafe"
        apt_major=$(/usr/bin/apt-get --version | awk 'NR==1 {split($2,v,"."); print v[1]}')
        systemd_major=$(systemctl --version | awk 'NR==1 {print $2}')
        [[ "$apt_major" =~ ^[0-9]+$ && "$systemd_major" =~ ^[0-9]+$ ]] || fail "Cannot determine apt/systemd versions"
        [ "$apt_major" -ge 2 ] && [ "$systemd_major" -ge 241 ] || fail "Detached apt requires apt2+/systemd241+"
        for tool in /usr/bin/apt-get /usr/bin/dpkg /usr/bin/dpkg-query /usr/bin/apt-mark; do secure_file "$tool"; done
        command -v python3 >/dev/null 2>&1 || fail "Preinstalled Python3 is required for native package-lock-safe migration"
        secure_file "$(readlink -f "$(command -v python3)")"
        MAINT_SUPPORTED=true
    fi
    # DietPi updates run inside the detached maintenance worker; without it
    # there is nothing to opt in to, so refuse before any change.
    if [ "$EXTRA_GRANT" = true ] && [ "$MAINT_SUPPORTED" != true ]; then
        fail "DietPi updates need the detached apt maintenance path, which this host does not support; remove dietpi from commands.allow_extra_updaters; no files changed"
    fi
    # The agent runs as riot, and unprivileged systemctl reaches systemd only
    # over the D-Bus system bus (root uses a private socket, so root checks pass
    # without it). Minimal images such as DietPi omit dbus; without it every
    # host action is refused. Install it before taking the native package locks.
    if [ ! -S /run/dbus/system_bus_socket ]; then
        echo "==> D-Bus system bus missing; installing dbus (required for the unprivileged agent)"
        if command -v apt-get >/dev/null 2>&1; then
            apt_install_dbus() { DEBIAN_FRONTEND=noninteractive apt-get install -y -q --no-install-recommends dbus; }
            apt_install_dbus || { apt-get update -q && apt_install_dbus; } || fail "Could not install dbus; resolve apt/dpkg errors and re-run"
        elif command -v dnf >/dev/null 2>&1; then
            dnf -y -q install dbus || fail "Could not install dbus; resolve dnf errors and re-run"
        else
            fail "D-Bus system bus is required; install dbus with the host package manager and re-run"
        fi
        systemctl enable --now dbus.socket 2>/dev/null || systemctl start dbus.service || fail "Could not start the D-Bus system bus"
        for _ in {1..50}; do
            [ ! -S /run/dbus/system_bus_socket ] || break
            sleep 0.1
        done
        [ -S /run/dbus/system_bus_socket ] || fail "D-Bus installed but /run/dbus/system_bus_socket did not appear"
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
import datetime, json, os, re, sys, uuid
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
    assert type(d['version']) is int and d['version'] in (1, 2)
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
    # EXTRA-UPD-001: a version 1 record carries no updater keys. A version 2
    # record is closed: exact keys and enums, no running entry, bounded sizes.
    extra_keys = ('requested_extra_updaters', 'apt_outcome', 'extra_updaters')
    if d['version'] == 1:
        assert not any(k in d for k in extra_keys)
    else:
        known = {'version','command_id','mode','include_reboot_class','started_at','updated_at','finished_at','phase','outcome','exit_code','interrupted','summary','output','output_truncated','holds','holds_released','hold_restore_error','hold_cleanup_failed','version_changes','requested_extra_updaters','apt_outcome','extra_updaters'}
        assert set(d) <= known and d['mode'] != 'repair'
        ident = re.compile(r'[a-z][a-z0-9_-]{0,31}')
        req = d['requested_extra_updaters']
        assert type(req) is list and 1 <= len(req) <= 8 and len(set(req)) == len(req) and all(type(x) is str and ident.fullmatch(x) for x in req)
        assert d['apt_outcome'] in ('success','error','interrupted')
        results = d.get('extra_updaters', [])
        assert type(results) is list and len(results) <= 8
        reasons = ('apt_not_successful','not_enabled','status_unknown','nothing_pending','insufficient_time','non_default_update_source','unsafe_dietpi_path','min_interval','tmp_not_private','mount_propagation_unverified','dietpi_busy','dpkg_locked','rundir_not_private')
        result_keys = {'id','outcome','reason','exit_code','started_at','finished_at','version_before','version_after','source_owner','source_branch','reboot_required_after','note'}
        seen_ids = set()
        for r in results:
            assert type(r) is dict and set(r) <= result_keys and type(r['id']) is str and ident.fullmatch(r['id']) and r['id'] not in seen_ids
            seen_ids.add(r['id'])
            assert r['outcome'] in ('success','failed','timed_out','interrupted','skipped')
            if r['outcome'] == 'skipped': assert r.get('reason') in reasons
            else: assert 'reason' not in r
            assert len(r.get('note', '').encode()) <= 256 and len(r.get('version_before', '').encode()) <= 32 and len(r.get('version_after', '').encode()) <= 32
            assert len(r.get('source_owner', '').encode()) <= 64 and len(r.get('source_branch', '').encode()) <= 64
        if d['outcome'] == 'success':
            assert d['apt_outcome'] == 'success'
            # DIETPI-PATH-001: a skip before the updater started leaves a run successful, whatever the reason.
            assert all(r['outcome'] in ('success','skipped') for r in results)
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
            # sudo>=1.9.10 argument regexes avoid apt-mark option injection.
            # Only package operands are allowed; no option, path or environment.
            echo 'riot ALL=(root) NOPASSWD: /usr/bin/apt-mark ^hold [a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$'
            echo 'riot ALL=(root) NOPASSWD: /usr/bin/apt-mark ^unhold [a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$'
        elif command -v dnf >/dev/null 2>&1; then
            DNF_PATH=$(command -v dnf)
            echo "riot ALL=(root) NOPASSWD: ${DNF_PATH} makecache"
            echo "riot ALL=(root) NOPASSWD: ${DNF_PATH} -y update"
            echo "riot ALL=(root) NOPASSWD: ${DNF_PATH} -y --security update"
            echo 'riot ALL=(root) NOPASSWD: /usr/bin/install -m 0644 -o root -g root /var/lib/riot/dnf-holds.staged /etc/dnf/libdnf5.conf.d/60-riot-holds.conf'
            echo 'riot ALL=(root) NOPASSWD: /usr/bin/rm -f /etc/dnf/libdnf5.conf.d/60-riot-holds.conf'
        fi
        echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} reboot"
        if [ "$SHUTDOWN_GRANT" = true ]; then
            echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} poweroff"
        fi
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
    for path in "$RIOT_BIN" "$RIOT_APPLY_DIR" /etc/riot /etc/sudoers.d/riot-agent /etc/systemd/system/riot-agent.service /etc/systemd/system/riot-agent.service.d /etc/systemd/system/riot-maintenance.service "$EXTRA_POLICY_DIR" "$EXTRA_DROPIN_DIR"; do
        if [ -e "$path" ] || [ -L "$path" ]; then
            mkdir -p "$BACKUP$(dirname "$path")"
            cp -a -- "$path" "$BACKUP$path"
        fi
    done
    echo "==> Existing files preserved in $BACKUP"
    # Validate the effective policy before stopping the old agent or replacing
    # any executable/unit. A failed merged policy restores the working drop-in.
    atomic_install "$STAGE/riot-agent.sudoers" /etc/sudoers.d/riot-agent 0440
    GRANT_PENDING="$SHUTDOWN_GRANT"
    if ! visudo -cf /etc/sudoers >/dev/null; then
        GRANT_PENDING=false
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
        # EXPECT-008: tell the server first, so it expects this restart. Stdin,
        # stderr and the maintenance-lock descriptor are not given to the notice.
        if [ "$CONFIG_EXISTED" = true ] && [ -z "$RIOT_BOOTSTRAP_KEY" ] && systemctl is-active --quiet riot-agent.service; then
            reinstall_notice </dev/null 2>/dev/null 9>&- || true
        fi
        systemctl stop riot-agent
    fi
    for directory in "$RIOT_CONFIG_DIR" "$RIOT_DATA_DIR"; do
        [ ! -L "$directory" ] || fail "Refusing symlink directory: $directory"
    done
    mkdir -p "$RIOT_CONFIG_DIR" "$RIOT_DATA_DIR"
    chown root:root "$RIOT_CONFIG_DIR"
    chmod 0700 "$RIOT_CONFIG_DIR"
    # The config directory is now root-only and the agent is stopped, so nothing
    # can change agent.yaml between these checks and the write below.
    refuse_unsafe_config_path "changed during installation and" "the agent is left stopped; re-run the installer"
    if [ "$CONFIG_EXISTED" = true ] && [ -z "$RIOT_BOOTSTRAP_KEY" ]; then
        require_shutdown_agreement
    fi
    chown riot:riot "$RIOT_DATA_DIR"
    mkdir -p "$RIOT_APPLY_DIR" /var/lib/riot-maintenance
    chmod 0755 "$RIOT_APPLY_DIR"
    chown root:root "$RIOT_APPLY_DIR"
    chown root:riot /var/lib/riot-maintenance
    chmod 0750 /var/lib/riot-maintenance
else
    mkdir -p "$RIOT_CONFIG_DIR" "$RIOT_DATA_DIR"
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

# ── Build config sections from answers ───────────────────────────────
COMMANDS_SECTION=""
if [ "$ALLOW_REBOOT" = "true" ] || [ "$SHUTDOWN_GRANT" = "true" ] || [ "$ALLOW_PATCHING" = "true" ] || [ "$EXTRA_GRANT" = "true" ]; then
    SHUTDOWN_LINE=""
    if [ "$SHUTDOWN_GRANT" = "true" ]; then
        SHUTDOWN_LINE="
  allow_shutdown: true"
    fi
    EXTRA_LINE=""
    if [ "$EXTRA_GRANT" = "true" ]; then
        EXTRA_LINE="
  allow_extra_updaters: [dietpi]"
    fi
    COMMANDS_SECTION="
commands:
  allow_reboot: ${ALLOW_REBOOT}${SHUTDOWN_LINE}
  allow_patching: ${ALLOW_PATCHING}${EXTRA_LINE}"
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
    # The config is final and the directory is root-only. Confirm the drop-in
    # installed earlier still matches it before any binary is replaced.
    require_shutdown_agreement
    GRANT_PENDING=false
    atomic_install "$STAGE/$BINARY_NAME" "$RIOT_BIN" 0755
    atomic_install "$STAGE/riot-agent-apply-${SUFFIX}" "$RIOT_APPLY_BIN" 0755
    atomic_install "$STAGE/riot-maintenance-${SUFFIX}" "$MAINT_BIN" 0755
    # DietPi opt-in, root side. Revocation empties the policy before any unit
    # change; an opt-in waits until the lengthened backstop is verified (below).
    EXTRA_MODE=none
    if [ "$MAINT_SUPPORTED" = true ]; then
        if [ "$EXTRA_GRANT" = true ]; then
            EXTRA_MODE=optin
        elif extra_files_exist; then
            EXTRA_MODE=revoke
            write_extra_policy revoked
        fi
    fi
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
    case "$EXTRA_MODE" in
        optin) write_extra_dropin 85min ;;
        revoke) write_extra_dropin 35min ;;
    esac
    chown riot:riot "$RIOT_CONFIG_DIR"
    chmod 0700 "$RIOT_CONFIG_DIR"
    if ! visudo -cf /etc/sudoers >/dev/null; then
        GRANT_PENDING=false
        restore_policy
        fail "Effective policy changed during installation; previous managed policy restored; backup: $BACKUP"
    fi
    systemctl daemon-reload
    pre=$(systemctl show riot-agent.service -p ExecStartPre --value) || fail "Cannot inspect effective agent precommand"
    [[ "$pre" == *"path=${RIOT_APPLY_BIN} ;"* && "$pre" == *"argv[]=${RIOT_APPLY_BIN} --boot ;"* ]] || fail "Unexpected effective boot hook; review preserved drop-ins before starting"
    [ "$(printf '%s' "$pre" | grep -o 'path=' | wc -l)" -eq 1 ] || fail "Unexpected additional boot hooks; review preserved drop-ins before starting"
    case "$EXTRA_MODE" in
        optin)
            verify_extra_unit 85min
            write_extra_policy optin
            echo "==> DietPi updates: root-side opt-in recorded (${EXTRA_POLICY}); maintenance backstop 85min"
            ;;
        revoke)
            verify_extra_unit 35min
            echo "==> DietPi updates: root-side opt-in revoked (${EXTRA_POLICY} lists no updater); maintenance backstop 35min"
            ;;
    esac
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
