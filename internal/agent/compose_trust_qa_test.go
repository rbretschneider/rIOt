package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Tests added by QA Engineer (AUTOUPD-HARDEN-001). Each names the AC or standing
// requirement it covers. They close gaps found by mutation testing: a mutant
// that removed the guard under test survived the existing suite.

// ---------------------------------------------------------------------------
// AC-013: each of the three re-checks is pinned on its own
// ---------------------------------------------------------------------------

// Covers AC-013. The file set changes while the first pull runs and is restored
// before the retry. Only check #2 can see it: without check #2's comparison the
// sanitized copy would be written from a changed file set and the update would
// go on, because check #3 sees the restored set. Added by QA Engineer.
func TestQA_AC013_Check2ComparesFileSetBeforeSanitizedCopyIsWritten(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	fs := h.useFakeTrust(dir)
	override := filepath.Join(dir, "compose.override.yml")
	failFirstPull(h, func() { fs.file(override, 0, 0, 0644, "") }, func(bool) { delete(fs.nodes, filepath.Clean(override)) })

	res := h.run(idA, "docker_update", stackParams(dir))

	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "(dir_changed)", "[AC-013] check #2 refuses a file set that differs from check #1")
	assert.Equal(t, []string{"pull"}, h.procs.subcommands(), "[AC-013] no retry pull and no up: the sanitized copy is never written")
	assert.False(t, fileExists(filepath.Join(dir, sanitizedComposeName)))
}

// Covers AC-013. The file set changes during the sanitized retry pull. Check #1
// and #2 agree; only check #3 can refuse before up runs with the new file.
// Added by QA Engineer.
func TestQA_AC013_Check3ComparesFileSetBeforeUp(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	h.composeService(dir, "web", true, "")
	fs := h.useFakeTrust(dir)
	failFirstPull(h, nil, func(bool) { fs.file(filepath.Join(dir, "compose.override.yml"), 0, 0, 0644, "") })

	res := h.run(idA, "docker_update", stackParams(dir))

	assert.Equal(t, models.StepPrecheck, res.Step)
	assert.Contains(t, res.Message, "(dir_changed)", "[AC-013] check #3 refuses a file set that changed during the retry pull")
	assert.NotContains(t, h.procs.subcommands(), "up", "[AC-013] compose up never ran with the unvalidated file")
	assert.False(t, fileExists(filepath.Join(dir, sanitizedComposeName)), "temp file removed on the refusal path")
	for _, op := range []string{"stop:", "rename:", "create:", "remove:"} {
		assert.Empty(t, h.docker.callsWithPrefix(op), "no container changed (%s)", op)
	}
}

// ---------------------------------------------------------------------------
// AC-027 / standing requirement: every message is size-capped
// ---------------------------------------------------------------------------

// Covers AC-027 and the Bounds section. A label value of any length is cut to
// maxRefusalValueBytes before it is quoted, so one refusal stays small even
// before the whole-message cap. Added by QA Engineer.
func TestQA_AC027_LongValueIsCutBeforeTheWholeMessageCap(t *testing.T) {
	x := newTrustFx(t)
	long := x.p(strings.Repeat("a", 5000))
	_, err := x.ct.Check(x.snap, trustRequest{Dir: long, Project: "app"})
	require.Error(t, err)
	// The directory appears twice (header and "cannot resolve"), each at most
	// 200 bytes plus quoting; the whole-message cap of 1024 is far above this.
	assert.Less(t, len(err.Error()), 700, "[AC-027] each value is cut to ~200 bytes, independent of the 1024 cap")
}

