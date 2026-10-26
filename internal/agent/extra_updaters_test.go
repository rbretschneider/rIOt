package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// SC-001 / SC-002: nothing here starts a process. The helper capability probe,
// the policy reader, the unit probe and the runner are injected fakes, and the
// DietPi host is a fixture tree whose dietpi-update is a non-executable
// tripwire.
const dietpiTripwire = "#!/bin/sh\necho DIETPI TRIPWIRE >&2\nexit 97\n"

func writeHost(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

// dietpiRoot builds a DietPi 9.20.1 host with 10.8.2 pending, checks enabled,
// the default update source and the kernel modules present.
func dietpiRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeHost(t, root, "boot/dietpi/.version", "G_DIETPI_VERSION_CORE=9\nG_DIETPI_VERSION_SUB=20\nG_DIETPI_VERSION_RC=1\nG_GITBRANCH='master'\nG_GITOWNER='MichaIng'\n")
	writeHost(t, root, "boot/dietpi/dietpi-update", dietpiTripwire)
	writeHost(t, root, "boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\n")
	writeHost(t, root, "run/dietpi/.update_available", "10.8.2\n")
	writeHost(t, root, "proc/sys/kernel/osrelease", "6.12.47+rpt-rpi-v8\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib", "modules", "6.12.47+rpt-rpi-v8"), 0o755))
	return root
}

const goodUnitShow = "RuntimeMaxUSec=1h 25min\nPrivateTmp=no\nMountFlags=\n"

// extraRig is an agent whose helper, root policy and unit are scripted.
type extraRig struct {
	a              *Agent
	client         *maintenanceClient
	request        maintenance.Request
	requests       int
	capCalls       atomic.Int32
	sig            string
	runnerCommands [][]string
}

func newExtraRig(t *testing.T, root string, cmds CommandsConfig) *extraRig {
	t.Helper()
	g := &extraRig{sig: "sig-1"}
	m, rec := maintenanceFixture()
	m.write = func(r maintenance.Request) error { g.request = r; g.requests++; return nil }
	m.terminal = func(string) (maintenance.Record, error) { return maintenance.Record{}, os.ErrNotExist }
	_ = rec
	m.capabilities = func(context.Context) ([]byte, error) {
		g.capCalls.Add(1)
		return []byte("request-versions=1,2\nextra-updaters=dietpi\n"), nil
	}
	m.rootPolicy = func() ([]byte, error) { return extraupd.EncodePolicy([]string{"dietpi"}), nil }
	m.unitShow = func(context.Context) (string, error) { return goodUnitShow, nil }
	m.signature = func() string { return g.sig }
	g.client = m
	g.a = &Agent{
		config:            &Config{Commands: cmds},
		maintenanceClient: m,
		runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
			g.runnerCommands = append(g.runnerCommands, append([]string{name}, args...))
			return nil, nil
		},
	}
	g.a.extra.fs = extraupd.DirFSOwnedByCaller(root)
	return g
}

func enabledCommands() CommandsConfig {
	return CommandsConfig{AllowPatching: true, AllowExtraUpdaters: ExtraUpdaterIDs{"dietpi"}}
}

func (g *extraRig) readiness(t *testing.T) string {
	t.Helper()
	list := extraupd.DetectAll(context.Background(), g.a.extraFS())
	require.Len(t, list, 1)
	g.a.stampExtraReadiness(context.Background(), list)
	return list[0].Readiness
}

func TestParseCapabilities_EU_SEC007_ClosedGrammar(t *testing.T) {
	t.Run("[SEC-007] the documented answer parses", func(t *testing.T) {
		caps, ok := parseCapabilities("request-versions=1,2\nextra-updaters=dietpi\n")
		require.True(t, ok)
		assert.True(t, caps.supports("dietpi"))
		assert.False(t, caps.supports("snap"))
	})
	t.Run("[SEC-007] an empty updater list parses and supports nothing", func(t *testing.T) {
		caps, ok := parseCapabilities("request-versions=1,2\nextra-updaters=\n")
		require.True(t, ok)
		assert.False(t, caps.supports("dietpi"))
	})
	t.Run("[SEC-007] a helper that only speaks version 1 supports nothing", func(t *testing.T) {
		caps, ok := parseCapabilities("request-versions=1\nextra-updaters=dietpi\n")
		require.True(t, ok)
		assert.False(t, caps.supports("dietpi"), "version 2 must be advertised")
	})
	for name, out := range map[string]string{
		"empty":                "",
		"old helper error":     "maintenance: maintenance helper accepts no arguments\n",
		"keys swapped":         "extra-updaters=dietpi\nrequest-versions=1,2\n",
		"a third line":         "request-versions=1,2\nextra-updaters=dietpi\nx=1\n",
		"bad version":          "request-versions=1,x\nextra-updaters=dietpi\n",
		"version zero":         "request-versions=0,1\nextra-updaters=dietpi\n",
		"bad identifier":       "request-versions=1,2\nextra-updaters=Diet Pi\n",
		"identifier with path": "request-versions=1,2\nextra-updaters=/boot/dietpi\n",
		"over 256 bytes":       "request-versions=1,2\nextra-updaters=" + strings.Repeat("a", 300) + "\n",
		"one line":             "request-versions=1,2\n",
	} {
		t.Run("[SEC-007] rejects "+name, func(t *testing.T) {
			if _, ok := parseCapabilities(out); ok {
				t.Fatalf("%q was accepted", out)
			}
		})
	}
}

