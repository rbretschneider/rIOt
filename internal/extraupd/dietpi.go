package extraupd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DietPi paths, relative to the filesystem root for detection and absolute for
// the apply spec. Facts verified against DietPi master @ 65a12a6.
const (
	DietPiID         = models.ExtraUpdaterDietPi
	DietPiUpdatePath = "/boot/dietpi/dietpi-update"
	DietPiLogPath    = "/var/lib/dietpi/logs/dietpi-update.log"

	dietpiVersionRel  = "boot/dietpi/.version"
	dietpiUpdateRel   = "boot/dietpi/dietpi-update"
	dietpiTxtRel      = "boot/dietpi.txt"
	dietpiFlagRel     = "run/dietpi/.update_available"
	dietpiLivePatches = "run/dietpi/.live_patches"

	// MaxReadBytes bounds every detection read (FR-015). A longer file is
	// unreadable, never silently truncated.
	MaxReadBytes = 64 << 10
	// DetectTimeout bounds one detection (FR-015).
	DetectTimeout = time.Second

	// DietPiStepLimit is the DietPi step limit (FR-030, AD-006).
	DietPiStepLimit = 40 * time.Minute
	// DietPiOutputCap is the DietPi section cap inside the run output (FR-036).
	DietPiOutputCap = 128 << 10
	// DietPiDescendantSettle lets DietPi's tee outlive the leader briefly.
	DietPiDescendantSettle = 10 * time.Second

	defaultOwner  = "MichaIng"
	defaultBranch = "master"
	// DietPi 10.4 carries Patch_10_4, the /var/tmp/dietpi migration. Below it
	// that patch is still pending.
	patch104Core = 10
	patch104Sub  = 4
)

var (
	versionKeyLine = regexp.MustCompile(`^(G_DIETPI_VERSION_CORE|G_DIETPI_VERSION_SUB|G_DIETPI_VERSION_RC|G_GITBRANCH|G_GITOWNER)=(.*)$`)
	versionNumber  = regexp.MustCompile(`^-?[0-9]{1,9}$`)
	flagPattern    = regexp.MustCompile(`^([0-9]{1,9})\.([0-9]{1,9})\.(-?[0-9]{1,9})$`)
)

// dietpiVersion is a parsed CORE.SUB.RC triple.
type dietpiVersion struct{ core, sub, rc int }

func (v dietpiVersion) String() string { return fmt.Sprintf("%d.%d.%d", v.core, v.sub, v.rc) }

// newerThan reports whether v sorts after o.
func (v dietpiVersion) newerThan(o dietpiVersion) bool {
	if v.core != o.core {
		return v.core > o.core
	}
	if v.sub != o.sub {
		return v.sub > o.sub
	}
	return v.rc > o.rc
}

// versionFile is the data parsed out of /boot/dietpi/.version. The file is
// shell assignments that DietPi sources; rIOt only ever reads it as data
// (FR-009).
type versionFile struct {
	version     dietpiVersion
	valid       bool
	gitBranch   string
	gitOwner    string
	hasGitOwner bool
	hasGitBrnch bool
}

func parseVersionFile(data []byte) versionFile {
	var out versionFile
	seen := map[string]int{}
	var core, sub, rc int
	for _, line := range strings.Split(string(data), "\n") {
		m := versionKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, value := m[1], unquoteOnce(m[2])
		seen[key]++
		if seen[key] > 1 {
			continue
		}
		switch key {
		case "G_GITBRANCH":
			out.gitBranch, out.hasGitBrnch = value, true
		case "G_GITOWNER":
			out.gitOwner, out.hasGitOwner = value, true
		default:
			if !versionNumber.MatchString(value) {
				seen[key] = 2 // an unparseable value invalidates the version
				continue
			}
			n, err := strconv.Atoi(value)
			if err != nil {
				seen[key] = 2
				continue
			}
			switch key {
			case "G_DIETPI_VERSION_CORE":
				core = n
			case "G_DIETPI_VERSION_SUB":
				sub = n
			case "G_DIETPI_VERSION_RC":
				rc = n
			}
		}
	}
	out.valid = seen["G_DIETPI_VERSION_CORE"] == 1 && seen["G_DIETPI_VERSION_SUB"] == 1 && seen["G_DIETPI_VERSION_RC"] == 1
	if out.valid {
		out.version = dietpiVersion{core, sub, rc}
	}
	return out
}

