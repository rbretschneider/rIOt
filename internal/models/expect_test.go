package models

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEX_AC008_PolicyTableExact(t *testing.T) {
	want := map[string]struct {
		class      WindowClass
		offline    bool
		containers ContainerCover
		settle     time.Duration
		post       time.Duration
	}{
		"reboot":                {WindowHost, true, CoverAllContainers, 3 * time.Minute, 15 * time.Minute},
		"os_update":             {WindowHost, true, CoverAllContainers, 3 * time.Minute, 15 * time.Minute},
		"shutdown":              {WindowShutdown, true, CoverAllContainers, 0, 15 * time.Minute},
		"agent_update":          {WindowAgent, true, CoverNoContainers, time.Minute, 10 * time.Minute},
		"docker_update":         {WindowContainer, false, CoverTargetContainers, 2 * time.Minute, 2 * time.Minute},
		"docker_bulk_update":    {WindowContainer, false, CoverTargetContainers, 2 * time.Minute, 2 * time.Minute},
		"docker_restart":        {WindowContainer, false, CoverTargetContainers, 2 * time.Minute, 2 * time.Minute},
		"docker_stack_redeploy": {WindowContainer, false, CoverTargetContainers, 2 * time.Minute, 2 * time.Minute},
		"docker_stack_restart":  {WindowContainer, false, CoverTargetContainers, 2 * time.Minute, 2 * time.Minute},
		"docker_stack_up":       {WindowContainer, false, CoverTargetContainers, 2 * time.Minute, 2 * time.Minute},
		"docker_stop":           {WindowContainerDown, false, CoverTargetContainers, 2 * time.Minute, 2 * time.Minute},
		"docker_stack_down":     {WindowContainerDown, false, CoverTargetContainers, 2 * time.Minute, 2 * time.Minute},
	}
	require.Len(t, ExpectActions(), len(want), "[AC-008] exactly the 12 actions of Table 4.1 open a window")
	for action, w := range want {
		p, ok := ExpectPolicy(action)
		require.True(t, ok, "[AC-008] %s must open a window", action)
		assert.Equal(t, w.class, p.Class, "[AC-008] class of %s", action)
		assert.Equal(t, w.offline, p.CoversOffline, "[AC-008] offline coverage of %s", action)
		assert.Equal(t, w.containers, p.Containers, "[AC-008] container coverage of %s", action)
		assert.Equal(t, w.settle, p.Settle, "[AC-008] settle of %s", action)
		assert.Equal(t, w.post, p.PostTerminal, "[AC-008] post-terminal period of %s", action)
	}
}

func TestEX_AC008_ActionsOutsideTableOpenNoWindow(t *testing.T) {
	for _, a := range []string{"docker_start", "docker_stack_pull", "docker_remove_items", "docker_inventory_refresh",
		"docker_check_updates", "enable_auto_updates", "fetch_logs", "run_device_probe", "agent_uninstall", "", "REBOOT"} {
		_, ok := ExpectPolicy(a)
		assert.False(t, ok, "[AC-008] %q must not open a window", a)
	}
}

func TestEX_AC008_MigrationIndexPredicateEqualsActions(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/riot-server/migrations/000033_expected_activity.up.sql")
	require.NoError(t, err)
	m := regexp.MustCompile(`(?s)idx_commands_window_pending.*?action IN \(([^)]*)\)`).FindStringSubmatch(string(raw))
	require.Len(t, m, 2, "index predicate must be found in migration 000033")
	var got []string
	for _, f := range strings.Split(m[1], ",") {
		got = append(got, strings.Trim(strings.TrimSpace(f), "'"))
	}
	assert.Equal(t, ExpectActions(), got, "[AD-012] the index predicate list must equal models.ExpectActions()")
	assert.Equal(t, strings.Join(wrapQuoted(got), ","), ExpectActionsSQL(), "ExpectActionsSQL renders the same list")
}

func wrapQuoted(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = "'" + s + "'"
	}
	return out
}

