package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/agent"
	"gopkg.in/yaml.v3"
)

// SHUTDOWN-SUDO-001 static and differential tests for scripts/install.sh. The
// container fixtures in test_install_maintenance.py run the installer; these
// check its source and its config reader. Nothing here runs the installer or a
// power command (SC-001, SC-003): the only thing executed is the classifier
// function, on files this test writes.

const (
	classifierBegin = "# BEGIN shutdown-config-classifier\n"
	classifierEnd   = "# END shutdown-config-classifier\n"
)

func installSource(t *testing.T) string {
	t.Helper()
	return readPackagingFile(t, "install.sh")
}

func offsetOf(t *testing.T, src, needle string) int {
	t.Helper()
	i := strings.Index(src, needle)
	if i < 0 {
		t.Fatalf("install.sh does not contain %q", needle)
	}
	return i
}

func offsetAfter(t *testing.T, src, needle string, from int) int {
	t.Helper()
	i := strings.Index(src[from:], needle)
	if i < 0 {
		t.Fatalf("install.sh does not contain %q after offset %d", needle, from)
	}
	return from + i
}

func TestInstallSh_SS_AC003_PromptOrder(t *testing.T) {
	src := installSource(t)
	prompts := []string{
		`ask_yn "Allow remote reboot from dashboard?" "n"`,
		`ask_yn "Allow remote shutdown from dashboard?" "n"`,
		`ask_yn "Allow remote OS patching from dashboard?" "n"`,
		`ask_yn "Allow remote terminal access to this host?" "n"`,
		`ask_yn "Allow remote exec into Docker containers?" "n"`,
	}
	last := -1
	for _, p := range prompts {
		if strings.Count(src, p) != 1 {
			t.Fatalf("[AC-003] the prompt %q must appear exactly once", p)
		}
		at := strings.Index(src, p)
		if at <= last {
			t.Errorf("[AC-003] prompt %q is out of order: the order must be reboot, shutdown, patching, terminal, docker exec", p)
		}
		last = at
	}
	shutdown := strings.Index(src, prompts[1])
	if !strings.Contains(src[shutdown-60:shutdown], `if [ "$OS" = "linux" ]; then`) {
		t.Errorf("[AC-003][AC-018] the shutdown question must sit directly inside a Linux-only branch")
	}
	for _, kept := range []string{
		"    if ask_yn \"Allow remote reboot from dashboard?\" \"n\"; then\n        ALLOW_REBOOT=\"true\"\n    fi\n",
		"    if ask_yn \"Allow remote OS patching from dashboard?\" \"n\"; then\n        ALLOW_PATCHING=\"true\"\n    fi\n",
		"    if ask_yn \"Allow remote terminal access to this host?\" \"n\"; then\n        ALLOW_TERMINAL=\"true\"\n    fi\n",
		"        if ask_yn \"Allow remote exec into Docker containers?\" \"n\"; then\n            ALLOW_DOCKER_TERMINAL=\"true\"\n        fi\n",
	} {
		if !strings.Contains(src, kept) {
			t.Errorf("[AC-003][FR-002] an existing prompt block changed:\n%s", kept)
		}
	}
	if strings.Count(src, "ask_yn() {") != 1 {
		t.Errorf("[AC-003] ask_yn must be defined exactly once (the block moved, it was not duplicated)")
	}
}