// Covers AC-027 and the Bounds section. A refusal that names five long values
// (directory, mount source, destination, container) never exceeds
// maxRefusalBytes. Added by QA Engineer.
func TestQA_AC027_WholeMessageCapHoldsWhenEveryValueIsLong(t *testing.T) {
	x := newTrustFx(t)
	// A directory name just under the per-value cap, so it is not itself cut.
	deep := x.p("opt", "stacks", "app", strings.Repeat("d", 150))
	x.fs.chain(deep)
	x.fs.file(filepath.Join(deep, "compose.yaml"), 0, 0, 0644, "services: {}\n")
	x.dir = deep
	x.addStackContainer("c9", "app-deep-1", "app", deep)
	x.snap.Containers = x.snap.Containers[1:] // drop the original stack container
	x.writer(strings.Repeat("n", 5000), bindRW(deep, strings.Repeat("/m", 3000)))

	te := x.refuse(ruleContainerWritable)

	assert.LessOrEqual(t, len(te.Error()), maxRefusalBytes, "[AC-027] the whole message is capped")
	assert.Contains(t, te.Error(), "container_writable")
	// Without the whole-message cap this refusal is about 1.3 KB.
	assert.Greater(t, len(te.Error()), 900, "precondition: the message is long enough to need the cap")
}

// ---------------------------------------------------------------------------
// AC-008 / edge cases from the QA brief
// ---------------------------------------------------------------------------

// Covers AC-008. A stack directory literally named "..foo" under a root is
// inside the root (the check is separator-aware, not a string prefix test).
// Added by QA Engineer.
func TestQA_AC008_DotDotPrefixedDirectoryNameIsInsideRoot(t *testing.T) {
	x := newTrustFx(t)
	odd := x.p("opt", "stacks", "..foo")
	x.fs.chain(odd)
	x.fs.file(filepath.Join(odd, "compose.yaml"), 0, 0, 0644, "services: {}\n")
	x.dir = odd
	x.snap.Containers = nil
	x.addStackContainer("c1", "odd-web-1", "app", odd)
	x.ct.roots = []string{x.p("opt", "stacks")}

	td := x.accept()

	assert.Equal(t, odd, td.Path(), "[AC-008] /opt/stacks/..foo is inside /opt/stacks")
}

// Covers AC-010. A trailing slash and doubled separators in the requested
// directory are cleaned before resolution. Added by QA Engineer.
func TestQA_AC010_TrailingSlashAndDoubleSeparatorsAreCleaned(t *testing.T) {
	x := newTrustFx(t)
	sep := string(filepath.Separator)
	for _, dir := range []string{x.dir + sep, x.dir + sep + sep, filepath.Join(x.p("opt"), "stacks") + sep + "." + sep + "app" + sep} {
		td, err := x.ct.Check(x.snap, trustRequest{Dir: dir, Project: "app"})
		require.NoError(t, err, "%q", dir)
		assert.Equal(t, x.dir, td.Path(), "compose runs at the cleaned, resolved path for %q", dir)
	}
}

// Covers AC-009. A container with an empty working-dir label value is not a
// compose source, so it can neither name a directory nor make a project
// ambiguous. Added by QA Engineer.
func TestQA_AC009_EmptyWorkingDirLabelIsNotASource(t *testing.T) {
	snap := daemonSnapshot{excluded: map[string]string{}, Containers: []container.Summary{
		{ID: "a", Labels: map[string]string{models.LabelComposeProject: "app", models.LabelComposeWorkingDir: "/opt/stacks/app"}},
		{ID: "b", Labels: map[string]string{models.LabelComposeProject: "app", models.LabelComposeWorkingDir: ""}},
		{ID: "c", Labels: map[string]string{models.LabelComposeProject: "ghost", models.LabelComposeWorkingDir: ""}},
	}}

	dir, err := snap.resolveProject("app")
	require.Nil(t, err, "an empty label must not make the project ambiguous")
	assert.Equal(t, "/opt/stacks/app", dir)

	_, err = snap.resolveProject("ghost")
	require.NotNil(t, err)
	assert.Equal(t, ruleProjectNotFound, err.Rule, "a project whose only working-dir label is empty is not found")
	assert.Empty(t, snap.projectsForDir(""), "an empty directory maps to no project")
}

// ---------------------------------------------------------------------------
// AC-020: .env line endings
// ---------------------------------------------------------------------------

