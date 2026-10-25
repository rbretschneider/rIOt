package agent

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SHUTDOWN-DPKG-001 AD-010 (security condition C-1). Nothing in this file calls a
// power exec: the proof is the guard's state plus an AST check that every real
// branch consults it first.

// powerExecFuncs are the functions whose real-process branch must be guarded.
var powerExecFuncs = []string{"execReboot", "execShutdown"}

func TestPowerGuard_SD_C1_TestBinaryIsDetected(t *testing.T) {
	assert.True(t, inTestBinary(), "[C-1] a go test binary must be detected, or the power guard is dead code")
	assert.Error(t, errPowerProcessInTest, "[C-1] the guard error must exist")
}

// powerGuardProblems checks funcName in src against the AD-010 rules and returns
// every violation.
func powerGuardProblems(t *testing.T, filename string, src []byte, funcName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	require.NoError(t, err, "parse %s", filename)

	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == funcName {
			fn = f
		}
	}
	if fn == nil || fn.Body == nil {
		return []string{funcName + " not found"}
	}

	isGuardCall := func(e ast.Expr) bool {
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return false
		}
		id, ok := call.Fun.(*ast.Ident)
		return ok && id.Name == "inTestBinary"
	}
	endsInReturn := func(b *ast.BlockStmt) bool {
		if len(b.List) == 0 {
			return false
		}
		_, ok := b.List[len(b.List)-1].(*ast.ReturnStmt)
		return ok
	}

	var guards []*ast.IfStmt
	var execs []*ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.IfStmt:
			if isGuardCall(x.Cond) && endsInReturn(x.Body) {
				guards = append(guards, x)
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
					execs = append(execs, x)
				}
			}
		}
		return true
	})

	var problems []string
	if len(execs) == 0 {
		problems = append(problems, "has no exec.Command call, so the check would be vacuous")
	}
	if len(guards) != 1 {
		return append(problems, fmt.Sprintf("has %d `if inTestBinary() { ... return }` statements, want exactly 1", len(guards)))
	}
	guard := guards[0]

	// The guard must be a direct statement of a block that contains every launch.
	var host *ast.BlockStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if b, ok := n.(*ast.BlockStmt); ok {
			for _, s := range b.List {
				if s == ast.Stmt(guard) {
					host = b
				}
			}
		}
		return true
	})
	if host == nil {
		return append(problems, "the inTestBinary guard is not a direct statement of a block")
	}
	for _, c := range execs {
		line := fset.Position(c.Pos()).Line
		switch {
		case c.Pos() < guard.End():
			problems = append(problems, fmt.Sprintf("the exec call on line %d is not after the guard", line))
		case c.Pos() < host.Pos() || c.End() > host.End():
			problems = append(problems, fmt.Sprintf("the exec call on line %d is outside the block that holds the guard", line))
		}
	}
	return problems
}

// inTestBinaryProblems checks that src declares inTestBinary as a function whose
// body is exactly `return testing.Testing()`, and not as a variable a test could
// reassign.
func inTestBinaryProblems(t *testing.T, src []byte) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "powerguard.go", src, 0)
	require.NoError(t, err)
	var problems []string
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Name.Name == "inTestBinary" {
				fn = x
			}
		case *ast.GenDecl:
			for _, sp := range x.Specs {
				if vs, ok := sp.(*ast.ValueSpec); ok {
					for _, n := range vs.Names {
						if n.Name == "inTestBinary" {
							problems = append(problems, "inTestBinary is a variable")
						}
					}
				}
			}
		}
	}
	if fn == nil {
		return append(problems, "inTestBinary is not a function")
	}
	if len(fn.Body.List) != 1 {
		return append(problems, "inTestBinary has more than one statement")
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return append(problems, "inTestBinary does not return one value")
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok {
		return append(problems, "inTestBinary does not return a call")
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return append(problems, "inTestBinary does not call testing.Testing")
	}
	if pkg, _ := sel.X.(*ast.Ident); pkg == nil || pkg.Name != "testing" || sel.Sel.Name != "Testing" {
		problems = append(problems, "inTestBinary does not call testing.Testing")
	}
	return problems
}

