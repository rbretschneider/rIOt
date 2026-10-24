package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

// execRebootCallSites lists, per enclosing function, where execReboot() is called
// in non-test source, and whether the function checks Commands.AllowReboot
// before that call.
func execRebootCallSites(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	sites := map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		file, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "execReboot" {
					return true
				}
				body := string(src[fset.Position(fn.Body.Pos()).Offset:fset.Position(call.Pos()).Offset])
				sites[fn.Name.Name] = strings.Contains(body, "Commands.AllowReboot")
				return true
			})
		}
	}
	return sites
}

func TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites(t *testing.T) {
	sites := execRebootCallSites(t)

	var callers []string
	for name := range sites {
		callers = append(callers, name)
	}
	sort.Strings(callers)
	want := append([]string(nil), rebootCallers...)
	sort.Strings(want)
	assert.Equal(t, want, callers, "[C-8] execReboot may only be called from the three reviewed functions")
	for name, guarded := range sites {
		assert.True(t, guarded, "[C-8] %s must check Commands.AllowReboot before execReboot", name)
	}
}
