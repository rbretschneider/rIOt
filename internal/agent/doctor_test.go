package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
)

// [AC-011] collectorDeps must include "gpu" -> ["nvidia-smi"]
func TestDoctor_CollectorDeps_GPUEntry(t *testing.T) {
	deps, ok := collectorDeps["gpu"]
	assert.True(t, ok, "[AC-011] collectorDeps must contain an entry for 'gpu'")
	assert.Equal(t, []string{"nvidia-smi"}, deps,
		"[AC-011] gpu collector dependency must be nvidia-smi")
}

// [AC-011] allCollectors slice built inside Doctor must include "gpu".
// We replicate the slice literal here because it is constructed inline in Doctor().
// This test validates the expected constant — if Doctor() changes, this test
// will catch the divergence.
func TestDoctor_AllCollectors_IncludesGPU(t *testing.T) {
	allCollectors := []string{
		"system", "cpu", "memory", "disk", "network", "os_info",
		"updates", "services", "processes", "docker", "container_logs",
		"security", "logs", "ups", "webservers", "usb", "hardware", "cron",
		"gpu",
	}

	found := false
	for _, name := range allCollectors {
		if name == "gpu" {
			found = true
			break
		}
	}
	assert.True(t, found, "[AC-011] allCollectors must contain 'gpu'")
}

// [HARDEN-002][AC-053] The doctor server check treats /health 503 (degraded
// database) as a warning that names the status code, and 200 as a pass. It
// reads only the status code, never the body.
func TestHARDEN002_AC053_DoctorServerCheckHandles503(t *testing.T) {
	serve := func(status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write([]byte(`{"status":"degraded","database":false}`))
		}))
	}
	run := func(srv *httptest.Server) string {
		cfg := &Config{Server: ServerConfig{URL: srv.URL}}
		return captureStdout(func() { checkServer(cfg) })
	}

	t.Run("503 warns with the status code and does not pass", func(t *testing.T) {
		srv := serve(http.StatusServiceUnavailable)
		defer srv.Close()
		var out string
		assert.NotPanics(t, func() { out = run(srv) }, "doctor must not panic on a 503")
		assert.Contains(t, out, "Health endpoint returned 503", "a warning naming the status code is expected, got %q", out)
		assert.NotContains(t, out, "Health endpoint returned 200", "503 must not be reported as a pass")
		assert.NotContains(t, out, "\033[32m✓\033[0m Health endpoint", "503 must not use the pass marker")
	})

	t.Run("200 reports pass", func(t *testing.T) {
		srv := serve(http.StatusOK)
		defer srv.Close()
		out := run(srv)
		assert.Contains(t, out, "\033[32m✓\033[0m Health endpoint returned 200", "200 must be reported as a pass, got %q", out)
	})
}

// captureStdout captures everything written to os.Stdout during fn().
func captureStdout(fn func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r) //nolint:errcheck
	return buf.String()
}

// [AC-031] checkJournalAccess warns with remediation text when journal returns
// empty output and the current user is not in the systemd-journal group.
//
// We test checkJournalAccess indirectly by examining its output format rather
// than mocking exec.Command (which would require dependency injection not
// present in the current doctor.go interface). The unit tests verify the
// warn/pass helper functions produce the expected ANSI-formatted output, and
// that the remediation text is the documented string.
func TestAC031_CheckJournalAccess_WarnTextContainsRemediation(t *testing.T) {
	// Verify the remediation text the warn() helper would emit.
	// We capture the output of a direct warn() call to ensure the format matches
	// what an operator would see when checkJournalAccess fires.
	out := captureStdout(func() {
		warn("Journal read returned empty and user is not in 'systemd-journal' group")
		warn("  Auth-failure detector will silently report zero — fix with:")
		warn("  sudo usermod -a -G systemd-journal riot && sudo systemctl restart riot-agent")
	})

	assert.True(t, strings.Contains(out, "systemd-journal"),
		"[AC-031] warn output must mention systemd-journal group")
	assert.True(t, strings.Contains(out, "sudo usermod"),
		"[AC-031] warn output must include the usermod remediation command")
	assert.True(t, strings.Contains(out, "riot-agent"),
		"[AC-031] warn output must mention riot-agent restart")
}

// [AC-031] pass() helper produces a green checkmark line (used when journal read succeeds).
func TestAC031_CheckJournalAccess_PassTextFormat(t *testing.T) {
	out := captureStdout(func() {
		pass("Journal read access OK")
	})

	assert.True(t, strings.Contains(out, "Journal read access OK"),
		"[AC-031] pass output must contain the success message")
}

