package agent

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
)

// EXPECT-007 AZ: the AST rules, the process-guard self-tests and the safety
// scan of ADD section 8 and AD-015. Nothing in this file starts a process: the
// proof is state, a refusing runner and source-level checks.

func parseSource(t *testing.T, filename string, src []byte) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	require.NoError(t, err, "parse %s", filename)
	return fset, file
}

func funcName(d ast.Decl) string {
	if fn, ok := d.(*ast.FuncDecl); ok {
		return fn.Name.Name
	}
	return ""
}

func calledIdentName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// ---------------------------------------------------------------------------
// AC-007 (C-3 d): the gate calls only enter on the reporter
// ---------------------------------------------------------------------------

// gateReporterProblems checks awaitPowerClearance in src: every value bound
// from powerWaitReporterFrom may be used only to call enter, and enter must be
// called at least once.
func gateReporterProblems(t *testing.T, src []byte) []string {
	t.Helper()
	fset, file := parseSource(t, "reboot_clearance.go", src)
	var gate *ast.FuncDecl
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "awaitPowerClearance" {
			gate = fn
		}
	}
	if gate == nil {
		return []string{"awaitPowerClearance not found"}
	}
	reporters := map[string]bool{}
	ast.Inspect(gate.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		if call, ok := as.Rhs[0].(*ast.CallExpr); ok && calledIdentName(call) == "powerWaitReporterFrom" {
			for _, l := range as.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					reporters[id.Name] = true
				}
			}
		}
		return true
	})
	var problems []string
	enters := 0
	calledSel := map[*ast.SelectorExpr]bool{}
	ast.Inspect(gate.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				calledSel[sel] = true
			}
		}
		return true
	})
	ast.Inspect(gate.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || !reporters[id.Name] {
			return true
		}
		line := fset.Position(sel.Pos()).Line
		switch {
		case sel.Sel.Name != "enter":
			problems = append(problems, fmt.Sprintf("line %d: the gate uses %s.%s; only enter may be called on a reporter", line, id.Name, sel.Sel.Name))
		case !calledSel[sel]:
			problems = append(problems, fmt.Sprintf("line %d: enter is referenced but not called", line))
		default:
			enters++
		}
		return true
	})
	if len(reporters) == 0 {
		problems = append(problems, "the gate does not read a reporter from its context")
	}
	if enters != 1 {
		problems = append(problems, fmt.Sprintf("want exactly one call to enter in the gate, found %d", enters))
	}
	return problems
}

func TestEX7_AC007_GateCallsOnlyEnterOnReporter(t *testing.T) {
	src, err := os.ReadFile("reboot_clearance.go")
	require.NoError(t, err)
	assert.Empty(t, gateReporterProblems(t, src), "[C-3 d] awaitPowerClearance may call only enter() on the wait reporter")

	t.Run("the rule catches a blocking method in the gate", func(t *testing.T) {
		bad := []byte(`package agent
func (a *Agent) awaitPowerClearance() {
	if r := powerWaitReporterFrom(ctx); r != nil {
		r.enter()
		r.settle()
	}
}`)
		assert.NotEmpty(t, gateReporterProblems(t, bad), "a gate that calls settle must be flagged")
		bad2 := []byte(`package agent
func (a *Agent) awaitPowerClearance() {
	r := powerWaitReporterFrom(ctx)
	r.leave()
}`)
		assert.NotEmpty(t, gateReporterProblems(t, bad2), "a gate that calls leave must be flagged")
	})
}

// ---------------------------------------------------------------------------
// AC-009: only the two standalone handlers attach a reporter
// ---------------------------------------------------------------------------

func TestEX7_AC009_OnlyStandaloneHandlersAttachWaitReporter(t *testing.T) {
	owners := map[string][]string{}
	for _, name := range agentSourceFiles(t) {
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		_, file := parseSource(t, name, src)
		for _, d := range file.Decls {
			owner := funcName(d)
			ast.Inspect(d, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					switch calledIdentName(call) {
					case "withPowerWaitReporter", "newPowerWaitReporter":
						owners[calledIdentName(call)] = append(owners[calledIdentName(call)], owner)
					}
				}
				return true
			})
		}
	}
	for _, fn := range []string{"withPowerWaitReporter", "newPowerWaitReporter"} {
		got := append([]string(nil), owners[fn]...)
		sort.Strings(got)
		assert.Equal(t, []string{"handleReboot", "handleShutdown"}, got,
			"[AC-009] %s may be called only by handleReboot and handleShutdown (post-patch reboots never carry a reporter)", fn)
	}
}