func TestPowerGuard_SD_C1_RealBranchesConsultTheGuardFirst(t *testing.T) {
	for _, name := range powerExecFuncs {
		found := false
		for _, file := range agentSourceFiles(t) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)
			if !strings.Contains(string(src), "func (a *Agent) "+name+"(") {
				continue
			}
			found = true
			assert.Empty(t, powerGuardProblems(t, file, src, name),
				"[C-1] %s must return before any exec.Command when inTestBinary()", name)
		}
		assert.True(t, found, "[C-1] %s must exist in the package", name)
	}

	src, err := os.ReadFile("powerguard.go")
	require.NoError(t, err)
	assert.Empty(t, inTestBinaryProblems(t, src), "[C-1] inTestBinary must be exactly `func inTestBinary() bool { return testing.Testing() }`")
}

func TestPowerGuard_SD_C1_SelfTest(t *testing.T) {
	const good = `package agent
func (a *Agent) execReboot() error {
	if a.runner != nil { return nil }
	if inTestBinary() { return errPowerProcessInTest }
	cmd := exec.Command("sudo", "systemctl", "reboot")
	return cmd.Start()
}`
	const nested = `package agent
func (a *Agent) execReboot() error {
	run := func() error {
		if inTestBinary() { return errPowerProcessInTest }
		return exec.Command("sudo", "x").Start()
	}
	return run()
}`
	const execBeforeGuard = `package agent
func (a *Agent) execReboot() error {
	cmd := exec.Command("sudo", "systemctl", "reboot")
	if inTestBinary() { return errPowerProcessInTest }
	return cmd.Start()
}`
	const siblingBranch = `package agent
func (a *Agent) execReboot() error {
	if a.x {
		if inTestBinary() { return errPowerProcessInTest }
	} else {
		return exec.Command("sudo", "x").Start()
	}
	return nil
}`
	const noReturn = `package agent
func (a *Agent) execReboot() error {
	if inTestBinary() { println("in test") }
	return exec.Command("sudo", "x").Start()
}`
	const noGuard = `package agent
func (a *Agent) execReboot() error {
	return exec.Command("sudo", "x").Start()
}`
	const twoGuards = `package agent
func (a *Agent) execReboot() error {
	if inTestBinary() { return errPowerProcessInTest }
	if inTestBinary() { return errPowerProcessInTest }
	return exec.Command("sudo", "x").Start()
}`

	check := func(src string) []string { return powerGuardProblems(t, "synthetic.go", []byte(src), "execReboot") }
	assert.Empty(t, check(good), "[C-1] the real shape must pass")
	assert.Empty(t, check(nested), "[C-1] a guard inside a func literal that holds the launch must pass")
	assert.NotEmpty(t, check(execBeforeGuard), "[C-1] an exec.Command before the guard must be flagged")
	assert.NotEmpty(t, check(siblingBranch), "[C-1] a guard in a sibling branch must be flagged")
	assert.NotEmpty(t, check(noReturn), "[C-1] a guard whose body does not return must be flagged")
	assert.NotEmpty(t, check(noGuard), "[C-1] a missing guard must be flagged")
	assert.NotEmpty(t, check(twoGuards), "[C-1] more than one guard must be flagged")

	// inTestBinary as a variable would let a test reassign it; any other body would
	// make the guard depend on something else.
	const variable = `package agent
var inTestBinary = func() bool { return testing.Testing() }`
	const otherBody = `package agent
func inTestBinary() bool { return false }`
	const realShape = `package agent
func inTestBinary() bool { return testing.Testing() }`
	assert.Empty(t, inTestBinaryProblems(t, []byte(realShape)), "[C-1] the real shape must pass")
	assert.NotEmpty(t, inTestBinaryProblems(t, []byte(variable)), "[C-1] a variable inTestBinary must be flagged")
	assert.NotEmpty(t, inTestBinaryProblems(t, []byte(otherBody)), "[C-1] a body other than testing.Testing() must be flagged")
}