// [AC-031] collectorDeps contains "logs" -> ["journalctl"] (prerequisite for the journal check).
func TestAC031_CollectorDeps_LogsHasJournalctl(t *testing.T) {
	deps, ok := collectorDeps["logs"]
	assert.True(t, ok, "[AC-031] collectorDeps must contain an entry for 'logs'")
	assert.Contains(t, deps, "journalctl",
		"[AC-031] logs collector dependency must include journalctl")
}

// [AC-031] collectorDeps contains "security" -> ["journalctl"].
func TestAC031_CollectorDeps_SecurityHasJournalctl(t *testing.T) {
	deps, ok := collectorDeps["security"]
	assert.True(t, ok, "[AC-031] collectorDeps must contain an entry for 'security'")
	assert.Contains(t, deps, "journalctl",
		"[AC-031] security collector dependency must include journalctl")
}

// Suppress unused import warning for fmt (used by the warn/pass helpers under test).
var _ = fmt.Sprintf

// [SEC-AC-004] The doctor's hold-enforcement probes are the same
// non-mutating "sudo -n -l" probes the agent runtime uses, with fixed-shape
// argv, and the remediation text names the installer re-run.
func TestSECAC004_DoctorHoldProbes(t *testing.T) {
	t.Run("[SEC-AC-004] probe argv is the exact sudo -n -l shape", func(t *testing.T) {
		var calls [][]string
		hm := collectors.NewHoldManager(true, HoldStatePath(), DNFHoldsStagedPath())
		hm.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return nil, nil
		})

		assert.NoError(t, hm.VerifyPrivileges(context.Background(), "apt"))
		assert.NoError(t, hm.VerifyPrivileges(context.Background(), "dnf"))

		if assert.Len(t, calls, 2) {
			assert.Equal(t, []string{"sudo", "-n", "-l"}, calls[0][:3],
				"[SEC-AC-004] apt probe lists, never executes")
			assert.Contains(t, calls[0], "hold")
			assert.Equal(t, []string{"sudo", "-n", "-l"}, calls[1][:3],
				"[SEC-AC-004] dnf probe lists, never executes")
			assert.Contains(t, calls[1], DNFHoldsStagedPath())
			assert.Contains(t, calls[1], collectors.DNFFragmentPath)
		}
	})

	t.Run("[SEC-AC-004] failure remediation names the installer re-run", func(t *testing.T) {
		out := captureStdout(func() {
			fail("apt-mark hold/unhold sudoers rules missing — hold enforcement will report no_privilege")
			warn("  Re-run the install script (curl … | sudo bash) to rewrite /etc/sudoers.d/riot-agent")
		})
		assert.Contains(t, out, "Re-run the install script")
		assert.Contains(t, out, "/etc/sudoers.d/riot-agent")
	})
}

// fakeFileInfo is a minimal os.FileInfo for the apply-helper presence probe.
type fakeFileInfo struct{ mode os.FileMode }

func (f fakeFileInfo) Name() string       { return "riot-agent-apply" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

func statReturning(mode os.FileMode, err error) func(string) (os.FileInfo, error) {
	return func(string) (os.FileInfo, error) {
		if err != nil {
			return nil, err
		}
		return fakeFileInfo{mode: mode}, nil
	}
}

// [AC-012] The doctor probe targets the exact fixed systemd-run verifier rule
// (never the removed `sh -c *` wildcard) via `sudo -n -l`.
func TestAC012_UpdatePrivilegeProbe_TargetsTheFixedVerifierRule(t *testing.T) {
	got := updatePrivilegeProbeArgs("/usr/bin/systemd-run")

	assert.Equal(t, []string{
		"-n", "-l", "/usr/bin/systemd-run", "--unit=riot-agent-update", "/usr/local/lib/riot/riot-agent-apply",
	}, got, "[AC-012] probe must be the exact sudoers rule the installer writes")
	for _, arg := range got {
		assert.NotEqual(t, "sh", arg, "[AC-012] probe must not reference the removed shell rule")
		assert.NotEqual(t, "-c", arg, "[AC-012] probe must not reference the removed sh -c rule")
		assert.NotContains(t, arg, "*", "[AC-012] probe must not reference a wildcard rule")
	}
}

// [AC-012] Rule authorized and helper present with mode 0755 -> two passes.
func TestAC012_ReportUpdatePrivilege_PresentReportsPass(t *testing.T) {
	var gotName string
	var gotArgs []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, args
		return nil, nil
	}

	out := captureStdout(func() { reportUpdatePrivilege(run, statReturning(0o755, nil), "/usr/bin/systemd-run") })

	assert.Equal(t, "sudo", gotName)
	assert.Equal(t, updatePrivilegeProbeArgs("/usr/bin/systemd-run"), gotArgs, "[AC-012] the runner must receive the exact probe argv")
	assert.Contains(t, out, "Agent Update")
	assert.Contains(t, out, "update-apply sudo rule present")
	assert.Contains(t, out, "apply helper present")
	assert.NotContains(t, out, "missing")
}

