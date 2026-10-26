package agent

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rebootCallers are the only functions allowed to start a reboot (PATCH-FLOW-001
// NFR-001, security review C-7 / INFO-3): nothing automated may reach execReboot
// from a reboot_required signal, and each caller re-checks commands.allow_reboot.
var rebootCallers = []string{"handleReboot", "maybeRebootAfterPatch", "rebootAfterDelivery"}

// execSpec describes one power exec function for the call-site scan
// (SHUTDOWN-DPKG-001 AD-006.1): the function, the gate entry point that must feed
// it, the only functions allowed to call it, and the config flag a caller checks.
type execSpec struct {
	Exec    string
	Gate    string
	Callers []string
	Allow   string
}

var (
	rebootSpec   = execSpec{Exec: "execReboot", Gate: "awaitRebootClearance", Callers: rebootCallers, Allow: "Commands.AllowReboot"}
	shutdownSpec = execSpec{Exec: "execShutdown", Gate: "awaitShutdownClearance", Callers: []string{"handleShutdown"}, Allow: "Commands.AllowShutdown"}
)

// execRef is one reference to a power exec function in non-test source.
// REBOOT-DPKG-001 AD-007 (security review C-3) records every reference, not one
// result per function, so a second unguarded call or a method value cannot hide
// behind a guarded one.
type execRef struct {
	Spec   execSpec
	Pos    string // file:line
	Func   string // enclosing function; "" outside any function
	IsCall bool   // the reference is the Fun of a call expression
	// Guarded: the function body text before the reference contains Spec.Allow
	// (the C-8 check).
	Guarded bool
	// ArgIdent is X of the single argument "X.Permit"; "" for any other shape.
	ArgIdent string
	// GateAssigned: ArgIdent is assigned from a.<Spec.Gate>( earlier in the same
	// function. ClearedChecked: "if !ArgIdent.Cleared" comes earlier too.
	GateAssigned   bool
	ClearedChecked bool
}

// problems lists everything wrong with ref against the AD-007 checks 1 to 4.
func (r execRef) problems() []string {
	var p []string
	if !r.IsCall {
		p = append(p, "is not a direct call (a method value or other reference)")
	}
	if !slices.Contains(r.Spec.Callers, r.Func) {
		p = append(p, fmt.Sprintf("is in %q, not one of the reviewed callers %v", r.Func, r.Spec.Callers))
	}
	if !r.Guarded {
		p = append(p, "has no "+r.Spec.Allow+" check before it")
	}
	if r.ArgIdent == "" {
		p = append(p, "does not pass <clearance>.Permit")
	} else {
		if !r.GateAssigned {
			p = append(p, fmt.Sprintf("passes %s.Permit but %s is not assigned from a.%s earlier", r.ArgIdent, r.ArgIdent, r.Spec.Gate))
		}
		if !r.ClearedChecked {
			p = append(p, fmt.Sprintf("passes %s.Permit without an earlier \"if !%s.Cleared\"", r.ArgIdent, r.ArgIdent))
		}
	}
	return p
}

// scanExecRefs records every reference to spec.Exec in one source file.
func scanExecRefs(t *testing.T, filename string, src []byte, spec execSpec) []execRef {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	require.NoError(t, err, "parse %s", filename)

	calls := map[*ast.SelectorExpr]*ast.CallExpr{}
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == spec.Exec {
				calls[sel] = call
			}
		}
		return true
	})
	enclosing := func(pos token.Pos) *ast.FuncDecl {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil && fn.Body.Pos() <= pos && pos <= fn.Body.End() {
				return fn
			}
		}
		return nil
	}

	var refs []execRef
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != spec.Exec {
			return true
		}
		ref := execRef{Spec: spec, Pos: fmt.Sprintf("%s:%d", filename, fset.Position(sel.Pos()).Line)}
		call := calls[sel]
		ref.IsCall = call != nil
		if fn := enclosing(sel.Pos()); fn != nil {
			ref.Func = fn.Name.Name
			start, at := fset.Position(fn.Body.Pos()).Offset, fset.Position(sel.Pos()).Offset
			ref.Guarded = strings.Contains(string(src[start:at]), spec.Allow)
			if call != nil && len(call.Args) == 1 {
				if arg, ok := call.Args[0].(*ast.SelectorExpr); ok && arg.Sel.Name == "Permit" {
					if id, ok := arg.X.(*ast.Ident); ok {
						ref.ArgIdent = id.Name
						ref.GateAssigned, ref.ClearedChecked = gateEvidence(fn, spec.Gate, id.Name, sel.Pos())
					}
				}
			}
		}
		refs = append(refs, ref)
		return true
	})
	return refs
}

