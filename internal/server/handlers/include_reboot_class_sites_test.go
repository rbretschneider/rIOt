package handlers

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The only places that may set include_reboot_class under the server tree are
// checkAutoPatch (PATCH-GATE) and applyManualOSUpdateAuth (PATCH-FLOW-001 BR-003).
// A third site would be a new way to release reboot-class holds, which the
// security review (C-8) requires to stay at exactly two.
func TestSendCommand_PF_C8_IncludeRebootClassHasExactlyTwoSetters(t *testing.T) {
	const setter = `"include_reboot_class"] = true`
	var found []string
	err := filepath.Walk(filepath.Join("..", "..", "server"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(src), setter) {
			found = append(found, filepath.Base(path))
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(found)
	assert.Equal(t, []string{"auto_update.go", "os_update_auth.go"}, found,
		"[C-8] include_reboot_class may be set only by checkAutoPatch and applyManualOSUpdateAuth")
}