// unquoteOnce strips one surrounding pair of matching quotes, with no escape
// processing.
func unquoteOnce(s string) string {
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// dietpiTxt exposes values of /boot/dietpi.txt the way DietPi reads them: the
// first line matching ^[[:blank:]]*KEY= wins and the value is taken verbatim
// (DietPi's `sed -n '/^[[:blank:]]*KEY=/{s/^[^=]*=//p;q}'`).
type dietpiTxt []byte

func (t dietpiTxt) raw(key string) (string, bool) {
	for _, line := range strings.Split(string(t), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, key+"=") {
			return strings.TrimPrefix(trimmed, key+"="), true
		}
	}
	return "", false
}

// trimmed is raw with trailing blanks and a carriage return removed, the form
// used for yes/no settings.
func (t dietpiTxt) trimmed(key string) string {
	v, _ := t.raw(key)
	return strings.TrimRight(v, " \t\r")
}

// DietPi is the DietPi extra updater (identifier "dietpi").
type DietPi struct {
	// Evidence reports whether DietPi's own check has completed since boot and
	// found nothing (row D3). DietPi leaves no such marker, so production never
	// sets it and a quiet host reports unknown (OQ-A1, AF-1). Tests set it.
	Evidence func(FS) bool
	// Timeout overrides DetectTimeout. Zero means the default.
	Timeout time.Duration

	inflight atomic.Bool
}

// NewDietPi returns the production DietPi updater.
func NewDietPi() *DietPi { return &DietPi{} }

// ID implements Updater.
func (d *DietPi) ID() string { return DietPiID }

func (d *DietPi) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return DetectTimeout
}

// Detect implements Updater. It runs the detection in one goroutine under a
// deadline. A call whose predecessor is still blocked does not start another
// read, so a stuck filesystem leaks at most one goroutine (FR-015).
func (d *DietPi) Detect(ctx context.Context, fsys FS) (models.ExtraUpdater, bool) {
	failed := models.ExtraUpdater{ID: DietPiID, Status: models.ExtraStatusUnknown, UnknownReason: models.ExtraReasonDetectionFailed}
	if ctx.Err() != nil || !d.inflight.CompareAndSwap(false, true) {
		return failed, true
	}
	type outcome struct {
		entry   models.ExtraUpdater
		present bool
	}
	done := make(chan outcome, 1)
	go func() {
		defer d.inflight.Store(false)
		entry, present := d.detect(fsys)
		done <- outcome{entry, present}
	}()
	timer := time.NewTimer(d.timeout())
	defer timer.Stop()
	select {
	case r := <-done:
		return r.entry, r.present
	case <-timer.C:
	case <-ctx.Done():
	}
	return failed, true
}

// Source implements Updater with DietPi's own resolution (SEC-003): the first
// matching dietpi.txt line wins and is taken verbatim; an empty value means the
// default; an unreadable file means the source is unknown.
func (d *DietPi) Source(fsys FS) Source {
	data, err := fsys.ReadFile(dietpiTxtRel, MaxReadBytes)
	return resolveSource(dietpiTxt(data), err == nil)
}

func resolveSource(txt dietpiTxt, readable bool) Source {
	if !readable {
		return Source{}
	}
	owner, _ := txt.raw("DEV_GITOWNER")
	branch, _ := txt.raw("DEV_GITBRANCH")
	if owner == "" {
		owner = defaultOwner
	}
	if branch == "" {
		branch = defaultBranch
	}
	return Source{Owner: owner, Branch: branch, Readable: true}
}