// scanExecRebootRefs records every reference to execReboot in one source file.
func scanExecRebootRefs(t *testing.T, filename string, src []byte) []execRef {
	t.Helper()
	return scanExecRefs(t, filename, src, rebootSpec)
}

// gateEvidence reports whether, before pos in fn, name was assigned from a call
// to the gate entry point and whether "if !name.Cleared" was evaluated.
func gateEvidence(fn *ast.FuncDecl, gate, name string, pos token.Pos) (assigned, checked bool) {
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if s.Pos() >= pos || len(s.Rhs) != 1 {
				return true
			}
			call, ok := s.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != gate {
				return true
			}
			for _, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
					assigned = true
				}
			}
		case *ast.IfStmt:
			if s.Pos() >= pos {
				return true
			}
			not, ok := s.Cond.(*ast.UnaryExpr)
			if !ok || not.Op != token.NOT {
				return true
			}
			if sel, ok := not.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Cleared" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == name {
					checked = true
				}
			}
		}
		return true
	})
	return assigned, checked
}

// agentSourceFiles lists the non-test Go files of this package.
func agentSourceFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	var out []string
	for _, name := range files {
		if !strings.HasSuffix(name, "_test.go") {
			out = append(out, name)
		}
	}
	return out
}

// execRefs lists every reference to spec.Exec in the package's non-test source,
// one entry per reference.
func execRefs(t *testing.T, spec execSpec) []execRef {
	t.Helper()
	var refs []execRef
	for _, name := range agentSourceFiles(t) {
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		refs = append(refs, scanExecRefs(t, name, src, spec)...)
	}
	return refs
}

// execRebootRefs lists every reference to execReboot in the package's non-test
// source, one entry per reference.
func execRebootRefs(t *testing.T) []execRef { t.Helper(); return execRefs(t, rebootSpec) }

func TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites(t *testing.T) {
	refs := execRebootRefs(t)

	seen := map[string]bool{}
	for _, ref := range refs {
		seen[ref.Func] = true
	}
	var callers []string
	for name := range seen {
		callers = append(callers, name)
	}
	sort.Strings(callers)
	want := append([]string(nil), rebootCallers...)
	sort.Strings(want)
	assert.Equal(t, want, callers, "[C-8] execReboot may only be called from the three reviewed functions")
	assert.Len(t, refs, len(rebootCallers), "[C-8] execReboot is referenced exactly once in each of the three reviewed functions")
	for _, ref := range refs {
		assert.True(t, ref.Guarded, "[C-8] %s (%s) must check Commands.AllowReboot before execReboot", ref.Func, ref.Pos)
	}
}

func TestRebootCallSites_RD_AC020_GateClearanceFeedsEveryExecReboot(t *testing.T) {
	refs := execRebootRefs(t)
	require.NotEmpty(t, refs, "[AC-020] the scan must find the execReboot references")

	for _, ref := range refs {
		assert.Empty(t, ref.problems(),
			"[AC-020] %s (%s): every reference to execReboot must be a call that passes the permit of a checked awaitRebootClearance", ref.Func, ref.Pos)
	}
}