func TestEX_AC002_ActorPhraseOrigins(t *testing.T) {
	id := 7
	cases := []struct {
		name   string
		cmd    Command
		target string
		want   string
	}{
		{"user", Command{Origin: OriginUser, ActorName: "ryan"}, "", "by ryan"},
		{"user without a name", Command{Origin: OriginUser}, "", "by an operator"},
		{"auto update with target", Command{Origin: OriginAutoUpdate, PolicyID: &id}, "plex", "by auto-update policy for plex"},
		{"auto update target unresolved", Command{Origin: OriginAutoUpdate, PolicyID: &id}, "", "by auto-update policy #7"},
		{"auto update without policy id", Command{Origin: OriginAutoUpdate}, "", "by an auto-update policy"},
		{"auto patch", Command{Origin: OriginAutoPatch, ActorName: "ignored"}, "", "by the auto-patch schedule"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, ActorPhrase(tc.cmd, tc.target), "[AC-002] %s", tc.name)
	}
}

func TestEX_AC046_SafeName(t *testing.T) {
	long := strings.Repeat("a", 300) + "\x00\x01\x1b[31m" + string([]byte{0xff, 0xfe}) + "tail"
	got := SafeName(long)
	assert.LessOrEqual(t, len(got), 128, "[AC-046] name part at most 128 bytes")
	assert.True(t, utf8.ValidString(got), "[AC-046] valid UTF-8")
	for _, r := range got {
		assert.True(t, unicode.IsPrint(r), "[AC-046] printable rune %U", r)
	}
	assert.Equal(t, "plex", SafeName("pl\x00ex"), "[AC-046] NUL removed")
	assert.Equal(t, "plex", SafeName("pl\x07e\x7fx"), "[AC-046] controls and DEL removed")
	assert.True(t, utf8.ValidString(SafeName("a\xffb")), "[AC-046] invalid UTF-8 repaired")
}

func TestEX_SEC006_SafeNameStripsFormatChars(t *testing.T) {
	assert.Equal(t, "plex...", SafeName("plex‮..."), "[SEC-006] bidi override stripped")
	assert.Equal(t, "plex", SafeName("pl​ex"), "[SEC-006] zero-width space stripped")
	assert.Equal(t, "plex", SafeName("\ufeffplex"), "[SEC-006] BOM stripped")
	assert.Equal(t, "plex", SafeName("p⁦l⁩ex"), "[SEC-006] isolate controls stripped")
	assert.Equal(t, "ab", SafeName("a b"), "line separator stripped")
	assert.Equal(t, "plex-é", SafeName("plex-é"), "ordinary non-ASCII letters survive")
}

func TestEX_SEC006_ContainerNameKeyCollisionFreeAndIdempotent(t *testing.T) {
	assert.Equal(t, "plex", ContainerNameKey("/plex"), "[SEC-006] ordinary name loses only its slash")
	assert.Equal(t, "my-app_1", ContainerNameKey("my-app_1"), "ordinary names are unchanged")

	prefix := strings.Repeat("x", 128)
	a, b := ContainerNameKey(prefix+"A"), ContainerNameKey(prefix+"B")
	assert.NotEqual(t, a, b, "[SEC-006] 129-byte names sharing a 128-byte prefix keep distinct keys")
	assert.LessOrEqual(t, len(a), 128)
	assert.LessOrEqual(t, len(b), 128)

	spoof := ContainerNameKey("plex‮")
	assert.NotEqual(t, "plex", spoof, "[SEC-006] a name altered by sanitizing must not equal the clean name")
	assert.NotEqual(t, ContainerNameKey("plex​"), spoof, "[SEC-006] two different altered names stay distinct")

	for _, raw := range []string{"plex", prefix + "A", "plex‮", "a\x00b", string([]byte{0xff, 0xfe, 'a'}), strings.Repeat("é", 200)} {
		k := ContainerNameKey(raw)
		assert.Equal(t, k, ContainerNameKey(k), "[SEC-006] key of %q must be idempotent", raw)
		assert.True(t, utf8.ValidString(k), "[SEC-006] valid UTF-8")
		assert.LessOrEqual(t, len(k), 128, "[SEC-006] at most 128 bytes")
	}
}

