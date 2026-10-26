"""MAINT-RECOVERY AC-011/012: run only inside a disposable root container.

Example: RIOT_INSTALLER_FIXTURE=1 python3 /src/scripts/test_install_maintenance.py
The repository is read-only; all installed paths belong to the test container.
"""
import base64
import hashlib
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import unittest


ROOT = Path('/fixture')
SOURCE = Path(__file__).resolve().parent / 'install.sh'
PIN = '31W+lKF8LVms4ar/Qb9DI0//nj8CUT3zEx2aRhVbBjY='

# SHUTDOWN-SUDO-001. SC-001: nothing here may run a power command. The power
# tools are replaced by tripwires, systemctl is a stub, and policy is only ever
# listed with sudo -l.
POWER_TOOLS = ('poweroff', 'halt', 'shutdown', 'reboot')
SERVER_URL = 'https://new.test:7331'
GRANT_LINE = 'riot ALL=(root) NOPASSWD: /usr/bin/systemctl poweroff\n'
REBOOT_LINE = 'riot ALL=(root) NOPASSWD: /usr/bin/systemctl reboot\n'
Q_REBOOT = 'Allow remote reboot from dashboard?'
Q_SHUTDOWN = 'Allow remote shutdown from dashboard?'
Q_PATCHING = 'Allow remote OS patching from dashboard?'
Q_EXTRA = 'Allow rIOt to apply DietPi updates during OS patching?'
Q_TERMINAL = 'Allow remote terminal access to this host?'
Q_DOCKER = 'Allow remote exec into Docker containers?'
PROMPT_RE = re.compile(r'    (.+?) \[(?:y/N|Y/n)\]: $')
POWER_TRACE_RE = re.compile(r'^(systemctl\s+(poweroff|reboot|halt|kexec|suspend|hibernate)\b|POWER TRIPWIRE)', re.M)

# EXTRA-UPD-001. SC-001: no DietPi program may run. Every file under
# /boot/dietpi in these fixtures is a tripwire that records itself if executed.
# The installer only looks at the two marker files, and policy is only ever
# listed with sudo -l.
DIETPI_TRIPWIRE_RE = re.compile(r'^DIETPI TRIPWIRE', re.M)
DIETPI_TRIPWIRE = '#!/bin/sh\necho "DIETPI TRIPWIRE $0 $*" >> /fixture/trace\nexit 97\n'
DIETPI_VERSION = ("G_DIETPI_VERSION_CORE=9\nG_DIETPI_VERSION_SUB=20\nG_DIETPI_VERSION_RC=1\n"
                  "G_GITBRANCH='master'\nG_GITOWNER='MichaIng'\n")
EXTRA_POLICY = Path('/etc/riot-maintenance/extra-updaters.json')
EXTRA_DROPIN = Path('/etc/systemd/system/riot-maintenance.service.d/50-riot-extra-updaters.conf')
POLICY_ON = '{"version":1,"extra_updaters":["dietpi"]}\n'
POLICY_OFF = '{"version":1,"extra_updaters":[]}\n'
DROPIN_ON = '[Service]\nRuntimeMaxSec=85min\n'
DROPIN_OFF = '[Service]\nRuntimeMaxSec=35min\n'
EXTRA_LINE = 'allow_extra_updaters: [dietpi]'
# Golden bytes. Both were produced by running the same fixture scenario with
# scripts/install.sh from 71a4dea (main before SHUTDOWN-SUDO-001) inside this
# container image. Do not regenerate them from the new installer.
PRE_STORY_DROPIN = '''# rIOt fixed privileges; package transactions run only in the detached worker.
riot ALL=(root) NOPASSWD: /usr/bin/systemctl start --no-block riot-maintenance.service
riot ALL=(root) NOPASSWD: /usr/bin/apt-mark ^hold [a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$
riot ALL=(root) NOPASSWD: /usr/bin/apt-mark ^unhold [a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$
riot ALL=(root) NOPASSWD: /usr/bin/systemctl reboot
riot ALL=(root) NOPASSWD: /usr/bin/systemd-run --unit=riot-agent-update /usr/local/lib/riot/riot-agent-apply
riot ALL=(root) NOPASSWD: /usr/bin/systemctl reset-failed riot-agent-update
# smartmontools absent: install separately if SMART monitoring is wanted.
'''
# Fresh install on a stubbed Darwin host, answered with -y, server URL SERVER_URL.
PRE_STORY_DARWIN_CONFIG = '''server:
  url: "https://new.test:7331"
  tls_verify: true

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

commands:
  allow_reboot: true
  allow_patching: true

host_terminal:
  enabled: true

'''
PRE_STORY_DARWIN_STDOUT = '''==> rIOt Agent Installer
==> Platform: darwin/arm64 (riot-agent-darwin-arm64)

==> Configure remote management features
    These can be changed later in /etc/riot/agent.yaml

    Allow remote reboot from dashboard? [y/N]: y (auto)
    Allow remote OS patching from dashboard? [y/N]: y (auto)
    Allow remote terminal access to this host? [y/N]: y (auto)

==> Writing config to /etc/riot/agent.yaml
==> rIOt agent installed at /usr/local/bin/riot-agent; run manually with -config /etc/riot/agent.yaml
'''


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
echo "DOWNLOAD ${url##*/}" >> /fixture/trace
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
      *RuntimeMaxUSec*)
        # EXTRA-UPD-001: answer from the drop-in as the last daemon-reload saw it,
        # and record what the root-side policy looked like at this instant.
        echo "POLICY-AT-VERIFY: $(cat /etc/riot-maintenance/extra-updaters.json 2>/dev/null || echo absent)" >> /fixture/trace
        if [ -f /fixture/force-backstop ]; then cat /fixture/force-backstop
        elif [ -f /fixture/loaded-dropin ] && grep -q 'RuntimeMaxSec=85min' /fixture/loaded-dropin; then echo '1h 25min'
        else echo 35min; fi;;
      *PrivateTmp*) if [ -f /fixture/private-tmp ]; then echo yes; else echo no; fi;;
      *MountFlags*) if [ -f /fixture/mount-flags ]; then cat /fixture/mount-flags; else echo; fi;;
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
    [ ! -f /fixture/fail-stop ] || exit 1
    [ ! -x /fixture/on-stop ] || /fixture/on-stop
    ;;
  enable)
    if [ "$*" = 'enable --now dbus.socket' ]; then
      [ -f /fixture/dbus-installed ] || exit 5
      python3 -c "import socket; socket.socket(socket.AF_UNIX).bind('/run/dbus/system_bus_socket')"
    fi;;
  daemon-reload)
    # What systemd would now load: the drop-in as it is on disk at reload time.
    echo "POLICY-AT-RELOAD: $(cat /etc/riot-maintenance/extra-updaters.json 2>/dev/null || echo absent)" >> /fixture/trace
    cp /etc/systemd/system/riot-maintenance.service.d/50-riot-extra-updaters.conf /fixture/loaded-dropin 2>/dev/null || rm -f /fixture/loaded-dropin;;
  start) ;;
  *) exit 3;;
