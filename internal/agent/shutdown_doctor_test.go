package agent

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SHUTDOWN-SUDO-001 doctor tests. Nothing here runs sudo or a power command:
// every invocation goes to an injected recorder (SC-001, SC-002).

// probeRecorder is an injected collectors.CommandRunner that records every call.
type probeRecorder struct {
	calls     [][]string // name followed by its arguments
	out       []byte
	err       error
	blockOnce bool // wait for the context instead of answering
	deadline  time.Time
}

func (r *probeRecorder) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if dl, ok := ctx.Deadline(); ok {
		r.deadline = dl
	}
	if r.blockOnce {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return r.out, r.err
}

func outputLinesWith(out, marker string) []string {
	var found []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, marker) {
			found = append(found, line)
		}
	}
	return found
}

func TestShutdownDoctor_SS_AC019_PassNamesTheGrant(t *testing.T) {
	rec := &probeRecorder{}
	out := captureStdout(func() { reportShutdownPrivilege("linux", true, rec.run, shutdownProbeTimeout) })

	passes := outputLinesWith(out, "✓")
	require.Len(t, passes, 1, "[AC-019] exactly one pass line, got output %q", out)
	assert.Contains(t, passes[0], "shutdown sudo rule present", "[AC-019] the pass line says the rule is present")
	assert.Contains(t, passes[0], strings.Join(shutdownSudoCommand(), " "), "[AC-019] the pass line names the granted command")
	assert.Empty(t, outputLinesWith(out, "✗"), "[AC-019] no fail line when the rule is present")
	assert.Len(t, rec.calls, 1, "[AC-019] exactly one sudo invocation")
}

func TestShutdownDoctor_SS_AC020_FailWithGuidance(t *testing.T) {
	cases := []struct {
		name    string
		rec     *probeRecorder
		timeout time.Duration
		reason  string
	}{
		{"refused", &probeRecorder{err: errors.New("exit status 1")}, time.Second, "not allowed without a password"},
		{"password required", &probeRecorder{out: []byte("sudo: a password is required"), err: errors.New("exit status 1")}, time.Second, "not allowed without a password"},
		{"sudo not found", &probeRecorder{err: &exec.Error{Name: "sudo", Err: exec.ErrNotFound}}, time.Second, "sudo not found"},
		{"no answer before the timeout", &probeRecorder{blockOnce: true}, 50 * time.Millisecond, "timed out after 50ms"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			out := captureStdout(func() { reportShutdownPrivilege("linux", true, tc.rec.run, tc.timeout) })
			assert.Less(t, time.Since(start), 2*time.Second, "[AC-020] %s must not wait beyond the stated timeout", tc.name)

			fails := outputLinesWith(out, "✗")
			require.Len(t, fails, 1, "[AC-020] exactly one fail line, got output %q", out)
			assert.Contains(t, fails[0], tc.reason, "[AC-020] the fail line says why")
			assert.Contains(t, fails[0], "remote shutdown will report failed", "[AC-020] the fail line says what the operator will see")
			assert.Contains(t, out, "re-run the install script as root", "[AC-020] guidance to re-run the installer as root")
			assert.Contains(t, out, "commands.allow_shutdown: true", "[AC-020] guidance names the config key")
			assert.Empty(t, outputLinesWith(out, "✓"), "[AC-020] no pass line on failure")
			require.Len(t, tc.rec.calls, 1, "[AC-020] exactly one sudo invocation")
			assert.Equal(t, "-n", tc.rec.calls[0][1], "[AC-020] sudo must never prompt for a password")
		})
	}
}

func TestShutdownDoctor_SS_AC020_TimeoutBounded(t *testing.T) {
	assert.Equal(t, 10*time.Second, shutdownProbeTimeout, "[AC-020] the stated timeout is 10 seconds")
	assert.LessOrEqual(t, shutdownProbeTimeout, 15*time.Second, "[FR-028] the bound must not exceed the 15 s of the other doctor sudo checks")

	rec := &probeRecorder{}
	before := time.Now()
	captureStdout(func() { reportShutdownPrivilege("linux", true, rec.run, shutdownProbeTimeout) })
	require.False(t, rec.deadline.IsZero(), "[AC-020] the runner must receive a context with a deadline")
	assert.LessOrEqual(t, rec.deadline.Sub(before), shutdownProbeTimeout+time.Second, "[AC-020] the deadline is the stated timeout from the call")
	assert.Greater(t, rec.deadline.Sub(before), time.Duration(0), "[AC-020] the deadline is in the future")
}