// ---------------------------------------------------------------------------
// AC-036: no new sudo invocation
// ---------------------------------------------------------------------------

// sudoSites is every call site that mentions the "sudo" string literal in the
// agent's non-test sources at e120031, by file and function with the number of
// literals. EXPECT-007 adds none (AD-012).
var sudoSites = map[string]int{
	"commands.go|enableDNFAutomatic":                    2,
	"commands.go|enableUnattendedUpgrades":              3,
	"commands.go|execReboot":                            2,
	"commands.go|execShutdown":                          2,
	"commands.go|handleOSUpdateWithRun":                 3,
	"doctor.go|Doctor":                                  1,
	"doctor.go|checkDpkgRepair":                         1,
	"doctor.go|reportUpdatePrivilege":                   1,
	"maintenance.go|runDetachedMaintenanceWith":         1,
	"maintenance_doctor.go|agentPolicyRunner":           1,
	"maintenance_linux.go|maintenanceDeniedPrivileges":  1,
	"maintenance_linux.go|maintenanceReadiness":         2,
	"shutdown_doctor.go|reportShutdownPrivilege":        1,
	"update_preflight_linux.go|probeUpdateInstallation": 1,
	"updater.go|applyStaged":                            2,
}

func TestEX7_AC036_NoNewSudoInvocation(t *testing.T) {
	got := map[string]int{}
	for _, name := range agentSourceFiles(t) {
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		_, file := parseSource(t, name, src)
		for _, d := range file.Decls {
			owner := funcName(d)
			ast.Inspect(d, func(n ast.Node) bool {
				if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING && bl.Value == `"sudo"` {
					got[name+"|"+owner]++
				}
				return true
			})
		}
	}
	assert.Equal(t, sudoSites, got,
		"[AC-036] the agent's sudo call sites must be exactly today's: EXPECT-007 invokes nothing new through sudo")
}

// ---------------------------------------------------------------------------
// AC-037: the process guard on the update and hand-off paths (C-1)
// ---------------------------------------------------------------------------

// sideEffects counts everything a refused real-process path must not touch.
type sideEffects struct {
	fs     *faultFS
	pauses atomic.Int32
}

func newSideEffects() *sideEffects { return &sideEffects{fs: newFaultFS()} }

func (s *sideEffects) pause(context.Context, time.Duration) bool {
	s.pauses.Add(1)
	return true
}

func (s *sideEffects) assertNone(t *testing.T, what string) {
	t.Helper()
	assert.Empty(t, s.fs.ops(), "[C-1] %s: no file-system operation may happen", what)
	assert.Zero(t, s.pauses.Load(), "[C-1] %s: no pause may happen", what)
}

func TestEX7_AC037_UpdatePipelineRefusesRealProcessInTest(t *testing.T) {
	require.True(t, inTestBinary(), "[C-1] a go test binary must be detected")
	fx := newSideEffects()
	a := &Agent{config: &Config{}, pause: fx.pause} // no runner on purpose
	require.Nil(t, a.runner, "[C-1] the point of this test is a nil runner")

	ctx := context.Background()
	_, err := a.processRunner()(ctx, "sudo", "-n", "-l")
	assert.ErrorIs(t, err, errProcessInTest, "[C-1] the fallback runner refuses in a test binary")

	p := a.newUpdatePipeline()
	require.NotNil(t, p.runner, "[C-1] the pipeline has a runner")
	_, err = p.runner(ctx, "sudo", "systemctl", "reset-failed", updateUnit)
	assert.ErrorIs(t, err, errProcessInTest, "[C-1] the pipeline's unit reset is refused")

	p.status = func() (maintenance.UpdateStatus, error) {
		t.Error("[C-1] the pipeline must not poll a status after a refused launch")
		return maintenance.UpdateStatus{}, errors.New("unexpected")
	}
	p.wait = func(context.Context) error {
		t.Error("[C-1] the pipeline must not wait after a refused launch")
		return nil
	}
	err = p.applyStaged(ctx, strings.Repeat("ab", 32), time.Unix(0, 0))
	require.Error(t, err, "[C-1] the apply launch must fail")
	assert.ErrorIs(t, err, errProcessInTest, "[C-1] the apply launch is refused with errProcessInTest: %v", err)

	if p.preflight != nil {
		assert.Error(t, p.preflight(ctx), "[C-1] the preflight probe fails closed with no fake runner")
	}
	fx.assertNone(t, "update pipeline")
}