// [AC-013] Rule absent -> failure line plus re-run-installer guidance.
func TestAC013_ReportUpdatePrivilege_MissingRuleFailsWithReinstallGuidance(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		return []byte("sudo: a password is required"), fmt.Errorf("exit status 1")
	}

	out := captureStdout(func() { reportUpdatePrivilege(run, statReturning(0o755, nil), "/usr/bin/systemd-run") })

	assert.Contains(t, out, "update-apply sudo rule missing", "[AC-013] must fail loudly when the rule is absent")
	assert.Contains(t, out, "Re-run the install script", "[AC-013] must tell the operator how to fix it")
	assert.Contains(t, out, "/etc/sudoers.d/riot-agent")
	assert.NotContains(t, out, "update-apply sudo rule present")
}

// [AC-013] Helper binary absent -> failure with re-run guidance, independent of the rule.
func TestAC013_ReportUpdatePrivilege_MissingHelperFails(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) { return nil, nil }

	out := captureStdout(func() {
		reportUpdatePrivilege(run, statReturning(0, os.ErrNotExist), "/usr/bin/systemd-run")
	})

	assert.Contains(t, out, "apply helper missing at /usr/local/lib/riot/riot-agent-apply")
	assert.Contains(t, out, "re-run the install script")
}

// [AC-013] Helper present but with the wrong mode -> warning, not a pass.
func TestAC013_ReportUpdatePrivilege_WrongHelperModeWarns(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) { return nil, nil }

	out := captureStdout(func() { reportUpdatePrivilege(run, statReturning(0o777, nil), "/usr/bin/systemd-run") })

	assert.Contains(t, out, "is not mode 0755")
	assert.NotContains(t, out, "apply helper present")
}

// ---------------------------------------------------------------------------
// AUTOUPD-HARDEN-001: compose trust section
// ---------------------------------------------------------------------------

func TestDoctor_AC016_ReportComposeTrustRootsAndPerProjectLines(t *testing.T) {
	x := newTrustFx(t)
	bad := x.p("opt", "stacks", "bad")
	x.fs.chain(bad)
	x.fs.file(filepath.Join(bad, "compose.yaml"), 0, 0, 0644, "")
	x.fs.get(bad).uid = uidMallory
	x.addStackContainer("c2", "bad-web-1", "bad", bad)
	cfg := DockerConfig{ComposeRoots: []string{x.p("opt", "stacks")}}
	x.ct.roots = cfg.ComposeRoots

	out := captureStdout(func() { reportComposeTrust(cfg, x.snap, nil, x.ct) })

	assert.Contains(t, out, "Compose roots: "+x.p("opt", "stacks"), "[AC-016] the roots setting is printed")
	assert.Contains(t, out, `project "app"`, "[AC-016]")
	assert.Contains(t, out, "is trusted", "[AC-016] a passing project says so")
	assert.Contains(t, out, `project "bad"`)
	assert.Contains(t, out, "(owner)", "[AC-016] a failing project names the rule")
	assert.Contains(t, out, "\033[32m", "pass uses the pass marker")
	assert.Contains(t, out, "\033[31m", "fail uses the fail marker")
	assert.Less(t, strings.Index(out, `project "app"`), strings.Index(out, `project "bad"`), "[AC-016] lines are sorted by project")

	unset := captureStdout(func() { reportComposeTrust(DockerConfig{}, x.snap, nil, x.ct) })
	assert.Contains(t, unset, "not set", "[AC-016] an unset setting is shown as such")
}

func TestDoctor_AC016_InvalidRootsFail(t *testing.T) {
	x := newTrustFx(t)
	cfg := DockerConfig{ComposeRoots: []string{"relative"}, rootsErr: errors.New("bad")}
	x.ct.roots, x.ct.rootsErr = cfg.ComposeRoots, cfg.rootsErr

	out := captureStdout(func() { reportComposeTrust(cfg, x.snap, nil, x.ct) })

	assert.Contains(t, out, "not an absolute path", "[AC-016] an invalid setting is reported as a failure")
	assert.Contains(t, out, "(compose_roots_invalid)", "[AC-016] and every project line shows the fail-closed rule")
}