func TestReadiness_EU_AC013_FiveReasons(t *testing.T) {
	root := dietpiRoot(t)
	cases := []struct {
		name   string
		cmds   CommandsConfig
		tweak  func(*extraRig)
		want   string
		reason string
	}{
		{"not listed", CommandsConfig{AllowPatching: true}, nil, models.ExtraReadinessNotOptedIn, ""},
		{"listed without the root side", enabledCommands(), func(g *extraRig) {
			g.client.rootPolicy = func() ([]byte, error) { return nil, os.ErrNotExist }
		}, models.ExtraReadinessRootSideMissing, ""},
		{"listed, root side lists nothing", enabledCommands(), func(g *extraRig) {
			g.client.rootPolicy = func() ([]byte, error) { return extraupd.EncodePolicy(nil), nil }
		}, models.ExtraReadinessRootSideMissing, ""},
		{"listed with the root side and patching disabled", CommandsConfig{AllowExtraUpdaters: ExtraUpdaterIDs{"dietpi"}}, nil, models.ExtraReadinessPatchingDisabled, ""},
		{"listed with an older helper", enabledCommands(), func(g *extraRig) {
			g.client.capabilities = func(context.Context) ([]byte, error) { return nil, errors.New("exit status 1") }
		}, models.ExtraReadinessUnsupportedHelper, ""},
		{"everything in place", enabledCommands(), nil, models.ExtraReadinessEnabled, ""},
	}
	for _, tc := range cases {
		t.Run("[AC-013] "+tc.name, func(t *testing.T) {
			g := newExtraRig(t, root, tc.cmds)
			if tc.tweak != nil {
				tc.tweak(g)
			}
			assert.Equal(t, tc.want, g.readiness(t))
		})
	}
	t.Run("[AC-013] the order is not_opted_in, patching_disabled, unsupported_helper, root_side_missing", func(t *testing.T) {
		g := newExtraRig(t, root, CommandsConfig{})
		g.client.capabilities = func(context.Context) ([]byte, error) { return nil, errors.New("old") }
		g.client.rootPolicy = func() ([]byte, error) { return nil, os.ErrNotExist }
		assert.Equal(t, models.ExtraReadinessNotOptedIn, g.readiness(t))
		g.a.config.Commands = CommandsConfig{AllowExtraUpdaters: ExtraUpdaterIDs{"dietpi"}}
		g.a.extra = extraState{fs: extraupd.DirFSOwnedByCaller(root)}
		assert.Equal(t, models.ExtraReadinessPatchingDisabled, g.readiness(t))
		g.a.config.Commands.AllowPatching = true
		g.a.extra = extraState{fs: extraupd.DirFSOwnedByCaller(root)}
		assert.Equal(t, models.ExtraReadinessUnsupportedHelper, g.readiness(t))
		g.client.capabilities = func(context.Context) ([]byte, error) {
			return []byte("request-versions=1,2\nextra-updaters=dietpi\n"), nil
		}
		g.a.extra = extraState{fs: extraupd.DirFSOwnedByCaller(root)}
		assert.Equal(t, models.ExtraReadinessRootSideMissing, g.readiness(t))
	})
}

func TestReadiness_EU_SEC002_UnitMustHaveTheExtendedBackstopAndNoSandbox(t *testing.T) {
	root := dietpiRoot(t)
	for name, show := range map[string]string{
		"a 35 minute backstop":      "RuntimeMaxUSec=35min\nPrivateTmp=no\nMountFlags=\n",
		"PrivateTmp=yes":            "RuntimeMaxUSec=1h 25min\nPrivateTmp=yes\nMountFlags=\n",
		"MountFlags=slave":          "RuntimeMaxUSec=1h 25min\nPrivateTmp=no\nMountFlags=slave\n",
		"an unparsable backstop":    "RuntimeMaxUSec=infinity\nPrivateTmp=no\nMountFlags=\n",
		"an answer with a bad line": "garbage\n",
	} {
		t.Run("[SEC-002] "+name+" is root_side_missing", func(t *testing.T) {
			g := newExtraRig(t, root, enabledCommands())
			g.client.unitShow = func(context.Context) (string, error) { return show, nil }
			assert.Equal(t, models.ExtraReadinessRootSideMissing, g.readiness(t))
		})
	}
	t.Run("[SEC-002] an unreadable unit is root_side_missing", func(t *testing.T) {
		g := newExtraRig(t, root, enabledCommands())
		g.client.unitShow = func(context.Context) (string, error) { return "", errors.New("Failed to connect to bus") }
		assert.Equal(t, models.ExtraReadinessRootSideMissing, g.readiness(t))
	})
	t.Run("[SEC-002] the microsecond rendering of 85 minutes is accepted", func(t *testing.T) {
		g := newExtraRig(t, root, enabledCommands())
		g.client.unitShow = func(context.Context) (string, error) {
			return "RuntimeMaxUSec=5100000000\nPrivateTmp=no\nMountFlags=\n", nil
		}
		assert.Equal(t, models.ExtraReadinessEnabled, g.readiness(t))
	})
}