func TestEX7_AC037_HandoffWatcherRefusesRealProcessInTest(t *testing.T) {
	fx := newSideEffects()
	a := &Agent{config: &Config{}, pause: fx.pause}
	a.cmdCtx, a.cmdCancel = context.WithCancel(context.Background())
	defer a.cmdCancel()
	require.Nil(t, a.runner, "[C-1] the point of this test is a nil runner")

	_, err := a.processRunner()(context.Background(), "/usr/bin/systemctl", "show", updateUnit)
	assert.ErrorIs(t, err, errProcessInTest, "[C-1] the unit-state query is refused")

	var reads atomic.Int32
	a.updateStatusReader = func() (maintenance.UpdateStatus, error) {
		reads.Add(1)
		return maintenance.UpdateStatus{}, errors.New("no status yet")
	}
	waits := make(chan struct{}, 8)
	a.maintenanceClient = &maintenanceClient{wait: func(ctx context.Context) error {
		waits <- struct{}{}
		if reads.Load() >= 2 {
			a.cmdCancel()
		}
		return ctx.Err()
	}}
	a.out = nil // finishSignedHandoff would need it: it must never be reached

	a.watchSignedHandoff(updateHandoff{SHA256: strings.Repeat("ab", 32), RequestedAt: time.Unix(1, 0)})
	a.cmdWG.Wait()

	assert.GreaterOrEqual(t, int(reads.Load()), 2, "[C-1] the watcher went round twice, so it asked for the unit state and kept watching")
	assert.GreaterOrEqual(t, len(waits), 1, "[C-1] the watcher waited through the injected seam")
	fx.assertNone(t, "hand-off watcher")
}

// realRunnerDirectUses is the reviewed table of non-test uses of execRunner
// outside processRunner: file|function -> reason. A stale entry fails.
var realRunnerDirectUses = map[string]string{
	"doctor.go|Doctor":                             "`riot-agent doctor` CLI entry point run by Ryan, never reached from an Agent instance or by any test",
	"doctor.go|checkUpdatePrivilege":               "doctor CLI probe, same entry point as Doctor",
	"doctor.go|checkMemoryLimit":                   "doctor CLI probe, same entry point as Doctor",
	"maintenance_doctor.go|checkMaintenancePolicy": "doctor CLI probe, same entry point as Doctor",
}

// directExecFallbacks is the reviewed table of functions that run exec.Command
// behind an `a.runner != nil` test seam and fall back to a real process. They
// serve os_update and other handlers this story's tests never reach. Extending
// the process guard to them is follow-on F-1 (ADD AD-015, R-5).
var directExecFallbacks = map[string]string{
	"commands.go|runCommand":        "os_update and other handlers; follow-on F-1",
	"commands.go|runUpgradeCommand": "os_update package-manager runs; follow-on F-1",
}

// realRunnerProblems reports every unguarded use of execRunner in one source
// file, plus the uses it found (file|function) for the stale-entry check.
func realRunnerProblems(t *testing.T, filename string, src []byte) (problems []string, uses []string) {
	t.Helper()
	fset, file := parseSource(t, filename, src)
	for _, d := range file.Decls {
		owner := funcName(d)
		fn, isFn := d.(*ast.FuncDecl)
		// A statement of the form `if X == nil { X = execRunner }` is never allowed.
		ast.Inspect(d, func(n ast.Node) bool {
			ifs, ok := n.(*ast.IfStmt)
			if !ok {
				return true
			}
			bin, ok := ifs.Cond.(*ast.BinaryExpr)
			if !ok || bin.Op != token.EQL {
				return true
			}
			if y, ok := bin.Y.(*ast.Ident); !ok || y.Name != "nil" {
				return true
			}
			for _, s := range ifs.Body.List {
				if as, ok := s.(*ast.AssignStmt); ok && len(as.Rhs) == 1 {
					if id, ok := as.Rhs[0].(*ast.Ident); ok && id.Name == "execRunner" {
						problems = append(problems, fmt.Sprintf("%s:%d: `if X == nil { X = execRunner }` is forbidden; use a.processRunner()", filename, fset.Position(ifs.Pos()).Line))
					}
				}
			}
			return true
		})
		// Every other reference.
		ast.Inspect(d, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || id.Name != "execRunner" {
				return true
			}
			if isFn && fn.Name == id {
				return true // the declaration itself
			}
			line := fset.Position(id.Pos()).Line
			key := filename + "|" + owner
			if owner == "processRunner" {
				if !processRunnerGuarded(fn, id.Pos()) {
					problems = append(problems, fmt.Sprintf("%s:%d: execRunner in processRunner is not preceded by `if inTestBinary()`", filename, line))
				}
				return true
			}
			if _, ok := realRunnerDirectUses[key]; !ok {
				problems = append(problems, fmt.Sprintf("%s:%d: execRunner used in %q without the test-binary guard and not in realRunnerDirectUses", filename, line, owner))
				return true
			}
			uses = append(uses, key)
			return true
		})
	}
	return problems, uses
}