func TestRebootCallSites_RD_C3_MethodValueReferenceIsFlagged(t *testing.T) {
	const good = `package agent
func (a *Agent) handleReboot() {
	if !a.config.Commands.AllowReboot { return }
	c := a.awaitRebootClearance()
	if !c.Cleared { return }
	_ = a.execReboot(c.Permit)
}`
	const methodValue = `package agent
func (a *Agent) handleReboot() {
	if !a.config.Commands.AllowReboot { return }
	c := a.awaitRebootClearance()
	if !c.Cleared { return }
	f := a.execReboot
	_ = f(c.Permit)
}`
	const discardedGate = `package agent
func (a *Agent) handleReboot() {
	if !a.config.Commands.AllowReboot { return }
	_ = a.awaitRebootClearance()
	_ = a.execReboot(powerPermit{})
}`
	const clearedNeverChecked = `package agent
func (a *Agent) handleReboot() {
	if !a.config.Commands.AllowReboot { return }
	c := a.awaitRebootClearance()
	_ = a.execReboot(c.Permit)
}`
	const secondCallHidden = `package agent
func (a *Agent) handleReboot() {
	_ = a.execReboot(powerPermit{})
	if !a.config.Commands.AllowReboot { return }
	c := a.awaitRebootClearance()
	if !c.Cleared { return }
	_ = a.execReboot(c.Permit)
}`
	const methodExpression = `package agent
var forced = (*Agent).execReboot`

	t.Run("a well-formed caller has no problems", func(t *testing.T) {
		refs := scanExecRebootRefs(t, "good.go", []byte(good))
		require.Len(t, refs, 1)
		assert.Empty(t, refs[0].problems(), "[C-3] the self-test must accept the real call shape")
	})
	t.Run("a method value is flagged", func(t *testing.T) {
		refs := scanExecRebootRefs(t, "methodvalue.go", []byte(methodValue))
		require.Len(t, refs, 1, "[C-3] the method value must be recorded as a reference")
		assert.False(t, refs[0].IsCall, "[C-3] f := a.execReboot is not a call")
		assert.Contains(t, refs[0].problems(), "is not a direct call (a method value or other reference)", "[C-3] a method value must be flagged")
	})
	t.Run("a method expression outside any function is flagged", func(t *testing.T) {
		refs := scanExecRebootRefs(t, "methodexpr.go", []byte(methodExpression))
		require.Len(t, refs, 1, "[C-3] the method expression must be recorded as a reference")
		assert.NotEmpty(t, refs[0].problems(), "[C-3] a reference outside the three callers must be flagged")
	})
	t.Run("a discarded gate result is flagged", func(t *testing.T) {
		refs := scanExecRebootRefs(t, "discarded.go", []byte(discardedGate))
		require.Len(t, refs, 1)
		assert.Contains(t, refs[0].problems(), "does not pass <clearance>.Permit", "[C-3] an execReboot that does not take the gate's permit must be flagged")
	})
	t.Run("a clearance whose Cleared is never checked is flagged", func(t *testing.T) {
		refs := scanExecRebootRefs(t, "unchecked.go", []byte(clearedNeverChecked))
		require.Len(t, refs, 1)
		assert.Contains(t, refs[0].problems(), `passes c.Permit without an earlier "if !c.Cleared"`, "[C-3] an unchecked clearance must be flagged")
	})
	t.Run("an unguarded call is not hidden by a later guarded one", func(t *testing.T) {
		refs := scanExecRebootRefs(t, "second.go", []byte(secondCallHidden))
		require.Len(t, refs, 2, "[C-3] one entry per reference, not one per function")
		assert.NotEmpty(t, refs[0].problems(), "[C-3] the first, unguarded call must be flagged on its own")
		assert.Empty(t, refs[1].problems(), "[C-3] the second call is well-formed")
	})
}

// ---------------------------------------------------------------------------
// SHUTDOWN-DPKG-001 AD-006.1: execShutdown has one guarded call site
// ---------------------------------------------------------------------------