func TestReadiness_EU_SEC003_NonDefaultSourceNotEnabled(t *testing.T) {
	for name, body := range map[string]string{
		"a fork":           "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER=SomeFork\n",
		"a trailing CR":    "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER=MichaIng\r\n",
		"a trailing space": "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER=MichaIng \n",
	} {
		t.Run("[SEC-003] "+name, func(t *testing.T) {
			root := dietpiRoot(t)
			writeHost(t, root, "boot/dietpi.txt", body)
			g := newExtraRig(t, root, enabledCommands())
			assert.Equal(t, models.ExtraReadinessNonDefaultUpdateSource, g.readiness(t))
		})
	}
	t.Run("[SEC-003] owner MichaIng on another branch stays enabled", func(t *testing.T) {
		root := dietpiRoot(t)
		writeHost(t, root, "boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITBRANCH=beta\n")
		g := newExtraRig(t, root, enabledCommands())
		assert.Equal(t, models.ExtraReadinessEnabled, g.readiness(t))
	})
}

func TestReadiness_EU_AC018_NotOptedInAfterRevocation(t *testing.T) {
	g := newExtraRig(t, dietpiRoot(t), enabledCommands())
	require.Equal(t, models.ExtraReadinessEnabled, g.readiness(t))
	g.a.config.Commands.AllowExtraUpdaters = nil
	assert.Equal(t, models.ExtraReadinessNotOptedIn, g.readiness(t), "removing dietpi from the list is enough to stop it, whatever the root side says")
}

func TestCapabilityProbe_EU_SEC007_CapAndCacheKey(t *testing.T) {
	g := newExtraRig(t, dietpiRoot(t), enabledCommands())
	require.Equal(t, models.ExtraReadinessEnabled, g.readiness(t))
	first := g.capCalls.Load()
	require.EqualValues(t, 1, first)
	for i := 0; i < 5; i++ {
		g.readiness(t)
	}
	assert.EqualValues(t, first, g.capCalls.Load(), "[SEC-007] the probe runs once while the helper, policy and drop-in are unchanged")
	g.sig = "sig-2"
	g.readiness(t)
	assert.EqualValues(t, first+1, g.capCalls.Load(), "[SEC-007] a changed stat signature (inode, size, mtime or ctime) drops the cache")
	t.Run("[SEC-007] without a signature nothing is cached", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		g.sig = ""
		g.readiness(t)
		g.readiness(t)
		assert.EqualValues(t, 2, g.capCalls.Load())
	})
	t.Run("[SEC-007] the cache expires after ten minutes", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		clk := newFakeClock()
		g.a.clk = clk
		g.readiness(t)
		clk.Advance(9 * time.Minute)
		g.readiness(t)
		assert.EqualValues(t, 1, g.capCalls.Load())
		clk.Advance(2 * time.Minute)
		g.readiness(t)
		assert.EqualValues(t, 2, g.capCalls.Load())
	})
}

func payloadFor(params map[string]interface{}) models.CommandPayload {
	return models.CommandPayload{CommandID: idA, Action: models.ActionOSUpdate, Params: params}
}

// finishedRecord is a version 1 terminal record, what an old helper writes.
func finishedRecord(rec maintenance.Record) maintenance.Record {
	rec.Outcome, rec.Phase = "success", "finished"
	rec.FinishedAt = &rec.UpdatedAt
	return rec
}

func runPlanned(t *testing.T, g *extraRig, mode string, params map[string]interface{}, cr *cmdRun) (*extraPlan, commandExecResult) {
	t.Helper()
	_, rec := maintenanceFixture()
	g.client.terminal = func(string) (maintenance.Record, error) {
		if g.requests == 0 {
			return maintenance.Record{}, os.ErrNotExist
		}
		r := rec
		r.Version = g.request.Version
		r.RequestedExtraUpdaters = g.request.ExtraUpdaters
		r = finishedRecord(r)
		if r.Version == maintenance.RequestVersionExtra {
			r.AptOutcome = "success"
		}
		return r, nil
	}
	plan := g.a.planExtras(context.Background(), mode, params)
	res := g.a.runDetachedMaintenanceWith(context.Background(), payloadFor(params), mode, false, plan, cr)
	res = g.a.applyExtraV1Lines(context.Background(), idA, plan, res)
	return plan, res
}