func TestInstallSh_SS_AC007_RebootLineUnchangedAndPoweroffLineExact(t *testing.T) {
	src := installSource(t)
	reboot := "        echo \"riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} reboot\"\n"
	grant := "        if [ \"$SHUTDOWN_GRANT\" = true ]; then\n            echo \"riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} poweroff\"\n        fi\n"
	if strings.Count(src, reboot) != 1 {
		t.Fatalf("[AC-007] the reboot echo must occur exactly once, byte-identical")
	}
	if strings.Count(src, grant) != 1 {
		t.Fatalf("[AC-007] the poweroff echo must occur exactly once, inside the SHUTDOWN_GRANT guard")
	}
	if !strings.Contains(src, reboot+grant) {
		t.Errorf("[AC-007] the poweroff block must directly follow the reboot echo")
	}

	// Every sudoers echo, in order. The pre-story script has these lines in this
	// order; the story adds only the poweroff echo, after the reboot echo.
	want := []string{
		`echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} start --no-block riot-maintenance.service"`,
		`echo 'riot ALL=(root) NOPASSWD: /usr/bin/apt-mark ^hold [a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$'`,
		`echo 'riot ALL=(root) NOPASSWD: /usr/bin/apt-mark ^unhold [a-z0-9][a-z0-9+._:-]*( [a-z0-9][a-z0-9+._:-]*)*$'`,
		`echo "riot ALL=(root) NOPASSWD: ${DNF_PATH} makecache"`,
		`echo "riot ALL=(root) NOPASSWD: ${DNF_PATH} -y update"`,
		`echo "riot ALL=(root) NOPASSWD: ${DNF_PATH} -y --security update"`,
		`echo 'riot ALL=(root) NOPASSWD: /usr/bin/install -m 0644 -o root -g root /var/lib/riot/dnf-holds.staged /etc/dnf/libdnf5.conf.d/60-riot-holds.conf'`,
		`echo 'riot ALL=(root) NOPASSWD: /usr/bin/rm -f /etc/dnf/libdnf5.conf.d/60-riot-holds.conf'`,
		`echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} reboot"`,
		`echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} poweroff"`,
		`echo "riot ALL=(root) NOPASSWD: ${SYSTEMD_RUN_PATH} --unit=riot-agent-update ${RIOT_APPLY_BIN}"`,
		`echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} reset-failed riot-agent-update"`,
		`echo "riot ALL=(root) NOPASSWD: ${NGINX_PATH} -t"`,
		`echo "riot ALL=(root) NOPASSWD: ${NGINX_PATH} -T"`,
		`echo "riot ALL=(root) NOPASSWD: ${SMARTCTL_PATH} --version"`,
		`echo "riot ALL=(root) NOPASSWD: ${SMARTCTL_PATH} --json=c --all /dev/${disk}"`,
	}
	var got []string
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "riot ALL=(root)") && strings.Contains(strings.TrimSpace(line), "echo ") {
			got = append(got, strings.TrimSpace(line))
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("[AC-007][FR-013] sudoers lines changed.\n got: %q\nwant: %q", got, want)
	}
}

func TestInstallSh_SS_AC008_NoOtherPowerGrant(t *testing.T) {
	src := installSource(t)
	var grantLines []string
	for _, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, "riot ALL=") {
			continue
		}
		lower := strings.ToLower(line)
		for _, word := range []string{"halt", "kexec", "suspend", "hibernate", "/shutdown", "/poweroff", "init 0", "telinit"} {
			if strings.Contains(lower, word) {
				t.Errorf("[AC-008] a sudoers line names %q: %s", word, strings.TrimSpace(line))
			}
		}
		if strings.Contains(lower, "poweroff") {
			grantLines = append(grantLines, strings.TrimSpace(line))
		}
	}
	if len(grantLines) != 1 {
		t.Fatalf("[AC-008] exactly one sudoers line may name poweroff, found %d: %q", len(grantLines), grantLines)
	}
	line := grantLines[0]
	if line != `echo "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} poweroff"` {
		t.Errorf("[AC-008] the poweroff grant must be exactly the riot-only, argument-free line, got: %s", line)
	}
	// Nothing follows the verb: no argument, wildcard or pattern.
	command := strings.TrimPrefix(strings.TrimSuffix(line, `"`), `echo "`)
	if rest, ok := strings.CutPrefix(command, "riot ALL=(root) NOPASSWD: ${SYSTEMCTL_PATH} "); !ok || rest != "poweroff" {
		t.Errorf("[AC-008] the grant must name riot only and end at the bare verb poweroff: %s", command)
	}
}