func TestShutdownCallSites_SD_AC020_ExecShutdownHasExactlyOneGuardedCallSite(t *testing.T) {
	refs := execRefs(t, shutdownSpec)

	require.Len(t, refs, 1, "[AC-020] execShutdown is referenced exactly once in non-test code")
	assert.Equal(t, "handleShutdown", refs[0].Func, "[AC-020] only handleShutdown may power the host off")
	assert.True(t, refs[0].Guarded, "[AC-020] handleShutdown must check Commands.AllowShutdown before execShutdown")
}

func TestShutdownCallSites_SD_AC020_GateClearanceFeedsExecShutdown(t *testing.T) {
	refs := execRefs(t, shutdownSpec)
	require.NotEmpty(t, refs, "[AC-020] the scan must find the execShutdown reference")

	for _, ref := range refs {
		assert.Empty(t, ref.problems(),
			"[AC-020] %s (%s): execShutdown must be a call that passes the permit of a checked awaitShutdownClearance", ref.Func, ref.Pos)
	}
}

func TestShutdownCallSites_SD_C3_SelfTest(t *testing.T) {
	const good = `package agent
func (a *Agent) handleShutdown() {
	if !a.config.Commands.AllowShutdown { return }
	c := a.awaitShutdownClearance()
	if !c.Cleared { return }
	_ = a.execShutdown(c.Permit)
}`
	const wrongGate = `package agent
func (a *Agent) handleShutdown() {
	if !a.config.Commands.AllowShutdown { return }
	c := a.awaitRebootClearance()
	if !c.Cleared { return }
	_ = a.execShutdown(c.Permit)
}`
	const wrongCaller = `package agent
func (a *Agent) handleReboot() {
	if !a.config.Commands.AllowShutdown { return }
	c := a.awaitShutdownClearance()
	if !c.Cleared { return }
	_ = a.execShutdown(c.Permit)
}`
	const noAllowCheck = `package agent
func (a *Agent) handleShutdown() {
	c := a.awaitShutdownClearance()
	if !c.Cleared { return }
	_ = a.execShutdown(c.Permit)
}`
	const forgedPermit = `package agent
func (a *Agent) handleShutdown() {
	if !a.config.Commands.AllowShutdown { return }
	_ = a.execShutdown(powerPermit{})
}`
	const methodValue = `package agent
func (a *Agent) handleShutdown() {
	if !a.config.Commands.AllowShutdown { return }
	f := a.execShutdown
	_ = f
}`
	const crossRebootToShutdown = `package agent
func (a *Agent) handleReboot() {
	if !a.config.Commands.AllowReboot { return }
	c := a.awaitShutdownClearance()
	if !c.Cleared { return }
	_ = a.execReboot(c.Permit)
}`

	shutdown := func(src string) []execRef { return scanExecRefs(t, "x.go", []byte(src), shutdownSpec) }
	t.Run("a well-formed caller has no problems", func(t *testing.T) {
		refs := shutdown(good)
		require.Len(t, refs, 1)
		assert.Empty(t, refs[0].problems(), "[C-3] the real call shape must be accepted")
	})
	t.Run("a reboot clearance fed to execShutdown is flagged", func(t *testing.T) {
		refs := shutdown(wrongGate)
		require.Len(t, refs, 1)
		assert.NotEmpty(t, refs[0].problems(), "[C-3] a clearance from the reboot gate must not feed execShutdown")
	})
	t.Run("a call from another function is flagged", func(t *testing.T) {
		refs := shutdown(wrongCaller)
		require.Len(t, refs, 1)
		assert.NotEmpty(t, refs[0].problems(), "[C-3] only handleShutdown may call execShutdown")
	})
	t.Run("a call without the allow check is flagged", func(t *testing.T) {
		refs := shutdown(noAllowCheck)
		require.Len(t, refs, 1)
		assert.NotEmpty(t, refs[0].problems(), "[C-3] the allow_shutdown check must come before the call")
	})
	t.Run("a forged permit is flagged", func(t *testing.T) {
		refs := shutdown(forgedPermit)
		require.Len(t, refs, 1)
		assert.NotEmpty(t, refs[0].problems(), "[C-3] a permit that is not <clearance>.Permit must be flagged")
	})
	t.Run("a method value is flagged", func(t *testing.T) {
		refs := shutdown(methodValue)
		require.Len(t, refs, 1)
		assert.NotEmpty(t, refs[0].problems(), "[C-3] a method value must be flagged")
	})
	t.Run("a shutdown clearance fed to execReboot is flagged", func(t *testing.T) {
		refs := scanExecRefs(t, "x.go", []byte(crossRebootToShutdown), rebootSpec)
		require.Len(t, refs, 1)
		assert.NotEmpty(t, refs[0].problems(), "[C-3] a clearance from the shutdown gate must not feed execReboot")
	})
}