// Covers AC-020. A .env saved with CRLF line endings (edited on Windows) is
// parsed correctly, quoted and unquoted, and matches what compose itself reads.
// Added by QA Engineer.
func TestQA_AC020_CRLFEnvIsParsed(t *testing.T) {
	x := newTrustFx(t)
	x.file("base.yml", 0, 0, 0644, "")
	x.file("extra.yml", 0, 0, 0644, "")
	x.file(".env", 0, 0, 0644, "FOO=bar\r\nCOMPOSE_FILE=\"base.yml:extra.yml\"\r\nOTHER=1\r\n")

	td := x.accept()

	assert.Equal(t, []string{filepath.Join(x.dir, "base.yml"), filepath.Join(x.dir, "extra.yml")}, td.Files(),
		"[AC-020] CRLF does not leak into the file names")
}

// Covers AC-020. A custom separator line with CRLF endings. Added by QA Engineer.
func TestQA_AC020_CRLFSeparatorLine(t *testing.T) {
	x := newTrustFx(t)
	x.file("base.yml", 0, 0, 0644, "")
	x.file("extra.yml", 0, 0, 0644, "")
	x.file(".env", 0, 0, 0644, "COMPOSE_PATH_SEPARATOR=|\r\nCOMPOSE_FILE=base.yml|extra.yml\r\n")

	td := x.accept()

	assert.Equal(t, []string{filepath.Join(x.dir, "base.yml"), filepath.Join(x.dir, "extra.yml")}, td.Files())
}

// ---------------------------------------------------------------------------
// AC-019: a writable mount source that is a symlink into the stack directory
// ---------------------------------------------------------------------------

// Covers AC-019. A container's read-write bind source that is a symlink to the
// stack directory is resolved before comparison, so the link cannot hide the
// mount. Added by QA Engineer.
func TestQA_AC019_SymlinkedBindSourceIntoStackDirRefused(t *testing.T) {
	x := newTrustFx(t)
	hop := x.p("srv", "hop")
	x.fs.chain(x.p("srv"))
	x.fs.link(hop, x.dir, 0)
	x.writer("sneaky", bindRW(hop, "/app"))

	te := x.refuse(ruleContainerWritable)

	assert.Contains(t, te.Error(), "sneaky", "[AC-019] names the container")
}

// ---------------------------------------------------------------------------
// AC-003 / AC-025 adversarial: malformed requests
// ---------------------------------------------------------------------------

// Covers AC-001 and AC-028. NUL bytes, a "..", and a name of dots are refused
// or confined; none reaches compose. Added by QA Engineer.
func TestQA_AC001_HostileDirectoryStringsNeverAccepted(t *testing.T) {
	x := newTrustFx(t)
	for _, dir := range []string{
		x.dir + "\x00/../../etc",
		x.p("opt", "stacks", "app", "..", "..", "..", "etc"),
		x.p("opt", "stacks", "app") + string(filepath.Separator) + "..",
		x.p("opt", "stacks", "nope"),
	} {
		td, err := x.ct.Check(x.snap, trustRequest{Dir: dir, Project: "app"})
		var te *composeTrustError
		require.True(t, errors.As(err, &te), "%q must be refused", dir)
		assert.False(t, td.valid(), "%q", dir)
		assert.NotContains(t, te.Error(), "\x00")
	}
}

// Covers AC-011 and AC-002. The old-server form (compose_work_dir only) works
// for either spelling of a symlinked stack directory when containers carry both
// spellings. Added by QA Engineer.
func TestQA_AC011_WorkDirOnlyFormAcceptsEitherSpellingOfASymlinkedDir(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	fs := h.useFakeTrust(dir)
	link := filepath.Join(filepath.Dir(dir), "link-to-stack")
	fs.link(link, dir, 0)
	h.composeService(dir, "a", true, "")
	h.composeService(link, "b", true, "")

	res := h.run(idA, "docker_update", stackParams(link))

	assert.Equal(t, "success", res.Status, "%s", res.Message)
}

// helper so the compile fails loudly if the fixture API changes
var _ = os.ModeDir