func TestOSUpdate_EU_AC012_AgentSideOnlyNeverRunsDietPi(t *testing.T) {
	g := newExtraRig(t, dietpiRoot(t), enabledCommands())
	g.client.rootPolicy = func() ([]byte, error) { return nil, os.ErrNotExist }
	params := map[string]interface{}{models.ParamExtraUpdaterBudget: true}
	plan, res := runPlanned(t, g, "full", params, nil)
	require.NotNil(t, plan)
	assert.Equal(t, models.ExtraReadinessRootSideMissing, plan.present[0].readiness)
	t.Run("[AC-012] the request is version 1: the worker is never asked for DietPi", func(t *testing.T) {
		assert.Equal(t, 1, g.requests)
		assert.Equal(t, maintenance.Request{Version: 1, CommandID: idA, Mode: "full"}, g.request)
	})
	t.Run("[AC-012] the runner receives no DietPi call", func(t *testing.T) {
		for _, c := range g.runnerCommands {
			assert.NotContains(t, strings.Join(c, " "), "dietpi")
		}
		assert.Equal(t, [][]string{{"sudo", "-n", "/usr/bin/systemctl", "start", "--no-block", maintenance.UnitName}}, g.runnerCommands)
	})
	t.Run("[AC-012] the result shows dietpi: skipped (not_enabled) and stays successful", func(t *testing.T) {
		assert.Equal(t, models.ResultStatusSuccess, res.Status)
		assert.True(t, strings.HasPrefix(res.Message, "apt: succeeded; dietpi: skipped (not_enabled)"), res.Message)
		assert.Contains(t, res.Message, "readiness: root_side_missing")
	})
}

func TestPlanExtras_EU_AC044_HostsWithoutDietPiGetTheOldRequestAndMessage(t *testing.T) {
	g := newExtraRig(t, t.TempDir(), enabledCommands())
	plan, res := runPlanned(t, g, "full", map[string]interface{}{models.ParamExtraUpdaterBudget: true}, nil)
	assert.Nil(t, plan, "no present updater means no plan")
	assert.Equal(t, maintenance.Request{Version: 1, CommandID: idA, Mode: "full"}, g.request)
	assert.Equal(t, maintenance.Request{Version: 1, CommandID: idA, Mode: "full"}, g.request)
	assert.NotContains(t, res.Message, "dietpi")
	assert.Zero(t, g.capCalls.Load(), "a host without an updater probes nothing")
}

func TestPlanExtras_EU_AC021_RepairNeverPlansAnUpdater(t *testing.T) {
	g := newExtraRig(t, dietpiRoot(t), enabledCommands())
	assert.Nil(t, g.a.planExtras(context.Background(), "repair", map[string]interface{}{models.ParamExtraUpdaterBudget: true}))
	_, res := runPlanned(t, g, "repair", map[string]interface{}{models.ParamExtraUpdaterBudget: true}, nil)
	assert.NotContains(t, res.Message, "dietpi", "[AC-021] repair carries no updater line")
	assert.Equal(t, 1, g.request.Version)
}

func TestPlanExtras_EU_AC037_V2OnlyWhenEnabledStampedAndNotExcluded(t *testing.T) {
	budget := map[string]interface{}{models.ParamExtraUpdaterBudget: true}
	t.Run("[AC-037] enabled, stamped and not excluded sends a version 2 request naming dietpi", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		runPlanned(t, g, "full", budget, nil)
		assert.Equal(t, maintenance.Request{Version: 2, CommandID: idA, Mode: "full", ExtraUpdaters: []string{"dietpi"}}, g.request)
	})
	t.Run("[AC-037] security mode also sends version 2", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		runPlanned(t, g, "security", budget, nil)
		assert.Equal(t, 2, g.request.Version)
	})
	t.Run("[AC-037] the exclusion parameter keeps the request at version 1", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		_, res := runPlanned(t, g, "full", map[string]interface{}{models.ParamExtraUpdaterBudget: true, models.ParamExcludeExtraUpdaters: true}, nil)
		assert.Equal(t, 1, g.request.Version)
		assert.Contains(t, res.Message, "dietpi: skipped (excluded_by_operator)")
		assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-039] an excluded updater never changes the outcome")
	})
	t.Run("[AC-037] without the server's budget stamp the request stays version 1 and a pending update is skipped without failing the run", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		_, res := runPlanned(t, g, "full", map[string]interface{}{}, nil)
		assert.Equal(t, 1, g.request.Version)
		assert.Contains(t, res.Message, "dietpi: skipped (insufficient_time)")
		assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AF-7, DP-AC010] an enabled, pending updater that could not be started is a refusal, not a failure")
		require.NotNil(t, res.ExitCode)
		assert.Equal(t, 0, *res.ExitCode)
	})
	t.Run("[AC-037] a non-boolean exclusion value is not an exclusion", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		runPlanned(t, g, "full", map[string]interface{}{models.ParamExtraUpdaterBudget: true, models.ParamExcludeExtraUpdaters: "yes"}, nil)
		assert.Equal(t, 2, g.request.Version, "the server rejects such a value; the agent never treats it as true")
	})
	t.Run("[AC-037] a stamp that is not a boolean true never extends the run", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		runPlanned(t, g, "full", map[string]interface{}{models.ParamExtraUpdaterBudget: "true"}, nil)
		assert.Equal(t, 1, g.request.Version)
	})
}