// Apply implements Updater: exactly /boot/dietpi/dietpi-update 1, with a
// compiled environment, working directory / and null stdin (FR-021, FR-029).
func (d *DietPi) Apply() ApplySpec {
	return ApplySpec{
		Path:             DietPiUpdatePath,
		Args:             []string{"1"},
		Env:              FixedEnv("G_INTERACTIVE=0"),
		Dir:              "/",
		StepLimit:        DietPiStepLimit,
		OutputCap:        DietPiOutputCap,
		DescendantSettle: DietPiDescendantSettle,
		LogPath:          DietPiLogPath,
		Label:            "DietPi",
		TrustRoots:       []string{"/boot/dietpi", "/boot/dietpi.txt", "/run/dietpi", "/var/tmp/dietpi"},
		RequiredRoots: func(installed models.ExtraUpdater) []string {
			// While Patch_10_4 is pending, DietPi migrates /var/tmp/dietpi, so it
			// must exist and pass the trust walk (D-10).
			if v, ok := parseVersionString(installed.InstalledVersion); ok && !v.atLeast(patch104Core, patch104Sub) {
				return []string{"/var/tmp/dietpi"}
			}
			return nil
		},
		BusyMatch:    dietpiBusy,
		TrustedOwner: defaultOwner,
		PrivateTmp:   true,
	}
}

func (v dietpiVersion) atLeast(core, sub int) bool {
	return v.core > core || (v.core == core && v.sub >= sub)
}

func parseVersionString(s string) (dietpiVersion, bool) {
	m := flagPattern.FindStringSubmatch(s)
	if m == nil {
		return dietpiVersion{}, false
	}
	core, e1 := strconv.Atoi(m[1])
	sub, e2 := strconv.Atoi(m[2])
	rc, e3 := strconv.Atoi(m[3])
	if e1 != nil || e2 != nil || e3 != nil {
		return dietpiVersion{}, false
	}
	return dietpiVersion{core, sub, rc}, true
}

// dietpiBusy matches the command lines of any other DietPi program: one whose
// first or second argument lives under /boot/dietpi/ (except the long-running
// cloudshell) or is the daily cron job.
func dietpiBusy(argv0, argv1 string) bool {
	for _, arg := range [...]string{argv0, argv1} {
		if arg == "/etc/cron.daily/dietpi" {
			return true
		}
		if strings.HasPrefix(arg, "/boot/dietpi/") && !strings.HasPrefix(arg, "/boot/dietpi/dietpi-cloudshell") {
			return true
		}
	}
	return false
}

func regularFile(fsys FS, name string) bool {
	info, err := fsys.Lstat(name)
	return err == nil && info.Mode().IsRegular()
}

// isAbsent reports whether err means the path does not exist.
func isAbsent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func unknownEntry(e models.ExtraUpdater, reason string) models.ExtraUpdater {
	e.Status = models.ExtraStatusUnknown
	e.UnknownReason = reason
	return e
}

