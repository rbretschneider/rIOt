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

// EXPECT-002 AC-029 (NFR-001, ADD AD-014, security condition C-1): no test, no
// helper and no source of this story can run a real power action, a package
// manager or the agent updater, start a process, or execute an installed
// riot-agent. It is a static scan of Go sources; nothing is executed.
//
// The files scanned are the union of three parts, so a file added later is
// picked up by itself:
//
//	(a) every Go file of ADD section 4, asserted by name;
//	(b) every *_test.go of the repository whose source contains the story
//	    marker "TestEX2_";
//	(c) every *_test.go in a package directory this story adds files to
//	    (non-recursive).
//
// The EXPECT-001 patterns (safety_test.go) apply, plus: imports of os/exec,
// syscall, golang.org/x/sys/unix, golang.org/x/sys/windows and the agent
// package; a call to exec.Command, exec.CommandContext or os.StartProcess; and
// string literals that name a package manager, the DietPi updater, the root
// apply helper or systemd-run. The patterns are assembled from fragments so
// that this file does not trip its own scan.

var (
	ex2Marker = "TestEX2" + "_"

	// ex2ForbiddenImports are forbidden in addition to forbiddenImports.
	ex2ForbiddenImports = []string{
		"golang.org/x/" + "sys/unix",
		"golang.org/x/" + "sys/windows",
		"github.com/DesyncTheThird/rIOt/internal/" + "agent",
	}

	// ex2ForbiddenLiterals are matched against the unquoted value of every string
	// literal, in addition to forbiddenLiterals.
	ex2ForbiddenLiterals = []*regexp.Regexp{
		regexp.MustCompile(`apt` + `-get`),
		regexp.MustCompile(`\bapt` + ` `),
		regexp.MustCompile(`dp` + `kg`),
		regexp.MustCompile(`dietpi-` + `update`),
		regexp.MustCompile(`riot-agent-` + `apply`),
		regexp.MustCompile(`systemd-` + `run`),
	}

	// ex2PackageDirs are the package directories this story adds files to; every
	// *_test.go directly in one of them is scanned (part c).
	ex2PackageDirs = []string{
		"internal/models",
		"internal/server/db",
		"internal/server/commands",
		"internal/server/expect",
		"internal/server/handlers",
		"internal/server",
	}

	// ex2MustScan is every Go file of ADD section 4 (outside web/), plus the
	// helper files this story adds beyond that list. A test asserts that each one
	// exists and is in the scan set.
	ex2MustScan = []string{
		"internal/models/self_update.go",
		"internal/models/self_update_test.go",
		"internal/models/command_policy.go",
		"internal/models/command_state.go",
		"internal/models/expect.go",
		"internal/models/events.go",
		"internal/server/db/self_update_repo.go",
		"internal/server/db/self_update_repo_pg_test.go",
		"internal/server/db/self_update_parity_pg_test.go",
		"internal/server/db/command_repo.go",
		"internal/server/db/event_repo.go",
		"internal/server/db/interfaces.go",
		"internal/testutil/mocks.go",
		"internal/server/commands/self_update.go",
		"internal/server/commands/self_update_test.go",
		"internal/server/commands/service.go",
		"internal/server/commands/sweeper.go",
		"internal/server/expect/selfupdate.go",
		"internal/server/expect/selfupdate_test.go",
		"internal/server/expect/safety_ex2_test.go",
		"internal/server/expect/harness_test.go",
		"internal/server/expect/window.go",
		"internal/server/expect/tracker.go",
		"internal/server/expect/finish.go",
		"internal/server/expect/tick.go",
		"internal/server/expect/escalate.go",
		"internal/server/expect/activity.go",
		"internal/server/handlers/handlers.go",
		"internal/server/handlers/agent_event_self_update.go",
		"internal/server/handlers/agent_event_self_update_test.go",
		"internal/server/handlers/command_events.go",
		"internal/server/handlers/command_events_test.go",
		"internal/server/router.go",
		"internal/server/server.go",
		"internal/server/expect2_docs_test.go",
		"internal/server/expect2_register_test.go",
	}

	// ex2Preexisting lists the hits that predate this story, in files the scan
	// covers. Each entry is keyed by file plus pattern and carries a one-line
	// reason. A hit that is not listed fails the test, and so does a listed entry
	// that no longer matches anything (a stale exception).
	ex2Preexisting = []ex2Exception{
		{"internal/models/expect.go", dpkgPat, "StepWaitDpkgLock is the wait-step token EXPECT-001 reserved: a data constant, never a command"},
		{"internal/server/commands/observer_test.go", dpkgPat, "an EXPECT-001 observer test reports the wait step token as data"},
		{"internal/server/commands/sweeper_test.go", dpkgPat, "CMD-LIFECYCLE sweeper test names, in prose, a reboot that waits for the lock"},
		{"internal/server/expect/activity_test.go", dpkgPat, "EXPECT-001 label tests name the wait step in prose and assert its label"},
		{"internal/server/expect/host_test.go", dpkgPat, "an EXPECT-001 host test names the wait in a message"},
		{"internal/server/handlers/activity_test.go", dpkgPat, "the activity DTO test asserts the waiting_ field name"},
		{"internal/server/handlers/fleet_patch_badge_test.go", dpkgPat, "a patch-badge fixture carries a failure reason text"},
		{"internal/server/handlers/patch_badge_test.go", aptPat, "a patch-badge fixture carries a failure reason text"},
		{"internal/server/handlers/extra_updaters_server_test.go", aptPat, "an EXTRA-UPD-001 test name says what the updater does after the first step"},
		{"internal/server/handlers/reg_harden_qa_test.go", agentImportPat, "REG-HARDEN-001 QA drives the agent's HTTP registration client against an in-process handler; it starts no process and runs no update"},
		{"internal/server/server.go", syscallPat, "graceful shutdown registers for the termination signal (syscall.SIGTERM); nothing is sent or run"},
	}
)

