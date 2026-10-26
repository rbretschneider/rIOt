package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/maintenance"
)

// reportExtraUpdaters prints the Extra Updaters section of `riot-agent doctor`
// (EXTRA-UPD-001 FR-061, FR-062). It prints nothing unless
// commands.allow_extra_updaters lists something. It only reads: the filesystem
// detection, the helper's --capabilities answer, the root policy file and the
// unit properties. It never runs an updater, and the probes are injected so a
// test can prove it.
func reportExtraUpdaters(a *Agent, cfg CommandsConfig, goos string, timeout time.Duration) {
	if goos != "linux" || len(cfg.AllowExtraUpdaters) == 0 {
		return
	}
	fmt.Println()
	section("Extra Updaters")
	for _, ignored := range cfg.ignoredExtraUpdaters() {
		warn("commands.allow_extra_updaters lists %q, which this agent does not implement; it is ignored", ignored)
	}
	listed := cfg.EnabledExtraUpdaters()
	if len(listed) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fs := a.extraFS()
	facts := a.extraRootFacts(ctx)
	for _, id := range listed {
		u, _ := extraupd.Lookup(id)
		entry, present := u.Detect(ctx, fs)
		if !present {
			info("%s: not applicable here (it was not detected on this host)", id)
			continue
		}
		version := entry.InstalledVersion
		if version == "" {
			version = "version unreadable"
		}
		pass("%s detected (%s)", id, version)
		if !cfg.AllowPatching {
			warn("%s: commands.allow_patching is false, so it will not be applied; set it to true to patch", id)
		}
		if facts.policyOK && extraupd.PolicyListsImplemented(facts.policy, id) {
			pass("%s: the root-side opt-in lists it (%s)", id, extraupd.PolicyPath)
		} else {
			fail("%s: the root-side opt-in does not list it; list it in agent.yaml and re-run install.sh as root on this host", id)
		}
		if facts.capsOK && facts.caps.supports(id) {
			pass("%s: the maintenance helper can run it (request version %d)", id, maintenance.RequestVersionExtra)
		} else {
			fail("%s: the installed maintenance helper cannot run extra updaters; re-run install.sh as root to install the newer helper", id)
		}
		switch {
		case !facts.unitOK:
			fail("%s: cannot read the maintenance unit (RuntimeMaxUSec, PrivateTmp, MountFlags); re-run install.sh as root", id)
		case facts.unit.RuntimeMax < maintenance.ExtraMinBackstop:
			fail("%s: the maintenance service stops after %s, shorter than a DietPi run needs; re-run install.sh as root to extend it", id, facts.unit.RuntimeMax)
		case facts.unit.PrivateTmp != "no" || facts.unit.MountFlags != "":
			fail("%s: the maintenance service has a mount sandbox (PrivateTmp=%s, MountFlags=%s); DietPi's mounts would not reach the host", id, facts.unit.PrivateTmp, facts.unit.MountFlags)
		default:
			pass("%s: the maintenance service allows %s with no mount sandbox", id, facts.unit.RuntimeMax)
		}
		if src := u.Source(fs); !src.Readable || src.Owner != u.Apply().TrustedOwner {
			fail("%s: its update source is not the trusted default; rIOt will not run it (DEV_GITOWNER must be empty or %s)", id, u.Apply().TrustedOwner)
		}
	}
}