func TestShutdownDoctor_SS_AC021_RootAsksAboutRiot(t *testing.T) {
	t.Run("root lists the riot user's policy", func(t *testing.T) {
		rec := &probeRecorder{}
		captureStdout(func() { reportShutdownPrivilege("linux", true, agentPolicyRunner(0, rec.run), time.Second) })
		require.Len(t, rec.calls, 1)
		assert.Equal(t, append([]string{"sudo", "-n", "-U", "riot", "-l"}, shutdownSudoCommand()...), rec.calls[0], "[AC-021] as root the listing is for user riot, not root")
	})
	t.Run("a non-root user lists its own policy", func(t *testing.T) {
		rec := &probeRecorder{}
		captureStdout(func() { reportShutdownPrivilege("linux", true, agentPolicyRunner(1000, rec.run), time.Second) })
		require.Len(t, rec.calls, 1)
		assert.Equal(t, append([]string{"sudo", "-n", "-l"}, shutdownSudoCommand()...), rec.calls[0], "[AC-021] as a non-root user the invocation matches the existing doctor sudo checks")
	})
}

func TestAgentPolicyRunner_SS_RewriteMatchesMaintenance(t *testing.T) {
	cases := []struct {
		name string
		euid int
		in   []string
		want []string
	}{
		{"root rewrites a sudo -n -l listing", 0, []string{"sudo", "-n", "-l", "x", "y"}, []string{"sudo", "-n", "-U", "riot", "-l", "x", "y"}},
		{"root rewrites a bare listing", 0, []string{"sudo", "-n", "-l"}, []string{"sudo", "-n", "-U", "riot", "-l"}},
		{"root leaves another program alone", 0, []string{"systemctl", "-n", "-l"}, []string{"systemctl", "-n", "-l"}},
		{"root leaves sudo -n -u alone", 0, []string{"sudo", "-n", "-u", "riot", "x"}, []string{"sudo", "-n", "-u", "riot", "x"}},
		{"root leaves sudo -l without -n alone", 0, []string{"sudo", "-l", "x"}, []string{"sudo", "-l", "x"}},
		{"root leaves a single argument alone", 0, []string{"sudo", "-n"}, []string{"sudo", "-n"}},
		{"root leaves no argument alone", 0, []string{"sudo"}, []string{"sudo"}},
		{"a non-root user is never rewritten", 1000, []string{"sudo", "-n", "-l", "x"}, []string{"sudo", "-n", "-l", "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &probeRecorder{out: []byte("listing"), err: errors.New("passed through")}
			out, err := agentPolicyRunner(tc.euid, rec.run)(context.Background(), tc.in[0], tc.in[1:]...)
			require.Len(t, rec.calls, 1)
			assert.Equal(t, tc.want, rec.calls[0], "[AC-021] rewritten argv")
			assert.Equal(t, []byte("listing"), out, "the inner output is returned")
			assert.EqualError(t, err, "passed through", "the inner error is returned")
		})
	}

	t.Run("the caller's slice is not modified", func(t *testing.T) {
		rec := &probeRecorder{}
		args := []string{"-n", "-l", "x"}
		_, _ = agentPolicyRunner(0, rec.run)(context.Background(), "sudo", args...)
		assert.Equal(t, []string{"-n", "-l", "x"}, args, "the rewrite works on a copy")
	})
}

func TestShutdownDoctor_SS_AC022_NoCheckWhenOffOrNotLinux(t *testing.T) {
	cases := []struct {
		goos  string
		allow bool
	}{{"linux", false}, {"darwin", true}, {"windows", true}, {"freebsd", true}}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s allow=%v", tc.goos, tc.allow), func(t *testing.T) {
			rec := &probeRecorder{}
			out := captureStdout(func() { reportShutdownPrivilege(tc.goos, tc.allow, rec.run, time.Second) })
			assert.Empty(t, rec.calls, "[AC-022] the runner gets no sudo call")
			assert.Empty(t, out, "[AC-022] no shutdown pass, fail or section line is printed")
		})
	}
}