func TestInstallSh_SS_AC018_ShutdownIsLinuxOnly(t *testing.T) {
	src := installSource(t)
	decision := "SHUTDOWN_GRANT=false\nif [ \"$OS\" = \"linux\" ]; then\n"
	start := offsetOf(t, src, decision)
	end := offsetAfter(t, src, "# Validate privileged path parents", start)
	region := src[start:end]
	if strings.Count(src, "SHUTDOWN_GRANT=true") != 1 || !strings.Contains(region, "SHUTDOWN_GRANT=true") {
		t.Errorf("[AC-018] SHUTDOWN_GRANT=true may be assigned only inside the Linux-only decision block")
	}
	if strings.Count(src, `SHUTDOWN_GRANT="$ALLOW_SHUTDOWN"`) != 1 || !strings.Contains(region, `SHUTDOWN_GRANT="$ALLOW_SHUTDOWN"`) {
		t.Errorf("[AC-018] the prompt answer may feed the grant only inside the Linux-only decision block")
	}
	if strings.Count(region, "config_enables_shutdown ") != 1 {
		t.Errorf("[AC-018] the decision block calls the classifier exactly once")
	}
	for _, guarded := range []string{
		"if [ \"$OS\" = linux ]; then\n    refuse_unsafe_config_path \"exists but\"",
		"if [ \"$OS\" = linux ]; then\n    # The config is final and the directory is root-only.",
	} {
		if strings.Count(src, guarded) != 1 {
			t.Errorf("[AC-018] expected a Linux-only guard around:\n%s", guarded)
		}
	}
	// The checks that run after the config directory is locked are inside the
	// large Linux branch, which ends where the non-Linux branch begins.
	linuxStart := offsetOf(t, src, "if [ \"$OS\" = linux ]; then\n    for tool in systemctl systemd-run")
	linuxEnd := offsetAfter(t, src, "else\n    mkdir -p \"$RIOT_CONFIG_DIR\" \"$RIOT_DATA_DIR\"\nfi\n", linuxStart)
	for _, call := range []string{`refuse_unsafe_config_path "changed during installation and"`, "        require_shutdown_agreement\n"} {
		at := offsetOf(t, src, call)
		if at < linuxStart || at > linuxEnd {
			t.Errorf("[AC-018] %q must be inside the Linux-only installer branch", call)
		}
	}
	if strings.Count(src, "GRANT_PENDING=\"$SHUTDOWN_GRANT\"") != 1 || offsetOf(t, src, "GRANT_PENDING=\"$SHUTDOWN_GRANT\"") < linuxStart {
		t.Errorf("[AC-018] the pending flag is set only inside the Linux-only branch")
	}
}

func TestInstallSh_SS_AC026_FlagsAndUsageUnchanged(t *testing.T) {
	src := installSource(t)
	lines := strings.Split(src, "\n")
	header := strings.Join(lines[:17], "\n")
	if header != pinnedHeader {
		t.Errorf("[AC-026][FR-035] the usage comment block changed:\n%s", header)
	}
	if !strings.Contains(src, pinnedArgs) {
		t.Errorf("[AC-026][FR-035] the flag parsing or the server URL check changed")
	}
	if !strings.Contains(src, "    echo \"    These can be changed later in ${RIOT_CONFIG_DIR}/agent.yaml\"\n") ||
		!strings.Contains(src, "    echo \"==> Configure remote management features\"\n") {
		t.Errorf("[AC-026] the prompt block header text changed")
	}
}

func TestInstallSh_SS_OQA1_PromptsPrecedeLocksAndStop(t *testing.T) {
	src := installSource(t)
	first := offsetOf(t, src, `ask_yn "`)
	for _, later := range []string{
		"flock -n 9",
		"NATIVE_LOCK_PID=$!",
		"atomic_install \"$STAGE/riot-agent.sudoers\"",
		"systemctl stop riot-agent",
		"BACKUP=$(mktemp",
	} {
		at := offsetOf(t, src, later)
		if first >= at {
			t.Errorf("[OQ-A1] the first prompt (offset %d) must come before %q (offset %d), so no human wait happens while locks are held or the agent is stopped", first, later, at)
		}
	}
	if verify := offsetOf(t, src, `verify_asset "$BINARY_NAME"`); verify >= first {
		t.Errorf("[OQ-A1] the prompts come after the signed assets are verified")
	}
}

func classifierAwk(t *testing.T, src string) string {
	t.Helper()
	fn := classifierFunction(t, src)
	start := offsetOf(t, fn, "awk '") + len("awk '")
	end := offsetAfter(t, fn, "' \"$snap\"", start)
	return fn[start:end]
}

func classifierFunction(t *testing.T, src string) string {
	t.Helper()
	if strings.Count(src, classifierBegin) != 1 || strings.Count(src, classifierEnd) != 1 {
		t.Fatalf("[AD-003] the classifier markers must each occur exactly once")
	}
	start := strings.Index(src, classifierBegin) + len(classifierBegin)
	end := strings.Index(src, classifierEnd)
	if end <= start {
		t.Fatalf("[AD-003] the classifier markers are out of order")
	}
	return src[start:end]
}

