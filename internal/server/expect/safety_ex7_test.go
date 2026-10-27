package expect_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-007 AC-037 (NFR-001, ADD AD-015): no server test, helper or source of this
// story can run a real power action, a package manager, the updater or riot-agent,
// start a process, or import the agent. It is a static scan of Go sources; nothing is
// executed. It reuses the EXPECT-002 scan (ex2ScanSource), whose patterns are
// assembled from fragments, so this file needs no exception either.
//
// The agent's own tests are scanned by their own rules in internal/agent (power and
// package-manager words are data there, execution forms are forbidden); this scan
// covers everything outside internal/agent.
//
// The file set is the union of three parts, so a file added later is picked up by
// itself:
//
//	(a) every Go file of ADD section 4 outside internal/agent, asserted by name;
//	(b) every *_test.go outside internal/agent that contains the story marker;
//	(c) every *_test.go in a package directory this story adds files to
//	    (non-recursive), the EXPECT-002 part (c) directories.

var (
	ex7Marker = "TestEX7" + "_"

	// ex7MustScan is every Go file of ADD section 4 outside internal/agent.
	ex7MustScan = []string{
		"internal/models/agent_activity.go",
		"internal/models/agent_activity_test.go",
		"internal/models/events.go",
		"internal/models/self_update.go",
		"internal/server/db/self_update_repo.go",
		"internal/server/db/interfaces.go",
		"internal/server/db/self_update_attempt_pg_test.go",
		"internal/testutil/mocks.go",
		"internal/server/commands/self_update.go",
		"internal/server/commands/self_update_ref_test.go",
		"internal/server/handlers/agent_event_self_update.go",
		"internal/server/handlers/handlers.go",
		"internal/server/handlers/agent_event_ref_test.go",
		"internal/server/expect/activity.go",
		"internal/server/expect/harness_test.go",
		"internal/server/expect/expect7_test.go",
		"internal/server/expect/safety_ex7_test.go",
		"internal/server/expect7_docs_test.go",
	}

	// ex7Preexisting lists the hits that predate this story, in files the scan
	// covers: the EXPECT-002 table, which the same files already pass. No file of
	// this story may appear in it or in ex2Preexisting (security condition C-5).
	ex7Preexisting = append([]ex2Exception(nil), ex2Preexisting...)
)

// ex7IsStoryFile reports whether a repository-relative path is a file this story
// created: the ones the exception tables must never name.
func ex7IsStoryFile(rel string) bool {
	for _, f := range ex7MustScan {
		if rel == f && (strings.Contains(f, "ex7") || strings.Contains(f, "expect7") || strings.Contains(f, "agent_activity") ||
			strings.Contains(f, "self_update_ref") || strings.Contains(f, "self_update_attempt") || strings.Contains(f, "agent_event_ref")) {
			return true
		}
	}
	return false
}

func ex7ScanSet(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, rel := range ex7MustScan {
		seen[rel] = true
	}
	for _, rel := range ex2ScanSetPartC(t, root) { // part (c), the EXPECT-002 directories
		seen[rel] = true
	}
	skip := map[string]bool{".git": true, ".claude": true, "node_modules": true, "vendor": true}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
		if strings.HasPrefix(rel, "internal/agent/") {
			return nil // the agent's own scan covers these
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), ex7Marker) { // part (b)
			seen[rel] = true
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

// ex2ScanSetPartC returns the test files of the EXPECT-002 part (c) directories.
func ex2ScanSetPartC(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, dir := range ex2PackageDirs {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		require.NoError(t, err, "read %s", dir)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
				out = append(out, dir+"/"+e.Name())
			}
		}
	}
	return out
}

func TestEX7_AC037_NoPowerUpdateOrAgentExecution(t *testing.T) {
	root := repoRoot(t)
	files := ex7ScanSet(t, root)

	// QA reads this list to confirm that every new or modified Go file of the
	// story outside the agent is in the scan (run with -v).
	t.Logf("AC-037 (Go, server side) scanned %d files:\n  %s", len(files), strings.Join(files, "\n  "))

	scanned := map[string]bool{}
	for _, f := range files {
		scanned[f] = true
	}
	for _, must := range ex7MustScan {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(must)))
		require.NoError(t, err, "[C-5] the named file %s must exist", must)
		assert.True(t, scanned[must], "[C-5] the scan set must include %s", must)
	}

	// No file of this story may be excepted by name, here or in the EXPECT-002 table.
	for _, e := range append(append([]ex2Exception(nil), ex7Preexisting...), ex2Preexisting...) {
		assert.False(t, ex7IsStoryFile(e.file), "[C-5] %s is a file of this story and must not appear in an exception table", e.file)
	}

	var violations []string
	used := make([]bool, len(ex7Preexisting))
	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		require.NoError(t, err, "read %s", rel)
		hits, err := ex2ScanSource(rel, src)
		require.NoError(t, err)
		for _, v := range hits {
			if i, ok := ex2Excepted(v, ex7Preexisting); ok {
				used[i] = true
				continue
			}
			violations = append(violations, v)
		}
	}
	assert.Empty(t, violations, "[AC-037] no test, helper or source may run a power action, a package manager, the updater or riot-agent:\n%s", strings.Join(violations, "\n"))
	for i, e := range ex7Preexisting {
		assert.NotEmpty(t, e.reason, "every exception carries a reason: %s %s", e.file, e.pattern)
		if scanned[e.file] { // an entry for a file outside this scan is the EXPECT-002 scan's to keep
			assert.True(t, used[i], "[C-5] stale exception: %s no longer matches %s", e.file, e.pattern)
		}
	}
}

// Every form the scan exists to catch is flagged, and prose and the story's own
// words are not; each source is parsed, never compiled or run.
func TestEX7_AC037_ScannerCatchesEachForbiddenForm_Server(t *testing.T) {
	lock := "dp" + "kg"
	cases := map[string]string{
		"os/" + "exec import":      "package p\nimport \"os/" + "exec\"\nvar _ = exec.ErrNotFound\n",
		"exec.Command call":        "package p\nfunc f() { exec.Command(\"ls\") }\n",
		"os.Start" + "Process":     "package p\nimport \"os\"\nfunc f() { os.Start" + "Process(\"/bin/x\", nil, nil) }\n",
		"the agent package import": "package p\nimport \"github.com/DesyncTheThird/rIOt/internal/" + "agent\"\nvar _ = agent.X\n",
		"x/sys/unix import":        "package p\nimport \"golang.org/x/" + "sys/unix\"\nvar _ = unix.Getpid\n",
		"the package lock":         "package p\nvar s = \"wait_" + lock + "_lock\"\n",
		"the package manager":      "package p\nvar s = \"apt-" + "get install x\"\n",
		"the root update helper":   "package p\nvar s = \"riot-agent-" + "apply\"\n",
		"the transient unit tool":  "package p\nvar s = \"systemd-" + "run --scope x\"\n",
		"a service manager verb":   "package p\nvar s = \"system" + "ctl reboot\"\n",
		"the DietPi updater":       "package p\nvar s = \"dietpi-" + "update\"\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ex2ScanSource("sample.go", []byte(src))
			require.NoError(t, err)
			assert.NotEmpty(t, got, "[AC-037] the scan must flag: %s", name)
		})
	}
	clean := "package p\nvar a = \"Rebooting (waiting)\"\nvar b = \"agent_update_started\"\nvar c = \"power_action\"\nvar d = \"attempt_id\"\n"
	got, err := ex2ScanSource("clean.go", []byte(clean))
	require.NoError(t, err)
	assert.Empty(t, got, "the story's own words are not violations: %v", got)
}
