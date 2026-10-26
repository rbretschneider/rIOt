package extraupd

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	osReleaseRel       = "proc/sys/kernel/osrelease"
	rebootMarkerRel    = "run/reboot-required"
	modulesDirRel      = "lib/modules"
	maxOSReleaseBytes  = 256
	modulesMissingText = "kernel modules for running kernel %s are missing"
)

// releasePattern is the charset and length filter applied to the running
// kernel release before it is used in a path (AD-009).
var releasePattern = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)

// DietPiPresent reports whether the host is a DietPi host (FR-008): both
// /boot/dietpi/.version and /boot/dietpi/dietpi-update are regular files.
func DietPiPresent(fsys FS) bool {
	return regularFile(fsys, dietpiVersionRel) && regularFile(fsys, dietpiUpdateRel)
}

// ModulesMissing reports FR-016: on a DietPi host, /lib/modules/<running
// kernel release> does not exist. It is false on every other host, and false
// whenever the answer cannot be read, because rIOt never claims a reboot it
// cannot show. The release is returned for the reason text.
func ModulesMissing(fsys FS) (missing bool, release string) {
	if !DietPiPresent(fsys) {
		return false, ""
	}
	data, err := fsys.ReadFile(osReleaseRel, maxOSReleaseBytes)
	if err != nil {
		return false, ""
	}
	release = strings.TrimSpace(string(data))
	if !releasePattern.MatchString(release) {
		return false, ""
	}
	if _, err = fsys.Lstat(modulesDirRel + "/" + release); err != nil && isAbsent(err) {
		return true, release
	}
	return false, release
}

// RebootIndicators evaluates the updater-related reboot signals. It is the
// single place the worker uses to answer "is a reboot required after the
// step?": the apt reboot-required marker or the DietPi modules rule.
func RebootIndicators(fsys FS) (required bool, reasons []string) {
	if _, err := fsys.Lstat(rebootMarkerRel); err == nil {
		required = true
		reasons = append(reasons, "/run/reboot-required exists")
	}
	if missing, release := ModulesMissing(fsys); missing {
		required = true
		reasons = append(reasons, fmt.Sprintf(modulesMissingText, release))
	}
	return required, reasons
}

// ModulesMissingReason is the reason line reported for ModulesMissing.
func ModulesMissingReason(release string) string { return fmt.Sprintf(modulesMissingText, release) }
