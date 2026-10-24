//go:build !linux

package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPlatformProbe_RD_AC012_NonLinuxIsAbsent(t *testing.T) {
	got := platformDpkgProbe(time.Now)

	assert.Equal(t, dpkgLockAbsent, got.State, "[AC-012] a non-Linux host (Windows) has no dpkg lock, so reboots are unchanged and never wait")
}
