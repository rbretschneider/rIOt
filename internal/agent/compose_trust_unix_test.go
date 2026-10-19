//go:build !windows

package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real osTrustFS reports the owner of a directory the test creates, and a
// fresh directory has no extended ACL.
func TestOSTrustFS_ReportsOwnerAndNoACL(t *testing.T) {
	dir := t.TempDir()
	fs := osTrustFS{}

	m, err := fs.Lstat(dir)
	require.NoError(t, err)
	assert.True(t, m.HasOwner, "a Unix filesystem reports an owner")
	assert.Equal(t, uint32(os.Geteuid()), m.UID, "a directory created by this process is owned by its euid")
	assert.True(t, m.Mode.IsDir())
	assert.Equal(t, os.Geteuid(), fs.EUID())

	has, err := fs.HasAccessACL(dir)
	require.NoError(t, err)
	assert.False(t, has, "a fresh directory has no extended access ACL")

	file := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(file, []byte("abc"), 0600))
	data, err := fs.ReadFile(file, 10)
	require.NoError(t, err)
	assert.Equal(t, "abc", string(data))
	_, err = fs.ReadFile(file, 2)
	assert.ErrorIs(t, err, errFileTooLarge, "a file over the cap is refused, not truncated")

	names, err := fs.ReadDirNames(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"f"}, names)
}
