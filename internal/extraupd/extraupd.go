// Package extraupd defines the extra-updater contract (EXTRA-UPD-001): a
// read-only detector per platform updater that works outside the package
// manager, plus one fixed, compile-time apply spec. DietPi is the only
// implementation. The unprivileged agent collector, the agent's readiness check
// and the root maintenance worker all use the same detector, so a "freshly
// re-read status" means the same thing on both sides of the privilege boundary.
//
// The package starts no process, opens no network connection and writes no
// file. Its only inputs are an FS and the compiled specs below.
package extraupd

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Source is where an updater fetches its code from, resolved by the updater's
// own rules. Readable is false when the answer cannot be determined.
type Source struct {
	Owner    string
	Branch   string
	Readable bool
}

// ApplySpec is the one fixed apply operation of an updater. It is a compile-time
// constant: no request, config value or file can change Path, Args, Env or Dir
// (FR-029, NFR-001).
type ApplySpec struct {
	Path string
	Args []string
	Env  []string
	Dir  string

	// StepLimit bounds the whole step; OutputCap bounds its captured output.
	// DescendantSettle is how long group members may outlive the leader before
	// they count as stray processes.
	StepLimit        time.Duration
	OutputCap        int
	DescendantSettle time.Duration

	// LogPath is the updater's own full log; Label names it in results.
	LogPath string
	Label   string

	// TrustRoots are paths the root worker walks before it believes any
	// on-disk fact and before it runs the step. A root is skipped when it does
	// not exist unless RequiredRoots names it.
	TrustRoots    []string
	RequiredRoots func(installed models.ExtraUpdater) []string

	// BusyMatch reports whether a process command line (its first two
	// arguments) belongs to the updater's own tooling.
	BusyMatch func(argv0, argv1 string) bool

	// TrustedOwner is the only update-source owner a rIOt-initiated run accepts.
	TrustedOwner string

	// PrivateTmp makes the worker start the step through the launcher, which
	// gives it a private /tmp and leaves every other mount shared (AD-007).
	PrivateTmp bool
}

// Updater is the contract every extra updater fulfils (FR-001).
type Updater interface {
	// ID is the fixed identifier, matching models.ExtraUpdaterIDPattern.
	ID() string
	// Detect reads the host through fs only. The bool is false when the updater
	// is not present, in which case the entry is the zero value. Readiness is
	// left empty for the caller to stamp.
	Detect(ctx context.Context, fs FS) (models.ExtraUpdater, bool)
	// Source resolves the update source the way the updater itself does.
	Source(fs FS) Source
	// Apply returns the fixed apply spec.
	Apply() ApplySpec
}

var registry = []Updater{NewDietPi()}

// Registry returns the implemented updaters in their fixed run order. snap and
// flatpak would be appended here later.
func Registry() []Updater {
	out := make([]Updater, len(registry))
	copy(out, registry)
	return out
}

// Lookup returns the implemented updater with the given identifier.
func Lookup(id string) (Updater, bool) {
	for _, u := range registry {
		if u.ID() == id {
			return u, true
		}
	}
	return nil, false
}

// IsImplemented reports whether id names an implemented updater.
func IsImplemented(id string) bool {
	_, ok := Lookup(id)
	return ok
}

// DetectAll runs every registered detector in registry order and returns the
// present ones, at most models.MaxExtraUpdaters (FR-005, NFR-002). The result
// is nil when no updater is present.
func DetectAll(ctx context.Context, fs FS) []models.ExtraUpdater {
	var out []models.ExtraUpdater
	for _, u := range registry {
		if len(out) >= models.MaxExtraUpdaters {
			break
		}
		if entry, present := u.Detect(ctx, fs); present {
			out = append(out, entry)
		}
	}
	return out
}

// FixedEnv is the compiled environment of an apply spec: the maintenance
// worker's fixed environment plus the updater's non-interactive switch. The
// worker pins it equal to maintenance.FixedEnvironment() in a test.
func FixedEnv(extra ...string) []string {
	env := []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LANG=C",
		"LC_ALL=C",
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"NEEDRESTART_MODE=l",
	}
	return append(env, extra...)
}