// The doctor wiring is checked on the source: the Permissions line is unchanged
// and the report is called once with the real runtime values.
func TestShutdownDoctor_SS_AC022_DoctorWiring(t *testing.T) {
	src, err := os.ReadFile("doctor.go")
	require.NoError(t, err)
	assert.Contains(t, strings.ReplaceAll(string(src), "\r\n", "\n"), "fmt.Printf(\"  Shutdown:     %v\\n\", cfg.Commands.AllowShutdown)\n", "[AC-022] the Permissions line is unchanged")

	var calls []string
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "doctor.go", src, 0)
	require.NoError(t, err)
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "reportShutdownPrivilege" {
				var args []string
				for _, a := range call.Args {
					args = append(args, types.ExprString(a))
				}
				calls = append(calls, strings.Join(args, " | "))
			}
		}
		return true
	})
	assert.Equal(t, []string{"runtime.GOOS | cfg.Commands.AllowShutdown | agentPolicyRunner(os.Geteuid(), execRunner) | shutdownProbeTimeout"}, calls,
		"[AC-022] doctor calls the report once, with the OS, the config value, the riot-aware runner and the timeout")

	maint, err := os.ReadFile("maintenance_doctor.go")
	require.NoError(t, err)
	assert.Contains(t, string(maint), "run := agentPolicyRunner(os.Geteuid(), execRunner)", "[AC-021] the maintenance check uses the shared rewrite")
}

// AC-023: the doctor probe asks about exactly what the shutdown path launches.
// execShutdown is not edited (FR-032), so the guarantee is a build-time check on
// its source.
func TestShutdownDoctor_SS_AC023_ProbeMatchesExecShutdownArgv(t *testing.T) {
	src, err := os.ReadFile("commands.go")
	require.NoError(t, err)
	file, err := parser.ParseFile(token.NewFileSet(), "commands.go", src, 0)
	require.NoError(t, err)

	var execFn *ast.FuncDecl
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "execShutdown" && fd.Recv != nil {
			execFn = fd
		}
	}
	require.NotNil(t, execFn, "[AC-023] execShutdown must exist")

	var sudoLaunches [][]string
	ast.Inspect(execFn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok || !((recv.Name == "a" && sel.Sel.Name == "runner") || (recv.Name == "exec" && sel.Sel.Name == "Command")) {
			return true
		}
		var strs []string
		for _, arg := range call.Args {
			if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				v, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				strs = append(strs, v)
			}
		}
		if len(strs) > 0 && strs[0] == "sudo" {
			sudoLaunches = append(sudoLaunches, strs[1:])
		}
		return true
	})
	require.Len(t, sudoLaunches, 2, "[AC-023] execShutdown launches sudo in exactly two places: the runner seam and the Linux branch")
	for i, argv := range sudoLaunches {
		assert.Equal(t, shutdownSudoCommand(), argv, "[AC-023] launch site %d must run the command the doctor probe asks about", i)
	}
	assert.Equal(t, append([]string{"-n", "-l"}, shutdownSudoCommand()...), shutdownProbeArgs(), "[AC-023] the probe lists that same command, non-interactively")
}