func TestInstallSh_SS_ClassifierIsPortableAwk(t *testing.T) {
	src := installSource(t)
	fn := classifierFunction(t, src)
	program := classifierAwk(t, src)
	if strings.Contains(program, "'") {
		t.Errorf("[AD-003] the awk program must not contain a single quote")
	}
	for _, banned := range []string{"gensub", "IGNORECASE", `\s`, `\y`, `\<`, `\>`, "strftime", "systime", "asort", "PROCINFO", "@include", "BEGINFILE", "toupper(", "tolower("} {
		if strings.Contains(program, banned) {
			t.Errorf("[AD-003] the awk program uses %q, which is not portable to mawk and busybox awk", banned)
		}
	}
	if regexp.MustCompile(`\{[0-9]+(,[0-9]*)?\}`).MatchString(program) {
		t.Errorf("[AD-003] the awk program uses a regex interval expression")
	}
	if regexp.MustCompile(`length\((top|ck)\)`).MatchString(program) {
		t.Errorf("[AD-003] the awk program takes the length of an array")
	}
	if regexp.MustCompile(`match\([^)]*,[^)]*,[^)]*\)`).MatchString(program) {
		t.Errorf("[AD-003] the awk program uses the three-argument match")
	}
	for _, needed := range []string{"timeout 5 head -c 65537", "LC_ALL=C awk", `[ "$size" -le 65536 ] || return 1`, `[ -f "$p" ] && [ ! -L "$p" ] || return 1`, "tr -d '\\000'"} {
		if !strings.Contains(fn, needed) {
			t.Errorf("[AD-003] the classifier must contain %q", needed)
		}
	}
	for _, banned := range []string{"python", "yq ", "ruby", "perl"} {
		if strings.Contains(fn, banned) {
			t.Errorf("[AD-003] the classifier must not depend on %q", banned)
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(fn, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "local ") || strings.HasPrefix(trimmed, "[ ") || strings.HasPrefix(trimmed, "timeout ") {
			if !strings.Contains(trimmed, "|| return 1") && !strings.HasPrefix(trimmed, "local ") {
				t.Errorf("[AD-003] every classifier step must end in || return 1 (set -e is off inside an if): %s", trimmed)
			}
		}
	}
}

// SEC-003 and SEC-001: the order of the runtime checks is what makes them
// effective, so it is checked on the source.
func TestInstallSh_SS_SEC003_RuntimeChecksFollowTheLockdownAndPrecedeAnyWrite(t *testing.T) {
	src := installSource(t)

	chownRoot := offsetOf(t, src, "    chown root:root \"$RIOT_CONFIG_DIR\"\n    chmod 0700 \"$RIOT_CONFIG_DIR\"\n")
	lock := offsetAfter(t, src, "chmod 0700 \"$RIOT_CONFIG_DIR\"\n", chownRoot)
	late := offsetOf(t, src, `refuse_unsafe_config_path "changed during installation and"`)
	preAgree := offsetOf(t, src, "    if [ \"$CONFIG_EXISTED\" = true ] && [ -z \"$RIOT_BOOTSTRAP_KEY\" ]; then\n        require_shutdown_agreement\n")
	bootstrapRm := offsetOf(t, src, `rm -f "$RIOT_CONFIG_DIR/agent.yaml"`)
	write := offsetOf(t, src, `cat > "$RIOT_CONFIG_DIR/agent.yaml"`)
	finalAgree := offsetOf(t, src, "    require_shutdown_agreement\n    GRANT_PENDING=false\n    atomic_install \"$STAGE/$BINARY_NAME\"")
	firstBinary := offsetOf(t, src, `atomic_install "$STAGE/$BINARY_NAME" "$RIOT_BIN"`)
	order := []struct {
		name string
		at   int
	}{{"config directory lockdown", lock}, {"late agent.yaml refusal", late}, {"agreement re-read before any write", preAgree},
		{"bootstrap removal of agent.yaml", bootstrapRm}, {"config write", write}, {"final agreement check", finalAgree}, {"first binary replaced", firstBinary}}
	for i := 1; i < len(order); i++ {
		if order[i-1].at >= order[i].at {
			t.Errorf("[SEC-003] %s (offset %d) must come before %s (offset %d)", order[i-1].name, order[i-1].at, order[i].name, order[i].at)
		}
	}

	// The pending flag is set right after the drop-in is installed, once.
	if strings.Count(src, `GRANT_PENDING="$SHUTDOWN_GRANT"`) != 1 ||
		!strings.Contains(src, "atomic_install \"$STAGE/riot-agent.sudoers\" /etc/sudoers.d/riot-agent 0440\n    GRANT_PENDING=\"$SHUTDOWN_GRANT\"\n") {
		t.Errorf("[SEC-003] GRANT_PENDING must be set immediately after the drop-in install")
	}
	// Every call site of restore_policy clears it first (the trap restores it otherwise).
	lines := strings.Split(src, "\n")
	restores := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "restore_policy" && !strings.HasPrefix(trimmed, "restore_policy ||") {
			continue
		}
		restores++
		prev := strings.TrimSpace(lines[i-1])
		if strings.HasPrefix(trimmed, "restore_policy ||") {
			if prev != "GRANT_PENDING=false" {
				t.Errorf("[SEC-003] the trap must clear GRANT_PENDING before restoring, line %d", i+1)
			}
			continue
		}
		if prev != "GRANT_PENDING=false" {
			t.Errorf("[SEC-003] restore_policy at line %d is not preceded by GRANT_PENDING=false", i+1)
		}
	}
	if restores != 4 {
		t.Errorf("[SEC-003] expected four restore_policy call sites (trap, effective check, agreement, final policy check), found %d", restores)
	}
	// The trap restores the policy before it kills the lock helper.
	cleanup := src[offsetOf(t, src, "cleanup() {\n"):offsetOf(t, src, "trap cleanup EXIT")]
	restore, kill, stage := strings.Index(cleanup, "restore_policy"), strings.Index(cleanup, `kill "$NATIVE_LOCK_PID"`), strings.Index(cleanup, `rm -rf -- "$STAGE"`)
	if restore < 0 || kill < 0 || stage < 0 || !(restore < kill && kill < stage) {
		t.Errorf("[SEC-003] cleanup must restore the policy, then kill the lock helper, then remove the stage directory (offsets %d, %d, %d)", restore, kill, stage)
	}
}