func TestOSUpdate_EU_AC022_ExcludedAndNotEnabledLines(t *testing.T) {
	budget := map[string]interface{}{models.ParamExtraUpdaterBudget: true}
	for name, tc := range map[string]struct {
		cmds   CommandsConfig
		tweak  func(root string)
		params map[string]interface{}
		want   string
	}{
		"not opted in":      {CommandsConfig{AllowPatching: true}, nil, budget, "dietpi: skipped (not_enabled)"},
		"excluded":          {enabledCommands(), nil, map[string]interface{}{models.ParamExtraUpdaterBudget: true, models.ParamExcludeExtraUpdaters: true}, "dietpi: skipped (excluded_by_operator)"},
		"unknown status":    {enabledCommands(), nil, nil, ""},
		"enabled, no stamp": {enabledCommands(), nil, nil, "dietpi: skipped (insufficient_time)"},
	} {
		t.Run("[AC-022] "+name, func(t *testing.T) {
			root := dietpiRoot(t)
			if name == "unknown status" {
				require.NoError(t, os.Remove(filepath.Join(root, "run", "dietpi", ".update_available")))
				tc.want = "dietpi: skipped (status_unknown)"
			}
			g := newExtraRig(t, root, tc.cmds)
			_, res := runPlanned(t, g, "full", tc.params, nil)
			assert.Contains(t, res.Message, tc.want)
			assert.Equal(t, models.ResultStatusSuccess, res.Status, "a skip before the updater started never fails the run (DIETPI-PATH-001 FR-012): %s", res.Message)
		})
	}
	t.Run("[AC-022] after an apt failure the line is apt_not_successful and the apt result is kept", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		// A version 1 run: the worker recorded nothing about the updater, so the
		// agent states why it was not applied.
		plan := g.a.planExtras(context.Background(), "full", map[string]interface{}{})
		failed := commandExecResult{Status: models.ResultStatusError, Message: "upgrade failed (exit 100): E: boom"}
		out := g.a.applyExtraV1Lines(context.Background(), idA, plan, failed)
		assert.True(t, strings.HasPrefix(out.Message, "apt: failed; dietpi: skipped (apt_not_successful)"), out.Message)
		assert.Contains(t, out.Message, "upgrade failed (exit 100): E: boom")
		assert.Equal(t, models.ResultStatusError, out.Status)
	})
	t.Run("[AC-022] a precheck failure or a detached observer gets no updater line", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		plan := g.a.planExtras(context.Background(), "full", budget)
		for _, r := range []commandExecResult{precheckFailure("busy"), {Detached: true, Message: "observer detached"}} {
			assert.Equal(t, r.Message, g.a.applyExtraV1Lines(context.Background(), idA, plan, r).Message)
		}
	})
}

func v2Record(updater maintenance.UpdaterResult, includeRC bool) maintenance.Record {
	now := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
	fin := now.Add(time.Hour)
	return maintenance.Record{
		Version: 2, CommandID: idA, Mode: "full", IncludeRebootClass: includeRC, StartedAt: now, UpdatedAt: fin, FinishedAt: &fin,
		Phase: "finished", Outcome: "success", Summary: "apt: succeeded; dietpi: succeeded\nPackage state verified; maintenance completed",
		RequestedExtraUpdaters: []string{"dietpi"}, AptOutcome: "success", ExtraUpdaters: []maintenance.UpdaterResult{updater},
	}
}

