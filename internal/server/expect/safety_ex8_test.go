package expect_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-008 AC-026 (NFR-001, ADD AD-018, inherited EXPECT-002 C-1): no test,
// helper or source of this story's Go part can run a real power action, a
// package manager or the agent updater, start a process, or execute an installed
// riot-agent. It is a static scan of Go sources; nothing is executed. It reuses
// the EXPECT-002 scanner (ex2ScanSource) and its forbidden set, and builds its
// own file set from three parts:
//
//	(a) every Go file of ADD section 4.1, asserted by name;
//	(b) every *_test.go of the repository whose source contains the story
//	    marker "TestEX8_";
//	(c) every *_test.go in a package directory this story adds files to
//	    (non-recursive).
//
// The patterns are assembled from fragments so that this file does not trip its
// own scan.

var (
	ex8Marker = "TestEX8" + "_"

	// ex8MustScan is every Go file of ADD section 4.1 that is new or modified,
	// plus the helper files the story adds beyond that list.
	ex8MustScan = []string{
		"internal/models/reinstall.go",
		"internal/models/reinstall_test.go",
		"internal/models/command_policy.go",
		"internal/models/command_state.go",
		"internal/models/expect.go",
		"internal/models/events.go",
		"internal/server/db/reinstall_repo.go",
		"internal/server/db/reinstall_repo_pg_test.go",
		"internal/server/db/reinstall_parity_pg_test.go",
		"internal/server/db/command_repo.go",
		"internal/server/db/self_update_repo.go",
		"internal/server/db/interfaces.go",
		"internal/testutil/mocks.go",
		"internal/server/commands/reinstall.go",
		"internal/server/commands/reinstall_test.go",
		"internal/server/commands/service.go",
		"internal/server/commands/sweeper.go",
		"internal/server/expect/reinstall.go",
		"internal/server/expect/reinstall_test.go",
		"internal/server/expect/safety_ex8_test.go",
		"internal/server/expect/window.go",
		"internal/server/expect/finish.go",
		"internal/server/expect/tracker.go",
		"internal/server/expect/tick.go",
		"internal/server/expect/escalate.go",
		"internal/server/expect/activity.go",
		"internal/server/expect/selfupdate.go",
		"internal/server/expect/harness_test.go",
		"internal/server/handlers/agent_event_reinstall.go",
		"internal/server/handlers/agent_event_reinstall_test.go",
		"internal/server/handlers/handlers.go",
		"internal/server/server.go",
	}

	// ex8Preexisting lists the hits that predate this story, in files the scan
	// covers. The story's package directories are the EXPECT-002 ones, so the
	// reviewed EXPECT-002 table applies unchanged; this story adds no entry. A hit
	// that is not listed fails the test, and so does a listed entry that no
	// longer matches anything (a stale exception).
	ex8Preexisting = ex2Preexisting
)

// ex8ScanSet computes the files to scan: parts (a), (b) and (c). The EXPECT-002 walk
// already collects every *_test.go of the story's package directories (part c)
// and of the repository that carries a story marker; this story's directories are
// the same, and a file carrying only the EX8 marker is added here (part b).
func ex8ScanSet(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, rel := range ex8MustScan {
		seen[rel] = true
	}
	for _, rel := range ex2ScanSet(t, root) {
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
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), ex8Marker) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			seen[filepath.ToSlash(rel)] = true
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

func TestEX8_AC026_NoPowerUpdateOrAgentExecution(t *testing.T) {
	root := repoRoot(t)
	files := ex8ScanSet(t, root)

	// QA reads this list to confirm that every new or modified Go file of the
	// story is in the scan (run with -v).
	t.Logf("AC-026 (Go) scanned %d files:\n  %s", len(files), strings.Join(files, "\n  "))

	scanned := map[string]bool{}
	for _, f := range files {
		scanned[f] = true
	}
	for _, must := range ex8MustScan {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(must)))
		require.NoError(t, err, "[C-1] the named file %s must exist", must)
		assert.True(t, scanned[must], "[C-1] the scan set must include %s", must)
	}

	var violations []string
	used := make([]bool, len(ex8Preexisting))
	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		require.NoError(t, err, "read %s", rel)
		hits, err := ex2ScanSource(rel, src)
		require.NoError(t, err)
		for _, v := range hits {
			if i, ok := ex2Excepted(v, ex8Preexisting); ok {
				used[i] = true
				continue
			}
			violations = append(violations, v)
		}
	}
	assert.Empty(t, violations, "[AC-026] no test, helper or source may run a power action, a package manager, the updater or riot-agent:\n%s", strings.Join(violations, "\n"))
	for i, e := range ex8Preexisting {
		assert.NotEmpty(t, e.reason, "every exception carries a reason: %s %s", e.file, e.pattern)
		assert.True(t, used[i], "[C-1] stale exception: %s no longer matches %s", e.file, e.pattern)
	}
}

// The scanner this story relies on really catches each call shape; each source
// is parsed, never compiled or run. The story's own words are not violations.
func TestEX8_AC026_ScannerCatchesEachForbiddenForm(t *testing.T) {
	cases := map[string]string{
		"os/" + "exec import":      "package p\nimport \"os/" + "exec\"\nvar _ = exec.ErrNotFound\n",
		"exec.Command call":        "package p\nfunc f() { exec.Command(\"ls\") }\n",
		"exec.CommandContext call": "package p\nfunc f() { exec.CommandContext(nil, \"ls\") }\n",
		"os.Start" + "Process":     "package p\nimport \"os\"\nfunc f() { os.Start" + "Process(\"/bin/x\", nil, nil) }\n",
		"sys" + "call import":      "package p\nimport \"sys" + "call\"\nvar _ = syscall.SIGTERM\n",
		"agent package import":     "package p\nimport \"github.com/DesyncTheThird/rIOt/internal/" + "agent\"\nvar _ = agent.X\n",
		"power command":            "package p\nvar s = \"power" + "off\"\n",
		"service manager":          "package p\nvar s = \"system" + "ctl stop riot-agent\"\n",
		"agent binary by path":     "package p\nvar s = \"/usr/local/bin/riot-" + "agent\"\n",
		"apt-" + "get literal":     "package p\nvar s = \"apt-" + "get install x\"\n",
		"dp" + "kg literal":        "package p\nvar s = \"dp" + "kg --configure -a\"\n",
		"root apply helper":        "package p\nvar s = \"riot-agent-" + "apply\"\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ex2ScanSource("sample.go", []byte(src))
			require.NoError(t, err)
			assert.NotEmpty(t, got, "[C-1] the scan must flag: %s", name)
		})
	}
	clean := "package p\nimport \"fmt\"\nvar a = \"Agent reinstall by installer\"\nvar b = \"Reinstalling agent\"\nvar c = \"agent_reinstall_started\"\nfunc f() { fmt.Println(a, b, c) }\n"
	got, err := ex2ScanSource("clean.go", []byte(clean))
	require.NoError(t, err)
	assert.Empty(t, got, "the story's own words are not violations: %v", got)
}