// AC-024 and SC-001: every invocation made in every doctor case is a policy
// listing. None runs the command, and none can prompt.
func TestShutdownDoctor_SS_AC024_OnlyListingInvocations(t *testing.T) {
	rec := &probeRecorder{}
	runners := map[string]func(){
		"pass":            func() { rec.err, rec.blockOnce = nil, false },
		"refused":         func() { rec.err, rec.blockOnce = errors.New("exit status 1"), false },
		"not found":       func() { rec.err, rec.blockOnce = &exec.Error{Name: "sudo", Err: exec.ErrNotFound}, false },
		"timeout":         func() { rec.err, rec.blockOnce = nil, true },
		"off":             func() { rec.err, rec.blockOnce = nil, false },
		"not linux":       func() { rec.err, rec.blockOnce = nil, false },
		"root pass":       func() { rec.err, rec.blockOnce = nil, false },
		"root refused":    func() { rec.err, rec.blockOnce = errors.New("exit status 1"), false },
		"non-root refuse": func() { rec.err, rec.blockOnce = errors.New("exit status 1"), false },
	}
	type scenario struct {
		goos  string
		allow bool
		euid  int
		wait  time.Duration
	}
	scenarios := map[string]scenario{
		"pass": {"linux", true, 1000, time.Second}, "refused": {"linux", true, 1000, time.Second}, "not found": {"linux", true, 1000, time.Second},
		"timeout": {"linux", true, 1000, 20 * time.Millisecond}, "off": {"linux", false, 1000, time.Second}, "not linux": {"darwin", true, 1000, time.Second},
		"root pass": {"linux", true, 0, time.Second}, "root refused": {"linux", true, 0, time.Second}, "non-root refuse": {"linux", true, 1000, time.Second},
	}
	names := make([]string, 0, len(runners))
	for name := range runners {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		runners[name]()
		s := scenarios[name]
		captureStdout(func() { reportShutdownPrivilege(s.goos, s.allow, agentPolicyRunner(s.euid, rec.run), s.wait) })
	}
	require.NotEmpty(t, rec.calls, "[AC-024] the cases must have exercised the runner")
	for _, call := range rec.calls {
		require.Equal(t, "sudo", call[0], "[AC-024] only sudo is ever invoked: %v", call)
		require.Equal(t, "-n", call[1], "[AC-024] sudo never prompts: %v", call)
		list := indexOf(call, "-l")
		require.True(t, list == 2 || (list == 4 && call[2] == "-U" && call[3] == "riot"), "[AC-024] the call is a -l listing of the riot policy: %v", call)
		require.Equal(t, shutdownSudoCommand(), call[list+1:], "[AC-024] what follows -l is only the command to look up: %v", call)
		for _, arg := range call[:list+1] {
			assert.NotContains(t, []string{"systemctl", "reboot", "halt", "shutdown", "poweroff"}, arg, "[AC-024] no power word before the listing flag: %v", call)
		}
	}
}

// isStringLiterals reports whether e is a []string literal of exactly these strings.
func isStringLiterals(e ast.Expr, want ...string) bool {
	lit, ok := e.(*ast.CompositeLit)
	if !ok || types.ExprString(lit.Type) != "[]string" || len(lit.Elts) != len(want) {
		return false
	}
	for i, elt := range lit.Elts {
		bl, ok := elt.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return false
		}
		if v, err := strconv.Unquote(bl.Value); err != nil || v != want[i] {
			return false
		}
	}
	return true
}

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

// SEC-002 / AD-011: shutdownSudoCommand is allowed past the C-4 string rule only
// because nothing but the probe can reach it. This is the rule that keeps it so.
const (
	listingCmdName  = "shutdownSudoCommand"
	listingArgsName = "shutdownProbeArgs"
	listingDocFile  = "shutdown_doctor.go"
	listingReporter = "reportShutdownPrivilege"
)