// processRunnerGuarded reports whether an `if inTestBinary() { return ... }`
// statement of fn ends before pos.
func processRunnerGuarded(fn *ast.FuncDecl, pos token.Pos) bool {
	for _, s := range fn.Body.List {
		ifs, ok := s.(*ast.IfStmt)
		if !ok || ifs.End() > pos {
			continue
		}
		if call, ok := ifs.Cond.(*ast.CallExpr); ok && calledIdentName(call) == "inTestBinary" && len(ifs.Body.List) > 0 {
			if _, ok := ifs.Body.List[len(ifs.Body.List)-1].(*ast.ReturnStmt); ok {
				return true
			}
		}
	}
	return false
}

// directExecProblems lists functions that call exec.Command(Context) behind a
// reference to a.runner and are not in directExecFallbacks.
func directExecProblems(t *testing.T, filename string, src []byte) (problems []string, found []string) {
	t.Helper()
	fset, file := parseSource(t, filename, src)
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		usesRunner, execLine := false, 0
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "a" && x.Sel.Name == "runner" {
					usesRunner = true
				}
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") && execLine == 0 {
						execLine = fset.Position(x.Pos()).Line
					}
				}
			}
			return true
		})
		if !usesRunner || execLine == 0 {
			continue
		}
		key := filename + "|" + fn.Name.Name
		// The power exec functions carry their own inTestBinary guard
		// (SHUTDOWN-DPKG-001 C-1); the rule below is about everything else.
		if fn.Name.Name == "execReboot" || fn.Name.Name == "execShutdown" {
			continue
		}
		if _, ok := directExecFallbacks[key]; !ok {
			problems = append(problems, fmt.Sprintf("%s:%d: %s runs exec.Command behind a.runner without the process guard and is not in directExecFallbacks", filename, execLine, fn.Name.Name))
			continue
		}
		found = append(found, key)
	}
	return problems, found
}

func TestEX7_AC037_RealRunnerFallbacksGuarded(t *testing.T) {
	var problems, uses, direct []string
	for _, name := range agentSourceFiles(t) {
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		p, u := realRunnerProblems(t, name, src)
		problems = append(problems, p...)
		uses = append(uses, u...)
		p, f := directExecProblems(t, name, src)
		problems = append(problems, p...)
		direct = append(direct, f...)
	}
	assert.Empty(t, problems, "[C-1] every real-runner fallback must be guarded or reviewed")
	for key := range realRunnerDirectUses {
		assert.True(t, slices.Contains(uses, key), "[C-1] stale realRunnerDirectUses entry %q: no such use exists any more", key)
	}
	for key := range directExecFallbacks {
		assert.True(t, slices.Contains(direct, key), "[C-1] stale directExecFallbacks entry %q: no such function exists any more", key)
	}

	t.Run("the pin catches an unguarded fallback", func(t *testing.T) {
		bad := []byte("package agent\nfunc (a *Agent) newThing() {\n\trunner := a.runner\n\tif runner == nil {\n\t\trunner = execRunner\n\t}\n\t_ = runner\n}\n")
		p, _ := realRunnerProblems(t, "bad.go", bad)
		assert.NotEmpty(t, p, "an `if X == nil { X = execRunner }` outside processRunner must be flagged")
		bad2 := []byte("package agent\nfunc (a *Agent) other() { run(execRunner) }\n")
		p, _ = realRunnerProblems(t, "bad2.go", bad2)
		assert.NotEmpty(t, p, "any other reference outside the reviewed table must be flagged")
		unguarded := []byte("package agent\nfunc (a *Agent) processRunner() int {\n\treturn execRunner\n}\n")
		p, _ = realRunnerProblems(t, "bad3.go", unguarded)
		assert.NotEmpty(t, p, "processRunner without the test-binary check must be flagged")
	})
}

// ---------------------------------------------------------------------------
// AC-037: the agent test scan
// ---------------------------------------------------------------------------

// ex7ForbiddenImports are the packages an EX7 agent test may not import.
var ex7ForbiddenImports = []string{"os/exec", "syscall", "golang.org/x/sys/unix", "golang.org/x/sys/windows"}

