#!/usr/bin/env bash
# MAINT-RECOVERY AC-003/004: real dpkg recovery and apt's no-removal guard.
# Run only in a NEW disposable Debian container without a host filesystem mount.
set -euo pipefail
[ "${1:-}" = --disposable-container ] && [ -f /.dockerenv ] && [ "$(id -u)" = 0 ] || {
    echo 'This test changes package state; a disposable root Docker container is required.' >&2
    exit 2
}
export DEBIAN_FRONTEND=noninteractive LC_ALL=C
work=$(mktemp -d /tmp/riot-apt-fixture.XXXXXXXX)
chmod 0755 "$work"
mkdir "$work/repo" "$work/lists" "$work/lists/partial"
make_package() {
    local name=$1 version=$2 dependency=${3:-}
    local dir="$work/$name-$version"
    mkdir -p "$dir/DEBIAN"
    printf 'Package: %s\nVersion: %s\nArchitecture: all\nMaintainer: rIOt fixture <fixture@example.invalid>\nDescription: disposable maintenance fixture\n' "$name" "$version" > "$dir/DEBIAN/control"
    if [ -n "$dependency" ]; then printf 'Depends: %s\n' "$dependency" >> "$dir/DEBIAN/control"; fi
    if [ "$name" = riot-test-interrupted ]; then
        printf '#!/bin/sh\n[ -f /tmp/riot-test-postinst-ready ]\n' > "$dir/DEBIAN/postinst"
        chmod 0755 "$dir/DEBIAN/postinst"
    fi
    dpkg-deb --build --root-owner-group "$dir" "$work/repo/$name-$version.deb" >/dev/null
}
common=(-y --no-remove -o DPkg::Lock::Timeout=120 -o Acquire::Retries=0 -o Dpkg::Options::=--force-confold -o Dpkg::Options::=--force-confdef)
configure=(--force-confold --force-confdef --configure -a)

make_package riot-test-interrupted 1
if dpkg -i "$work/repo/riot-test-interrupted-1.deb"; then echo 'Expected failed postinst' >&2; exit 1; fi
if dpkg "${configure[@]}"; then echo 'Unrepaired postinst unexpectedly succeeded' >&2; exit 1; fi
touch /tmp/riot-test-postinst-ready
dpkg "${configure[@]}"
test -z "$(dpkg --audit)"
echo '[MAINT-RECOVERY AC-003] interrupted configuration repaired and audited'

make_package riot-test-provider 1
make_package riot-test-needs 1 'riot-test-provider (= 1)'
dpkg --unpack "$work/repo/riot-test-needs-1.deb"
if apt-get check; then echo 'Expected broken dependency' >&2; exit 1; fi
# Local fixture repository supplies the missing dependency without network.
provider="$work/repo/riot-test-provider-1.deb"
{
    dpkg-deb -f "$provider"
    printf 'Filename: ./riot-test-provider-1.deb\nSize: %s\nSHA256: %s\n\n' "$(stat -c %s "$provider")" "$(sha256sum "$provider" | cut -d' ' -f1)"
} > "$work/repo/Packages"
printf 'deb [trusted=yes] file:%s ./\n' "$work/repo" > "$work/sources.list"
repo=(-o "Dir::Etc::sourcelist=$work/sources.list" -o Dir::Etc::sourceparts=- -o "Dir::State::lists=$work/lists")
apt-get "${repo[@]}" -o Acquire::Retries=0 update
apt-get "${repo[@]}" "${common[@]}" --fix-broken install
dpkg "${configure[@]}"
test -z "$(dpkg --audit)"
apt-get check
echo '[MAINT-RECOVERY AC-003] one no-removal dependency repair installed the missing provider'

make_package riot-test-needs 2 'riot-test-nonexistent (= 999)'
dpkg --unpack "$work/repo/riot-test-needs-2.deb"
if apt-get "${repo[@]}" "${common[@]}" --fix-broken install; then
    echo 'Removal-required repair must be refused' >&2; exit 1
fi
test "$(dpkg-query -W -f='${Version}' riot-test-needs)" = 2
test "$(dpkg-query -W -f='${db:Status-Status}' riot-test-needs)" = unpacked
echo '[MAINT-RECOVERY AC-004] apt refused a removal-required repair and retained the package'
echo "Disposable evidence retained in $work"