func TestInstallSh_SS_SEC001_AgentYamlIsOnlyWrittenWhenAbsentAndLinksAreCounted(t *testing.T) {
	src := installSource(t)
	refuse := src[offsetOf(t, src, "refuse_unsafe_config_path() {"):offsetOf(t, src, "# The drop-in was built from an earlier decision.")]
	for _, needed := range []string{`[ -e "$p" ] || [ -L "$p" ]`, `[ -f "$p" ] || fail`, `stat -c %h -- "$p"`, `[ "$links" = 1 ] || fail`} {
		if !strings.Contains(refuse, needed) {
			t.Errorf("[SEC-001] the config path check must contain %q", needed)
		}
	}
	if strings.Count(src, "refuse_unsafe_config_path \"") != 2 {
		t.Errorf("[SEC-001] the path check runs twice: before any change and after the lockdown")
	}
	// Every write, ownership change or mode change of agent.yaml sits in the
	// branch that runs only when the path does not exist as a regular file.
	blockStart := offsetOf(t, src, "if [ ! -f \"$RIOT_CONFIG_DIR/agent.yaml\" ]; then\n    echo \"==> Writing config")
	blockEnd := offsetAfter(t, src, "else\n    echo \"==> Config already exists", blockStart)
	offset := 0
	for _, line := range strings.Split(src, "\n") {
		start := offset
		offset += len(line) + 1
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(line, "agent.yaml") || strings.HasPrefix(trimmed, "#") || strings.Contains(line, "rm -f") {
			continue // the bootstrap removal unlinks the name, never the data behind another link
		}
		for _, verb := range []string{"chown ", "chmod ", "cat >", "tee ", "mv ", "cp ", "install ", "> \""} {
			if strings.Contains(line, verb) && (start < blockStart || start > blockEnd) {
				t.Errorf("[SEC-001] a root write or ownership change of agent.yaml outside the not-yet-existing branch: %s", trimmed)
			}
		}
	}
}

// ---- Differential test: the installer's reader against the agent's loader ----

func findBash(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		// Windows' system bash.exe is a WSL launcher, not a local shell.
		bash := filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe")
		if _, err := os.Stat(bash); err != nil {
			t.Skip("classifier test requires Linux bash or Git Bash")
		}
		return bash
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("classifier test requires bash")
	}
	return bash
}

type classifierRunner struct {
	name string
	bash string
	fn   string // path to the extracted function
	shim string // directory placed first on PATH, "" for the default awk
	temp string
}

func (r classifierRunner) enabled(t *testing.T, name string, content []byte) bool {
	t.Helper()
	file := filepath.Join(r.temp, "case-"+r.name+"-"+name+".yaml")
	if err := os.WriteFile(file, content, 0600); err != nil {
		t.Fatal(err)
	}
	pathExpr := "/usr/bin:/bin:$PATH"
	if r.shim != "" {
		pathExpr = filepath.ToSlash(r.shim) + ":" + pathExpr
	}
	cmd := exec.Command(r.bash, "-c", `export PATH=`+pathExpr+`; STAGE="$2"; export STAGE; . "$1"; config_enables_shutdown "$3"`,
		"_", filepath.ToSlash(r.fn), filepath.ToSlash(r.temp), filepath.ToSlash(file))
	err := cmd.Run()
	if err == nil {
		return true
	}
	if _, ok := err.(*exec.ExitError); ok {
		return false
	}
	t.Fatalf("running the classifier: %v", err)
	return false
}