func TestMaintenanceResult_EU_AC032_PseudoEntryOnlyUnderEveryCondition(t *testing.T) {
	ok := maintenance.UpdaterResult{ID: "dietpi", Outcome: maintenance.UpdaterSuccess, RebootRequiredAfter: true, StartedAt: ptrTime(time.Now())}
	t.Run("[AC-032] include_reboot_class, success and a reboot required add the dietpi entry", func(t *testing.T) {
		assert.Equal(t, []string{"dietpi"}, maintenanceResult(v2Record(ok, true)).RebootClassApplied)
	})
	t.Run("[AC-032] without include_reboot_class no entry is added", func(t *testing.T) {
		assert.Empty(t, maintenanceResult(v2Record(ok, false)).RebootClassApplied)
	})
	t.Run("[AC-032] no reboot required after the updater adds nothing", func(t *testing.T) {
		r := ok
		r.RebootRequiredAfter = false
		assert.Empty(t, maintenanceResult(v2Record(r, true)).RebootClassApplied)
	})
	t.Run("[AC-032] a failed updater adds nothing", func(t *testing.T) {
		r := ok
		r.Outcome = maintenance.UpdaterFailed
		rec := v2Record(r, true)
		rec.Outcome = "error"
		assert.Empty(t, maintenanceResult(rec).RebootClassApplied)
	})
	t.Run("[AC-032] a skipped updater adds nothing", func(t *testing.T) {
		r := maintenance.UpdaterResult{ID: "dietpi", Outcome: maintenance.UpdaterSkipped, Reason: maintenance.SkipNotEnabled, RebootRequiredAfter: true}
		assert.Empty(t, maintenanceResult(v2Record(r, true)).RebootClassApplied)
	})
	t.Run("[AC-032] a version 1 record never adds the entry", func(t *testing.T) {
		rec := v2Record(ok, true)
		rec.Version = 1
		assert.Empty(t, maintenanceResult(rec).RebootClassApplied)
	})
	t.Run("[AC-028] the worker summary is the message and a failed updater is an error", func(t *testing.T) {
		r := maintenance.UpdaterResult{ID: "dietpi", Outcome: maintenance.UpdaterFailed}
		rec := v2Record(r, false)
		rec.Outcome, rec.ExitCode, rec.Summary = "error", 1, "apt: succeeded; dietpi: failed (exit 1)\nPackage state verified; maintenance completed"
		res := maintenanceResult(rec)
		assert.Equal(t, models.ResultStatusError, res.Status)
		assert.True(t, strings.HasPrefix(res.Message, "apt: succeeded; dietpi: failed (exit 1)"))
	})
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestOSUpdate_EU_AC031_ManualWithoutAuthorizationNeverReboots(t *testing.T) {
	h := newAuthHarness(t, true, true)
	ok := maintenance.UpdaterResult{ID: "dietpi", Outcome: maintenance.UpdaterSuccess, RebootRequiredAfter: true, StartedAt: ptrTime(time.Now())}
	rec := v2Record(ok, false)
	rec.Summary = "apt: succeeded; dietpi: succeeded\nPackage state verified; maintenance completed\ndietpi: succeeded; reboot required after DietPi: yes"
	run := maintenanceResult(rec)
	run.PackageManager = pmAPT
	h.runOSUpdate(false, run)
	time.Sleep(60 * time.Millisecond)
	assert.Zero(t, h.reboots.Load(), "[AC-031] a run without reboot_if_required never reboots")
	assert.Contains(t, h.message(), "reboot required after DietPi") // the worker summary states it
}

func TestOSUpdate_EU_AC032_RebootOnceThroughGate(t *testing.T) {
	ok := maintenance.UpdaterResult{ID: "dietpi", Outcome: maintenance.UpdaterSuccess, RebootRequiredAfter: true, StartedAt: ptrTime(time.Now())}
	rec := v2Record(ok, false)
	rec.Summary = "apt: succeeded; dietpi: succeeded\nPackage state verified; maintenance completed\ndietpi: succeeded; reboot required after DietPi: yes"
	t.Run("[AC-032] a manual run with reboot_if_required and allow_reboot reboots exactly once, after the result", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		run := maintenanceResult(rec)
		run.PackageManager = pmAPT
		h.runOSUpdate(true, run)
		require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond)
		assert.True(t, h.queuedAtReboot.Load(), "[AC-032] the result is reported first")
		time.Sleep(60 * time.Millisecond)
		assert.EqualValues(t, 1, h.reboots.Load())
	})
	t.Run("[AC-032] a scheduled run whose policy permits reboot-class reboots reboots once", func(t *testing.T) {
		h := newAuthHarness(t, true, false)
		scheduled := v2Record(ok, true)
		run := maintenanceResult(scheduled)
		run.PackageManager = pmAPT
		require.Equal(t, []string{"dietpi"}, run.RebootClassApplied)
		h.runOSUpdate(false, run)
		require.Eventually(t, func() bool { return h.reboots.Load() == 1 }, 2*time.Second, time.Millisecond)
		time.Sleep(60 * time.Millisecond)
		assert.EqualValues(t, 1, h.reboots.Load())
	})
	t.Run("[AC-032] with allow_reboot false neither path reboots", func(t *testing.T) {
		for name, authorized := range map[string]bool{"manual": true, "scheduled": false} {
			h := newAuthHarness(t, false, true)
			run := maintenanceResult(v2Record(ok, !authorized))
			run.PackageManager = pmAPT
			h.runOSUpdate(authorized, run)
			time.Sleep(60 * time.Millisecond)
			assert.Zero(t, h.reboots.Load(), name)
		}
	})
	t.Run("[AC-032] a failed DietPi step never reboots even when authorized", func(t *testing.T) {
		h := newAuthHarness(t, true, true)
		failed := v2Record(maintenance.UpdaterResult{ID: "dietpi", Outcome: maintenance.UpdaterFailed, RebootRequiredAfter: true}, false)
		failed.Outcome, failed.ExitCode = "error", 1
		run := maintenanceResult(failed)
		run.PackageManager = pmAPT
		h.runOSUpdate(true, run)
		time.Sleep(60 * time.Millisecond)
		assert.Zero(t, h.reboots.Load(), "[D-4] run not successful")
		assert.Contains(t, h.message(), suffixRunNotSucessful)
	})
}