// ---------------------------------------------------------------------------
// SHUTDOWN-DPKG-001 AD-006.2: permit and gate binding
// ---------------------------------------------------------------------------

// powerBindingProblems applies the AD-006.2 rules to a set of non-test sources
// (file name -> source) and returns one line per violation, each starting with
// "rule N".
func powerBindingProblems(t *testing.T, sources map[string][]byte) []string {
	t.Helper()
	fset := token.NewFileSet()

	var problems []string
	flag := func(rule int, pos token.Pos, format string, args ...any) {
		problems = append(problems, fmt.Sprintf("rule %d: %s: %s", rule, fset.Position(pos), fmt.Sprintf(format, args...)))
	}
	calledName := func(call *ast.CallExpr) string {
		switch f := call.Fun.(type) {
		case *ast.SelectorExpr:
			return f.Sel.Name
		case *ast.Ident:
			return f.Name
		}
		return ""
	}
	isCallTo := func(e ast.Expr, name string) bool {
		call, ok := e.(*ast.CallExpr)
		return ok && calledName(call) == name
	}
	isIdent := func(e ast.Expr, name string) bool {
		id, ok := e.(*ast.Ident)
		return ok && id.Name == name
	}

	var (
		literals, mints, gates []string // owners, for the exact-count rules
		gateCalls              = map[string]*ast.CallExpr{}
		consumeCalls           = map[string][]*ast.CallExpr{}
	)
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		file, err := parser.ParseFile(fset, name, sources[name], 0)
		require.NoError(t, err, "parse %s", name)
		for _, decl := range file.Decls {
			owner := "" // package-level declarations have no enclosing function
			if fn, ok := decl.(*ast.FuncDecl); ok {
				owner = fn.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CompositeLit:
					if id, ok := x.Type.(*ast.Ident); ok {
						switch id.Name {
						case "powerPermit":
							literals = append(literals, owner)
							if owner != "mintPowerPermit" {
								flag(1, x.Pos(), "a powerPermit literal in %q, outside mintPowerPermit", owner)
							}
						case "powerGate":
							gates = append(gates, owner)
							if owner != "rebootGate" && owner != "shutdownGate" {
								flag(7, x.Pos(), "a powerGate literal in %q, outside rebootGate and shutdownGate", owner)
							}
						}
					}
				case *ast.SelectorExpr:
					switch x.Sel.Name {
					case "permitID", "permitAction":
						if owner != "consumePowerPermit" {
							flag(3, x.Pos(), "%s is selected in %q, outside consumePowerPermit", x.Sel.Name, owner)
						}
					case "powerPermitSeq", "powerPermitLive":
						if owner != "mintPowerPermit" && owner != "consumePowerPermit" {
							flag(4, x.Pos(), "%s is selected in %q, outside mintPowerPermit and consumePowerPermit", x.Sel.Name, owner)
						}
					}
				case *ast.CallExpr:
					switch calledName(x) {
					case "mintPowerPermit":
						mints = append(mints, owner)
						if owner != "awaitPowerClearance" {
							flag(2, x.Pos(), "mintPowerPermit is called in %q, outside awaitPowerClearance", owner)
						}
					case "consumePowerPermit":
						consumeCalls[owner] = append(consumeCalls[owner], x)
					case "awaitPowerClearance":
						gateCalls[owner] = x
						if owner != "awaitRebootClearance" && owner != "awaitShutdownClearance" {
							flag(6, x.Pos(), "awaitPowerClearance is called in %q, outside the two gate entry points", owner)
						}
					case "awaitRebootClearance":
						if len(x.Args) != 4 || !isCallTo(x.Args[3], "commonRebootGuards") {
							flag(8, x.Pos(), "awaitRebootClearance in %q must pass a.commonRebootGuards(...) as its guards", owner)
						}
					case "commonPowerGuards":
						if owner != "commonRebootGuards" && owner != "commonShutdownGuards" {
							flag(9, x.Pos(), "commonPowerGuards is called in %q, outside commonRebootGuards and commonShutdownGuards", owner)
						}
					}
				}
				return true
			})
		}
	}

	if len(literals) != 1 {
		flag(1, 1, "want exactly one powerPermit literal, found %d (%v)", len(literals), literals)
	}
	if len(mints) != 1 {
		flag(2, 1, "want exactly one mintPowerPermit call, found %d (%v)", len(mints), mints)
	}
	if len(gates) != 2 {
		flag(7, 1, "want exactly two powerGate literals, found %d (%v)", len(gates), gates)
	}

	// Rule 5: consumePowerPermit is called exactly twice, each with its own action.
	total := 0
	for _, calls := range consumeCalls {
		total += len(calls)
	}
	if total != 2 {
		flag(5, 1, "want exactly two consumePowerPermit calls, found %d", total)
	}
	for owner, want := range map[string]string{"execReboot": "powerActionReboot", "execShutdown": "powerActionShutdown"} {
		calls := consumeCalls[owner]
		if len(calls) != 1 {
			flag(5, 1, "%s must call consumePowerPermit exactly once, found %d", owner, len(calls))
			continue
		}
		if len(calls[0].Args) != 2 || !isIdent(calls[0].Args[1], want) {
			flag(5, calls[0].Pos(), "%s must call consumePowerPermit(p, %s)", owner, want)
		}
	}
	for owner := range consumeCalls {
		if owner != "execReboot" && owner != "execShutdown" {
			flag(5, consumeCalls[owner][0].Pos(), "consumePowerPermit is called in %q, outside the two exec functions", owner)
		}
	}

	// Rule 6: each entry point calls the gate with its own spec (and shutdown with its own guards).
	if len(gateCalls) != 2 {
		flag(6, 1, "want awaitPowerClearance called by awaitRebootClearance and awaitShutdownClearance only, found %d callers", len(gateCalls))
	}
	if call, ok := gateCalls["awaitRebootClearance"]; ok && (len(call.Args) != 5 || !isCallTo(call.Args[1], "rebootGate")) {
		flag(6, call.Pos(), "awaitRebootClearance must pass rebootGate() to the gate")
	}
	if call, ok := gateCalls["awaitShutdownClearance"]; ok {
		if len(call.Args) != 5 || !isCallTo(call.Args[1], "shutdownGate") {
			flag(6, call.Pos(), "awaitShutdownClearance must pass shutdownGate() to the gate")
		} else if !isCallTo(call.Args[4], "commonShutdownGuards") {
			flag(6, call.Pos(), "awaitShutdownClearance must pass a.commonShutdownGuards() as its guards")
		}
	}
	return problems
}

