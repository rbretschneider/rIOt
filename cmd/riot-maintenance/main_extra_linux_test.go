//go:build linux

package main

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
)

func TestCapabilities_EU_SEC007_FirstAndNoSideEffects(t *testing.T) {
	source, err := filepath.Abs("main_linux.go")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("RIOT_EXTRA_LAUNCH", "")
	var out bytes.Buffer
	if err := runWith([]string{"--capabilities"}, &out); err != nil {
		t.Fatalf("--capabilities must succeed with no root, no lock, no state and no files: %v", err)
	}
	t.Run("[SEC-007] the output is exactly the two documented lines", func(t *testing.T) {
		if got, want := out.String(), "request-versions=1,2\nextra-updaters=dietpi\n"; got != want {
			t.Errorf("output = %q, want %q", got, want)
		}
		if out.Len() > 256 {
			t.Errorf("output is %d bytes, the agent accepts at most 256", out.Len())
		}
	})
	t.Run("[SEC-007] it leaves no file behind", func(t *testing.T) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Errorf("working directory has %v (%v)", entries, err)
		}
	})
	t.Run("[SEC-007] the capabilities branch is the first statement of the argv dispatcher", func(t *testing.T) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, source, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "runWith" {
				continue
			}
			first, ok := fn.Body.List[0].(*ast.IfStmt)
			if !ok {
				t.Fatal("the first statement of runWith is not the capabilities branch")
			}
			var lits []string
			ast.Inspect(first.Cond, func(n ast.Node) bool {
				if b, ok := n.(*ast.BasicLit); ok {
					lits = append(lits, b.Value)
				}
				return true
			})
			if !strings.Contains(strings.Join(lits, " "), `"--capabilities"`) {
				t.Errorf("first branch tests %v, want --capabilities", lits)
			}
			return
		}
		t.Fatal("runWith not found")
	})
	t.Run("[SEC-007] any other argument next to --capabilities is refused", func(t *testing.T) {
		for _, args := range [][]string{{"--capabilities", "x"}, {"--capabilities=1"}, {"-capabilities"}} {
			if err := runWith(args, &bytes.Buffer{}); err == nil {
				t.Errorf("accepted %q", args)
			}
		}
	})
}

func TestRunExtra_EU_SEC007_RefusesWithoutRootFifoParentToken(t *testing.T) {
	for _, args := range [][]string{{maintenance.LauncherArg, "dietpi"}, {maintenance.LauncherArg}, {maintenance.LauncherArg, "snap"}, {maintenance.LauncherArg, "dietpi", "1"}} {
		t.Run("[SEC-007] the production entry refuses "+strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("RIOT_EXTRA_LAUNCH", "1")
			err := runWith(args, &bytes.Buffer{})
			var le launcherExit
			if !errors.As(err, &le) {
				t.Fatalf("err = %v, want a launcher failure (exit %d)", err, launcherExitCode)
			}
			if le.err.Reason != maintenance.ReasonLauncherRefused {
				t.Errorf("reason = %q, want launcher_refused: a test process is not the maintenance helper with a status pipe", le.err.Reason)
			}
		})
	}
	t.Run("[SEC-007] the launcher failure exits 125", func(t *testing.T) {
		if launcherExitCode != 125 {
			t.Errorf("exit code = %d", launcherExitCode)
		}
	})
}

func TestClosedArguments_EU_AC044_WorkerStillTakesNoArguments(t *testing.T) {
	for _, args := range [][]string{{"--run"}, {"dietpi"}, {"--capabilities-extra"}, {"--run-extras", "dietpi"}} {
		if err := runWith(args, &bytes.Buffer{}); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}