// ex7ForbiddenCalls are the calls an EX7 agent test may not make (pkg.Name).
var ex7ForbiddenCalls = map[string]bool{
	"exec.Command": true, "exec.CommandContext": true, "os.StartProcess": true,
	"time.Sleep": true, "http.ListenAndServe": true,
}

// ex7ScanSource returns the forbidden forms in one EX7 agent test source.
func ex7ScanSource(t *testing.T, filename string, src []byte) []string {
	t.Helper()
	fset, file := parseSource(t, filename, src)
	var problems []string
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if slices.Contains(ex7ForbiddenImports, path) {
			problems = append(problems, fmt.Sprintf("%s: imports %s", filename, path))
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && ex7ForbiddenCalls[pkg.Name+"."+sel.Sel.Name] {
					problems = append(problems, fmt.Sprintf("%s:%d: calls %s.%s", filename, fset.Position(x.Pos()).Line, pkg.Name, sel.Sel.Name))
				}
			}
		case *ast.CompositeLit:
			if id, ok := x.Type.(*ast.Ident); ok && id.Name == "Agent" {
				hasProbe, hasRunner := false, false
				for _, e := range x.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok {
							hasProbe = hasProbe || k.Name == "dpkgLockProbe"
							hasRunner = hasRunner || k.Name == "runner"
						}
					}
				}
				if hasProbe && !hasRunner {
					problems = append(problems, fmt.Sprintf("%s:%d: an Agent literal sets dpkgLockProbe but no runner", filename, fset.Position(x.Pos()).Line))
				}
			}
		}
		return true
	})
	return problems
}

// ex7AgentTestFiles lists the agent test files the scan covers: every
// _test.go containing TestEX7_ plus the two named files, which must exist.
func ex7AgentTestFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	require.NoError(t, err)
	set := map[string]bool{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		if strings.Contains(string(src), "TestEX7_") {
			set[f] = true
		}
	}
	for _, must := range []string{"power_wait_step_test.go", "update_events_test.go"} {
		_, err := os.Stat(must)
		require.NoError(t, err, "[AC-037] %s must exist and be scanned", must)
		set[must] = true
	}
	var out []string
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func TestEX7_AC037_AgentTestsRunNothingReal(t *testing.T) {
	files := ex7AgentTestFiles(t)
	t.Logf("EX7 agent scan covers: %v", files)
	require.GreaterOrEqual(t, len(files), 3, "[AC-037] the scan must cover at least the AW, AU and AZ files")
	var problems []string
	for _, f := range files {
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		problems = append(problems, ex7ScanSource(t, f, src)...)
	}
	assert.Empty(t, problems, "[AC-037] EX7 agent tests must run nothing real and never sleep in real time")
}

func TestEX7_AC037_ScannerCatchesEachForbiddenForm(t *testing.T) {
	forbidden := map[string]string{
		"os/exec import":      "package agent\nimport \"os/exec\"\nvar _ = exec.ErrNotFound\n",
		"syscall import":      "package agent\nimport \"syscall\"\nvar _ = syscall.EINVAL\n",
		"x/sys/unix import":   "package agent\nimport \"golang.org/x/sys/unix\"\nvar _ = unix.EINVAL\n",
		"x/sys/windows":       "package agent\nimport \"golang.org/x/sys/windows\"\nvar _ = windows.ERROR_ACCESS_DENIED\n",
		"exec.Command":        "package agent\nfunc f() { exec.Command(\"x\") }\n",
		"exec.CommandContext": "package agent\nfunc f() { exec.CommandContext(nil, \"x\") }\n",
		"os.StartProcess":     "package agent\nfunc f() { os.StartProcess(\"x\", nil, nil) }\n",
		"time.Sleep":          "package agent\nfunc f() { time.Sleep(1) }\n",
		"http.ListenAndServe": "package agent\nfunc f() { http.ListenAndServe(\":0\", nil) }\n",
		"Agent literal with a probe and no runner": "package agent\nfunc f() { _ = &Agent{dpkgLockProbe: nil} }\n",
	}
	for name, src := range forbidden {
		assert.NotEmpty(t, ex7ScanSource(t, "fake_test.go", []byte(src)), "[AC-037] the scan must catch: %s", name)
	}
	clean := "package agent\nfunc f() { _ = &Agent{dpkgLockProbe: nil, runner: nil} }\n"
	assert.Empty(t, ex7ScanSource(t, "ok_test.go", []byte(clean)), "[AC-037] an Agent literal with a runner is allowed")
}
