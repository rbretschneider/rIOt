//go:build !linux

package agent

import "time"

// platformDpkgProbe: dpkg locks exist only on Linux. Elsewhere (Windows) there is
// nothing to wait for and reboots behave as before (FR-006).
func platformDpkgProbe(_ func() time.Time) dpkgLockStatus {
	return dpkgLockStatus{State: dpkgLockAbsent, UID: -1}
}