func (d *DietPi) detect(fsys FS) (models.ExtraUpdater, bool) {
	if !regularFile(fsys, dietpiVersionRel) || !regularFile(fsys, dietpiUpdateRel) {
		return models.ExtraUpdater{}, false
	}
	e := models.ExtraUpdater{ID: DietPiID}

	verData, verErr := fsys.ReadFile(dietpiVersionRel, MaxReadBytes)
	ver := parseVersionFile(verData)
	if verErr != nil || !ver.valid {
		return unknownEntry(e, models.ExtraReasonVersionUnreadable), true
	}
	e.InstalledVersion = ver.version.String()

	// Facts read from DietPi's own flag files.
	flagData, flagErr := fsys.ReadFile(dietpiFlagRel, MaxReadBytes)
	flagPresent, flagUnreadable := flagErr == nil, flagErr != nil && !isAbsent(flagErr)
	var flagVersion dietpiVersion
	flagParsed := false
	if flagPresent {
		flagVersion, flagParsed = parseVersionString(strings.TrimSpace(firstLine(string(flagData))))
	}
	_, liveErr := fsys.Lstat(dietpiLivePatches)
	livePatches, liveUnreadable := liveErr == nil, liveErr != nil && !isAbsent(liveErr)

	cfgData, cfgErr := fsys.ReadFile(dietpiTxtRel, MaxReadBytes)
	cfgReadable := cfgErr == nil
	txt := dietpiTxt(cfgData)

	// A parsed flag that is not newer than the installed version is stale and
	// is treated as absent (FR-010).
	versionUpdate := flagPresent && (!flagParsed || flagVersion.newerThan(ver.version))
	if versionUpdate && flagParsed {
		e.AvailableVersion = flagVersion.String()
	}

	e.Notices = notices(ver, flagParsed && versionUpdate, flagVersion, livePatches, txt, cfgReadable)

	if versionUpdate {
		e.PendingCount++
	}
	if livePatches {
		e.PendingCount++
	}
	switch {
	case versionUpdate || livePatches: // D1, D2
		e.Status = models.ExtraStatusUpdateAvailable
	case flagUnreadable || liveUnreadable:
		e = unknownEntry(e, models.ExtraReasonDetectionFailed)
	case !cfgReadable: // D6
		e = unknownEntry(e, models.ExtraReasonConfigUnreadable)
	case txt.trimmed("CONFIG_CHECK_DIETPI_UPDATES") != "1": // D5
		e = unknownEntry(e, models.ExtraReasonCheckDisabled)
	case d.Evidence != nil && d.Evidence(fsys): // D3, unreachable in production
		e.Status = models.ExtraStatusUpToDate
	default: // D4
		e = unknownEntry(e, models.ExtraReasonNotCheckedSinceBoot)
	}
	return e, true
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// notices builds the fixed-code notices in their documented order (FR-011 to
// FR-014): live patches, DietPi's own apt upgrade, a non-default update source,
// a major upgrade.
func notices(ver versionFile, versionUpdate bool, available dietpiVersion, livePatches bool, txt dietpiTxt, cfgReadable bool) []models.ExtraUpdaterNotice {
	var out []models.ExtraUpdaterNotice
	if livePatches {
		out = append(out, models.ExtraUpdaterNotice{Code: models.ExtraNoticeLivePatchesAvailable})
	}
	if cfgReadable && txt.trimmed("CONFIG_CHECK_APT_UPDATES") == "2" {
		out = append(out, models.ExtraUpdaterNotice{Code: models.ExtraNoticeDietPiAutoAptUpgrade})
	}
	if owner, branch, nonDefault := effectiveSource(ver, resolveSource(txt, cfgReadable)); nonDefault {
		out = append(out, models.ExtraUpdaterNotice{
			Code:   models.ExtraNoticeNonDefaultUpdateSource,
			Branch: printableValue(branch),
			Owner:  printableValue(owner),
		})
	}
	if versionUpdate && available.core > ver.version.core {
		out = append(out, models.ExtraUpdaterNotice{Code: models.ExtraNoticeMajorUpgrade})
	}
	return out
}

// effectiveSource is the source shown in the non_default_update_source notice:
// DietPi's resolved source when it is not the default, otherwise the source
// the installed version recorded.
func effectiveSource(ver versionFile, src Source) (owner, branch string, nonDefault bool) {
	if src.Readable && (src.Owner != defaultOwner || src.Branch != defaultBranch) {
		return src.Owner, src.Branch, true
	}
	if (ver.hasGitOwner && ver.gitOwner != defaultOwner) || (ver.hasGitBrnch && ver.gitBranch != defaultBranch) {
		owner, branch = defaultOwner, defaultBranch
		if ver.hasGitOwner {
			owner = ver.gitOwner
		}
		if ver.hasGitBrnch {
			branch = ver.gitBranch
		}
		return owner, branch, true
	}
	return "", "", false
}

// printableValue returns v when it is at most 64 printable ASCII bytes and
// "unreadable" otherwise (FR-013).
func printableValue(v string) string {
	if len(v) > models.MaxExtraUpdaterString {
		return models.ExtraNoticeValueUnreadable
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			return models.ExtraNoticeValueUnreadable
		}
	}
	return v
}
