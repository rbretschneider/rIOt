package agent

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// QA DEFECT REPRODUCTIONS (AUTOUPD-HARDEN-001). Each test below FAILS on the
// current implementation on purpose. They are the repro for a defect in
// docs/qa/AUTOUPD-HARDEN-001-qa-report.md and go green when senior dev fixes it.
// Added by QA Engineer.

// QA-D1. A stack whose working-dir label is a symlink path ends up with mixed
// labels after the agent recreates only some services: compose runs at the
// resolved path (AD-006), so the recreated services carry the resolved path and
// the untouched ones keep the link path. resolveProject then counts two
// directories and every later stack update is refused as ambiguous_project,
// until each service has been recreated. The agent created that state itself.
// Two labels whose directories resolve to the same real directory are one
// directory. Reproduced against a real Docker daemon and compose v5.6.0.
func TestQA_D1_MixedLinkAndResolvedLabelsDoNotBlockStackUpdate(t *testing.T) {
	captureLogs(t)
	h := newDockerHarness(t)
	dir := composeDir(t)
	fs := h.useFakeTrust(dir)
	link := filepath.Join(filepath.Dir(dir), "link-to-stack")
	fs.link(link, dir, 0)
	h.composeService(dir, "a", true, "")  // recreated by an earlier partial update: resolved label
	h.composeService(link, "b", true, "") // untouched: the label still names the symlink

	res := h.run(idA, "docker_update", projectParams("stack", ""))

	assert.Equal(t, "success", res.Status, "[QA-D1] one symlinked directory is one directory: %s", res.Message)
}

// QA-D3. A .env that starts with a UTF-8 byte order mark (Notepad and other
// Windows editors write one) and sets COMPOSE_FILE: compose v5.6.0 honours it,
// but the agent does not see the variable (the first line starts with the BOM
// bytes, so it is not a candidate), passes the default compose.yaml as -f, and
// starts the wrong services. AC-020 says a .env that cannot be parsed with
// certainty is refused; the agent must either read the BOM-prefixed line as
// compose does or refuse the file. Reproduced against a real daemon: the update
// created a service from compose.yaml that the operator's COMPOSE_FILE excluded.
func TestQA_D3_BOMPrefixedEnvComposeFileIsNotSilentlyIgnored(t *testing.T) {
	x := newTrustFx(t)
	x.file("base.yml", 0, 0, 0644, "")
	x.file(".env", 0, 0, 0644, "\xef\xbb\xbfCOMPOSE_FILE=base.yml\n")

	td, err := x.check()

	if err != nil {
		var te *composeTrustError
		require.ErrorAs(t, err, &te)
		assert.Equal(t, ruleEnvUnparsed, te.Rule, "[QA-D3] refusing the file is acceptable")
		return
	}
	assert.Equal(t, []string{filepath.Join(x.dir, "base.yml")}, td.Files(),
		"[QA-D3] accepted, so COMPOSE_FILE must have been honoured as compose does, not replaced by the default compose.yaml")
}