type ex2Exception struct {
	file, pattern, reason string
}

// ex2ScanSource returns the violations of one Go source: the EXPECT-001 scan plus
// the extra EXPECT-002 patterns, as "file: what".
func ex2ScanSource(name string, src []byte) ([]string, error) {
	out, err := scanSource(name, src)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("import in %s: %w", name, err)
		}
		for _, bad := range ex2ForbiddenImports {
			if path == bad || strings.HasPrefix(path, bad+"/") {
				out = append(out, fmt.Sprintf("%s: imports %s", name, path))
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "os" && sel.Sel.Name == "Start"+"Process" {
					out = append(out, fmt.Sprintf("%s: calls os.%s", name, sel.Sel.Name))
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
			for _, re := range ex2ForbiddenLiterals {
				if re.MatchString(s) {
					out = append(out, fmt.Sprintf("%s: string literal %q matches %s", name, abbreviate(s), re))
				}
			}
		}
		return true
	})
	return out, nil
}

// ex2ScanSet computes the files to scan, as slash-separated paths relative to
// root: parts (a), (b) and (c).
func ex2ScanSet(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, rel := range ex2MustScan {
		seen[rel] = true
	}
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
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), ex2Marker) { // part (b)
			seen[rel] = true
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		for _, pkg := range ex2PackageDirs { // part (c), non-recursive
			if dir == pkg {
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

// ex2Excepted reports whether a violation is a listed pre-existing hit, and which
// entry covers it.
func ex2Excepted(v string, table []ex2Exception) (int, bool) {
	for i, e := range table {
		if strings.HasPrefix(v, e.file+": ") && (strings.Contains(v, "matches "+e.pattern) || strings.Contains(v, e.pattern)) {
			return i, true
		}
	}
	return 0, false
}

func TestEX2_AC029_NoPowerUpdateOrAgentExecution(t *testing.T) {
	root := repoRoot(t)
	files := ex2ScanSet(t, root)

	// QA reads this list to confirm that every new or modified Go file of the
	// story is in the scan (run with -v).
	t.Logf("AC-029 (Go) scanned %d files:\n  %s", len(files), strings.Join(files, "\n  "))

	scanned := map[string]bool{}
	for _, f := range files {
		scanned[f] = true
	}
	for _, must := range ex2MustScan {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(must)))
		require.NoError(t, err, "[C-1] the named file %s must exist", must)
		assert.True(t, scanned[must], "[C-1] the scan set must include %s", must)
	}

	var violations []string
	used := make([]bool, len(ex2Preexisting))
	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		require.NoError(t, err, "read %s", rel)
		hits, err := ex2ScanSource(rel, src)
		require.NoError(t, err)
		for _, v := range hits {
			if i, ok := ex2Excepted(v, ex2Preexisting); ok {
				used[i] = true
				continue
			}
			violations = append(violations, v)
		}
	}
	assert.Empty(t, violations, "[AC-029] no test, helper or source may run a power action, a package manager, the updater or riot-agent:\n%s", strings.Join(violations, "\n"))
	for i, e := range ex2Preexisting {
		assert.NotEmpty(t, e.reason, "every exception carries a reason: %s %s", e.file, e.pattern)
		assert.True(t, used[i], "[C-1] stale exception: %s no longer matches %s", e.file, e.pattern)
	}
}