// listingReferenceProblems lists every way the two listing helpers are used that
// the story does not allow. It looks at the files it is given, so the self-test
// can feed it violations.
func listingReferenceProblems(t *testing.T, files []powerPathFile) []string {
	t.Helper()
	fset := token.NewFileSet()
	refs := map[string]map[string]int{listingCmdName: {}, listingArgsName: {}}
	var problems []string

	for _, f := range files {
		file, err := parser.ParseFile(fset, f.Path, f.Src, 0)
		require.NoError(t, err, "parse %s", f.Path)
		for _, decl := range file.Decls {
			owner := ""
			var root ast.Node = decl
			if fd, ok := decl.(*ast.FuncDecl); ok {
				owner = fd.Name.Name
				if fd.Recv != nil {
					owner = "(method)" + owner
				}
				if fd.Body == nil {
					continue
				}
				root = fd.Body // the declaration's own name is not a reference
			}
			ast.Inspect(root, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && refs[id.Name] != nil {
					refs[id.Name][f.Path+":"+owner]++
				}
				return true
			})
		}

		if f.Path != listingDocFile {
			continue
		}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			switch fd.Name.Name {
			case listingArgsName:
				// return append([]string{"-n", "-l"}, shutdownSudoCommand()...)
				ok := len(fd.Body.List) == 1
				if ok {
					ret, isRet := fd.Body.List[0].(*ast.ReturnStmt)
					ok = isRet && len(ret.Results) == 1
					if ok {
						call, isCall := ret.Results[0].(*ast.CallExpr)
						ok = isCall && types.ExprString(call.Fun) == "append" && call.Ellipsis.IsValid() && len(call.Args) == 2 &&
							isStringLiterals(call.Args[0], "-n", "-l") && types.ExprString(call.Args[1]) == listingCmdName+"()"
					}
				}
				if !ok {
					problems = append(problems, listingArgsName+" must be exactly: return append([]string{\"-n\", \"-l\"}, "+listingCmdName+"()...)")
				}
			case listingReporter:
				probeCalls, joinCalls := 0, 0
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					mentions := func(name string) bool {
						found := false
						for _, a := range call.Args {
							ast.Inspect(a, func(m ast.Node) bool {
								if id, ok := m.(*ast.Ident); ok && id.Name == name {
									found = true
								}
								return true
							})
						}
						return found
					}
					if mentions(listingArgsName) {
						probeCalls++
						if types.ExprString(call.Fun) != "run" || len(call.Args) != 3 || types.ExprString(call.Args[0]) != "ctx" ||
							types.ExprString(call.Args[1]) != `"sudo"` || types.ExprString(call.Args[2]) != listingArgsName+"()" || !call.Ellipsis.IsValid() {
							problems = append(problems, "the probe may only be passed as run(ctx, \"sudo\", "+listingArgsName+"()...), found "+types.ExprString(call))
						}
					}
					if mentions(listingCmdName) {
						joinCalls++
						if types.ExprString(call.Fun) != "strings.Join" || len(call.Args) != 2 || types.ExprString(call.Args[0]) != listingCmdName+"()" || types.ExprString(call.Args[1]) != `" "` {
							problems = append(problems, "the command may only be used as strings.Join("+listingCmdName+"(), \" \"), found "+types.ExprString(call))
						}
					}
					return true
				})
				if probeCalls != 1 {
					problems = append(problems, fmt.Sprintf("%s must pass the probe to the runner exactly once, found %d", listingReporter, probeCalls))
				}
				if joinCalls != 1 {
					problems = append(problems, fmt.Sprintf("%s must join the command for its message exactly once, found %d", listingReporter, joinCalls))
				}
			}
		}
	}

	want := map[string]map[string]int{
		listingCmdName:  {listingDocFile + ":" + listingArgsName: 1, listingDocFile + ":" + listingReporter: 1},
		listingArgsName: {listingDocFile + ":" + listingReporter: 1},
	}
	for name, allowed := range want {
		if fmt.Sprint(refs[name]) != fmt.Sprint(allowed) { // fmt prints maps in key order
			problems = append(problems, fmt.Sprintf("%s may be referenced exactly as %v, found %v", name, allowed, refs[name]))
		}
	}
	sort.Strings(problems)
	return problems
}

func TestShutdownDoctor_SS_C4_ListingCommandOnlyFeedsTheProbe(t *testing.T) {
	files := realPowerPathFiles(t)
	var names []string
	for _, f := range files {
		names = append(names, f.Path)
	}
	require.Contains(t, names, listingDocFile, "[SEC-002] the scan must cover the doctor file")
	require.Contains(t, names, "commands.go", "[SEC-002] the scan must cover the agent package")
	assert.Empty(t, listingReferenceProblems(t, files),
		"[SEC-002] shutdownSudoCommand may be referenced exactly twice and shutdownProbeArgs once, only by the probe")
}