func awkVariants(t *testing.T, dir string) []string {
	t.Helper()
	var shims []string
	for _, v := range []struct{ name, script string }{
		{"mawk", "#!/bin/sh\nexec mawk \"$@\"\n"},
		{"busybox", "#!/bin/sh\nexec busybox awk \"$@\"\n"},
		{"gawk", "#!/bin/sh\nexec gawk \"$@\"\n"},
	} {
		if _, err := exec.LookPath(v.name); err != nil || runtime.GOOS == "windows" {
			continue
		}
		shim := filepath.Join(dir, "shim-"+v.name)
		if err := os.MkdirAll(shim, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(shim, "awk"), []byte(v.script), 0755); err != nil {
			t.Fatal(err)
		}
		shims = append(shims, v.name+"="+shim)
	}
	return shims
}

var installerConfigHead = "server:\n  url: \"https://old.test:7331\"\n  tls_verify: true\n\nagent:\n  poll_interval: 60\n  heartbeat_interval: 15\n\ncollectors:\n  enabled:\n    - system\n    - cpu\n    - usb\n\n"

type classifierCase struct {
	name    string
	content []byte
	enabled bool // what the classifier must say
}

// msysTextModeAwk names cases whose expected result depends on awk reading
// bytes as they are. Git Bash's awk on Windows reads files in text mode and
// drops the carriage return, so these are pinned only where awk is a Linux awk.
// The classifier-enabled-implies-agent-enabled check still runs everywhere.
var msysTextModeAwk = map[string]bool{"CRLF line endings": true}