func agentSources(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, name := range agentSourceFiles(t) {
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		out[name] = src
	}
	return out
}

func TestPowerCallSites_SD_C3_GateEntryPointsBindTheAction(t *testing.T) {
	problems := powerBindingProblems(t, agentSources(t))

	assert.Empty(t, problems, "[C-3] the permit, the gate entry points and the guards must be bound as AD-006.2 requires")
}

func TestPowerCallSites_SD_C3_SelfTest(t *testing.T) {
	baseline := agentSources(t)
	require.Empty(t, powerBindingProblems(t, baseline), "precondition: the real sources satisfy every rule")

	with := func(extra string) map[string][]byte {
		m := map[string][]byte{}
		for k, v := range baseline {
			m[k] = v
		}
		m["synthetic.go"] = []byte("package agent\n" + extra)
		return m
	}
	mutated := func(file, from, to string) map[string][]byte {
		m := map[string][]byte{}
		for k, v := range baseline {
			m[k] = v
		}
		src := string(baseline[file])
		require.Contains(t, src, from, "the mutation target must exist in %s", file)
		m[file] = []byte(strings.Replace(src, from, to, 1))
		return m
	}

	cases := []struct {
		name    string
		sources map[string][]byte
		rule    string
	}{
		{"a permit id assigned from the live slot in a third function",
			with("func (a *Agent) evil() { var p powerPermit; p.permitID = a.powerPermitLive.Load(); _ = p }"), "rule 3"},
		{"the same, which also touches the live slot",
			with("func (a *Agent) evil() { var p powerPermit; p.permitID = a.powerPermitLive.Load(); _ = p }"), "rule 4"},
		{"a permit action assigned after a bare declaration",
			with("func (a *Agent) evil() { var p powerPermit; p.permitAction = powerActionShutdown; _ = a.execShutdown(p) }"), "rule 3"},
		{"a permit sequence bumped outside mint",
			with("func (a *Agent) evil() { a.powerPermitSeq.Add(1) }"), "rule 4"},
		{"a keyed permit literal outside mint",
			with("func (a *Agent) evil() powerPermit { return powerPermit{permitID: 1, permitAction: powerActionShutdown} }"), "rule 1"},
		{"a gate literal outside the two entry points",
			with("func (a *Agent) evil() powerGate { return powerGate{action: powerActionShutdown} }"), "rule 7"},
		{"awaitRebootClearance with nil guards",
			with(`func (a *Agent) evil(ctx context.Context) { _ = a.awaitRebootClearance(ctx, "x", "id", nil) }`), "rule 8"},
		{"awaitRebootClearance with the shutdown guards",
			with(`func (a *Agent) evil(ctx context.Context) { _ = a.awaitRebootClearance(ctx, "x", "id", a.commonShutdownGuards()) }`), "rule 8"},
		{"commonPowerGuards called from a shutdown helper",
			with(`func (a *Agent) handleShutdown2() { _ = a.commonPowerGuards(func() bool { return a.config.Commands.AllowReboot }, "x", "") }`), "rule 9"},
		{"awaitPowerClearance called from a third function",
			with(`func (a *Agent) evil(ctx context.Context) { _ = a.awaitPowerClearance(ctx, shutdownGate(), "x", "id", nil) }`), "rule 6"},
		{"awaitShutdownClearance passing rebootGate",
			mutated("reboot_clearance.go", `awaitPowerClearance(ctx, shutdownGate(), "shutdown"`, `awaitPowerClearance(ctx, rebootGate(), "shutdown"`), "rule 6"},
		{"awaitShutdownClearance passing the reboot guards",
			mutated("reboot_clearance.go", `"shutdown", commandID, a.commonShutdownGuards())`, `"shutdown", commandID, a.commonRebootGuards(""))`), "rule 6"},
		{"execShutdown consuming for the wrong action",
			mutated("commands.go", `a.consumePowerPermit(p, powerActionShutdown)`, `a.consumePowerPermit(p, powerActionReboot)`), "rule 5"},
		{"a second mint call",
			with("func (a *Agent) evil() { _ = a.mintPowerPermit(powerActionShutdown) }"), "rule 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := powerBindingProblems(t, tc.sources)
			assert.NotEmpty(t, problems, "[C-3] the violation must be flagged")
			found := false
			for _, p := range problems {
				found = found || strings.HasPrefix(p, tc.rule)
			}
			assert.True(t, found, "[C-3] %s must be flagged by %s, got %v", tc.name, tc.rule, problems)
		})
	}
}
