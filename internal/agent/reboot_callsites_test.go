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

// execRebootRef is one reference to execReboot in non-test source. REBOOT-DPKG-001
// AD-007 (security review C-3) records every reference, not one result per
// function, so a second unguarded call or a method value cannot hide behind a
// guarded one.
type execRebootRef struct {
	Pos    string // file:line
	Func   string // enclosing function; "" outside any function
	IsCall bool   // the reference is the Fun of a call expression
	// Guarded: the function body text before the reference contains
	// Commands.AllowReboot (the C-8 check).
	Guarded bool
	// ArgIdent is X of the single argument "X.Permit"; "" for any other shape.
	ArgIdent string
	// GateAssigned: ArgIdent is assigned from a.awaitRebootClearance( earlier in
	// the same function. ClearedChecked: "if !ArgIdent.Cleared" comes earlier too.
	GateAssigned   bool
	ClearedChecked bool
}

// problems lists everything wrong with ref against the AD-007 checks 1 to 4.
func (r execRebootRef) problems() []string {
	var p []string
	if !r.IsCall {
		p = append(p, "is not a direct call (a method value or other reference)")
	}
	if !slices.Contains(rebootCallers, r.Func) {
		p = append(p, fmt.Sprintf("is in %q, not one of the three reviewed callers", r.Func))
	}
	if !r.Guarded {
		p = append(p, "has no Commands.AllowReboot check before it")
	}
	if r.ArgIdent == "" {
		p = append(p, "does not pass <clearance>.Permit")
	} else {
		if !r.GateAssigned {
			p = append(p, fmt.Sprintf("passes %s.Permit but %s is not assigned from a.awaitRebootClearance earlier", r.ArgIdent, r.ArgIdent))
		}
		if !r.ClearedChecked {
			p = append(p, fmt.Sprintf("passes %s.Permit without an earlier \"if !%s.Cleared\"", r.ArgIdent, r.ArgIdent))
		}
	}
	return p
}

// scanExecRebootRefs records every reference to execReboot in one source file.
func scanExecRebootRefs(t *testing.T, filename string, src []byte) []execRebootRef {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	require.NoError(t, err, "parse %s", filename)

	calls := map[*ast.SelectorExpr]*ast.CallExpr{}
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "execReboot" {
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

	var refs []execRebootRef
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "execReboot" {
			return true
		}
		ref := execRebootRef{Pos: fmt.Sprintf("%s:%d", filename, fset.Position(sel.Pos()).Line)}
		call := calls[sel]
		ref.IsCall = call != nil
		if fn := enclosing(sel.Pos()); fn != nil {
			ref.Func = fn.Name.Name
			start, at := fset.Position(fn.Body.Pos()).Offset, fset.Position(sel.Pos()).Offset
			ref.Guarded = strings.Contains(string(src[start:at]), "Commands.AllowReboot")
			if call != nil && len(call.Args) == 1 {
				if arg, ok := call.Args[0].(*ast.SelectorExpr); ok && arg.Sel.Name == "Permit" {
					if id, ok := arg.X.(*ast.Ident); ok {
						ref.ArgIdent = id.Name
						ref.GateAssigned, ref.ClearedChecked = gateEvidence(fn, id.Name, sel.Pos())
					}
				}
			}
		}
		refs = append(refs, ref)
		return true
	})
	return refs
}

// gateEvidence reports whether, before pos in fn, name was assigned from a call
// to awaitRebootClearance and whether "if !name.Cleared" was evaluated.
func gateEvidence(fn *ast.FuncDecl, name string, pos token.Pos) (assigned, checked bool) {
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
			if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "awaitRebootClearance" {
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

// execRebootRefs lists every reference to execReboot in the package's non-test
// source, one entry per reference.
func execRebootRefs(t *testing.T) []execRebootRef {
	t.Helper()
	var refs []execRebootRef
	for _, name := range agentSourceFiles(t) {
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		refs = append(refs, scanExecRebootRefs(t, name, src)...)
	}
	return refs
}

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
	_ = a.execReboot(rebootPermit{})
}`
	const clearedNeverChecked = `package agent
func (a *Agent) handleReboot() {
	if !a.config.Commands.AllowReboot { return }
	c := a.awaitRebootClearance()
	_ = a.execReboot(c.Permit)
}`
	const secondCallHidden = `package agent
func (a *Agent) handleReboot() {
	_ = a.execReboot(rebootPermit{})
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

func TestRebootCallSites_RD_C3_PermitMintedOnlyByGate(t *testing.T) {
	fset := token.NewFileSet()
	literals, mints := 0, 0
	for _, name := range agentSourceFiles(t) {
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		file, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err, "parse %s", name)
		for _, decl := range file.Decls {
			owner := "" // package-level declarations have no enclosing function
			if fn, ok := decl.(*ast.FuncDecl); ok {
				owner = fn.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CompositeLit:
					if id, ok := x.Type.(*ast.Ident); ok && id.Name == "rebootPermit" {
						literals++
						assert.Equal(t, "mintRebootPermit", owner, "[C-3] %s builds a rebootPermit outside mintRebootPermit", fset.Position(x.Pos()))
					}
				case *ast.CallExpr:
					called := ""
					switch f := x.Fun.(type) {
					case *ast.SelectorExpr:
						called = f.Sel.Name
					case *ast.Ident:
						called = f.Name
					}
					if called == "mintRebootPermit" {
						mints++
						assert.Equal(t, "awaitRebootClearance", owner, "[C-3] %s mints a permit outside the gate", fset.Position(x.Pos()))
					}
				}
				return true
			})
		}
	}
	assert.Equal(t, 1, literals, "[C-3] exactly one rebootPermit literal, inside mintRebootPermit")
	assert.Equal(t, 1, mints, "[C-3] exactly one mintRebootPermit call, inside awaitRebootClearance")
}

func TestRebootCallSites_RD_AC020_ShutdownHasNoGate(t *testing.T) {
	// Source inspection only: handleShutdown runs a real shutdown and is never called by a test.
	src, err := os.ReadFile("commands.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "commands.go", src, 0)
	require.NoError(t, err)

	var shutdown *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "handleShutdown" {
			shutdown = fn
		}
	}
	require.NotNil(t, shutdown, "[AC-020] handleShutdown must exist")

	forbidden := map[string]bool{
		"awaitRebootClearance": true, "probeDpkg": true, "execReboot": true,
		"mintRebootPermit": true, "dpkgLockProbe": true, "commonRebootGuards": true,
	}
	ast.Inspect(shutdown.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			assert.False(t, forbidden[x.Name], "[AC-020] handleShutdown must not use %s: shutdown gets no dpkg wait", x.Name)
		case *ast.SelectorExpr:
			assert.False(t, forbidden[x.Sel.Name], "[AC-020] handleShutdown must not use %s: shutdown gets no dpkg wait", x.Sel.Name)
		}
		return true
	})
}