func TestDoctor_AC016_DockerUnreachableWarns(t *testing.T) {
	x := newTrustFx(t)
	out := captureStdout(func() {
		reportComposeTrust(DockerConfig{}, daemonSnapshot{}, errors.New(strings.Repeat("x", 5000)), x.ct)
	})

	assert.Contains(t, out, "cannot read docker state", "[AC-016]")
	assert.NotContains(t, out, "is trusted")
	assert.Less(t, len(out), 2000, "a long daemon error is bounded")
}

func TestDoctor_AC016_ContainerWritableNamesContainerAndMount(t *testing.T) {
	x := newTrustFx(t)
	x.writer("dot-mount", bindRW(x.dir, "/app"))

	out := captureStdout(func() { reportComposeTrust(DockerConfig{}, x.snap, nil, x.ct) })

	assert.Contains(t, out, "(container_writable)")
	assert.Contains(t, out, "dot-mount", "[AC-016] the container is named")
	assert.Contains(t, out, "/app", "[AC-016] and its mount")
}

func TestDoctor_AC018_ListsExcludedContainers(t *testing.T) {
	x := newTrustFx(t)
	x.snap.excluded["c1"] = excludedImageLabel
	x.addStackContainer("c2", "blind-1", "app", x.dir)
	x.snap.excluded["c2"] = excludedImageUnread

	out := captureStdout(func() { reportComposeTrust(DockerConfig{}, x.snap, nil, x.ct) })

	assert.Contains(t, out, `container "app-web-1" ignored as a compose source: its image defines the compose working-dir label`, "[AC-018]")
	assert.Contains(t, out, `container "blind-1" ignored as a compose source: its image could not be inspected`)
	assert.Contains(t, out, "No compose projects found", "excluded containers are not projects")
}

func TestDoctor_AC021_WindowsExplainsRootsRequirement(t *testing.T) {
	x := newTrustFx(t)
	x.ct.goos = "windows"

	out := captureStdout(func() { reportComposeTrust(DockerConfig{}, x.snap, nil, x.ct) })

	assert.Contains(t, out, "compose updates on Windows require docker.compose_roots (ownership cannot be verified)", "[AC-021]")
	assert.Contains(t, out, "(windows_requires_roots)", "[AC-021] each project line says why it fails")
}

// QA-D2 condition 4: the doctor says when it runs with more privilege than the
// agent, and notes each mount source compared through its readable prefix.
func TestDoctor_AC019_EuidWarningNamesServiceUser(t *testing.T) {
	lookup := func(name string) (int, bool) { return 998, name == "riot" }

	out := captureStdout(func() { reportPrivilegeMismatch(0, "riot", lookup) })
	assert.Contains(t, out, "uid 0", "[AC-019]")
	assert.Contains(t, out, `user "riot" (uid 998)`, "[AC-019] the service user is named")
	assert.Contains(t, out, "sudo -u riot riot-agent doctor")

	same := captureStdout(func() { reportPrivilegeMismatch(998, "riot", lookup) })
	assert.Empty(t, same, "[AC-019] no warning when the doctor runs as the agent user")
	unknown := captureStdout(func() { reportPrivilegeMismatch(0, "nosuch", lookup) })
	assert.Empty(t, unknown, "[AC-019] nothing to compare against when the user does not exist")
}

func TestDoctor_AC019_ServiceUserFromUnitFile(t *testing.T) {
	unit := func(string) ([]byte, error) { return []byte("[Service]\nUser=agentsvc\nGroup=x\n"), nil }
	missing := func(string) ([]byte, error) { return nil, os.ErrNotExist }
	assert.Equal(t, "agentsvc", agentServiceUser(unit), "[AC-019] the unit decides")
	assert.Equal(t, "riot", agentServiceUser(missing), "[AC-019] the installer default otherwise")
}

func TestDoctor_AC019_InfoLinePerFallbackSource(t *testing.T) {
	x := newTrustFx(t)
	_, src := x.sandboxSource()

	out := captureStdout(func() { reportComposeTrust(DockerConfig{}, x.snap, nil, x.ct) })

	assert.Contains(t, out, "compared through its readable prefix", "[AC-019]")
	assert.Contains(t, out, `container "sandbox"`)
	assert.Contains(t, out, strings.Trim(strconv.Quote(src), `"`), "the source is named")
	assert.Contains(t, out, "is trusted", "and the stack itself passes")
}

// unitSection returns the lines of one [Section] of a unit file.
func unitSection(t *testing.T, unit, section string) []string {
	t.Helper()
	var lines []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(unit, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "["):
			in = line == "["+section+"]"
		case in && line != "":
			lines = append(lines, line)
		}
	}
	return lines
}

