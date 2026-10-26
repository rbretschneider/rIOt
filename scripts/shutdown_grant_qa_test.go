package scripts

// Added by QA Engineer (SHUTDOWN-SUDO-001).
//
// Covers AC-011 and FR-017 ("the installer must never treat a config as
// shutdown-enabled when the agent would read commands.allow_shutdown as false").
// The story's own differential test pins a hand-written corpus of 58 files.
// This test attacks the same implication with seeded random mutations of those
// files, so a shape nobody thought to write down is still compared against the
// real agent.LoadConfig. The seed is fixed, so a failure is reproducible.
//
// Nothing here runs the installer or a power command: the only thing executed
// is the extracted classifier function, on files this test writes.

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/agent"
)

// qaInterestingLines are lines a hostile or careless edit could introduce.
var qaInterestingLines = []string{
	"commands:", "  allow_shutdown: true", "allow_shutdown: true", "    allow_shutdown: true", "  allow_shutdown: false",
	"---", "--- &a", "...", "%YAML 1.2", "%TAG ! tag:x,2000:", "&a", "  <<: *a", "base: &a", "x: |", "x: >", "x: |-", "x: \"", "x: '", "x: [", "]", "x: {", "}",
	"? a", ": b", "- a", "-", "\tfoo: 1", "  \tallow_shutdown: true", "!!str foo", "!!map", "# c", "", "\"", "'",
	"commands: {allow_shutdown: true}", "commands: &c", "commands: !!map", "commands: |", "key: !!bool true", "  allow_shutdown: !!bool true",
	"  allow_shutdown: True", "  allow_shutdown: yes", "  allow_shutdown: \"true\"", "  allow_shutdown: 'true'", "  allow_shutdown: true # x",
	"  allow_shutdown:  true", "  allow_shutdown:\ttrue", "  allow_shutdown: tru\\\ne", "  allow_shutdown: true\r", "  \"allow_shutdown\": true",
	"  allow_shutdown : true", "  allow_shutdown: [true]", "  allow_shutdown: {a: b}", "  ? allow_shutdown\n  : true", "agent:", "docker:",
}

var qaSpecialChars = []string{"\"", "'", "#", ":", "{", "}", "[", "]", "|", ">", "&", "*", "!", "%", "@", "`", "\t", "\r", "\\", " ", "-", "?", ",", "\xef\xbb\xbf", " ", " ", "\x85"}

func qaMutate(rng *rand.Rand, seed string) string {
	lines := strings.Split(seed, "\n")
	steps := 1 + rng.Intn(3)
	for s := 0; s < steps; s++ {
		if len(lines) == 0 {
			lines = []string{""}
		}
		i := rng.Intn(len(lines))
		switch rng.Intn(9) {
		case 0: // delete a line
			lines = append(lines[:i], lines[i+1:]...)
		case 1: // duplicate a line
			lines = append(lines[:i+1], lines[i:]...)
		case 2: // swap two lines
			j := rng.Intn(len(lines))
			lines[i], lines[j] = lines[j], lines[i]
		case 3, 4: // insert an interesting line
			ins := qaInterestingLines[rng.Intn(len(qaInterestingLines))]
			lines = append(lines[:i], append([]string{ins}, lines[i:]...)...)
		case 5: // change indentation
			if rng.Intn(2) == 0 {
				lines[i] = strings.Repeat(" ", 1+rng.Intn(3)) + lines[i]
			} else {
				lines[i] = strings.TrimPrefix(lines[i], " ")
			}
		case 6: // insert a special character at a random position
			pos := 0
			if len(lines[i]) > 0 {
				pos = rng.Intn(len(lines[i]) + 1)
			}
			lines[i] = lines[i][:pos] + qaSpecialChars[rng.Intn(len(qaSpecialChars))] + lines[i][pos:]
		case 7: // append a special character
			lines[i] += qaSpecialChars[rng.Intn(len(qaSpecialChars))]
		case 8: // truncate a line
			if len(lines[i]) > 0 {
				lines[i] = lines[i][:rng.Intn(len(lines[i]))]
			}
		}
	}
	return strings.Join(lines, "\n")
}