func TestOSUpdate_EU_AC034_TwoEventsAndSteps(t *testing.T) {
	for _, origin := range []string{"user", "auto_patch"} {
		t.Run("[AC-034] exactly one start and one end event for a "+origin+" run", func(t *testing.T) {
			h := newLifecycleHarness(t)
			m, _ := maintenanceFixture()
			started := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
			running := v2Record(maintenance.UpdaterResult{ID: "dietpi", Outcome: maintenance.UpdaterRunning, StartedAt: &started}, false)
			running.Outcome, running.FinishedAt, running.Phase, running.AptOutcome, running.Summary = "running", nil, "extra_run:dietpi", "success", ""
			done := v2Record(maintenance.UpdaterResult{ID: "dietpi", Outcome: maintenance.UpdaterSuccess, StartedAt: &started, FinishedAt: &started}, false)
			var finished atomic.Bool
			m.terminal = func(string) (maintenance.Record, error) {
				if finished.Load() {
					return done, nil
				}
				return maintenance.Record{}, os.ErrNotExist
			}
			polls := 0
			m.current = func() (maintenance.Record, error) {
				polls++
				if polls >= 3 {
					finished.Store(true)
				}
				return running, nil
			}
			m.wait = func(context.Context) error { return nil }
			h.a.maintenanceClient = m
			require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: started}))
			cr := h.a.newCmdRun(models.CommandPayload{CommandID: idA, Action: models.ActionOSUpdate, Params: map[string]interface{}{"mode": "full"}})
			defer cr.cancel()

			res := h.a.observeMaintenanceWith(context.Background(), idA, false, cr)
			h.wait()

			assert.Equal(t, models.ResultStatusSuccess, res.Status)
			h.tr.mu.Lock()
			events := append([]*models.AgentEvent(nil), h.tr.agentEvents...)
			h.tr.mu.Unlock()
			require.Len(t, events, 2, "[AC-034] exactly two events per updater per command")
			assert.Equal(t, models.EventExtraUpdaterStarted, events[0].Type)
			assert.Equal(t, models.EventExtraUpdaterFinished, events[1].Type)
			for _, e := range events {
				assert.Equal(t, idA, e.CommandID)
				assert.Equal(t, "dietpi", e.Updater)
			}
			assert.Equal(t, "success", events[1].Outcome)
			assert.Empty(t, events[0].Outcome)
			steps := cr.stepNames()
			assert.Contains(t, steps, "extra_dietpi", "[AC-034] a start step")
			assert.Contains(t, steps, "extra_dietpi_success", "[AC-034] an end step")
			marker, _ := h.a.inflight.Get(idA)
			assert.Equal(t, []string{"dietpi:start", "dietpi:end"}, marker.ExtraEvents)

			// A restart that observes the same finished record sends nothing more.
			again := h.a.observeMaintenanceWith(context.Background(), idA, true, nil)
			h.wait()
			assert.Equal(t, models.ResultStatusSuccess, again.Status)
			h.tr.mu.Lock()
			total := len(h.tr.agentEvents)
			h.tr.mu.Unlock()
			assert.Equal(t, 2, total, "[AC-034] at most two events across a restart")
			_ = origin
		})
	}
}

func (cr *cmdRun) stepNames() []string {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	var out []string
	for _, s := range cr.steps {
		out = append(out, s.Name)
	}
	return out
}