func TestShutdownDoctor_SS_C4_ReferenceRuleSelfTest(t *testing.T) {
	real, err := os.ReadFile(listingDocFile)
	require.NoError(t, err)
	base := string(real)
	doc := func(src string) powerPathFile { return powerPathFile{Path: listingDocFile, Src: []byte(src)} }
	replaced := func(old, repl string) string {
		require.Contains(t, base, old, "the self-test must mutate something that exists")
		return strings.Replace(base, old, repl, 1)
	}

	assert.Empty(t, listingReferenceProblems(t, []powerPathFile{doc(base)}), "the real file satisfies the rule")

	violations := []struct {
		name  string
		files []powerPathFile
	}{
		{"a launch elsewhere in the package", []powerPathFile{doc(base), {Path: "other.go", Src: []byte(`package agent
import "os/exec"
func launch() error { return exec.Command("sudo", shutdownSudoCommand()...).Start() }`)}}},
		{"a package-level use of the command", []powerPathFile{doc(base), {Path: "other.go", Src: []byte(`package agent
var listed = shutdownSudoCommand()`)}}},
		{"another function using the probe", []powerPathFile{doc(base), {Path: "other.go", Src: []byte(`package agent
func ask() { _ = shutdownProbeArgs() }`)}}},
		{"another function in the doctor file using the command", []powerPathFile{doc(base + "\nfunc extra() { _ = shutdownSudoCommand() }\n")}},
		{"a method using the command", []powerPathFile{doc(base), {Path: "other.go", Src: []byte(`package agent
func (a *Agent) ask() { _ = shutdownSudoCommand() }`)}}},
		{"a third use of the command in the reporter", []powerPathFile{doc(replaced("cmd := strings.Join(shutdownSudoCommand(), \" \")", "cmd := strings.Join(shutdownSudoCommand(), \" \")\n\t_ = shutdownSudoCommand()"))}},
		{"the probe passed with an extra argument", []powerPathFile{doc(replaced("run(ctx, \"sudo\", shutdownProbeArgs()...)", "run(ctx, \"sudo\", append(shutdownProbeArgs(), \"x\")...)"))}},
		{"the probe passed without spreading", []powerPathFile{doc(replaced("run(ctx, \"sudo\", shutdownProbeArgs()...)", "run(ctx, \"sudo\", shutdownProbeArgs())"))}},
		{"the probe run through another program", []powerPathFile{doc(replaced("run(ctx, \"sudo\", shutdownProbeArgs()...)", "run(ctx, \"doas\", shutdownProbeArgs()...)"))}},
		{"the probe run by something other than the injected runner", []powerPathFile{doc(replaced("run(ctx, \"sudo\", shutdownProbeArgs()...)", "exec.CommandContext(ctx, \"sudo\", shutdownProbeArgs()...).Run()"))}},
		{"the probe args built without the listing flags", []powerPathFile{doc(replaced("return append([]string{\"-n\", \"-l\"}, shutdownSudoCommand()...)", "return shutdownSudoCommand()"))}},
		{"the probe args built with another prefix", []powerPathFile{doc(replaced("return append([]string{\"-n\", \"-l\"}, shutdownSudoCommand()...)", "return append([]string{\"-n\"}, shutdownSudoCommand()...)"))}},
	}
	for _, tc := range violations {
		t.Run("flags "+tc.name, func(t *testing.T) {
			assert.NotEmpty(t, listingReferenceProblems(t, tc.files), "[SEC-002] %s must be flagged", tc.name)
		})
	}
}

// AC-029 (doctor part): the doctor check writes nothing to disk. The file may
// not import a package that can write or start processes, other than the
// exec error value it compares against.
func TestShutdownDoctor_SS_AC029_DoctorWritesNothing(t *testing.T) {
	src, err := os.ReadFile(listingDocFile)
	require.NoError(t, err)
	file, err := parser.ParseFile(token.NewFileSet(), listingDocFile, src, parser.ImportsOnly)
	require.NoError(t, err)
	var imports []string
	for _, im := range file.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		imports = append(imports, p)
	}
	sort.Strings(imports)
	assert.Equal(t, []string{
		"context", "errors", "fmt", "github.com/DesyncTheThird/rIOt/internal/agent/collectors", "os/exec", "strings", "time",
	}, imports, "[AC-029] the doctor check imports no file-writing API")

	var execUses []string
	body, err := parser.ParseFile(token.NewFileSet(), listingDocFile, src, 0)
	require.NoError(t, err)
	ast.Inspect(body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "exec" {
				execUses = append(execUses, sel.Sel.Name)
			}
		}
		return true
	})
	assert.Equal(t, []string{"ErrNotFound"}, execUses, "[AC-029][SC-001] os/exec is used only to recognise the sudo-not-found error, never to start anything")
}
