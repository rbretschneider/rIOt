package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Static guards for TIME-12H-001 (ADD AD-014). They parse non-test Go source
// with go/parser, so comments never match and a string that merely contains
// "Local" is not mistaken for code.

// minGuardedFiles is the floor for how many files a guard must have scanned, so
// a wrong root cannot turn a guard into a silent no-op.
const minGuardedFiles = 100

// goSourceFiles lists the non-test .go files under the given roots (relative to
// internal/server, where go test runs).
func goSourceFiles(t *testing.T, roots ...string) []string {
	t.Helper()
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				files = append(files, path)
			}
			return nil
		})
		require.NoError(t, err, "walking %s", root)
	}
	sort.Strings(files)
	return files
}

func parseGoFile(t *testing.T, path string, src any) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	require.NoError(t, err, "parsing %s", path)
	return fset, file
}

func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return strings.Trim(lit.Value, "`\""), true
}

// processZoneUses reports every read of the process-wide local zone in src:
// a call to a method named Local with no arguments (t.Local()) or the selector
// time.Local.
func processZoneUses(fset *token.FileSet, file *ast.File) []string {
	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Local" && len(x.Args) == 0 {
				found = append(found, fset.Position(x.Pos()).String()+": .Local()")
			}
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "time" && x.Sel.Name == "Local" {
				found = append(found, fset.Position(x.Pos()).String()+": time.Local")
			}
		}
		return true
	})
	return found
}

// humanClockLayouts reports every 24-hour clock layout literal in src, and every
// literal ending in " UTC" passed to Format: the forms human text must no
// longer use.
func humanClockLayouts(fset *token.FileSet, file *ast.File) []string {
	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if s, ok := stringLiteral(x); ok && strings.Contains(s, "15:04") {
				found = append(found, fset.Position(x.Pos()).String()+": literal "+x.Value)
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Format" {
				return true
			}
			for _, arg := range x.Args {
				if s, ok := stringLiteral(arg); ok && strings.HasSuffix(s, " UTC") {
					found = append(found, fset.Position(arg.Pos()).String()+": Format layout ending in UTC")
				}
			}
		}
		return true
	})
	return found
}

func TestTIME12H_AC049_NoProcessLocalZoneInServerText(t *testing.T) {
	files := goSourceFiles(t, "../models", ".")
	t.Logf("scanned %d files for .Local() and time.Local", len(files))
	require.Greater(t, len(files), minGuardedFiles, "the guard must scan the server and model trees")

	for _, path := range files {
		fset, file := parseGoFile(t, path, nil)
		for _, use := range processZoneUses(fset, file) {
			t.Errorf("[FR-007] server text must use the injected display zone, never the process zone: %s", use)
		}
	}
}

func TestTIME12H_OQ001_NoTwentyFourHourHumanLayouts(t *testing.T) {
	files := goSourceFiles(t, "../models", ".")
	t.Logf("scanned %d files for 24-hour human layouts", len(files))
	require.Greater(t, len(files), minGuardedFiles, "the guard must scan the server and model trees")

	for _, path := range files {
		fset, file := parseGoFile(t, path, nil)
		for _, use := range humanClockLayouts(fset, file) {
			t.Errorf("[OQ-001] human text must use models.FormatDisplay* (12-hour, labelled): %s", use)
		}
	}
}

func TestTIME12H_OQ001_GuardCatchesForbiddenForms(t *testing.T) {
	const src = `package x

import "time"

func f(t time.Time) {
	_ = t.Format("2006-01-02 15:04")
	_ = t.Format("2006-01-02 3:04 PM UTC")
	_ = t.Local()
	_ = time.Local
	// t.Local() and "15:04" in a comment are not code
	_ = "Local() is just text"
}
`
	fset, file := parseGoFile(t, "synthetic.go", src)

	zone := processZoneUses(fset, file)
	assert.Len(t, zone, 2, "both t.Local() and time.Local are caught, the comment and the string are not: %v", zone)

	layouts := humanClockLayouts(fset, file)
	assert.Len(t, layouts, 2, "the 15:04 layout and the layout ending in UTC are caught: %v", layouts)
}

func TestTIME12H_OQ001_GuardAllowsTheDisplayHelpers(t *testing.T) {
	const src = `package x

import "time"

func f(t time.Time) string {
	return t.Format("2006-01-02 3:04:05 PM")
}
`
	fset, file := parseGoFile(t, "synthetic.go", src)
	assert.Empty(t, humanClockLayouts(fset, file), "the 12-hour layouts used by models/display_time.go are allowed")
	assert.Empty(t, processZoneUses(fset, file))
}

// C-4 gate 3: ResolveDisplayZone has exactly one non-test call, inside
// Server.Start. It must not be reachable from setupRouter, which
// applyTLSAndRestart re-runs, or the log bound of one INFO and two WARN lines
// per process start would not hold.
func TestTIME12H_C4_ResolveDisplayZoneCalledOnlyFromStart(t *testing.T) {
	files := goSourceFiles(t, "../../cmd", "../../internal")
	t.Logf("scanned %d files for ResolveDisplayZone calls", len(files))
	require.Greater(t, len(files), minGuardedFiles, "the guard must scan cmd and internal")

	type call struct{ file, in string }
	var calls []call
	for _, path := range files {
		_, file := parseGoFile(t, path, nil)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch f := c.Fun.(type) {
				case *ast.Ident:
					name = f.Name
				case *ast.SelectorExpr:
					name = f.Sel.Name
				}
				if name == "ResolveDisplayZone" {
					calls = append(calls, call{file: filepath.Base(path), in: fn.Name.Name})
				}
				return true
			})
		}
	}

	require.Len(t, calls, 1, "ResolveDisplayZone must have exactly one non-test call, got %v", calls)
	assert.Equal(t, call{file: "server.go", in: "Start"}, calls[0], "the single call must be in Server.Start")
}