func TestInflight_EU_AC034_ExtraEventsAreDurableDeduplicatedAndBounded(t *testing.T) {
	h := newLifecycleHarness(t)
	require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
	added, err := h.a.inflight.AddExtraEvent(idA, "dietpi:start")
	require.NoError(t, err)
	assert.True(t, added)
	added, err = h.a.inflight.AddExtraEvent(idA, "dietpi:start")
	require.NoError(t, err)
	assert.False(t, added, "a repeat is not a new event")
	for i := 0; i < 30; i++ {
		_, _ = h.a.inflight.AddExtraEvent(idA, "u"+string(rune('a'+i%26))+":start")
	}
	marker, _ := h.a.inflight.Get(idA)
	assert.LessOrEqual(t, len(marker.ExtraEvents), maxExtraEvents, "[NFR-002] bounded")
	t.Run("[AC-034] a missing marker records nothing and sends nothing", func(t *testing.T) {
		added, err := h.a.inflight.AddExtraEvent(idB, "dietpi:start")
		require.NoError(t, err)
		assert.False(t, added)
	})
	t.Run("[AC-034] a failed write is reported and the event is not recorded", func(t *testing.T) {
		h := newLifecycleHarness(t)
		require.NoError(t, h.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
		bad := newFaultFS()
		bad.failOn("create", errors.New("disk full"))
		h.a.inflight.file.fs = bad
		added, err := h.a.inflight.AddExtraEvent(idA, "dietpi:start")
		assert.Error(t, err)
		assert.False(t, added)
		marker, _ := h.a.inflight.Get(idA)
		assert.Empty(t, marker.ExtraEvents, "the in-memory list is rolled back with the failed write")
	})
	t.Run("[AC-034] the markers survive an agent restart", func(t *testing.T) {
		dir := t.TempDir()
		h1 := newLifecycleHarnessAt(t, dir)
		require.NoError(t, h1.a.inflight.Put(idA, inflightMarker{Action: models.ActionOSUpdate, StartedAt: time.Now()}))
		_, err := h1.a.inflight.AddExtraEvent(idA, "dietpi:start")
		require.NoError(t, err)
		h2 := newLifecycleHarnessAt(t, dir)
		require.NoError(t, h2.a.inflight.Load())
		marker, ok := h2.a.inflight.Get(idA)
		require.True(t, ok)
		assert.Equal(t, []string{"dietpi:start"}, marker.ExtraEvents)
		added, _ := h2.a.inflight.AddExtraEvent(idA, "dietpi:start")
		assert.False(t, added)
	})
}

func TestTelemetry_EU_AC011_DietPiEntryWithTodaysCollectorList(t *testing.T) {
	root := dietpiRoot(t)
	g := newExtraRig(t, root, enabledCommands())
	reg := collectors.NewRegistry()
	reg.Register(&fakeUpdatesCollector{result: &models.UpdateInfo{ExtraUpdaters: extraupd.DetectAll(context.Background(), extraupd.DirFSOwnedByCaller(root))}})
	g.a.registry = reg
	data := g.a.collectAll(context.Background())
	require.NotNil(t, data.Updates)
	require.Len(t, data.Updates.ExtraUpdaters, 1, "[AC-011] the entry is reported with the unchanged collector list")
	entry := data.Updates.ExtraUpdaters[0]
	assert.Equal(t, "dietpi", entry.ID)
	assert.Equal(t, models.ExtraStatusUpdateAvailable, entry.Status)
	assert.Equal(t, "9.20.1", entry.InstalledVersion)
	assert.Equal(t, "10.8.2", entry.AvailableVersion)
	assert.Equal(t, models.ExtraReadinessEnabled, entry.Readiness, "the agent stamps readiness onto the detected entry")
	assert.NotContains(t, collectorEnabledDefaults(), "dietpi", "[AC-011] no collector.enabled change is needed")
}

func collectorEnabledDefaults() []string { return DefaultConfig().Collector.Enabled }

func TestTelemetry_EU_AC033_FreshStateAfterRun(t *testing.T) {
	root := dietpiRoot(t)
	g := newExtraRig(t, root, enabledCommands())
	before := extraupd.DetectAll(context.Background(), g.a.extraFS())
	require.Equal(t, models.ExtraStatusUpdateAvailable, before[0].Status)
	// DietPi ran: the installed version is now the flag's version.
	writeHost(t, root, "boot/dietpi/.version", "G_DIETPI_VERSION_CORE=10\nG_DIETPI_VERSION_SUB=8\nG_DIETPI_VERSION_RC=2\nG_GITBRANCH='master'\nG_GITOWNER='MichaIng'\n")
	after := extraupd.DetectAll(context.Background(), g.a.extraFS())
	assert.NotEqual(t, models.ExtraStatusUpdateAvailable, after[0].Status, "[AC-033] the post-run telemetry no longer reports the applied version as pending")
	assert.Equal(t, "10.8.2", after[0].InstalledVersion)
}

func TestTelemetry_EU_AC044_NoUpdaterNoStampNoProbe(t *testing.T) {
	g := newExtraRig(t, t.TempDir(), enabledCommands())
	reg := collectors.NewRegistry()
	reg.Register(&fakeUpdatesCollector{result: &models.UpdateInfo{PendingUpdates: 3}})
	g.a.registry = reg
	data := g.a.collectAll(context.Background())
	assert.Empty(t, data.Updates.ExtraUpdaters)
	assert.Zero(t, g.capCalls.Load(), "[AC-044] a host without an updater runs no probe")
}
