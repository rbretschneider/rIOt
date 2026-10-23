"""MAINT-RECOVERY AC-011/012: run only inside a disposable root container.

Example: RIOT_INSTALLER_FIXTURE=1 python3 /src/scripts/test_install_maintenance.py
The repository is read-only; all installed paths belong to the test container.
"""
import base64
import hashlib
import os
from pathlib import Path
import shutil
import stat
import subprocess
import unittest


ROOT = Path('/fixture')
SOURCE = Path(__file__).resolve().parent / 'install.sh'
PIN = '31W+lKF8LVms4ar/Qb9DI0//nj8CUT3zEx2aRhVbBjY='


def run(*args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, **kwargs)


def write(path, value, mode=0o755):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(value)
    path.chmod(mode)


class InstallerFixtures(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.geteuid() != 0 or os.environ.get('RIOT_INSTALLER_FIXTURE') != '1' or not Path('/.dockerenv').exists():
            raise RuntimeError('This test mutates system paths: disposable root Docker fixture required')
        ROOT.mkdir(exist_ok=True)
        run('openssl', 'genpkey', '-algorithm', 'ED25519', '-out', str(ROOT / 'private.pem'))
        public = run('openssl', 'pkey', '-in', str(ROOT / 'private.pem'), '-pubout', '-outform', 'DER').stdout[-32:]
        cls.public_key = base64.b64encode(public).decode()
        # Replace executables only inside this disposable container. Their paths
        # stay root-owned/nonwritable, so production ownership checks still run.
        write('/usr/bin/curl', '''#!/bin/bash
set -eu
dest=""; url=""
while [ $# -gt 0 ]; do
  if [ "$1" = -o ]; then dest="$2"; shift 2; else url="$1"; shift; fi
done
cp "/fixture/assets/${url##*/}" "$dest"
''')
        write('/usr/bin/apt-get', '''#!/bin/sh
if [ "$1" = --version ]; then echo 'apt 2.6.1 (amd64)'; exit 0; fi
case "$*" in
  'install -y -q --no-install-recommends dbus')
    echo "PACKAGE INSTALL dbus" >> /fixture/trace
    [ ! -f /fixture/fail-dbus ] || exit 100
    touch /fixture/dbus-installed; exit 0;;
  'update -q') echo "PACKAGE LISTS update" >> /fixture/trace; exit 0;;
esac
echo "UNEXPECTED PACKAGE MUTATION: $*" >> /fixture/trace
exit 99
''')
        write('/usr/bin/systemctl', '''#!/bin/sh
echo "systemctl $*" >> /fixture/trace
case "$1" in
  --version) echo 'systemd 252';;
  show)
    case "$*" in
      *ActiveState*) cat /fixture/active;;
      *LoadState*) echo loaded;;
      *ExecStartPre*) echo '{ path=/usr/local/lib/riot/riot-agent-apply ; argv[]=/usr/local/lib/riot/riot-agent-apply --boot ; ignore_errors=yes ; }';;
      *) exit 2;;
    esac;;
  stop)
    python3 - <<'LOCK'
import fcntl
with open('/var/lib/dpkg/lock-frontend','a') as f:
    try:
        fcntl.lockf(f, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        open('/fixture/native-lock-proof','w').write('held during stop')
    else:
        raise SystemExit('installer did not retain native package exclusion')
LOCK
    ;;
  enable)
    if [ "$*" = 'enable --now dbus.socket' ]; then
      [ -f /fixture/dbus-installed ] || exit 5
      python3 -c "import socket; socket.socket(socket.AF_UNIX).bind('/run/dbus/system_bus_socket')"
    fi;;
  start|daemon-reload) ;;
  *) exit 3;;
esac
''')
        if not Path('/usr/bin/sudo.fixture-real').exists():
            shutil.copy2('/usr/bin/sudo', '/usr/bin/sudo.fixture-real')
        Path('/usr/bin/sudo.fixture-real').chmod(0o4755)
        write('/usr/bin/sudo', '''#!/bin/sh
if [ "$1" = --version ] && [ -f /fixture/old-sudo ]; then echo 'Sudo version 1.9.9'; exit 0; fi
exec /usr/bin/sudo.fixture-real "$@"
''')
        if not Path('/usr/sbin/visudo.fixture-real').exists():
            shutil.copyfile('/usr/sbin/visudo', '/usr/sbin/visudo.fixture-real')
        Path('/usr/sbin/visudo.fixture-real').chmod(0o755)
        write('/usr/sbin/visudo', '''#!/bin/sh
case "$*" in
  *riot-agent.sudoers*) [ ! -f /fixture/fail-policy ] || exit 1;;
esac
if [ "$*" = '-cf /etc/sudoers' ] && [ -f /fixture/fail-merged-policy ] && grep -q 'start --no-block' /etc/sudoers.d/riot-agent; then exit 1; fi
exec /usr/sbin/visudo.fixture-real "$@"
''')
        subprocess.run(['useradd', '--system', '--no-create-home', '--shell', '/usr/sbin/nologin', 'riot'], check=False, capture_output=True)

    def setUp(self):
        for path in ['/usr/local/lib/riot', '/etc/riot', '/var/lib/riot', '/var/lib/riot-maintenance', '/etc/systemd/system/riot-agent.service.d']:
            p = Path(path)
            if p.is_symlink(): p.unlink()
            elif p.exists(): shutil.rmtree(p)
        for path in ['/usr/local/bin/riot-agent', '/etc/systemd/system/riot-agent.service', '/etc/systemd/system/riot-maintenance.service', '/etc/sudoers.d/riot-agent', '/fixture/fail-policy', '/fixture/fail-merged-policy', '/fixture/native-lock-proof', '/fixture/old-sudo', '/fixture/fail-dbus', '/fixture/dbus-installed']:
            Path(path).unlink(missing_ok=True)
        self.provide_system_bus()
        write(ROOT / 'trace', '', 0o600)
        write(ROOT / 'active', 'inactive\n', 0o600)
        assets = ROOT / 'assets'
        if assets.exists(): shutil.rmtree(assets)
        assets.mkdir()
        for name in ['riot-agent-linux-amd64', 'riot-agent-apply-linux-amd64', 'riot-maintenance-linux-amd64']:
            write(assets / name, '#!/bin/sh\n# signed fixture ' + name + '\nexit 0\n')
        shutil.copyfile(SOURCE.parent / 'riot-maintenance.service', assets / 'riot-maintenance.service')
        manifest = ''.join(hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + '\n' for p in sorted(assets.iterdir()))
        write(assets / 'checksums.txt', manifest, 0o600)
        sig = run('openssl', 'pkeyutl', '-sign', '-inkey', str(ROOT / 'private.pem'), '-rawin', '-in', str(assets / 'checksums.txt')).stdout
        write(assets / 'checksums.txt.sig', base64.b64encode(sig).decode() + '\n', 0o600)
        write(ROOT / 'install.sh', SOURCE.read_text().replace(PIN, self.public_key))
        # Existing legacy installation with a writable binary and blind root hook.
        write('/usr/local/bin/riot-agent', '#!/bin/sh\n# old agent\nexit 0\n')
        shutil.chown('/usr/local/bin/riot-agent', user='riot', group='riot')
        write('/etc/riot/agent.yaml', 'server:\n  url: "https://old.test:7331"\n', 0o600)
        write('/var/lib/riot/device-id', 'existing-device\n', 0o600)
        write('/etc/sudoers.d/riot-agent', 'riot ALL=(root) NOPASSWD: /bin/sh -c *\n', 0o440)
        write('/etc/systemd/system/riot-agent.service', '[Service]\nExecStartPre=/bin/sh -c bad-copy\nExecStart=/usr/local/bin/riot-agent\n')
        self.old_binary = Path('/usr/local/bin/riot-agent').read_bytes()
        self.old_inode = Path('/usr/local/bin/riot-agent').stat().st_ino
        self.old_policy = Path('/etc/sudoers.d/riot-agent').read_bytes()
        self.old_unit = Path('/etc/systemd/system/riot-agent.service').read_bytes()

    def provide_system_bus(self):
        import socket
        bus = Path('/run/dbus/system_bus_socket')
        bus.parent.mkdir(parents=True, exist_ok=True)
        bus.unlink(missing_ok=True)
        with socket.socket(socket.AF_UNIX) as sock:
            sock.bind(str(bus))

    def install(self):
        return subprocess.run(['bash', str(ROOT / 'install.sh'), '--non-interactive'], capture_output=True, text=True)

    def assert_refused_without_replacement(self, result):
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(Path('/usr/local/bin/riot-agent').read_bytes(), self.old_binary)
        self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), self.old_policy)
        self.assertEqual(Path('/etc/systemd/system/riot-agent.service').read_bytes(), self.old_unit)
        trace = (ROOT / 'trace').read_text()
        self.assertNotIn('systemctl stop', trace)
        self.assertNotIn('UNEXPECTED PACKAGE MUTATION', trace)

    def test_AC011_invalid_signature_preserves_running_install(self):
        write(ROOT / 'assets/checksums.txt.sig', base64.b64encode(bytes(64)).decode(), 0o600)
        self.assert_refused_without_replacement(self.install())

    def test_AC011_changed_signed_asset_preserves_running_install(self):
        write(ROOT / 'assets/riot-agent-linux-amd64', 'not the signed executable')
        self.assert_refused_without_replacement(self.install())

    def test_AC011_missing_helper_preserves_running_install(self):
        (ROOT / 'assets/riot-maintenance-linux-amd64').unlink()
        self.assert_refused_without_replacement(self.install())

    def test_AC012_active_worker_refuses_before_stop(self):
        write(ROOT / 'active', 'deactivating\n', 0o600)
        self.assert_refused_without_replacement(self.install())

    def test_AC012_candidate_policy_failure_preserves_old_policy(self):
        write(ROOT / 'fail-policy', '', 0o600)
        self.assert_refused_without_replacement(self.install())

    def test_AC012_old_sudo_preserves_binary_unit_policy_before_mutation(self):
        write(ROOT / 'old-sudo', '', 0o600)
        result = self.install()
        self.assert_refused_without_replacement(result)
        self.assertIn('sudo1.9.10+', result.stderr)

    def test_AC012_real_sudo_lists_safe_hold_names_and_denies_injection(self):
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        prefix = ['runuser', '-u', 'riot', '--', '/usr/bin/sudo.fixture-real', '-n', '-l', '--']
        listing = subprocess.run(prefix[:-1], capture_output=True, text=True)
        self.assertEqual(listing.returncode, 0, listing.stdout + listing.stderr)
        self.assertIn('/usr/bin/apt-mark ^hold ', listing.stdout)
        self.assertIn('/usr/bin/apt-mark ^unhold ', listing.stdout)
        self.assertNotIn('/usr/bin/apt-mark hold *', listing.stdout)
        self.assertNotIn('/usr/bin/apt-mark unhold *', listing.stdout)
        positives = [
            ['linux-image-amd64'], ['libc6:amd64'], ['linux-image-6.1.0-42-amd64'],
            ['g++-12'], ['0ad'], ['systemd', 'libc6:amd64', 'linux-image-amd64'],
        ]
        negatives = [
            [], ['-oDir::Bin::dpkg=/tmp/evil', 'linux-image-amd64'],
            ['linux-image-amd64', '-oDir::Bin::dpkg=/tmp/evil'],
            ['--file', '/tmp/state', 'linux-image-amd64'], ['linux-image-amd64', '--file=/tmp/state'],
            ['APT_CONFIG=/tmp/evil', 'linux-image-amd64'], ['/tmp/package'],
            ['linux-image-amd64;'], ['linux-image-amd64\n'], ['linux-image-amd64\nevil'],
            [''], ['--', 'linux-image-amd64'],
        ]
        for operation in ('hold', 'unhold'):
            for args in positives:
                with self.subTest(operation=operation, allowed=args):
                    probe = subprocess.run(prefix + ['/usr/bin/apt-mark', operation] + args, capture_output=True, text=True)
                    self.assertEqual(probe.returncode, 0, probe.stdout + probe.stderr)
            for args in negatives:
                with self.subTest(operation=operation, denied=args):
                    probe = subprocess.run(prefix + ['/usr/bin/apt-mark', operation] + args, capture_output=True, text=True)
                    self.assertNotEqual(probe.returncode, 0, probe.stdout + probe.stderr)
            probe = subprocess.run(prefix + ['/usr/bin/apt-mark', '-oDir::Bin::dpkg=/tmp/evil', operation, 'linux-image-amd64'], capture_output=True, text=True)
            self.assertNotEqual(probe.returncode, 0, probe.stdout + probe.stderr)
        env_wrapper = subprocess.run(prefix + ['/usr/bin/env', 'APT_CONFIG=/tmp/evil', '/usr/bin/apt-mark', 'hold', 'linux-image-amd64'], capture_output=True, text=True)
        self.assertNotEqual(env_wrapper.returncode, 0, env_wrapper.stdout + env_wrapper.stderr)

    def test_AC012_effective_policy_failure_restores_old_policy_before_stop(self):
        write(ROOT / 'fail-merged-policy', '', 0o600)
        result = self.install()
        self.assert_refused_without_replacement(result)
        self.assertIn('previous managed policy restored', result.stderr)

    def test_AC012_native_dpkg_lock_refuses_before_stop(self):
        import fcntl
        with open('/var/lib/dpkg/lock-frontend', 'a') as lock:
            fcntl.lockf(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.assert_refused_without_replacement(self.install())

    def test_AC012_invalid_signed_unit_preserves_binary_unit_policy(self):
        assets = ROOT / 'assets'
        write(assets / 'riot-maintenance.service', '[Service]\nType=exec\nExecStart=/bin/true\nExecStart=/bin/false\n')
        names = ['riot-agent-linux-amd64', 'riot-agent-apply-linux-amd64', 'riot-maintenance-linux-amd64', 'riot-maintenance.service']
        manifest = ''.join(hashlib.sha256((assets / name).read_bytes()).hexdigest() + '  ' + name + '\n' for name in names)
        write(assets / 'checksums.txt', manifest, 0o600)
        sig = run('openssl', 'pkeyutl', '-sign', '-inkey', str(ROOT / 'private.pem'), '-rawin', '-in', str(assets / 'checksums.txt')).stdout
        write(assets / 'checksums.txt.sig', base64.b64encode(sig).decode() + '\n', 0o600)
        result = self.install()
        self.assert_refused_without_replacement(result)
        self.assertIn('Candidate units did not validate', result.stderr)

    def test_AC012_unfinished_root_record_refuses_before_stop(self):
        write('/var/lib/riot-maintenance/current.json', '{"version":1,"outcome":"running"}', 0o640)
        self.assert_refused_without_replacement(self.install())

    def test_AC012_duplicate_status_keys_refuse_before_stop(self):
        write('/var/lib/riot-maintenance/current.json', '{"version":1,"outcome":"running","outcome":"success","finished_at":"2026-10-20T12:00:00Z"}', 0o640)
        self.assert_refused_without_replacement(self.install())

    def test_AC012_multilink_root_lock_refuses_before_stop(self):
        lock = Path('/run/lock/riot-maintenance.lock')
        lock.touch(mode=0o600, exist_ok=True)
        link = ROOT / 'extra-lock-link'
        link.unlink(missing_ok=True)
        link.hardlink_to(lock)
        try:
            self.assert_refused_without_replacement(self.install())
        finally:
            link.unlink()

    def test_AC012_symlink_helper_directory_refuses_before_stop(self):
        Path('/usr/local/lib/riot').symlink_to('/tmp', target_is_directory=True)
        self.assert_refused_without_replacement(self.install())

    def test_AC012_root_lock_contention_preserves_running_install(self):
        import fcntl
        path = Path('/run/lock/riot-maintenance.lock')
        path.touch(mode=0o600, exist_ok=True)
        with path.open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.assert_refused_without_replacement(self.install())

    def test_AC012_legacy_migration_uses_fresh_signed_inode_and_preserves_identity(self):
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        binary = Path('/usr/local/bin/riot-agent')
        self.assertNotEqual(binary.stat().st_ino, self.old_inode)
        self.assertEqual(binary.stat().st_uid, 0)
        self.assertEqual(binary.read_bytes(), (ROOT / 'assets/riot-agent-linux-amd64').read_bytes())
        self.assertEqual(Path('/var/lib/riot/device-id').read_text(), 'existing-device\n')
        self.assertIn('https://old.test:7331', Path('/etc/riot/agent.yaml').read_text())
        policy = Path('/etc/sudoers.d/riot-agent').read_text()
        self.assertIn('start --no-block riot-maintenance.service', policy)
        self.assertNotIn('/bin/sh', policy)
        self.assertNotIn('apt-get', policy)
        self.assertNotIn('dpkg', policy)
        hook = Path('/etc/systemd/system/riot-agent.service.d/zzzz-riot-signed-boot.conf').read_text()
        self.assertIn('ExecStartPre=\nExecStartPre=-+/usr/local/lib/riot/riot-agent-apply --boot', hook)
        self.assertEqual(Path('/usr/local/lib/riot/riot-agent-apply').stat().st_uid, 0)
        self.assertNotIn('UNEXPECTED PACKAGE MUTATION', (ROOT / 'trace').read_text())
        self.assertEqual((ROOT / 'native-lock-proof').read_text(), 'held during stop')
        import fcntl
        with open('/var/lib/dpkg/lock-frontend', 'a') as lock:
            fcntl.lockf(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)

    def test_DBUS001_AC005_present_system_bus_needs_no_package_operation(self):
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn('PACKAGE', (ROOT / 'trace').read_text())

    def test_DBUS001_AC006_missing_system_bus_is_installed_and_started_before_stop(self):
        Path('/run/dbus/system_bus_socket').unlink()
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        trace = (ROOT / 'trace').read_text()
        self.assertIn('PACKAGE INSTALL dbus', trace)
        self.assertIn('systemctl enable --now dbus.socket', trace)
        self.assertLess(trace.index('PACKAGE INSTALL dbus'), trace.index('systemctl stop'))
        self.assertTrue(stat.S_ISSOCK(os.stat('/run/dbus/system_bus_socket').st_mode))

    def test_DBUS001_AC007_failed_dbus_install_preserves_running_install(self):
        Path('/run/dbus/system_bus_socket').unlink()
        write(ROOT / 'fail-dbus', '', 0o600)
        result = self.install()
        self.assert_refused_without_replacement(result)
        self.assertIn('PACKAGE LISTS update', (ROOT / 'trace').read_text(), 'one list refresh before giving up')
        self.assertIn('Could not install dbus', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