func classifierCorpus(t *testing.T) []classifierCase {
	t.Helper()
	cfg := agent.DefaultConfig()
	cfg.Commands.AllowShutdown = true
	saved, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	savedOff, err := yaml.Marshal(agent.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defaultTemplate := `# rIOt Agent Configuration
server:
  url: "http://localhost:7331"
  tls_verify: true
  # api_key: ""            # Set during enrollment

agent:
  poll_interval: 60        # Telemetry push interval (seconds)
  heartbeat_interval: 15   # Heartbeat interval (seconds)

docker:
  enabled: "auto"          # "auto" (detect), "true", or "false"
  collect_stats: true      # Per-container CPU/memory stats
  # compose_roots: []      # Directories compose updates may run in, e.g. [/opt/stacks] (required on Windows)

commands:
  allow_reboot: false      # Allow remote reboot commands
  allow_shutdown: false    # Allow remote shutdown commands
  allow_patching: false    # Allow remote OS patching commands
  hold_reboot_class: false # Hold GPU driver + kernel packages at the OS level (release only during rIOt maintenance-window patch runs)

host_terminal:
  enabled: false           # Allow remote shell access to this host
`
	padded := "commands:\n  allow_shutdown: true\n" + strings.Repeat("#"+strings.Repeat("x", 62)+"\n", 1100)
	padded = padded[:65537]
	cmds := func(body string) []byte { return []byte(installerConfigHead + "commands:\n" + body) }
	cases := []classifierCase{
		// Expected enabled.
		{"installer template, true", cmds("  allow_reboot: false\n  allow_shutdown: true\n  allow_patching: false\n\n\n"), true},
		{"agent Save format, true", saved, true},
		{"agent default template edited to true", []byte(strings.Replace(defaultTemplate, "allow_shutdown: false    #", "allow_shutdown: true    #", 1)), true},
		{"four-space indentation", []byte("commands:\n    allow_shutdown: true\n"), true},
		{"a leading document marker", []byte("---\ncommands:\n  allow_shutdown: true\n"), true},
		{"comments and blank lines inside the block", []byte("commands:\n  allow_reboot: false\n\n  # note\n# column-zero comment\n  allow_shutdown: true\n\n  allow_patching: false\n"), true},
		{"trailing spaces and a comment", []byte("commands:\n  allow_shutdown: true   # remote\n"), true},
		{"commands is not the last section", cmds("  allow_shutdown: true\n\ndocker:\n  enabled: auto\n"), true},
		{"apostrophe in a plain value", []byte("agent:\n  device_name: Ryan's box\ncommands:\n  allow_shutdown: true\n"), true},
		{"quoted values with escapes", []byte("agent:\n  device_name: \"a \\\"b\\\" c\"\n  tags: ['x', 'it''s']\ncommands:\n  allow_shutdown: true\n"), true},
		// Expected not enabled: the config does not say true.
		{"saved config with the default false", savedOff, false},
		{"no commands section", []byte(installerConfigHead), false},
		{"commands without the key", cmds("  allow_reboot: true\n"), false},
		{"false", cmds("  allow_shutdown: false\n"), false},
		{"commented out", cmds("  # allow_shutdown: true\n  allow_reboot: true\n"), false},
		{"under docker", []byte("docker:\n  allow_shutdown: true\ncommands:\n  allow_reboot: true\n"), false},
		{"quoted maybe", cmds("  allow_shutdown: \"maybe\"\n"), false},
		{"empty file", []byte(""), false},
		// Expected not enabled: valid for the agent, deliberately not recognised.
		{"True", cmds("  allow_shutdown: True\n"), false},
		{"TRUE", cmds("  allow_shutdown: TRUE\n"), false},
		{"yes", cmds("  allow_shutdown: yes\n"), false},
		{"on", cmds("  allow_shutdown: on\n"), false},
		{"y", cmds("  allow_shutdown: y\n"), false},
		{"double-quoted true", cmds("  allow_shutdown: \"true\"\n"), false},
		{"single-quoted true", cmds("  allow_shutdown: 'true'\n"), false},
		{"tagged bool", cmds("  allow_shutdown: !!bool true\n"), false},
		{"flow mapping", []byte("commands: {allow_shutdown: true}\n"), false},
		{"anchor on commands", []byte("commands: &c\n  allow_shutdown: true\n"), false},
		{"merge key in the block", []byte("base: &b\n  allow_shutdown: true\ncommands:\n  <<: *b\n"), false},
		{"alias value", []byte("base: &b true\ncommands:\n  allow_shutdown: *b\n"), false},
		{"duplicate commands", []byte("commands:\n  allow_shutdown: true\ncommands:\n  allow_reboot: true\n"), false},
		{"duplicate allow_shutdown", cmds("  allow_shutdown: true\n  allow_shutdown: true\n"), false},
		{"tab indentation", []byte("commands:\n\tallow_shutdown: true\n"), false},
		{"CRLF line endings", []byte("commands:\r\n  allow_shutdown: true\r\n"), false},
		{"BOM on the first line", []byte("\xef\xbb\xbfcommands:\n  allow_shutdown: true\n"), false},
		{"commands in a second document", []byte("server:\n  url: x\n---\ncommands:\n  allow_shutdown: true\n"), false},
		{"document end marker", []byte("server:\n  url: x\n...\ncommands:\n  allow_shutdown: true\n"), false},
		{"YAML directive", []byte("%YAML 1.2\n---\ncommands:\n  allow_shutdown: true\n"), false},
		{"embedded NUL", []byte("commands:\n  allow_shutdown: true\n\x00"), false},
		{"65537 bytes", []byte(padded), false},
		{"unterminated quote before commands", []byte("agent:\n  device_name: \"abc\ncommands:\n  allow_shutdown: true\n"), false},
		{"unterminated single quote before commands", []byte("agent:\n  device_name: 'abc\ncommands:\n  allow_shutdown: true\n"), false},
		{"multi-line flow sequence before commands", []byte("agent:\n  tags: [a,\n         b]\ncommands:\n  allow_shutdown: true\n"), false},
		{"multi-line flow mapping before commands", []byte("agent:\n  x: {a: 1,\n       b: 2}\ncommands:\n  allow_shutdown: true\n"), false},
		{"nested commands sub-key", []byte("commands:\n  sub:\n    allow_shutdown: true\n"), false},
		{"mixed indentation in the block", []byte("commands:\n  allow_reboot: false\n    allow_shutdown: true\n"), false},
		{"commands with a space before the colon", []byte("commands :\n  allow_shutdown: true\n"), false},
		{"quoted commands key", []byte("\"commands\":\n  allow_shutdown: true\n"), false},
		{"no space after the colon", cmds("  allow_shutdown:true\n"), false},
		{"hash glued to the value", cmds("  allow_shutdown: true#x\n"), false},
		{"block scalar elsewhere", []byte("agent:\n  device_name: |\n    line\ncommands:\n  allow_shutdown: true\n"), false},
		{"folded block scalar with a hidden commands line", []byte("agent:\n  device_name: >\ncommands:\n  allow_shutdown: true\n"), false},
		{"sequence at the top level", []byte("- commands:\n    allow_shutdown: true\n"), false},
		{"complex key", []byte("? commands\n: {allow_shutdown: true}\n"), false},
		{"commands inline scalar", []byte("commands: true\n  allow_shutdown: true\n"), false},
		{"commands list", []byte("commands:\n  - allow_shutdown: true\n"), false},
		{"indented document", []byte("  commands:\n    allow_shutdown: true\n"), false},
		// QA-1: an indented first content line makes yaml.v3 start a second implicit
		// document at the next column-0 line, which the agent never reads.
		{"indented first line", []byte("  a: 1\ncommands:\n  allow_shutdown: true\n"), false},
		{"one-space first line", []byte(" a: 1\ncommands:\n  allow_shutdown: true\n"), false},
		{"blank line then indented first line", []byte("\n  a: 1\ncommands:\n  allow_shutdown: true\n"), false},
		{"comment then indented first line", []byte("# note\n  a: 1\ncommands:\n  allow_shutdown: true\n"), false},
		{"document start then indented first line", []byte("---\n  a: 1\ncommands:\n  allow_shutdown: true\n"), false},
		{"orphaned indented url", []byte("  url: \"https://old.test:7331\"\nagent:\n  poll_interval: 60\ncommands:\n  allow_shutdown: true\n"), false},
		{"comment then column-0 first line stays enabled", []byte("# note\n\ncommands:\n  allow_shutdown: true\n"), true},
		{"value with trailing text", cmds("  allow_shutdown: true false\n"), false},
	}
	return cases
}

func TestInstallSh_SS_FR017_ClassifierNeverExceedsAgentLoader(t *testing.T) {
	bash := findBash(t)
	if _, err := exec.LookPath("awk"); err != nil && runtime.GOOS != "windows" {
		t.Skip("classifier test requires awk")
	}
	temp := t.TempDir()
	fn := filepath.Join(temp, "classifier.sh")
	if err := os.WriteFile(fn, []byte(classifierFunction(t, installSource(t))), 0600); err != nil {
		t.Fatal(err)
	}

	runners := []classifierRunner{{name: "default-awk", bash: bash, fn: fn, temp: temp}}
	for _, shim := range awkVariants(t, temp) {
		name, dir, _ := strings.Cut(shim, "=")
		runners = append(runners, classifierRunner{name: name, bash: bash, fn: fn, shim: dir, temp: temp})
	}
	t.Logf("[FR-017] classifier runs with: %v", func() []string {
		var names []string
		for _, r := range runners {
			names = append(names, r.name)
		}
		return names
	}())

	cases := classifierCorpus(t)
	names := map[string]bool{}
	for i, c := range cases {
		if names[c.name] {
			t.Fatalf("duplicate corpus name %q", c.name)
		}
		names[c.name] = true
		file := filepath.Join(temp, fmt.Sprintf("agent-%d.yaml", i))
		if err := os.WriteFile(file, c.content, 0600); err != nil {
			t.Fatal(err)
		}
		loaded, loadErr := agent.LoadConfig(file)
		agentEnabled := loadErr == nil && loaded.Commands.AllowShutdown

		for _, r := range runners {
			got := r.enabled(t, fmt.Sprintf("%d", i), c.content)
			if got != c.enabled && !(msysTextModeAwk[c.name] && runtime.GOOS == "windows") {
				t.Errorf("[FR-017] %s (%s): classifier enabled = %v, want %v", c.name, r.name, got, c.enabled)
			}
			if got && !agentEnabled {
				t.Errorf("[FR-017] %s (%s): the classifier says enabled but the agent loads allow_shutdown as false (load error: %v)", c.name, r.name, loadErr)
			}
		}
		if c.enabled && !agentEnabled {
			t.Errorf("[FR-017] corpus error: %s is pinned as enabled but the agent does not read it as true (%v)", c.name, loadErr)
		}
	}

	// The test must have teeth: a spread of spellings the agent reads as true is
	// present in the corpus and the classifier turns them all down.
	agentTrueButNotEnabled := 0
	for i, c := range cases {
		loaded, err := agent.LoadConfig(filepath.Join(temp, fmt.Sprintf("agent-%d.yaml", i)))
		if err == nil && loaded.Commands.AllowShutdown && !c.enabled {
			agentTrueButNotEnabled++
		}
	}
	if agentTrueButNotEnabled < 8 {
		t.Errorf("[FR-017] the corpus holds only %d spellings the agent reads as true but the classifier rejects; it should hold many more", agentTrueButNotEnabled)
	}
	var enabledCount int
	for _, c := range cases {
		if c.enabled {
			enabledCount++
		}
	}
	if enabledCount < 8 {
		t.Errorf("[FR-017] the corpus holds only %d enabled cases", enabledCount)
	}
}

// Pinned text of the pre-story installer (71a4dea): the usage comment, the flag
// parsing and the server URL check. Nothing in the story may change them.
const pinnedHeader = `#!/usr/bin/env bash
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
`

const pinnedArgs = `while [ $# -gt 0 ]; do
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
`