// Covers AC-011, FR-017 (classifier enabled implies agent enabled), SEC-006.
func TestInstallSh_QA_FR017_MutationsNeverExceedAgentLoader(t *testing.T) {
	bash := findBash(t)
	if _, err := exec.LookPath("awk"); err != nil && runtime.GOOS != "windows" {
		t.Skip("classifier test requires awk")
	}
	n := 150
	if v := os.Getenv("QA_FUZZ_N"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}

	temp := t.TempDir()
	fn := filepath.Join(temp, "classifier.sh")
	if err := os.WriteFile(fn, []byte(classifierFunction(t, installSource(t))), 0600); err != nil {
		t.Fatal(err)
	}

	var seeds []string
	for _, c := range classifierCorpus(t) {
		if c.enabled {
			seeds = append(seeds, string(c.content))
		}
	}
	// Seeds the agent reads as true but the classifier rejects: a mutation can turn one into a shape the classifier accepts.
	for _, spelling := range []string{"True", "yes", "\"true\"", "on", "!!bool true"} {
		seeds = append(seeds, installerConfigHead+"commands:\n  allow_reboot: false\n  allow_shutdown: "+spelling+"\n  allow_patching: false\n")
	}
	if len(seeds) < 10 {
		t.Fatalf("expected at least 10 seeds, got %d", len(seeds))
	}

	dir := filepath.Join(temp, "mutants")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(20261026))
	for i := 0; i < n; i++ {
		content := qaMutate(rng, seeds[rng.Intn(len(seeds))])
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("m%05d.yaml", i)), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	runners := map[string]string{"default-awk": ""}
	for _, shim := range awkVariants(t, temp) {
		name, shimDir, _ := strings.Cut(shim, "=")
		runners[name] = shimDir
	}

	// agentReadsFalse is the FR-017 violation: the agent loads the file and reads
	// allow_shutdown as false. A file the agent cannot load at all is the accepted
	// residual R-1 (ADD section 10, SEC-006): agent.New returns the error, no agent
	// runs, and the grant cannot be used. It is counted and reported, not failed.
	agentReadsFalse := make([]bool, n)
	agentLoadFails := make([]bool, n)
	agentTrueCount, loadFailCount := 0, 0
	for i := 0; i < n; i++ {
		loaded, err := agent.LoadConfig(filepath.Join(dir, fmt.Sprintf("m%05d.yaml", i)))
		switch {
		case err != nil:
			agentLoadFails[i] = true
			loadFailCount++
		case loaded.Commands.AllowShutdown:
			agentTrueCount++
		default:
			agentReadsFalse[i] = true
		}
	}

	for name, shim := range runners {
		pathExpr := "/usr/bin:/bin:$PATH"
		if shim != "" {
			pathExpr = filepath.ToSlash(shim) + ":" + pathExpr
		}
		script := `export PATH=` + pathExpr + `; STAGE="$2"; export STAGE; . "$1"; for f in "$3"/m*.yaml; do if config_enables_shutdown "$f"; then echo "$(basename "$f") 1"; else echo "$(basename "$f") 0"; fi; done`
		out, err := exec.Command(bash, "-c", script, "_", filepath.ToSlash(fn), filepath.ToSlash(temp), filepath.ToSlash(dir)).Output()
		if err != nil {
			t.Fatalf("running the classifier with %s: %v", name, err)
		}
		enabled, residualR1 := 0, 0
		seen := 0
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			var file string
			var flag int
			if _, err := fmt.Sscanf(strings.TrimSpace(line), "%s %d", &file, &flag); err != nil {
				t.Fatalf("unreadable classifier output %q", line)
			}
			seen++
			if flag == 1 {
				enabled++
				var idx int
				fmt.Sscanf(file, "m%d.yaml", &idx)
				if agentLoadFails[idx] {
					residualR1++
				}
				if agentReadsFalse[idx] {
					content, _ := os.ReadFile(filepath.Join(dir, file))
					t.Errorf("[FR-017][%s] classifier says enabled but the agent loads the file and reads allow_shutdown as false: %s\n%q", name, file, content)
				}
			}
		}
		if seen != n {
			t.Errorf("[%s] classified %d of %d mutants", name, seen, n)
		}
		t.Logf("[%s] %d mutants: agent reads true %d, agent cannot load %d, classifier enables %d (of which the agent cannot load, residual R-1: %d)", name, n, agentTrueCount, loadFailCount, enabled, residualR1)
	}
}

// Covers AC-011, FR-017. Minimal reproductions of the shape the mutation test
// found: the first content line is indented, so yaml.v3 closes that first
// mapping at the next column-0 line and starts a second implicit document,
// which Unmarshal silently ignores. The agent loads the file without error and
// reads allow_shutdown as false. The classifier has no rule for an indented
// first content line, so it reports "enabled".
func TestInstallSh_QA_FR017_IndentedFirstLineHidesCommandsFromAgent(t *testing.T) {
	bash := findBash(t)
	temp := t.TempDir()
	fn := filepath.Join(temp, "classifier.sh")
	if err := os.WriteFile(fn, []byte(classifierFunction(t, installSource(t))), 0600); err != nil {
		t.Fatal(err)
	}
	r := classifierRunner{name: "default-awk", bash: bash, fn: fn, temp: temp}
	cases := map[string]string{
		"indented first line":           "  a: 1\ncommands:\n  allow_shutdown: true\n",
		"one-space first line":          " a: 1\ncommands:\n  allow_shutdown: true\n",
		"blank line then indented line": "\n  allow_reboot: false\ncommands:\n  allow_shutdown: true\n",
		"indented url orphan":           "  url: \"https://old.test:7331\"\nagent:\n  poll_interval: 60\ncommands:\n  allow_shutdown: true\n",
	}
	for name, content := range cases {
		file := filepath.Join(temp, "agent-"+strings.ReplaceAll(name, " ", "_")+".yaml")
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := agent.LoadConfig(file)
		if err != nil || loaded.Commands.AllowShutdown {
			t.Skipf("%s: the agent no longer loads this as false without error (err=%v); the test premise is gone", name, err)
		}
		if r.enabled(t, strings.ReplaceAll(name, " ", "_"), []byte(content)) {
			t.Errorf("[FR-017] %s: the agent loads the file without error and reads allow_shutdown as false, but the installer classifier says enabled and would grant poweroff", name)
		}
	}
}