esac
''')
        # EXTRA-UPD-001: log every atomic replacement the installer makes, in order,
        # so write order is observable without reading timestamps.
        if not Path('/usr/bin/mv.fixture-real').exists():
            shutil.copy2('/usr/bin/mv', '/usr/bin/mv.fixture-real')
        write('/usr/bin/mv', '#!/bin/sh\nfor last; do :; done\necho "INSTALLED $last" >> /fixture/trace\nexec /usr/bin/mv.fixture-real "$@"\n')
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
        # SHUTDOWN-SUDO-001 SC-001: the power tools are tripwires, not tools. On
        # systemd-sysv hosts they are symlinks to systemctl, so unlink first and
        # never write through the link. sudo -l needs the paths to exist.
        for name in POWER_TOOLS:
            tool = Path('/usr/sbin') / name
            tool.unlink(missing_ok=True)
            write(tool, '#!/bin/sh\necho "POWER TRIPWIRE ' + name + ' $*" >> /fixture/trace\nexit 99\n')
        subprocess.run(['useradd', '--system', '--no-create-home', '--shell', '/usr/sbin/nologin', 'riot'], check=False, capture_output=True)

    def setUp(self):
        for path in ['/usr/local/lib/riot', '/etc/riot', '/var/lib/riot', '/var/lib/riot-maintenance', '/etc/systemd/system/riot-agent.service.d', '/etc/riot-maintenance', '/etc/systemd/system/riot-maintenance.service.d', '/boot/dietpi']:
            p = Path(path)
            if p.is_symlink(): p.unlink()
            elif p.exists(): shutil.rmtree(p)
        for path in ['/usr/local/bin/riot-agent', '/etc/systemd/system/riot-agent.service', '/etc/systemd/system/riot-maintenance.service', '/etc/sudoers.d/riot-agent', '/fixture/fail-policy', '/fixture/fail-merged-policy', '/fixture/native-lock-proof', '/fixture/old-sudo', '/fixture/fail-dbus', '/fixture/dbus-installed', '/fixture/fail-stop', '/fixture/on-stop', '/fixture/late-link', '/fixture/linked.yaml', '/fixture/real-config.yaml', '/fixture/force-backstop', '/fixture/private-tmp', '/fixture/mount-flags', '/fixture/loaded-dropin', '/boot/dietpi.txt']:
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

    def tearDown(self):
        self.assert_no_power_action()
        self.assert_no_dietpi_action()

    def install(self):
        return subprocess.run(['bash', str(ROOT / 'install.sh'), '--non-interactive'], capture_output=True, text=True)

    # ---- SHUTDOWN-SUDO-001 helpers -------------------------------------------

    def assert_no_power_action(self):
        trace = (ROOT / 'trace').read_text()
        self.assertIsNone(POWER_TRACE_RE.search(trace), 'SC-001: a power verb or power tool reached the fixture:\n' + trace)

    def reinit(self):
        """Back to the setUp state (a legacy reinstall), between subtests."""
        self.setUp()

    def fresh(self, keep_dropin=True):
        """Turn the setUp reinstall state into a fresh install: no agent.yaml."""
        config = Path('/etc/riot/agent.yaml')
        if config.is_symlink() or config.is_file():
            config.unlink()
        elif config.is_dir():
            shutil.rmtree(config)
        if not keep_dropin:
            Path('/etc/sudoers.d/riot-agent').unlink(missing_ok=True)

    def set_config(self, data, mode=0o600, owner=None):
        path = Path('/etc/riot/agent.yaml')
        if path.is_symlink() or path.is_file():
            path.unlink()
        elif path.is_dir():
            shutil.rmtree(path)
        path.write_bytes(data.encode() if isinstance(data, str) else data)
        path.chmod(mode)
        if owner:
            shutil.chown(path, *owner)

    def set_dropin(self, text):
        write('/etc/sudoers.d/riot-agent', text, 0o440)
        self.old_policy = Path('/etc/sudoers.d/riot-agent').read_bytes()

    def run_installer(self, *args, env=None):
        """No terminal: new session, stdin from /dev/null."""
        return subprocess.run(['bash', str(ROOT / 'install.sh'), *args], capture_output=True, text=True,
                              stdin=subprocess.DEVNULL, start_new_session=True, env=env)

    def install_tty(self, answers, *args, timeout=180):
        """Run the installer on a pty and answer its yes/no prompts by text.

        Returns (exit code, output, prompts seen in order). A prompt that is not
        in answers fails the test, so a question that must not be asked is caught.
        """
        import pty
        import select
        import time
        pid, fd = pty.fork()
        if pid == 0:
            os.environ['TERM'] = 'dumb'
            os.execv('/bin/bash', ['bash', str(ROOT / 'install.sh'), *args])
            os._exit(127)
        raw, pending, prompts, unexpected = b'', '', [], []
        deadline = time.monotonic() + timeout
        timed_out = False
        while True:
            left = deadline - time.monotonic()
            if left <= 0:
                timed_out = True
                os.kill(pid, 9)
                break
            ready, _, _ = select.select([fd], [], [], min(left, 10))
            if not ready:
                continue
            try:
                chunk = os.read(fd, 4096)
            except OSError:
                break
            if not chunk:
                break
            raw += chunk
            pending += chunk.decode(errors='replace')
            match = PROMPT_RE.search(pending)
            if match:
                text = match.group(1)
                prompts.append(text)
                if text not in answers:
                    unexpected.append(text)
                os.write(fd, (answers.get(text, '') + '\n').encode())
                pending = ''
        _, status = os.waitpid(pid, 0)
        os.close(fd)
        output = raw.decode(errors='replace').replace('\r\n', '\n')
        self.assertFalse(timed_out, 'installer did not finish on the pty:\n' + output)
        self.assertEqual(unexpected, [], 'prompts that were not expected:\n' + output)
        return os.waitstatus_to_exitcode(status), output, prompts

    @staticmethod
    def answers(reboot='n', shutdown='n', patching='n', terminal='n', docker='n', extra='n'):
        return {Q_REBOOT: reboot, Q_SHUTDOWN: shutdown, Q_PATCHING: patching, Q_EXTRA: extra, Q_TERMINAL: terminal, Q_DOCKER: docker}

    @staticmethod
    def story_config(url=SERVER_URL, reboot=False, shutdown=False, patching=False, terminal=False, bootstrap_key=None, extra=False):
        """The installer's config for these answers, no Docker. With shutdown
        False this is exactly the pre-story layout (checked against the golden)."""
        low = lambda value: 'true' if value else 'false'
        commands = ''
        if reboot or shutdown or patching or extra:
            commands = ('\ncommands:\n  allow_reboot: ' + low(reboot) + ('\n  allow_shutdown: true' if shutdown else '') + '\n  allow_patching: ' + low(patching)
                        + ('\n  ' + EXTRA_LINE if extra else ''))
        host_terminal = '\nhost_terminal:\n  enabled: true' if terminal else ''
        bootstrap = '\n  bootstrap_key: "' + bootstrap_key + '"' if bootstrap_key else ''
        collectors = ''.join('    - ' + name + '\n' for name in ['system', 'cpu', 'memory', 'disk', 'network', 'os_info', 'updates', 'services', 'processes', 'docker', 'security', 'logs', 'ups', 'webservers', 'usb'])
        return ('server:\n  url: "' + url + '"\n  tls_verify: true' + bootstrap + '\n\nagent:\n  poll_interval: 60\n  heartbeat_interval: 15\n\n'
                'collectors:\n  enabled:\n' + collectors + commands + '\n' + host_terminal + '\n\n')

    def dropin(self):
        return Path('/etc/sudoers.d/riot-agent').read_text()

    @staticmethod
    def grant_lines(text):
        return [line for line in text.splitlines() if 'poweroff' in line]

    def riot_lists(self, *argv):
        """Listing only: does riot's sudo policy allow argv? Never runs argv."""
        cmd = ['/usr/bin/sudo.fixture-real', '-n', '-U', 'riot', '-l', *argv]
        return subprocess.run(cmd, capture_output=True, text=True).returncode

    def riot_lists_as_riot(self, *argv):
        cmd = ['runuser', '-u', 'riot', '--', '/usr/bin/sudo.fixture-real', '-n', '-l', *argv]
        return subprocess.run(cmd, capture_output=True, text=True)

    @staticmethod
    def python_reads_enabled(path='/etc/riot/agent.yaml'):
        """Independent strict reader for the shapes the installer and these tests
        write: one column-0 commands:, and in its block a plain allow_shutdown: true."""
        p = Path(path)
        if p.is_symlink() or not p.is_file():
            return False
        lines = p.read_text().splitlines()
        if sum(1 for line in lines if re.match(r'^commands:\s*(#.*)?$', line)) != 1:
            return False
        inside, hits = False, 0
        for line in lines:
            if re.match(r'^commands:\s*(#.*)?$', line):
                inside = True
            elif line and not line.startswith((' ', '#')):
                inside = False
            elif inside and re.match(r'^ +allow_shutdown: true *(#.*)?$', line):
                hits += 1
        return hits == 1

    def assert_agreement(self, expected):
        """FR-022: the drop-in grants poweroff exactly when the final config enables shutdown."""
        grants = self.grant_lines(self.dropin())
        self.assertEqual(len(grants), 1 if expected else 0, 'poweroff grant lines: %r' % grants)
        self.assertEqual(self.python_reads_enabled(), expected, 'final config disagrees with the expected shutdown state')
        self.assertEqual(self.python_reads_enabled(), bool(grants), 'FR-022: grant and config disagree')

    def assert_transparency(self, output, kind):
        """SEC-004 / AD-008: exactly one 'Remote shutdown' line on a reinstall, none on a fresh install."""
        lines = [line for line in output.splitlines() if line.startswith('==> Remote shutdown:')]
        if kind is None:
            self.assertEqual(lines, [], 'a fresh install prints no AD-008 line')
            return
        self.assertEqual(len(lines), 1, 'exactly one AD-008 line expected: %r' % lines)
        expected = {
            'granting': '==> Remote shutdown: commands.allow_shutdown is true in /etc/riot/agent.yaml; granting sudo systemctl poweroff',
            'not_enabled': '==> Remote shutdown: not enabled in /etc/riot/agent.yaml; no poweroff grant (only a plain "allow_shutdown: true" under "commands:" in a regular file enables it)',
            'reenrol': '==> Remote shutdown: re-enrolment resets remote features; no poweroff grant',
        }[kind]
        self.assertEqual(lines[0], expected)

    def resign(self):
        """Rebuild and sign the manifest over every asset currently present."""
        assets = ROOT / 'assets'
        names = sorted(p.name for p in assets.iterdir() if p.name not in ('checksums.txt', 'checksums.txt.sig'))
        manifest = ''.join(hashlib.sha256((assets / name).read_bytes()).hexdigest() + '  ' + name + '\n' for name in names)
        write(assets / 'checksums.txt', manifest, 0o600)
        sig = run('openssl', 'pkeyutl', '-sign', '-inkey', str(ROOT / 'private.pem'), '-rawin', '-in', str(assets / 'checksums.txt')).stdout
        write(assets / 'checksums.txt.sig', base64.b64encode(sig).decode() + '\n', 0o600)

    def darwin_install(self, *args):
        """Run the installer as if on macOS: a uname stub, a darwin asset. Nothing
        macOS-specific is executed; the installer's non-Linux branch only writes files."""
        write(ROOT / 'assets/riot-agent-darwin-arm64', '#!/bin/sh\n# signed fixture darwin\nexit 0\n')
        self.resign()
        write(ROOT / 'darwin-bin/uname', '#!/bin/sh\ncase "$1" in\n  -m) echo arm64;;\n  -s) echo Darwin;;\n  *) echo Darwin;;\nesac\n')
        env = dict(os.environ, PATH=str(ROOT / 'darwin-bin') + ':' + os.environ['PATH'])
        return self.run_installer(*args, env=env)

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

    # ---- SHUTDOWN-SUDO-001: fresh-install prompt (AC-001 to AC-006) ----------

    def test_SS_AC001_fresh_yes_grants_poweroff_and_config_agrees(self):
        self.fresh()
        rc, out, prompts = self.install_tty(self.answers(shutdown='y'), SERVER_URL)
        self.assertEqual(rc, 0, out)
        self.assertEqual(prompts, [Q_REBOOT, Q_SHUTDOWN, Q_PATCHING, Q_TERMINAL])
        self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config(shutdown=True), 'AC-001: config has commands.allow_shutdown: true')
        self.assertEqual(self.grant_lines(self.dropin()), [GRANT_LINE.strip()], 'AC-001: the poweroff grant appears exactly once')
        self.assertEqual(self.riot_lists('/usr/bin/systemctl', 'poweroff'), 0, 'AC-001: sudo -l (listing only) must allow the command')
        self.assert_transparency(out, None)
        self.assert_agreement(True)

    def test_SS_AC002_fresh_no_or_unrecognised_answer_grants_nothing(self):
        for answer in ('', 'n', 'no', 'maybe'):
            with self.subTest(answer=answer):
                self.reinit()
                self.fresh()
                rc, out, prompts = self.install_tty(self.answers(shutdown=answer), SERVER_URL)
                self.assertEqual(rc, 0, out)
                self.assertIn(Q_SHUTDOWN, prompts)
                self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config())
                self.assertEqual(self.grant_lines(self.dropin()), [])
                self.assert_agreement(False)
        with self.subTest(answer='unreadable terminal'):
            self.reinit()
            self.fresh()
            result = self.run_installer(SERVER_URL)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config())
            self.assertEqual(self.grant_lines(self.dropin()), [])
            self.assert_agreement(False)

    def test_SS_AC003_prompt_text_and_order(self):
        docker = Path('/usr/bin/docker')
        write(docker, '#!/bin/sh\nexit 0\n')
        try:
            self.fresh()
            rc, out, prompts = self.install_tty(self.answers(), SERVER_URL)
        finally:
            docker.unlink()
        self.assertEqual(rc, 0, out)
        self.assertEqual(prompts, [Q_REBOOT, Q_SHUTDOWN, Q_PATCHING, Q_TERMINAL, Q_DOCKER], 'AC-003: shutdown sits directly between reboot and patching')
        shown = re.findall(r'^    (Allow [^\n]*?) (\[[yYnN/]+\]): ', out, re.M)
        self.assertEqual(shown, [(q, '[y/N]') for q in prompts], 'AC-003: every question shows its [y/N] hint')
        self.assertLess(out.index('==> Docker detected'), out.index(Q_REBOOT))
        self.assertIn('terminal_enabled: false', Path('/etc/riot/agent.yaml').read_text())

    def test_SS_AC004_template_helper_matches_pre_story_golden(self):
        self.assertEqual(self.story_config(reboot=True, patching=True, terminal=True), PRE_STORY_DARWIN_CONFIG)

    def test_SS_AC004_reboot_and_shutdown_independent(self):
        for reboot in (True, False):
            for shutdown in (True, False):
                with self.subTest(reboot=reboot, shutdown=shutdown):
                    self.reinit()
                    self.fresh()
                    answers = self.answers(reboot='y' if reboot else 'n', shutdown='y' if shutdown else 'n')
                    rc, out, _ = self.install_tty(answers, SERVER_URL)
                    self.assertEqual(rc, 0, out)
                    config = Path('/etc/riot/agent.yaml').read_text()
                    self.assertEqual(config, self.story_config(reboot=reboot, shutdown=shutdown))
                    if not shutdown:
                        self.assertNotIn('allow_shutdown', config)
                    self.assertIn(REBOOT_LINE, self.dropin(), 'FR-033: the reboot grant is always present')
                    self.assertEqual(len(self.grant_lines(self.dropin())), 1 if shutdown else 0)
                    self.assert_agreement(shutdown)

    def test_SS_AC005_yes_flag_answers_yes(self):
        self.fresh()
        result = self.run_installer('-y', SERVER_URL)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('    Allow remote shutdown from dashboard? [y/N]: y (auto)\n', result.stdout)
        self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config(reboot=True, shutdown=True, patching=True, terminal=True))
        self.assert_transparency(result.stdout, None)
        self.assert_agreement(True)

    def test_SS_AC006_non_interactive_and_no_terminal_answer_no(self):
        with self.subTest(mode='--non-interactive'):
            self.fresh()
            result = self.run_installer('--non-interactive', SERVER_URL)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotIn('Allow remote', result.stdout)
            self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config())
            self.assert_agreement(False)
        with self.subTest(mode='no terminal'):
            self.reinit()
            self.fresh()
            tty, aside = Path('/dev/tty'), Path('/dev/tty.aside')
            moved = tty.exists()
            if moved:
                tty.rename(aside)
            try:
                result = self.run_installer(SERVER_URL)
            finally:
                if moved:
                    aside.rename(tty)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotIn('Allow remote shutdown', result.stdout)
            self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config())
            self.assertEqual(self.grant_lines(self.dropin()), [])
            self.assert_agreement(False)

    # ---- sudoers content and validation (AC-007 to AC-009) -------------------

    def test_SS_AC007_existing_lines_byte_identical(self):
        self.fresh()
        self.assertEqual(self.install_tty(self.answers(), SERVER_URL)[0], 0)
        without = Path('/etc/sudoers.d/riot-agent').read_bytes()
        self.reinit()
        self.fresh()
        self.assertEqual(self.install_tty(self.answers(shutdown='y'), SERVER_URL)[0], 0)
        with_grant = Path('/etc/sudoers.d/riot-agent').read_bytes()
        self.assertEqual(without, PRE_STORY_DROPIN.encode(), 'AC-007: the "no" drop-in equals the pre-story drop-in')
        self.assertEqual(with_grant.replace(GRANT_LINE.encode(), b''), without, 'AC-007: removing the grant line from the "yes" drop-in gives the "no" drop-in')
        self.assertEqual(with_grant.decode(), PRE_STORY_DROPIN.replace(REBOOT_LINE, REBOOT_LINE + GRANT_LINE), 'the grant line directly follows the reboot line')

    def test_SS_AC008_only_exact_poweroff_is_granted(self):
        self.fresh()
        self.assertEqual(self.install_tty(self.answers(shutdown='y'), SERVER_URL)[0], 0)
        listing = self.riot_lists_as_riot('/usr/bin/systemctl', 'poweroff')
        self.assertEqual(listing.returncode, 0, listing.stdout + listing.stderr)
        self.assertEqual(self.riot_lists_as_riot('/usr/bin/systemctl', 'reboot').returncode, 0, 'control: the reboot grant is still allowed')
        refused = [
            ['/usr/bin/systemctl', 'poweroff', '--force'], ['/usr/bin/systemctl', 'poweroff', '-i'], ['/usr/bin/systemctl', '-i', 'poweroff'],
            ['/usr/bin/systemctl', 'halt'], ['/usr/bin/systemctl', 'kexec'], ['/usr/bin/systemctl', 'suspend'],
            ['/usr/sbin/shutdown'], ['/usr/sbin/poweroff'], ['/usr/sbin/halt'],
        ]
        for argv in refused:
            with self.subTest(refused=argv):
                self.assertTrue(Path(argv[0]).exists(), 'a refusal only proves policy when the command exists: ' + argv[0])
                probe = self.riot_lists_as_riot(*argv)
                self.assertNotEqual(probe.returncode, 0, 'AC-008: must be refused: %r\n%s%s' % (argv, probe.stdout, probe.stderr))
        text = self.dropin()
        lines = self.grant_lines(text)
        self.assertEqual(lines, [GRANT_LINE.strip()])
        self.assertTrue(lines[0].startswith('riot '))
        self.assertFalse(set('*^$') & set(lines[0]), 'AC-008: no wildcard or argument pattern on the new line')
        for word in ('halt', 'kexec', 'suspend', '/shutdown'):
            self.assertNotIn(word, text, 'AC-008: nothing else power-related is granted: ' + word)

    def test_SS_AC009_policy_validation_still_guards_yes_install(self):
        with self.subTest(failure='candidate policy'):
            self.fresh()
            write(ROOT / 'fail-policy', '', 0o600)
            result = self.run_installer('-y', SERVER_URL)
            self.assertNotEqual(result.returncode, 0, result.stdout)
            self.assertIn('Candidate sudo policy is invalid', result.stderr)
            self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), self.old_policy)
            self.assertNotIn('systemctl stop', (ROOT / 'trace').read_text())
        with self.subTest(failure='effective policy'):
            self.reinit()
            self.fresh()
            write(ROOT / 'fail-merged-policy', '', 0o600)
            result = self.run_installer('-y', SERVER_URL)
            self.assertNotEqual(result.returncode, 0, result.stdout)
            self.assertIn('previous managed policy restored', result.stderr)
            self.assertNotIn('WARNING', result.stderr, 'the policy is restored once, not again by the trap')
            self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), self.old_policy)
            self.assertNotIn('systemctl stop', (ROOT / 'trace').read_text())

    # ---- reinstall with a preserved config (AC-010 to AC-014) ----------------

    def test_SS_AC010_reinstall_true_emits_grant_without_question(self):
        config = 'commands:\n  allow_shutdown: true\n'
        for flags in ((), ('-y',)):
            with self.subTest(flags=flags):
                self.reinit()
                self.set_config(config)
                rc, out, prompts = self.install_tty({}, *flags)
                self.assertEqual(rc, 0, out)
                self.assertEqual(prompts, [])
                self.assertNotIn('Allow remote', out, 'FR-007: a reinstall asks no question')
                self.assertEqual(self.grant_lines(self.dropin()), [GRANT_LINE.strip()])
                self.assert_transparency(out, 'granting')
                self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), config)
                self.assert_agreement(True)

    def test_SS_AC011_reinstall_fails_closed(self):
        cases = {
            'a no commands section': 'server:\n  url: "https://old.test:7331"\n',
            'b commands without the key': 'commands:\n  allow_reboot: true\n',
            'c false': 'commands:\n  allow_shutdown: false\n',
            'd commented out': 'commands:\n  # allow_shutdown: true\n  allow_reboot: true\n',
            'e under docker': 'docker:\n  allow_shutdown: true\ncommands:\n  allow_reboot: true\n',
            'f maybe': 'commands:\n  allow_shutdown: "maybe"\n',
            'g1 tab indentation': 'commands:\n\tallow_shutdown: true\n',
            'g2 unterminated quote before commands': 'agent:\n  device_name: "abc\ncommands:\n  allow_shutdown: true\n',
            'g3 duplicate commands': 'commands:\n  allow_shutdown: true\ncommands:\n  allow_reboot: true\n',
            'g4 NUL byte': b'commands:\n  allow_shutdown: true\n\x00',
            'x True': 'commands:\n  allow_shutdown: True\n',
            'x yes': 'commands:\n  allow_shutdown: yes\n',
            'x unparseable': 'commands: [unclosed\n  allow_shutdown: true\n',
        }
        for name, config in cases.items():
            with self.subTest(case=name):
                self.reinit()
                self.set_config(config)
                result = self.run_installer('--non-interactive')
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(self.grant_lines(self.dropin()), [], 'AC-011: no poweroff grant for ' + name)
                self.assert_transparency(result.stdout, 'not_enabled')
                if name[0] in 'abcdef':
                    self.assert_agreement(False)
        with self.subTest(case='h1 symlink to a regular file holding true'):
            self.reinit()
            write(ROOT / 'real-config.yaml', 'commands:\n  allow_shutdown: true\n', 0o600)
            config = Path('/etc/riot/agent.yaml')
            config.unlink()
            config.symlink_to(ROOT / 'real-config.yaml')
            result = self.run_installer('--non-interactive')
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(self.grant_lines(self.dropin()), [], 'a symlink is never read')
            self.assertEqual((ROOT / 'real-config.yaml').read_text(), 'commands:\n  allow_shutdown: true\n', 'nothing is written through the link')
            self.assert_transparency(result.stdout, 'not_enabled')
            self.assert_agreement(False)
        for name, make in (('h2 dangling symlink', lambda p: p.symlink_to('/nonexistent-target')), ('h3 directory', lambda p: p.mkdir())):
            with self.subTest(case=name):
                self.reinit()
                config = Path('/etc/riot/agent.yaml')
                config.unlink()
                make(config)
                result = self.run_installer('--non-interactive', SERVER_URL)
                self.assert_refused_without_replacement(result)
                self.assertIn('exists but is not a regular file', result.stderr)
                self.assertIn('no files changed', result.stderr)
                self.assertEqual([l for l in result.stdout.splitlines() if l.startswith('==> Remote shutdown:')], [])

    def test_SS_AC012_flags_do_not_override_config(self):
        with self.subTest(case='false with -y'):
            self.set_config('commands:\n  allow_shutdown: false\n')
            result = self.run_installer('-y')
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(self.grant_lines(self.dropin()), [])
            self.assert_agreement(False)
        with self.subTest(case='true with --non-interactive'):
            self.reinit()
            self.set_config('commands:\n  allow_shutdown: true\n')
            result = self.run_installer('--non-interactive')
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(self.grant_lines(self.dropin()), [GRANT_LINE.strip()])
            self.assert_agreement(True)

    def test_SS_AC013_reinstall_revokes_stale_grant(self):
        self.set_dropin(PRE_STORY_DROPIN.replace(REBOOT_LINE, REBOOT_LINE + GRANT_LINE))
        self.set_config('commands:\n  allow_shutdown: false\n')
        result = self.run_installer('--non-interactive')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.dropin(), PRE_STORY_DROPIN, 'AC-013: the grant is gone and every other line is unchanged')
        self.assert_transparency(result.stdout, 'not_enabled')
        self.assert_agreement(False)

    def test_SS_AC014_preserved_config_untouched(self):
        config = ('# kept by hand\nserver:\n    url:   "https://old.test:7331"    # odd spacing\n\n'
                  'commands:\n    allow_reboot: false\n\n\n    # remote shutdown on purpose\n    allow_shutdown: true     # yes\n    allow_patching: false\n')
        self.set_config(config, 0o640, ('riot', 'riot'))
        path = Path('/etc/riot/agent.yaml')
        before = (path.read_bytes(), path.stat().st_uid, path.stat().st_gid, stat.S_IMODE(path.stat().st_mode))
        result = self.run_installer('--non-interactive')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        after = (path.read_bytes(), path.stat().st_uid, path.stat().st_gid, stat.S_IMODE(path.stat().st_mode))
        self.assertEqual(after, before, 'AC-014: bytes, owner and mode are unchanged')
        self.assertEqual(self.grant_lines(self.dropin()), [GRANT_LINE.strip()], 'the odd formatting is still read as enabled')
        self.assert_agreement(True)

    # ---- bootstrap and agreement (AC-015 to AC-017) --------------------------

    def test_SS_AC015_bootstrap_reenrolment_resets_shutdown(self):
        for flags in ((), ('-y',)):
            with self.subTest(flags=flags):
                self.reinit()
                self.set_config('commands:\n  allow_shutdown: true\n')
                self.set_dropin(PRE_STORY_DROPIN.replace(REBOOT_LINE, REBOOT_LINE + GRANT_LINE))
                rc, out, prompts = self.install_tty({}, SERVER_URL, '--bootstrap-key', 'K', *flags)
                self.assertEqual(rc, 0, out)
                self.assertEqual(prompts, [], 'a re-enrolment asks no question')
                self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config(bootstrap_key='K'))
                self.assertEqual(self.grant_lines(self.dropin()), [])
                self.assert_transparency(out, 'reenrol')
                self.assert_agreement(False)

    def test_SS_AC016_bootstrap_on_fresh_install_asks_and_grants(self):
        self.fresh()
        rc, out, prompts = self.install_tty(self.answers(shutdown='y'), SERVER_URL, '--bootstrap-key', 'K')
        self.assertEqual(rc, 0, out)
        self.assertEqual(prompts, [Q_REBOOT, Q_SHUTDOWN, Q_PATCHING, Q_TERMINAL])
        self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config(shutdown=True, bootstrap_key='K'))
        self.assertEqual(self.grant_lines(self.dropin()), [GRANT_LINE.strip()])
        self.assert_transparency(out, None)
        self.assert_agreement(True)

    def test_SS_AC017_agreement_holds_when_the_config_changes_under_the_run(self):
        # The hook runs while the agent is being stopped, the moment riot could still write the file.
        flip_false = "#!/bin/sh\nprintf 'commands:\\n  allow_shutdown: false\\n' > /etc/riot/agent.yaml\n"
        flip_true = "#!/bin/sh\nprintf 'commands:\\n  allow_shutdown: true\\n' > /etc/riot/agent.yaml\n"
        cases = [
            ('reinstall true flipped to false', 'commands:\n  allow_shutdown: true\n', flip_false, ()),
            ('reinstall false flipped to true', 'commands:\n  allow_shutdown: false\n', flip_true, ()),
            ('fresh yes with a planted false config', None, flip_false, ('-y', SERVER_URL)),
        ]
        for name, config, hook, args in cases:
            with self.subTest(case=name):
                self.reinit()
                if config is None:
                    self.fresh()
                else:
                    self.set_config(config)
                write(ROOT / 'on-stop', hook, 0o755)
                result = self.run_installer(*(args or ('--non-interactive',)))
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIn('changed during installation; previous managed policy restored', result.stderr)
                self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), self.old_policy, 'AD-005: the previous policy is back')
                self.assertEqual(Path('/usr/local/bin/riot-agent').read_bytes(), self.old_binary, 'no binary is replaced before agreement')
                self.assertNotIn('WARNING', result.stderr)

    # ---- non-Linux (AC-018) --------------------------------------------------

    def test_SS_AC018_stubbed_darwin_install_unchanged(self):
        self.fresh()
        result = self.darwin_install('-y', SERVER_URL)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(result.stdout, PRE_STORY_DARWIN_STDOUT, 'FR-024: the non-Linux output is the pre-story output')
        self.assertNotIn('shutdown', result.stdout.lower(), 'FR-024: no shutdown question or line on a non-Linux install')
        config = Path('/etc/riot/agent.yaml').read_text()
        self.assertEqual(config, PRE_STORY_DARWIN_CONFIG)
        self.assertNotIn('allow_shutdown', config)

    # ---- containment, failure, bounded growth --------------------------------

    def test_SS_OQA2_failure_before_agreement_restores_previous_policy(self):
        self.fresh()
        write(ROOT / 'fail-stop', '', 0o600)
        result = self.run_installer('-y', SERVER_URL)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), self.old_policy, 'AD-006: no grant is left behind without a config')
        self.assertEqual(self.grant_lines(self.dropin()), [])
        self.assertEqual(Path('/usr/local/bin/riot-agent').read_bytes(), self.old_binary)
        self.assertFalse(Path('/etc/riot/agent.yaml').exists(), 'the failed fresh run wrote no config')

    def test_SS_OQA2_failure_with_no_previous_dropin_leaves_none(self):
        self.fresh(keep_dropin=False)
        write(ROOT / 'fail-stop', '', 0o600)
        result = self.run_installer('-y', SERVER_URL)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertFalse(Path('/etc/sudoers.d/riot-agent').exists(), 'AD-006: a clean host is returned to a clean host')

    def test_SS_A003_bare_systemctl_resolves_to_granted_path(self):
        self.fresh()
        self.assertEqual(self.install_tty(self.answers(shutdown='y'), SERVER_URL)[0], 0)
        listing = self.riot_lists_as_riot('systemctl', 'poweroff')
        self.assertEqual(listing.returncode, 0, listing.stdout + listing.stderr)
        self.assertIn('/usr/bin/systemctl poweroff', listing.stdout, 'sudo resolves the bare command to the path the installer wrote')

    def fs_paths(self):
        paths = set()
        for top in ('/etc', '/var', '/usr/local', '/run'):
            for dirpath, dirs, files in os.walk(top):
                if dirpath.startswith('/run/lock'):
                    dirs[:] = []
                    continue
                for name in dirs + files:
                    paths.add(re.sub(r'riot_install\.[A-Za-z0-9]+', 'riot_install.X', os.path.join(dirpath, name)))
        return paths

    def backup_files(self):
        backups = [p for p in Path('/var/backups').glob('riot_install.*') if p.is_dir()]
        self.assertEqual(len(backups), 1, 'exactly one backup directory per run')
        return sorted(str(p.relative_to(backups[0])) for p in backups[0].rglob('*'))

    def measured_run(self, setup, runner):
        self.reinit()
        for stale in Path('/var/backups').glob('riot_install.*'):
            shutil.rmtree(stale)
        setup()
        write(ROOT / 'trace', '', 0o600)
        result = runner()
        self.assertEqual(result.returncode if hasattr(result, 'returncode') else result[0], 0, str(result))
        downloads = [line for line in (ROOT / 'trace').read_text().splitlines() if line.startswith('DOWNLOAD')]
        return self.fs_paths(), self.backup_files(), len(self.dropin().splitlines()), downloads

    def test_SS_AC029_bounded_growth(self):
        expected_downloads = ['DOWNLOAD checksums.txt', 'DOWNLOAD checksums.txt.sig', 'DOWNLOAD riot-agent-linux-amd64',
                              'DOWNLOAD riot-agent-apply-linux-amd64', 'DOWNLOAD riot-maintenance-linux-amd64', 'DOWNLOAD riot-maintenance.service']
        fresh_no = self.measured_run(self.fresh, lambda: self.install_tty(self.answers(), SERVER_URL))
        fresh_yes = self.measured_run(self.fresh, lambda: self.install_tty(self.answers(shutdown='y'), SERVER_URL))
        self.assertEqual(fresh_yes[0] ^ fresh_no[0], set(), 'NFR-002: a "yes" install creates no extra path')
        self.assertEqual(fresh_yes[1], fresh_no[1], 'the backup holds the same files')
        self.assertEqual(fresh_yes[2] - fresh_no[2], 1, 'the drop-in grows by exactly one line')
        self.assertEqual(fresh_no[3], expected_downloads)
        self.assertEqual(fresh_yes[3], expected_downloads, 'NFR-003: no extra network request')
        re_off = self.measured_run(lambda: self.set_config('commands:\n  allow_shutdown: false\n'), lambda: self.run_installer('--non-interactive'))
        re_on = self.measured_run(lambda: self.set_config('commands:\n  allow_shutdown: true\n'), lambda: self.run_installer('--non-interactive'))
        self.assertEqual(re_on[0] ^ re_off[0], set(), 'a reinstall with the grant creates no extra path')
        self.assertEqual(re_on[1], re_off[1])
        self.assertEqual(re_on[2] - re_off[2], 1)
        self.assertEqual(re_on[3], expected_downloads)
        self.assertEqual(re_off[3], expected_downloads)

    # ---- security review conditions (SEC-001, SEC-003) -----------------------

    def test_SS_SEC001_hardlinked_config_refused_before_any_change(self):
        for flags in (('--non-interactive',), ('--non-interactive', '--bootstrap-key', 'K', SERVER_URL)):
            with self.subTest(flags=flags):
                self.reinit()
                self.set_config('commands:\n  allow_shutdown: true\n')
                path = Path('/etc/riot/agent.yaml')
                os.link(path, ROOT / 'linked.yaml')
                before = (path.read_bytes(), path.stat().st_ino, path.stat().st_nlink)
                result = self.run_installer(*flags)
                self.assert_refused_without_replacement(result)
                self.assertIn('has 2 links', result.stderr)
                self.assertIn('no files changed', result.stderr)
                self.assertEqual((path.read_bytes(), path.stat().st_ino, path.stat().st_nlink), before, 'SEC-001: the linked file is neither read into a decision nor changed')
                (ROOT / 'linked.yaml').unlink()

    def test_SS_SEC001_late_checks_run_after_the_stop_and_roll_back_the_grant(self):
        hooks = {
            'hard link appears': ("#!/bin/sh\nln /etc/riot/agent.yaml /fixture/late-link\n", 'has 2 links'),
            'dangling symlink appears': ("#!/bin/sh\nrm -f /etc/riot/agent.yaml\nln -s /nonexistent-late /etc/riot/agent.yaml\n", 'is not a regular file'),
            'directory appears': ("#!/bin/sh\nrm -f /etc/riot/agent.yaml\nmkdir /etc/riot/agent.yaml\n", 'is not a regular file'),
        }
        for name, (hook, message) in hooks.items():
            with self.subTest(case=name):
                self.reinit()
                self.set_config('commands:\n  allow_shutdown: true\n')
                write(ROOT / 'on-stop', hook, 0o755)
                result = self.run_installer('--non-interactive')
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIn('changed during installation and', result.stderr)
                self.assertIn(message, result.stderr)
                self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), self.old_policy, 'SEC-003: the pending grant is rolled back on the late refusal')
                self.assertEqual(Path('/usr/local/bin/riot-agent').read_bytes(), self.old_binary)
                self.assertNotIn('WARNING', result.stderr)


    # ---- Added by QA Engineer: adversarial probes (SHUTDOWN-SUDO-001) -------

    # Added by QA Engineer. Covers AC-001, AC-005, AC-017, FR-022. A Docker host
    # answered yes to everything, with an API key and a certificate pin, still
    # gets the grant and a config the agreement check accepts.
    def test_QA_fresh_yes_with_docker_key_and_pin_keeps_agreement(self):
        docker = Path('/usr/bin/docker')
        write(docker, '#!/bin/sh\nexit 0\n')
        try:
            self.fresh()
            result = self.run_installer('-y', '--key', 'k#1:x', '--fingerprint', 'SHA256:AA:BB:CC', SERVER_URL)
        finally:
            docker.unlink()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        config = Path('/etc/riot/agent.yaml').read_text()
        self.assertIn('terminal_enabled: true', config)
        self.assertIn('allow_shutdown: true', config)
        self.assertIn('api_key: "k#1:x"', config)
        self.assert_agreement(True)

    # Added by QA Engineer. Covers AC-010, AC-017, AC-029. The same action twice
    # in a row: the second run is a reinstall, asks nothing, and leaves the
    # drop-in byte-identical with exactly one grant.
    def test_QA_second_run_after_a_yes_install_is_stable(self):
        self.fresh()
        first = self.run_installer('-y', SERVER_URL)
        self.assertEqual(first.returncode, 0, first.stdout + first.stderr)
        first_dropin = Path('/etc/sudoers.d/riot-agent').read_bytes()
        first_config = Path('/etc/riot/agent.yaml').read_bytes()
        second = self.run_installer('-y')
        self.assertEqual(second.returncode, 0, second.stdout + second.stderr)
        self.assertNotIn('Allow remote', second.stdout)
        self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), first_dropin)
        self.assertEqual(Path('/etc/riot/agent.yaml').read_bytes(), first_config)
        self.assert_transparency(second.stdout, 'granting')
        self.assert_agreement(True)

    # Added by QA Engineer. Covers OQ-A2, SEC-003, AD-006. TERM sent to the
    # installer while the agent is being stopped (after the grant is installed,
    # before the agreement check): the trap puts the previous policy back.
    def test_QA_sigterm_between_install_and_agreement_restores_previous_policy(self):
        self.fresh()
        hook = "#!/bin/sh\ninstaller=$(awk '{print $4}' /proc/$PPID/stat)\nkill -TERM \"$installer\"\nsleep 3\n"
        write(ROOT / 'on-stop', hook, 0o755)
        result = self.run_installer('-y', SERVER_URL)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), self.old_policy, 'AD-006: TERM must not leave the new grant behind')
        self.assertEqual(self.grant_lines(self.dropin()), [])
        self.assertEqual(Path('/usr/local/bin/riot-agent').read_bytes(), self.old_binary)
        self.assertFalse(Path('/etc/riot/agent.yaml').exists())
        self.assertNotIn('WARNING', result.stderr)

    # Added by QA Engineer. Covers OQ-A1, AD-001. Ctrl-C at a prompt, after the
    # shutdown question was answered yes, leaves the host exactly as it was.
    def test_QA_ctrl_c_at_a_prompt_leaves_the_host_untouched(self):
        import pty
        import select
        import time
        self.fresh()
        backups_before = set(Path('/var/backups').glob('riot_install.*'))
        pid, fd = pty.fork()
        if pid == 0:
            os.environ['TERM'] = 'dumb'
            os.execv('/bin/bash', ['bash', str(ROOT / 'install.sh'), SERVER_URL])
            os._exit(127)
        raw, sent = b'', set()
        script = [(b'Allow remote reboot from dashboard? [y/N]: ', b'n\n'),
                  (b'Allow remote shutdown from dashboard? [y/N]: ', b'y\n'),
                  (b'Allow remote OS patching from dashboard? [y/N]: ', b'\x03')]
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            ready, _, _ = select.select([fd], [], [], 5)
            if not ready:
                continue
            try:
                chunk = os.read(fd, 4096)
            except OSError:
                break
            if not chunk:
                break
            raw += chunk
            for marker, reply in script:
                if marker in raw and marker not in sent:
                    sent.add(marker)
                    os.write(fd, reply)
        finished, status = os.waitpid(pid, os.WNOHANG)
        if finished == 0:
            os.kill(pid, 9)
            os.waitpid(pid, 0)
            self.fail('installer did not stop after Ctrl-C:\n' + raw.decode(errors='replace'))
        os.close(fd)
        self.assertEqual(len(sent), 3, raw.decode(errors='replace'))
        self.assertNotEqual(os.waitstatus_to_exitcode(status), 0)
        self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), self.old_policy)
        self.assertEqual(Path('/usr/local/bin/riot-agent').read_bytes(), self.old_binary)
        self.assertFalse(Path('/etc/riot/agent.yaml').exists())
        self.assertEqual(set(Path('/var/backups').glob('riot_install.*')), backups_before, 'no backup directory is created before the prompts end')
        self.assertEqual(list(Path('/tmp').glob('riot_install.*')), [], 'the stage directory is removed by the trap')
        trace = (ROOT / 'trace').read_text()
        self.assertNotIn('systemctl stop', trace)
        self.assertNotIn('PACKAGE', trace)

    # Added by QA Engineer. Covers AC-011 (h), SEC-001. A FIFO in place of
    # agent.yaml is refused before any change and cannot hang the run.
    def test_QA_fifo_agent_yaml_is_refused_without_hanging(self):
        self.reinit()
        config = Path('/etc/riot/agent.yaml')
        config.unlink()
        os.mkfifo(config)
        result = subprocess.run(['bash', str(ROOT / 'install.sh'), '--non-interactive', SERVER_URL], capture_output=True, text=True,
                                stdin=subprocess.DEVNULL, start_new_session=True, timeout=90)
        self.assert_refused_without_replacement(result)
        self.assertIn('exists but is not a regular file', result.stderr)
        self.assertIn('no files changed', result.stderr)


    # ---- EXTRA-UPD-001: DietPi opt-in (AC-015 to AC-019, AC-044, AC-045) ----

    def assert_no_dietpi_action(self):
        trace = (ROOT / 'trace').read_text()
        self.assertIsNone(DIETPI_TRIPWIRE_RE.search(trace), 'SC-001/AC-045: a DietPi program was executed:\n' + trace)

    def dietpi(self, executable=False):
        """A fake DietPi host. Every program is a tripwire; the marker files only
        need to exist as regular files. executable=True is for sudo -l listings,
        which need the command to exist and never run it."""
        mode = 0o755 if executable else 0o644
        write('/boot/dietpi/.version', DIETPI_VERSION, 0o644)
        for name in ('dietpi-update', 'dietpi-software', 'dietpi-services', 'func/dietpi-set_software'):
            write('/boot/dietpi/' + name, DIETPI_TRIPWIRE, mode)
        write('/boot/dietpi.txt', 'CONFIG_CHECK_DIETPI_UPDATES=1\n', 0o644)

    def listed_config(self, body='  allow_extra_updaters: [dietpi]\n', extra_top=''):
        return extra_top + 'commands:\n' + body

    def opted_in(self):
        return EXTRA_POLICY.exists() and EXTRA_POLICY.read_text() == POLICY_ON

    def assert_recorded(self, on):
        """Both root-side files agree: opted in (85min) or recorded as revoked (35min)."""
        if on:
            self.assertEqual(EXTRA_POLICY.read_text(), POLICY_ON)
            self.assertEqual(EXTRA_DROPIN.read_text(), DROPIN_ON)
        else:
            self.assertEqual(EXTRA_POLICY.read_text(), POLICY_OFF)
            self.assertEqual(EXTRA_DROPIN.read_text(), DROPIN_OFF)

    def assert_nothing_recorded(self):
        self.assertFalse(EXTRA_POLICY.exists(), 'no policy file may be created')
        self.assertFalse(EXTRA_DROPIN.exists(), 'no drop-in may be created')
        self.assertFalse(Path('/etc/riot-maintenance').exists())
        self.assertFalse(Path('/etc/systemd/system/riot-maintenance.service.d').exists())

    def installed_order(self):
        return [line[len('INSTALLED '):] for line in (ROOT / 'trace').read_text().splitlines() if line.startswith('INSTALLED ')]

    def test_EU_AC015_fresh_question_dietpi_only(self):
        with self.subTest(case='interactive yes on DietPi'):
            self.dietpi()
            self.fresh()
            rc, out, prompts = self.install_tty(self.answers(extra='y'), SERVER_URL)
            self.assertEqual(rc, 0, out)
            self.assertEqual(prompts, [Q_REBOOT, Q_SHUTDOWN, Q_PATCHING, Q_EXTRA, Q_TERMINAL], 'the DietPi question sits directly after the OS patching question')
            shown = re.findall(r'^    (Allow rIOt to apply DietPi[^\n]*?) (\[[yYnN/]+\]): ', out, re.M)
            self.assertEqual(shown, [(Q_EXTRA, '[y/N]')], 'the question shows the [y/N] hint')
            self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config(extra=True))
            self.assert_recorded(True)
            self.assert_agreement(False)
        for answer in ('', 'n', 'no', 'maybe'):
            with self.subTest(case='answer %r on DietPi' % answer):
                self.reinit()
                self.dietpi()
                self.fresh()
                rc, out, prompts = self.install_tty(self.answers(extra=answer), SERVER_URL)
                self.assertEqual(rc, 0, out)
                self.assertIn(Q_EXTRA, prompts)
                self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config())
                self.assert_nothing_recorded()
        with self.subTest(case='-y on DietPi'):
            self.reinit()
            self.dietpi()
            self.fresh()
            result = self.run_installer('-y', SERVER_URL)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('    ' + Q_EXTRA + ' [y/N]: y (auto)\n', result.stdout)
            self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config(reboot=True, shutdown=True, patching=True, terminal=True, extra=True))
            self.assert_recorded(True)
        with self.subTest(case='--non-interactive on DietPi'):
            self.reinit()
            self.dietpi()
            self.fresh()
            result = self.run_installer('--non-interactive', SERVER_URL)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotIn('DietPi updates during', result.stdout)
            self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config())
            self.assert_nothing_recorded()
        with self.subTest(case='no terminal on DietPi'):
            self.reinit()
            self.dietpi()
            self.fresh()
            tty, aside = Path('/dev/tty'), Path('/dev/tty.aside')
            moved = tty.exists()
            if moved:
                tty.rename(aside)
            try:
                result = self.run_installer(SERVER_URL)
            finally:
                if moved:
                    aside.rename(tty)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assert_nothing_recorded()
        with self.subTest(case='interactive on a host without DietPi'):
            self.reinit()
            self.fresh()
            rc, out, prompts = self.install_tty(self.answers(extra='y'), SERVER_URL)
            self.assertEqual(rc, 0, out)
            self.assertEqual(prompts, [Q_REBOOT, Q_SHUTDOWN, Q_PATCHING, Q_TERMINAL], 'the question is not asked without DietPi')
            self.assertEqual(Path('/etc/riot/agent.yaml').read_text(), self.story_config())
            self.assert_nothing_recorded()
        with self.subTest(case='-y on a host without DietPi'):
            self.reinit()
            self.fresh()
            result = self.run_installer('-y', SERVER_URL)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotIn('DietPi', result.stdout)
            self.assert_nothing_recorded()
        with self.subTest(case='only one of the two DietPi files'):
            self.reinit()
            write('/boot/dietpi/.version', DIETPI_VERSION, 0o644)
            self.fresh()
            result = self.run_installer('-y', SERVER_URL)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotIn('DietPi updates during', result.stdout)
            self.assert_nothing_recorded()

    def test_EU_AC016_reinstall_fail_closed_shapes(self):
        self.dietpi()
        cases = {
            'a flow list': ('commands:\n  allow_extra_updaters: [dietpi]\n', True),
            'b flow list with spaces and a comment': ('commands:\n  allow_reboot: false\n  allow_extra_updaters: [ snap , dietpi ]   # both\n', True),
            'c block list': ('commands:\n  allow_extra_updaters:\n    - dietpi\n  allow_patching: true\n', True),
            'd block list with a comment': ('commands:\n  allow_extra_updaters:   # list\n    - snap\n    - dietpi # ours\n', True),
            'e no key': ('commands:\n  allow_patching: true\n', False),
            'f commented line': ('commands:\n  # allow_extra_updaters: [dietpi]\n  allow_patching: true\n', False),
            'g scalar': ('commands:\n  allow_extra_updaters: dietpi\n', False),
            'h scalar true': ('commands:\n  allow_extra_updaters: true\n', False),
            'i quoted items': ('commands:\n  allow_extra_updaters: ["dietpi"]\n', False),
            'j list under docker': ('docker:\n  allow_extra_updaters: [dietpi]\ncommands:\n  allow_patching: true\n', False),
            'k list under a nested key': ('commands:\n  sub:\n    allow_extra_updaters: [dietpi]\n', False),
            'l unparseable yaml': ('commands: [unclosed\n  allow_extra_updaters: [dietpi]\n', False),
            'm duplicate key': ('commands:\n  allow_extra_updaters: [dietpi]\n  allow_extra_updaters: [dietpi]\n', False),
            'n duplicate commands': ('commands:\n  allow_extra_updaters: [dietpi]\ncommands:\n  allow_patching: true\n', False),
            'o other identifier only': ('commands:\n  allow_extra_updaters: [snap]\n', False),
            'p empty list': ('commands:\n  allow_extra_updaters: []\n', False),
            'q upper case identifier': ('commands:\n  allow_extra_updaters: [DietPi]\n', False),
            'r items at the key indentation': ('commands:\n  allow_extra_updaters:\n  - dietpi\n', False),
            's tab indentation': ('commands:\n\tallow_extra_updaters: [dietpi]\n', False),
            't NUL byte': (b'commands:\n  allow_extra_updaters: [dietpi]\n\x00', False),
            'u nested sequence item': ('commands:\n  allow_extra_updaters:\n    - [dietpi]\n', False),
        }
        for name, (config, recorded) in cases.items():
            with self.subTest(case=name):
                self.reinit()
                self.dietpi()
                self.set_config(config, 0o640, ('riot', 'riot'))
                path = Path('/etc/riot/agent.yaml')
                before = (path.read_bytes(), path.stat().st_uid, path.stat().st_gid, stat.S_IMODE(path.stat().st_mode))
                result = self.run_installer('--non-interactive')
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertNotIn('Allow rIOt', result.stdout, 'a reinstall asks no question')
                after = (path.read_bytes(), path.stat().st_uid, path.stat().st_gid, stat.S_IMODE(path.stat().st_mode))
                self.assertEqual(after, before, 'the preserved config bytes, owner and mode are unchanged')
                if recorded:
                    self.assert_recorded(True)
                    self.assertIn('lists dietpi in /etc/riot/agent.yaml; recording root-side opt-in', result.stdout)
                else:
                    self.assertFalse(self.opted_in(), 'no opt-in may be recorded for ' + name)
                    self.assertNotIn('recording root-side opt-in', result.stdout)
        with self.subTest(case='reinstall asks nothing on a terminal'):
            self.reinit()
            self.dietpi()
            self.set_config(self.listed_config())
            rc, out, prompts = self.install_tty({})
            self.assertEqual(rc, 0, out)
            self.assertEqual(prompts, [])
            self.assert_recorded(True)
        with self.subTest(case='a symlinked config is never read'):
            self.reinit()
            self.dietpi()
            write(ROOT / 'real-config.yaml', self.listed_config(), 0o600)
            config = Path('/etc/riot/agent.yaml')
            config.unlink()
            config.symlink_to(ROOT / 'real-config.yaml')
            result = self.run_installer('--non-interactive')
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertFalse(self.opted_in())

    def test_EU_AC016_transparency_line_on_every_dietpi_reinstall(self):
        def lines(output):
            return [line for line in output.splitlines() if line.startswith('==> DietPi updates:')]
        with self.subTest(case='listed on DietPi'):
            self.dietpi()
            self.set_config(self.listed_config())
            result = self.run_installer('--non-interactive')
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(lines(result.stdout)[0], '==> DietPi updates: commands.allow_extra_updaters lists dietpi in /etc/riot/agent.yaml; recording root-side opt-in')
        with self.subTest(case='not listed on DietPi'):
            self.reinit()
            self.dietpi()
            self.set_config('commands:\n  allow_patching: true\n')
            result = self.run_installer('--non-interactive')
            self.assertEqual(lines(result.stdout), ['==> DietPi updates: commands.allow_extra_updaters does not list dietpi in /etc/riot/agent.yaml; no root-side opt-in'])
        with self.subTest(case='listed but DietPi absent'):
            self.reinit()
            self.set_config(self.listed_config())
            result = self.run_installer('--non-interactive')
            self.assertEqual(lines(result.stdout), ['==> DietPi updates: commands.allow_extra_updaters lists dietpi in /etc/riot/agent.yaml but DietPi is not present; nothing recorded'])
        with self.subTest(case='re-enrolment on DietPi'):
            self.reinit()
            self.dietpi()
            self.set_config(self.listed_config())
            result = self.run_installer('--non-interactive', SERVER_URL, '--bootstrap-key', 'K')
            self.assertEqual(lines(result.stdout), ['==> DietPi updates: re-enrolment resets remote features; no root-side opt-in'])
            self.assertFalse(self.opted_in())
        with self.subTest(case='a plain host prints nothing about DietPi'):
            self.reinit()
            self.set_config('commands:\n  allow_patching: true\n')
            result = self.run_installer('--non-interactive')
            self.assertEqual(lines(result.stdout), [])
            self.assertNotIn('DietPi', result.stdout)

    def test_EU_AC017_non_dietpi_records_nothing_and_root_owned(self):
        with self.subTest(case='a config that lists dietpi on a host without DietPi'):
            self.set_config(self.listed_config())
            result = self.run_installer('--non-interactive')
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assert_nothing_recorded()
        with self.subTest(case='files after an opt-in install are root-owned and not writable by riot'):
            self.reinit()
            self.dietpi()
            self.set_config(self.listed_config())
            result = self.run_installer('--non-interactive')
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            for path, mode in ((EXTRA_POLICY, 0o644), (EXTRA_DROPIN, 0o644), (EXTRA_POLICY.parent, 0o755), (EXTRA_DROPIN.parent, 0o755)):
                info = path.stat()
                self.assertEqual((info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)), (0, 0, mode), str(path))
            for path in (EXTRA_POLICY, EXTRA_DROPIN):
                probe = subprocess.run(['runuser', '-u', 'riot', '--', 'test', '-w', str(path)], capture_output=True)
                self.assertNotEqual(probe.returncode, 0, 'riot must not be able to write ' + str(path))
                write_try = subprocess.run(['runuser', '-u', 'riot', '--', 'sh', '-c', 'echo x >> "$1"', '_', str(path)], capture_output=True)
                self.assertNotEqual(write_try.returncode, 0)
            probe = subprocess.run(['runuser', '-u', 'riot', '--', 'sh', '-c', 'touch "$1/new"', '_', str(EXTRA_POLICY.parent)], capture_output=True)
            self.assertNotEqual(probe.returncode, 0, 'riot must not create files beside the policy')
            self.assertTrue(os.access(EXTRA_POLICY, os.R_OK), 'readable so the agent can compute readiness')
        with self.subTest(case='a symlinked policy is refused before any change'):
            self.reinit()
            self.dietpi()
            self.set_config(self.listed_config())
            EXTRA_POLICY.parent.mkdir(parents=True, exist_ok=True)
            EXTRA_POLICY.symlink_to('/etc/passwd')
            result = self.run_installer('--non-interactive')
            self.assert_refused_without_replacement(result)
            self.assertIn('Unsafe privileged file', result.stderr)
        with self.subTest(case='a writable policy directory is refused'):
            self.reinit()
            self.dietpi()
            self.set_config(self.listed_config())
            EXTRA_POLICY.parent.mkdir(parents=True, exist_ok=True)
            EXTRA_POLICY.parent.chmod(0o777)
            result = self.run_installer('--non-interactive')
            self.assert_refused_without_replacement(result)
            self.assertIn('root-owned and not group/world writable', result.stderr)

    def test_EU_AC018_revocation_policy_first_then_35min(self):
        self.dietpi()
        self.set_config(self.listed_config())
        first = self.run_installer('--non-interactive')
        self.assertEqual(first.returncode, 0, first.stdout + first.stderr)
        self.assert_recorded(True)
        write(ROOT / 'trace', '', 0o600)
        self.set_config('commands:\n  allow_patching: true\n')
        result = self.run_installer('--non-interactive')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assert_recorded(False)
        self.assertTrue(EXTRA_POLICY.exists() and EXTRA_DROPIN.exists(), 'revocation rewrites the files, it never deletes them')
        order = self.installed_order()
        policy_at, unit_at, dropin_at = order.index(str(EXTRA_POLICY)), order.index('/etc/systemd/system/riot-maintenance.service'), order.index(str(EXTRA_DROPIN))
        self.assertLess(policy_at, unit_at, 'the policy is emptied before any unit file is installed')
        self.assertLess(policy_at, dropin_at, 'the policy is emptied before the backstop is lowered')
        trace = (ROOT / 'trace').read_text()
        self.assertIn('POLICY-AT-RELOAD: ' + POLICY_OFF.strip(), trace, 'the policy was already empty at daemon-reload')
        self.assertIn('==> DietPi updates: root-side opt-in revoked', result.stdout)
        with self.subTest(case='a repeated run keeps both files byte-identical'):
            again = self.run_installer('--non-interactive')
            self.assertEqual(again.returncode, 0, again.stdout + again.stderr)
            self.assert_recorded(False)

    def test_EU_AC018_re_adding_dietpi_opts_in_again(self):
        self.dietpi()
        self.set_config(self.listed_config())
        self.assertEqual(self.run_installer('--non-interactive').returncode, 0)
        self.set_config('commands:\n  allow_patching: true\n')
        self.assertEqual(self.run_installer('--non-interactive').returncode, 0)
        self.assert_recorded(False)
        self.set_config(self.listed_config())
        self.assertEqual(self.run_installer('--non-interactive').returncode, 0)
        self.assert_recorded(True)

    def test_EU_AC016_bootstrap_reenrolment_revokes(self):
        self.dietpi()
        self.set_config(self.listed_config())
        self.assertEqual(self.run_installer('--non-interactive').returncode, 0)
        self.assert_recorded(True)
        result = self.run_installer('--non-interactive', SERVER_URL, '--bootstrap-key', 'K')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assert_recorded(False)
        self.assertNotIn('allow_extra_updaters', Path('/etc/riot/agent.yaml').read_text())

    def test_EU_AC019_sudo_lists_no_dietpi_program(self):
        self.dietpi(executable=True)
        self.set_config(self.listed_config())
        result = self.run_installer('--non-interactive')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assert_recorded(True)
        self.assertNotIn('dietpi', self.dropin().lower(), 'FR-021: no sudoers line names a DietPi program')
        refused = [
            ['/boot/dietpi/dietpi-update', '1'], ['/boot/dietpi/dietpi-update', '2'], ['/boot/dietpi/dietpi-update'],
            ['/boot/dietpi/dietpi-software'], ['/boot/dietpi/dietpi-services', 'stop'], ['/boot/dietpi/func/dietpi-set_software', 'x'],
        ]
        for argv in refused:
            with self.subTest(refused=argv):
                self.assertTrue(Path(argv[0]).exists(), 'a refusal only proves policy when the command exists: ' + argv[0])
                probe = self.riot_lists_as_riot(*argv)
                self.assertNotEqual(probe.returncode, 0, 'AF-6: must be refused: %r\n%s%s' % (argv, probe.stdout, probe.stderr))
        listing = self.riot_lists_as_riot().stdout
        self.assertNotIn('dietpi', listing.lower())

    def test_EU_AC044_sudoers_golden_unchanged(self):
        with self.subTest(case='non-DietPi host'):
            self.fresh()
            self.assertEqual(self.install_tty(self.answers(), SERVER_URL)[0], 0)
            self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), PRE_STORY_DROPIN.encode(), 'AC-044: the drop-in equals the pre-story golden')
            self.assert_nothing_recorded()
        with self.subTest(case='DietPi host, opted in: still no sudoers change'):
            self.reinit()
            self.dietpi()
            self.fresh()
            self.assertEqual(self.install_tty(self.answers(extra='y'), SERVER_URL)[0], 0)
            self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), PRE_STORY_DROPIN.encode(), 'the opt-in adds no sudoers line')
        with self.subTest(case='the unit file is the signed asset, unchanged'):
            self.assertEqual(Path('/etc/systemd/system/riot-maintenance.service').read_bytes(), (ROOT / 'assets/riot-maintenance.service').read_bytes())

    def test_EU_AC044_non_dietpi_install_leaves_the_same_paths_as_before(self):
        listed = self.measured_run(lambda: self.set_config(self.listed_config()), lambda: self.run_installer('--non-interactive'))
        plain = self.measured_run(lambda: self.set_config('commands:\n  allow_patching: true\n'), lambda: self.run_installer('--non-interactive'))
        self.assertEqual(listed[0] ^ plain[0], set(), 'a host without DietPi gains no policy file, no drop-in and no directory, whatever the config lists')
        self.assertEqual(listed[1], plain[1], 'the backup holds the same files')
        self.assertEqual(listed[2], plain[2])

    def test_EU_AC029_opt_in_growth_is_bounded(self):
        def prepare(config):
            def go():
                self.dietpi()
                self.set_config(config)
            return go
        plain = self.measured_run(prepare('commands:\n  allow_patching: true\n'), lambda: self.run_installer('--non-interactive'))
        opted = self.measured_run(prepare(self.listed_config()), lambda: self.run_installer('--non-interactive'))
        def opted_in_already():
            prepare(self.listed_config())()
            self.assertEqual(self.run_installer('--non-interactive').returncode, 0)
            for stale in Path('/var/backups').glob('riot_install.*'):
                shutil.rmtree(stale)
        twice = self.measured_run(opted_in_already, lambda: self.run_installer('--non-interactive'))
        live = lambda paths: {p for p in paths if not p.startswith('/var/backups')}
        self.assertEqual(sorted(live(opted[0]) - live(plain[0])),
                         ['/etc/riot-maintenance', '/etc/riot-maintenance/extra-updaters.json', '/etc/systemd/system/riot-maintenance.service.d', '/etc/systemd/system/riot-maintenance.service.d/50-riot-extra-updaters.conf'],
                         'an opt-in creates exactly the policy, the drop-in and their two directories')
        self.assertEqual(live(twice[0]), live(opted[0]), 'a repeated run creates no further path outside the one backup it always makes')
        kept = [p for p in twice[1] if p.endswith('etc/riot-maintenance') or '/riot-maintenance/' in p or p.endswith('riot-maintenance.service.d') or '/riot-maintenance.service.d/' in p]
        self.assertEqual(len(kept), 4, 'the backup of an opted-in host holds the two files and their two directories: %r' % kept)
        self.assertEqual(EXTRA_POLICY.stat().st_size, len(POLICY_ON), 'NFR-002: the policy is one small fixed-size record')
        self.assertEqual(opted[3], plain[3], 'no extra network request')

    def test_EU_SEC002_write_order_optin_and_revoke(self):
        self.dietpi()
        self.set_config(self.listed_config())
        result = self.run_installer('--non-interactive')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        trace = (ROOT / 'trace').read_text()
        order = self.installed_order()
        self.assertLess(order.index(str(EXTRA_DROPIN)), order.index(str(EXTRA_POLICY)), 'opt-in: the drop-in is written before the policy')
        self.assertIn('POLICY-AT-RELOAD: absent', trace, 'opt-in: the policy does not exist when systemd reloads')
        self.assertIn('POLICY-AT-VERIFY: absent', trace, 'opt-in: the effective backstop is verified before the policy is written')
        self.assertLess(trace.index('systemctl daemon-reload'), trace.index('POLICY-AT-VERIFY'))
        self.assertLess(trace.index('POLICY-AT-VERIFY'), trace.index('systemctl start riot-agent'))
        self.assertEqual(trace.count('systemctl show riot-maintenance.service -p RuntimeMaxUSec --value'), 1)
        with self.subTest(case='an effective 35 minute backstop leaves no policy behind'):
            self.reinit()
            self.dietpi()
            self.set_config(self.listed_config())
            write(ROOT / 'force-backstop', '35min\n', 0o600)
            result = self.run_installer('--non-interactive')
            self.assertNotEqual(result.returncode, 0, result.stdout)
            self.assertIn('The effective maintenance backstop is', result.stderr)
            self.assertFalse(EXTRA_POLICY.exists(), 'SEC-002: the policy is never written while the backstop is not in force')
        with self.subTest(case='an unreadable backstop answer is refused'):
            self.reinit()
            self.dietpi()
            self.set_config(self.listed_config())
            write(ROOT / 'force-backstop', 'infinity\n', 0o600)
            result = self.run_installer('--non-interactive')
            self.assertNotEqual(result.returncode, 0, result.stdout)
            self.assertFalse(EXTRA_POLICY.exists())

    def test_EU_OPS001_dropin_has_no_sandboxing(self):
        self.dietpi()
        self.set_config(self.listed_config())
        result = self.run_installer('--non-interactive')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(EXTRA_DROPIN.read_bytes(), DROPIN_ON.encode(), 'the drop-in bytes are exactly the fixed literal')
        unit = Path('/etc/systemd/system/riot-maintenance.service').read_text()
        for directive in ('PrivateTmp', 'MountFlags', 'ProtectSystem', 'ProtectHome', 'ReadOnlyPaths', 'InaccessiblePaths', 'ProtectProc', 'TemporaryFileSystem', 'PrivateMounts'):
            self.assertNotIn(directive, EXTRA_DROPIN.read_text())
            self.assertNotIn(directive, unit)
        for prop, value in (('PrivateTmp', 'no'), ('MountFlags', '')):
            self.assertIn('systemctl show riot-maintenance.service -p ' + prop + ' --value', (ROOT / 'trace').read_text())
        with self.subTest(case='PrivateTmp=yes in the effective unit refuses the opt-in'):
            self.reinit()
            self.dietpi()
            self.set_config(self.listed_config())
            write(ROOT / 'private-tmp', '', 0o600)
            result = self.run_installer('--non-interactive')
            self.assertNotEqual(result.returncode, 0, result.stdout)
            self.assertIn('PrivateTmp=', result.stderr)
            self.assertFalse(EXTRA_POLICY.exists())
        with self.subTest(case='MountFlags=slave in the effective unit refuses the opt-in'):
            self.reinit()
            self.dietpi()
            self.set_config(self.listed_config())
            write(ROOT / 'mount-flags', 'slave\n', 0o600)
            result = self.run_installer('--non-interactive')
            self.assertNotEqual(result.returncode, 0, result.stdout)
            self.assertIn('MountFlags=', result.stderr)
            self.assertFalse(EXTRA_POLICY.exists())

    def test_EU_SEC007_current_json_v2_closed(self):
        def record(**changes):
            base = {'version': 2, 'command_id': '3db2d182-61f4-4a4a-b515-243000a43d34', 'mode': 'full', 'include_reboot_class': False,
                    'started_at': '2026-10-26T03:00:00Z', 'updated_at': '2026-10-26T03:30:00Z', 'finished_at': '2026-10-26T03:30:00Z',
                    'phase': 'finished', 'outcome': 'error', 'exit_code': 1, 'interrupted': False, 'summary': 'apt: succeeded; dietpi: failed (exit 1)',
                    'output': 'x', 'output_truncated': False, 'holds': {}, 'holds_released': False, 'hold_cleanup_failed': False,
                    'requested_extra_updaters': ['dietpi'], 'apt_outcome': 'success',
                    'extra_updaters': [{'id': 'dietpi', 'outcome': 'failed', 'exit_code': 1, 'started_at': '2026-10-26T03:00:00Z', 'finished_at': '2026-10-26T03:30:00Z', 'version_before': '9.20.1', 'note': 'x'}]}
            base.update(changes)
            return base
        import json
        good = {'valid failed run': record(),
                'valid skipped run': record(outcome='success', exit_code=0, extra_updaters=[{'id': 'dietpi', 'outcome': 'skipped', 'reason': 'not_enabled'}]),
                'a version 1 record still passes': {k: v for k, v in record(version=1).items() if k not in ('requested_extra_updaters', 'apt_outcome', 'extra_updaters')}}
        for name, body in good.items():
            with self.subTest(valid=name):
                self.reinit()
                write('/var/lib/riot-maintenance/current.json', json.dumps(body), 0o640)
                result = self.install()
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        bad = {
            'a running updater entry': record(extra_updaters=[{'id': 'dietpi', 'outcome': 'running'}]),
            'an unknown top-level key': record(argv=['x']),
            'an unknown entry key': record(extra_updaters=[{'id': 'dietpi', 'outcome': 'failed', 'argv': ['x']}]),
            'an unknown skip reason': record(extra_updaters=[{'id': 'dietpi', 'outcome': 'skipped', 'reason': 'because'}]),
            'a reason on a non-skip': record(extra_updaters=[{'id': 'dietpi', 'outcome': 'failed', 'reason': 'not_enabled'}]),
            'an unknown outcome': record(extra_updaters=[{'id': 'dietpi', 'outcome': 'partial'}]),
            'an unknown apt outcome': record(apt_outcome='partial'),
            'a missing apt outcome': {k: v for k, v in record().items() if k != 'apt_outcome'},
            'success after a failed updater': record(outcome='success', exit_code=0),
            'success after a failed apt part': record(outcome='success', exit_code=0, apt_outcome='error', extra_updaters=[]),
            'nine results': record(extra_updaters=[{'id': 'u%d' % i, 'outcome': 'failed'} for i in range(9)]),
            'a bad identifier': record(extra_updaters=[{'id': 'Bad Id', 'outcome': 'failed'}]),
            'a duplicate identifier': record(extra_updaters=[{'id': 'dietpi', 'outcome': 'failed'}, {'id': 'dietpi', 'outcome': 'failed'}]),
            'a note over 256 bytes': record(extra_updaters=[{'id': 'dietpi', 'outcome': 'failed', 'note': 'n' * 257}]),
            'a version over 32 bytes': record(extra_updaters=[{'id': 'dietpi', 'outcome': 'failed', 'version_after': '9' * 33}]),
            'a repair run with updaters': record(mode='repair'),
            'a version 1 record with updater keys': record(version=1),
            'version 3': record(version=3),
            'an empty requested list': record(requested_extra_updaters=[]),
            'a requested list of numbers': record(requested_extra_updaters=[1]),
        }
        for name, body in bad.items():
            with self.subTest(refused=name):
                self.reinit()
                write('/var/lib/riot-maintenance/current.json', json.dumps(body), 0o640)
                self.assert_refused_without_replacement(self.install())

    def test_EU_AC017_agreement_holds_when_the_extra_list_changes_under_the_run(self):
        flip_off = "#!/bin/sh\nprintf 'commands:\\n  allow_patching: true\\n' > /etc/riot/agent.yaml\n"
        flip_on = "#!/bin/sh\nprintf 'commands:\\n  allow_extra_updaters: [dietpi]\\n' > /etc/riot/agent.yaml\n"
        cases = [('listed then removed', self.listed_config(), flip_off), ('not listed then added', 'commands:\n  allow_patching: true\n', flip_on)]
        for name, config, hook in cases:
            with self.subTest(case=name):
                self.reinit()
                self.dietpi()
                self.set_config(config)
                write(ROOT / 'on-stop', hook, 0o755)
                result = self.run_installer('--non-interactive')
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIn('commands.allow_extra_updaters changed during installation; previous managed policy restored', result.stderr)
                self.assertEqual(Path('/etc/sudoers.d/riot-agent').read_bytes(), self.old_policy)
                self.assertEqual(Path('/usr/local/bin/riot-agent').read_bytes(), self.old_binary)
                self.assert_nothing_recorded()

    def test_EU_AC016_shutdown_grant_survives_a_list_valued_key(self):
        """The classifier used to reject any list under commands:, silently dropping the poweroff grant."""
        self.dietpi()
        for name, body in {'flow list': '  allow_shutdown: true\n  allow_extra_updaters: [dietpi]\n',
                           'block list': '  allow_extra_updaters:\n    - dietpi\n  allow_shutdown: true\n',
                           'quoted items (no ids, file still read)': '  allow_shutdown: true\n  allow_extra_updaters: ["dietpi"]\n',
                           'scalar value (no ids, file still read)': '  allow_extra_updaters: dietpi\n  allow_shutdown: true\n'}.items():
            with self.subTest(case=name):
                self.reinit()
                self.dietpi()
                self.set_config('commands:\n' + body)
                result = self.run_installer('--non-interactive')
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(self.grant_lines(self.dropin()), [GRANT_LINE.strip()], 'the poweroff grant is kept')
                self.assertEqual(self.opted_in(), name in ('flow list', 'block list'))
                self.assert_transparency(result.stdout, 'granting')

    def test_EU_AC016_extra_decision_does_not_change_shutdown_for_non_list_files(self):
        self.set_config('commands:\n  allow_shutdown: false\n  allow_extra_updaters: [dietpi]\n')
        result = self.run_installer('--non-interactive')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.grant_lines(self.dropin()), [])
        self.assert_transparency(result.stdout, 'not_enabled')

    def test_EU_AC045_no_dietpi_program_runs_in_any_installer_fixture(self):
        self.dietpi(executable=True)
        self.fresh()
        rc, out, _ = self.install_tty(self.answers(extra='y'), SERVER_URL)
        self.assertEqual(rc, 0, out)
        self.assert_recorded(True)
        self.assert_no_dietpi_action()
        trace = (ROOT / 'trace').read_text()
        self.assertNotIn('dietpi-update', trace)
        for tripwire in Path('/boot/dietpi').rglob('dietpi-*'):
            self.assertEqual(tripwire.read_text(), DIETPI_TRIPWIRE, 'a fixture program was modified')


if __name__ == '__main__':
    unittest.main(verbosity=2)
