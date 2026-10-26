package expect_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC-047 (NFR-001, ADD 12 note 9, C-8): no test and no helper of this story can
// run a real power action or execute an installed riot-agent. This is a static
// scan of the Go sources; nothing is executed.
//
// The files scanned are computed, never listed, so a test file added later is
// picked up by itself:
//   - every .go file under internal/server/expect/;
//   - internal/server/offline*.go;
//   - every *_test.go file of the repository whose source contains the marker
//     "TestEX_" (the prefix every test of this story carries).
//
// It fails on an import of os/exec or syscall, a call to exec.Command (or
// exec.CommandContext), and a string literal that names a power command or runs
// the agent binary. The patterns are assembled from fragments so that this file
// does not trip its own scan.

var (
	storyMarker = "TestEX" + "_"

	forbiddenImports = []string{"os/" + "exec", "sys" + "call"}

	// forbiddenLiterals are matched against the unquoted value of every string
	// literal.
	forbiddenLiterals = []*regexp.Regexp{
		regexp.MustCompile(`system` + `ctl`),
		regexp.MustCompile(`power` + `off`),
		regexp.MustCompile(`reboot` + ` -`),
		regexp.MustCompile(`shut` + `down -`),
		regexp.MustCompile(`\bha` + `lt\b`),
		regexp.MustCompile(`riot-agent(\s+exec|\s*[/\\])|[/\\]riot-agent`),
	}
)

// scanSource returns the violations of one Go source, as "file: what".
func scanSource(name string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	var out []string
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("import in %s: %w", name, err)
		}
		for _, bad := range forbiddenImports {
			if path == bad {
				out = append(out, fmt.Sprintf("%s: imports %s", name, path))
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "exec" && strings.HasPrefix(sel.Sel.Name, "Command") {
					out = append(out, fmt.Sprintf("%s: calls exec.%s", name, sel.Sel.Name))
				}
			}
		case *ast.BasicLit:
			if v.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(v.Value)
			if err != nil {
				return true
			}
			for _, re := range forbiddenLiterals {
				if re.MatchString(s) {
					out = append(out, fmt.Sprintf("%s: string literal %q matches %s", name, abbreviate(s), re))
				}
			}
		}
		return true
	})
	return out, nil
}

func abbreviate(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

// repoRoot finds the module root from the test's working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.mod not found above the test directory")
		dir = parent
	}
}

// scanSet computes the files to scan, as slash-separated paths relative to root.
func scanSet(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	skip := map[string]bool{".git": true, ".claude": true, "node_modules": true, "vendor": true}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case strings.HasPrefix(rel, "internal/server/expect/"):
			seen[rel] = true
		case strings.HasPrefix(rel, "internal/server/offline") && !strings.Contains(strings.TrimPrefix(rel, "internal/server/"), "/"):
			seen[rel] = true
		case strings.HasSuffix(rel, "_test.go"):
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(src), storyMarker) {
				seen[rel] = true
			}
		}
		return nil
	})
	require.NoError(t, err)
	files := make([]string, 0, len(seen))
	for f := range seen {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

func TestEX_AC047_NoPowerOrAgentExecution(t *testing.T) {
	root := repoRoot(t)
	files := scanSet(t, root)

	// QA reads this list to confirm that every new Go test file of the story is in
	// the scan (run with -v).
	t.Logf("AC-047 scanned %d Go files:\n  %s", len(files), strings.Join(files, "\n  "))

	for _, must := range []string{
		"internal/server/expect/tracker.go",
		"internal/server/expect/harness_test.go",
		"internal/server/expect/safety_test.go",
		"internal/server/offline.go",
		"internal/server/offline_test.go",
		"internal/server/events/generator_expect_test.go",
		"internal/server/handlers/activity_test.go",
		"internal/server/handlers/fleet_actor_test.go",
		"internal/server/handlers/docker_attribution_keys_test.go",
		"internal/server/commands/observer_test.go",
		"internal/server/db/expect_repo_pg_test.go",
		"internal/server/storage/command_budget_expect_pg_test.go",
		"internal/models/expect_test.go",
	} {
		assert.Contains(t, files, must, "[AC-047] the scan set must include %s", must)
	}

	var violations []string
	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		require.NoError(t, err, "read %s", rel)
		v, err := scanSource(rel, src)
		require.NoError(t, err)
		violations = append(violations, v...)
	}
	assert.Empty(t, violations, "[AC-047] no test or helper may run a real power action or execute riot-agent:\n%s", strings.Join(violations, "\n"))
}

// The scanner itself must catch what it is meant to catch: each source below is
// parsed, never compiled or run.
func TestEX_AC047_ScannerCatchesEachForbiddenForm(t *testing.T) {
	cases := map[string]string{
		"os/" + "exec import":      "package p\nimport \"os/" + "exec\"\nvar _ = exec.ErrNotFound\n",
		"sys" + "call import":      "package p\nimport \"sys" + "call\"\nvar _ = syscall.SIGTERM\n",
		"exec.Command call":        "package p\nimport e \"os/" + "exec\"\nfunc f() { exec.Command(\"ls\") }\n",
		"exec.CommandContext call": "package p\nfunc f() { exec.CommandContext(nil, \"ls\") }\n",
		"service manager":          "package p\nvar s = \"system" + "ctl restart x\"\n",
		"power command":            "package p\nvar s = \"power" + "off\"\n",
		"reboot with a flag":       "package p\nvar s = \"reboot" + " -f\"\n",
		"shutdown with a flag":     "package p\nvar s = \"shut" + "down -h now\"\n",
		"a stop word":              "package p\nvar s = \"ha" + "lt\"\n",
		"agent binary by path":     "package p\nvar s = \"/usr/local/bin/riot-" + "agent\"\n",
		"agent binary with exec":   "package p\nvar s = \"riot-" + "agent exec\"\n",
		"raw string literal":       "package p\nvar s = `system" + "ctl`\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := scanSource("sample.go", []byte(src))
			require.NoError(t, err)
			assert.NotEmpty(t, got, "[AC-047] the scan must flag: %s", name)
		})
	}
	clean := "package p\nimport \"fmt\"\nvar a = \"Reboot\"\nvar b = \"reboot\"\nvar c = \"Shutdown by ryan\"\nvar d = \"asphalt\"\nvar e = \"riot-agent\"\nfunc f() { fmt.Println(a, b, c, d, e) }\n"
	got, err := scanSource("clean.go", []byte(clean))
	require.NoError(t, err)
	assert.Empty(t, got, "action names and prose are not violations")
}