func TestEX_SEC006_MarkerTargetBounded(t *testing.T) {
	prefix := strings.Repeat("x", 128)
	t1 := EscalationTarget(EscContainer, ContainerNameKey(prefix+"A"))
	t2 := EscalationTarget(EscContainer, ContainerNameKey(prefix+"B"))
	assert.NotEqual(t, t1, t2, "[SEC-006] distinct containers never share a marker")
	assert.LessOrEqual(t, len(t1), 42, "[SEC-006] marker target at most 42 bytes")
	assert.True(t, IsContainerEscalationTarget(t1))
	assert.Equal(t, "device", EscalationTarget(EscDeviceNotBack, ""))
	assert.Equal(t, "device", EscalationTarget(EscDeviceNonSuccess, ""))
	assert.Equal(t, "summary", EscalationTarget(EscContainerSummary, ""))
	assert.False(t, IsContainerEscalationTarget("device"))
}

func TestEX_SEC004_DeviceIsLive(t *testing.T) {
	assert.True(t, DeviceIsLive(DeviceStatusOnline), "[SEC-004] online is live")
	assert.True(t, DeviceIsLive(DeviceStatusWarning), "[SEC-004] warning is live")
	assert.False(t, DeviceIsLive(DeviceStatusOffline), "[SEC-004] offline is not live")
}

func TestEX_Messages_Templates(t *testing.T) {
	at := time.Date(2026, 10, 26, 14, 2, 0, 0, time.UTC)
	cov := Coverage{CommandID: "1a2b3c4d-0000", ShortID: "1a2b3c4d", ActionLabel: "Docker update", ActorPhrase: "by ryan"}
	assert.Equal(t, "Container plex died during Docker update by ryan (expected)", cov.DeathMessage("plex"))
	assert.Equal(t, "Container plex OOM killed (during Docker update by ryan, command 1a2b3c4d)", cov.OOMMessage("plex"))
	cov.Target = "media"
	assert.Equal(t, "Container plex died during Docker update of media by ryan (expected)", cov.DeathMessage("plex"))

	off := OfflineCoverage{Kind: OfflineKindWindow, ActionLabel: "Reboot", ActorPhrase: "by ryan"}
	assert.Equal(t, "Device vermont went offline during Reboot by ryan (expected)", off.Message("vermont"))
	back := time.Date(2026, 10, 26, 14, 32, 0, 0, time.UTC)
	off.BackBy = &back
	assert.Equal(t, "Device vermont went offline during Reboot by ryan (expected, back by 2:32 PM UTC)", off.Message("vermont"))
	po := OfflineCoverage{Kind: OfflineKindPoweredOff, ActorPhrase: "by ryan", ShutdownAt: at}
	assert.Equal(t, "Device PiCUPS powered off by ryan at 2:02 PM UTC", po.Message("PiCUPS"))

	n := OnlineNote{ActionLabel: "Reboot", ActorPhrase: "by ryan", Offline: 100 * time.Second}
	assert.Equal(t, "Device vermont came online after Reboot by ryan (offline 1m40s)", n.Message("vermont"))
	n.PoweredOff, n.Offline = true, 72*time.Hour
	assert.Equal(t, "Device PiCUPS came online after Shutdown by ryan (powered off 72h)", n.Message("PiCUPS"))
}

func TestEX_CommandTargetLabel(t *testing.T) {
	c := Command{Action: ActionDockerStackRedeploy, TargetKeys: []string{"docker:compose:/srv/media/"}}
	assert.Equal(t, "media", CommandTargetLabel(c))
	assert.Equal(t, "", CommandTargetLabel(Command{Action: ActionDockerUpdate, TargetKeys: []string{"docker:container:plex"}}))
}

func TestEX_EscalationRequest_EventType(t *testing.T) {
	assert.Equal(t, EventDeviceOffline, EscalationRequest{Kind: EscDeviceNotBack}.EventType())
	assert.Equal(t, EventDeviceOffline, EscalationRequest{Kind: EscDeviceNoResult}.EventType())
	assert.Equal(t, EventContainerDied, EscalationRequest{Kind: EscContainer}.EventType())
	assert.Equal(t, EventContainerDied, EscalationRequest{Kind: EscContainerSummary}.EventType())
}