// Every pattern of the EX2 scan really catches the call shape it is meant to
// catch; each source is parsed, never compiled or run.
func TestEX2_AC029_ScannerCatchesEachForbiddenForm(t *testing.T) {
	cases := map[string]string{
		"os/" + "exec import":          "package p\nimport \"os/" + "exec\"\nvar _ = exec.ErrNotFound\n",
		"sys" + "call import":          "package p\nimport \"sys" + "call\"\nvar _ = syscall.SIGTERM\n",
		"exec.Command call":            "package p\nfunc f() { exec.Command(\"ls\") }\n",
		"exec.CommandContext call":     "package p\nfunc f() { exec.CommandContext(nil, \"ls\") }\n",
		"os.Start" + "Process call":    "package p\nimport \"os\"\nfunc f() { os.Start" + "Process(\"/bin/x\", nil, nil) }\n",
		"x/sys/unix import":            "package p\nimport \"golang.org/x/" + "sys/unix\"\nvar _ = unix.Getpid\n",
		"x/sys/unix subpackage":        "package p\nimport \"golang.org/x/" + "sys/unix/sub\"\n",
		"x/sys/windows import":         "package p\nimport \"golang.org/x/" + "sys/windows\"\nvar _ = windows.GetCurrentProcessId\n",
		"agent package import":         "package p\nimport \"github.com/DesyncTheThird/rIOt/internal/" + "agent\"\nvar _ = agent.X\n",
		"agent subpackage import":      "package p\nimport \"github.com/DesyncTheThird/rIOt/internal/" + "agent/sub\"\n",
		"service manager":              "package p\nvar s = \"system" + "ctl restart x\"\n",
		"power command":                "package p\nvar s = \"power" + "off\"\n",
		"agent binary by path":         "package p\nvar s = \"/usr/local/bin/riot-" + "agent\"\n",
		"apt-" + "get literal":         "package p\nvar s = \"apt-" + "get install x\"\n",
		"package manager with a space": "package p\nvar s = \"apt" + " update\"\n",
		"dp" + "kg literal":            "package p\nvar s = \"dp" + "kg --configure -a\"\n",
		"dietpi updater":               "package p\nvar s = \"/boot/dietpi/dietpi-" + "update\"\n",
		"root apply helper":            "package p\nvar s = \"riot-agent-" + "apply\"\n",
		"systemd-" + "run literal":     "package p\nvar s = \"systemd-" + "run --scope x\"\n",
		"raw string with a pkg tool":   "package p\nvar s = `apt-" + "get`\n",
		"exec behind a local variable": "package p\nfunc f() { exec.Command(\"x\") }\nvar _ = 1\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ex2ScanSource("sample.go", []byte(src))
			require.NoError(t, err)
			assert.NotEmpty(t, got, "[C-1] the scan must flag: %s", name)
		})
	}
	// Words that merely contain a pattern are not violations.
	clean := "package p\nimport \"fmt\"\nvar a = \"adapt the agent\"\nvar b = \"Agent update 2.78.0\"\nvar c = \"capture \"\nvar d = \"riot-agent\"\nvar e = \"agent update started\"\nfunc f() { fmt.Println(a, b, c, d, e) }\n"
	got, err := ex2ScanSource("clean.go", []byte(clean))
	require.NoError(t, err)
	assert.Empty(t, got, "prose and the story's own words are not violations: %v", got)

	// The exception table matches by file plus pattern, and only that.
	table := []ex2Exception{{"a/b.go", "dp" + "kg", "reason"}}
	_, ok := ex2Excepted("a/b.go: string literal \"wait_dp"+"kg_lock\" matches dp"+"kg", table)
	assert.True(t, ok, "a listed hit is excepted")
	_, ok = ex2Excepted("a/c.go: string literal \"wait_dp"+"kg_lock\" matches dp"+"kg", table)
	assert.False(t, ok, "the same hit in another file is not")
	_, ok = ex2Excepted("a/b.go: string literal \"apt-"+"get\" matches apt"+"-get", table)
	assert.False(t, ok, "another pattern in the same file is not")
}

// Pattern strings of the exception table, assembled from fragments like the
// scan's own patterns.
const (
	dpkgPat        = "dp" + "kg"
	aptPat         = `\bapt` + ` `
	agentImportPat = "github.com/DesyncTheThird/rIOt/internal/" + "agent"
	syscallPat     = "sys" + "call"
)