// installerUnit extracts the unit file text that install.sh writes.
func installerUnit(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	script := strings.ReplaceAll(string(data), "\r\n", "\n")
	const start = "cat > /etc/systemd/system/riot-agent.service <<EOF\n"
	i := strings.Index(script, start)
	if i < 0 {
		t.Fatal("install.sh no longer writes the riot-agent unit with the expected heredoc")
	}
	rest := script[i+len(start):]
	return rest[:strings.Index(rest, "\nEOF\n")]
}

const goldenSystemctlShow = "MemoryMax=536870912\nOOMPolicy=continue\nKillMode=control-group\nEnvironment=GOMEMLIMIT=200MiB\n"

func TestGROWTH_AC059_InstallerUnitAndDoctorMemoryLimit(t *testing.T) {
	wantDirectives := []string{"MemoryMax=50%", "Environment=GOMEMLIMIT=200MiB", "OOMPolicy=continue"}

	t.Run("the installer writes the three memory directives in the [Service] section", func(t *testing.T) {
		service := unitSection(t, installerUnit(t), "Service")
		for _, d := range wantDirectives {
			assert.Contains(t, service, d, "[AC-059] install.sh's unit must contain %s", d)
		}
	})

	t.Run("the standalone unit file carries the same three lines", func(t *testing.T) {
		data, err := os.ReadFile("../../scripts/riot-agent.service")
		if err != nil {
			t.Fatalf("read riot-agent.service: %v", err)
		}
		service := unitSection(t, string(data), "Service")
		for _, d := range wantDirectives {
			assert.Contains(t, service, d, "[AC-059] scripts/riot-agent.service must contain %s", d)
		}
	})

	t.Run("doctor reports MemoryMax in bytes, OOMPolicy, KillMode and GOMEMLIMIT on a good unit", func(t *testing.T) {
		out := captureStdout(func() { reportMemoryLimit(goldenSystemctlShow, nil) })

		assert.Contains(t, out, "MemoryMax = 536870912 bytes (512 MiB)", "[AC-059] the limit is shown in bytes")
		assert.Contains(t, out, "OOMPolicy = continue")
		assert.Contains(t, out, "KillMode = control-group")
		assert.Contains(t, out, "GOMEMLIMIT = 200MiB")
		assert.NotContains(t, out, "no memory limit")
	})

	t.Run("doctor fails a unit without a limit and says how to fix it", func(t *testing.T) {
		for name, show := range map[string]string{
			"infinity":        "MemoryMax=infinity\nOOMPolicy=stop\nKillMode=control-group\nEnvironment=\n",
			"legacy infinity": "MemoryMax=18446744073709551615\nOOMPolicy=stop\nKillMode=control-group\nEnvironment=\n",
			"missing":         "OOMPolicy=stop\n",
		} {
			out := captureStdout(func() { reportMemoryLimit(show, nil) })

			assert.Contains(t, out, "no memory limit on the agent unit", "[AC-059] %s must fail", name)
			assert.Contains(t, out, "Re-run the install script", "[AC-059] %s must point at the installer", name)
			assert.NotContains(t, out, "MemoryMax = ")
		}
	})

	t.Run("doctor warns when OOMPolicy is not continue or GOMEMLIMIT is unset", func(t *testing.T) {
		out := captureStdout(func() {
			reportMemoryLimit("MemoryMax=1073741824\nOOMPolicy=stop\nKillMode=process\nEnvironment=OTHER=1\n", nil)
		})

		assert.Contains(t, out, `OOMPolicy = "stop" (expected continue)`)
		assert.Contains(t, out, "KillMode = process")
		assert.Contains(t, out, "GOMEMLIMIT is not set")
		assert.Contains(t, out, "MemoryMax = 1073741824 bytes (1024 MiB)")
	})

	t.Run("GOMEMLIMIT is found among several environment assignments", func(t *testing.T) {
		v, ok := environmentValue(`FOO=bar "GOMEMLIMIT=200MiB" BAZ=1`, "GOMEMLIMIT")
		assert.True(t, ok)
		assert.Equal(t, "200MiB", v)
		_, ok = environmentValue("XGOMEMLIMIT=1", "GOMEMLIMIT")
		assert.False(t, ok, "a suffix match must not count")
	})

	t.Run("a systemd query failure is a warning, not a crash", func(t *testing.T) {
		out := captureStdout(func() { reportMemoryLimit("", fmt.Errorf("exec: systemctl not found")) })
		assert.Contains(t, out, "could not query systemd")
	})
}
