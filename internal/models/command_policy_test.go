package models

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var t0 = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func TestCL_AC007_BackoffTable(t *testing.T) {
	cases := []struct {
		n    int
		want time.Duration
	}{
		{-3, 0},
		{0, 0},
		{1, 15 * time.Minute},
		{2, 30 * time.Minute},
		{3, 60 * time.Minute},
		{4, 2 * time.Hour},
		{5, 4 * time.Hour},
		{6, 6 * time.Hour},
		{7, 6 * time.Hour},
		{10, 6 * time.Hour},
		{64, 6 * time.Hour},
		{1 << 30, 6 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("n=%d", tc.n), func(t *testing.T) {
			if got := AutoUpdateBackoff(tc.n); got != tc.want {
				t.Errorf("AutoUpdateBackoff(%d) = %v, want %v", tc.n, got, tc.want)
			}
		})
	}
}

func TestCL_AC007_BackoffNeverDecreases(t *testing.T) {
	prev := time.Duration(0)
	for n := 0; n < 100; n++ {
		got := AutoUpdateBackoff(n)
		if got < prev {
			t.Fatalf("backoff decreased at n=%d: %v < %v", n, got, prev)
		}
		prev = got
	}
}

func TestCL_AC008_PolicyBackoffGate(t *testing.T) {
	failedAt := t0
	p := AutoUpdatePolicy{ConsecutiveFailures: 1, LastFailureAt: &failedAt}
	if !p.InBackoff(t0.Add(14 * time.Minute)) {
		t.Error("T+14m with n=1 must still be in backoff")
	}
	if p.InBackoff(t0.Add(15 * time.Minute)) {
		t.Error("exactly T+15m is the first eligible instant")
	}
	if p.InBackoff(t0.Add(16 * time.Minute)) {
		t.Error("T+16m with n=1 must be eligible")
	}
	if got := *p.BackoffUntil(); !got.Equal(t0.Add(15 * time.Minute)) {
		t.Errorf("BackoffUntil = %v, want T+15m", got)
	}
	none := AutoUpdatePolicy{}
	if none.InBackoff(t0) || none.BackoffUntil() != nil {
		t.Error("a policy with no failures is never in backoff")
	}
	reset := AutoUpdatePolicy{ConsecutiveFailures: 0, LastFailureAt: &failedAt}
	if reset.InBackoff(t0) {
		t.Error("n=0 clears the backoff even if a stale last_failure_at remains")
	}
}

func TestCL_AC029_ActionClassMapping(t *testing.T) {
	cases := map[string]CommandClass{
		"reboot":               ClassPower,
		"shutdown":             ClassPower,
		"os_update":            ClassOSPatch,
		"enable_auto_updates":  ClassOSPatch,
		"docker_update":        ClassDockerUpdate,
		"docker_bulk_update":   ClassDockerBulk,
		"agent_update":         ClassAgentUpdate,
		"docker_stop":          ClassOther,
		"docker_start":         ClassOther,
		"docker_restart":       ClassOther,
		"docker_check_updates": ClassOther,
		"fetch_logs":           ClassOther,
		"run_device_probe":     ClassOther,
		"agent_uninstall":      ClassOther,
		"":                     ClassOther,
		"nonsense":             ClassOther,
	}
	for action, want := range cases {
		if got := ActionClass(action); got != want {
			t.Errorf("ActionClass(%q) = %q, want %q", action, got, want)
		}
	}
}

func TestCL_AC029_ExpiresAtPerActionClass(t *testing.T) {
	cases := []struct {
		action string
		ttl    time.Duration
	}{
		{"reboot", 15 * time.Minute},
		{"shutdown", 15 * time.Minute},
		{"os_update", 30 * time.Minute},
		{"enable_auto_updates", 30 * time.Minute},
		{"docker_update", 60 * time.Minute},
		{"docker_bulk_update", 60 * time.Minute},
		{"agent_update", 60 * time.Minute},
		{"docker_restart", 30 * time.Minute},
		{"fetch_logs", 30 * time.Minute},
		{"run_device_probe", 30 * time.Minute},
		{"docker_check_updates", 30 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			if got := QueuedTTL(tc.action); got != tc.ttl {
				t.Errorf("QueuedTTL(%s) = %v, want %v", tc.action, got, tc.ttl)
			}
			if got := ExpiresAt(tc.action, t0, time.Time{}); !got.Equal(t0.Add(tc.ttl)) {
				t.Errorf("ExpiresAt(%s) = %v, want T+%v", tc.action, got, tc.ttl)
			}
		})
	}
}

func TestCL_AC030_ExpiresAtCappedAtWindowEnd(t *testing.T) {
	end := t0.Add(10 * time.Minute)
	if got := ExpiresAt("os_update", t0, end); !got.Equal(end) {
		t.Errorf("window ending in 10m must cap a 30m TTL: got %v, want %v", got, end)
	}
	if got := ExpiresAt("reboot", t0, t0.Add(time.Hour)); !got.Equal(t0.Add(15 * time.Minute)) {
		t.Errorf("a window farther away than the TTL must not extend it: got %v", got)
	}
	if got := ExpiresAt("reboot", t0, t0.Add(15*time.Minute)); !got.Equal(t0.Add(15 * time.Minute)) {
		t.Errorf("window end equal to the TTL: got %v", got)
	}
	if got := ExpiresAt("os_update", t0, time.Time{}); !got.Equal(t0.Add(30 * time.Minute)) {
		t.Errorf("zero windowEnd means no cap: got %v", got)
	}
	if got := ExpiresAt("os_update", t0, t0.Add(-time.Minute)); !got.Equal(t0) {
		t.Errorf("a window that already ended must yield a born-expired command (expires_at = now): got %v", got)
	}
}

func TestCL_AC029_ExecDeadlineTable(t *testing.T) {
	cases := []struct {
		action string
		n      int
		want   time.Duration
	}{
		{"reboot", 0, 20 * time.Minute}, // REBOOT-DPKG-001 AD-004: covers the 10m dpkg wait
		{"shutdown", 0, 10 * time.Minute},
		{"os_update", 0, 40 * time.Minute},
		{"enable_auto_updates", 0, 40 * time.Minute},
		{"docker_update", 0, 35 * time.Minute},
		{"agent_update", 0, 15 * time.Minute},
		{"fetch_logs", 0, 15 * time.Minute},
		{"docker_bulk_update", 0, 40 * time.Minute},  // N<1 treated as 1: 35m+5m
		{"docker_bulk_update", -4, 40 * time.Minute}, // never negative
		{"docker_bulk_update", 1, 40 * time.Minute},
		{"docker_bulk_update", 2, 75 * time.Minute},
		{"docker_bulk_update", 10, 355 * time.Minute},
		{"docker_bulk_update", 11, 6 * time.Hour},                 // 390m > 360m cap
		{"docker_bulk_update", 1000, 6 * time.Hour},               // cap
		{"docker_bulk_update", int(^uint(0) >> 1), 6 * time.Hour}, // overflow-safe
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/n=%d", tc.action, tc.n), func(t *testing.T) {
			if got := ExecDeadline(tc.action, tc.n); got != tc.want {
				t.Errorf("ExecDeadline(%s,%d) = %v, want %v", tc.action, tc.n, got, tc.want)
			}
			if got := ExecDeadlineSeconds(tc.action, tc.n); got != int(tc.want/time.Second) {
				t.Errorf("ExecDeadlineSeconds(%s,%d) = %d, want %d", tc.action, tc.n, got, int(tc.want/time.Second))
			}
		})
	}
}

func TestCL_DeadlineInvariant(t *testing.T) {
	actions := []string{
		"reboot", "shutdown", "os_update", "enable_auto_updates", "docker_update",
		"docker_bulk_update", "agent_update", "docker_stop", "fetch_logs", "run_device_probe",
	}
	for _, action := range actions {
		for _, n := range []int{1, 2, 5, 10, 11, 50, 500} {
			if action != "docker_bulk_update" && n > 1 {
				continue
			}
			t.Run(fmt.Sprintf("%s/n=%d", action, n), func(t *testing.T) {
				deadline := ExecDeadline(action, n)
				agentMax := AgentHardMax(action, n)
				if agentMax <= 0 {
					t.Fatalf("AgentHardMax(%s,%d) = %v must be positive", action, n, agentMax)
				}
				if deadline <= agentMax+DeadlineSlack {
					t.Errorf("deadline %v must exceed agent max %v + %v", deadline, agentMax, DeadlineSlack)
				}
			})
		}
	}
}

func TestCL_DeadlineInvariant_ComposeWorstCaseIs29m55s(t *testing.T) {
	if ComposeMaxWallTime != 29*time.Minute+55*time.Second {
		t.Errorf("ComposeMaxWallTime = %v, ADD AD-013 says 29m55s", ComposeMaxWallTime)
	}
	if AgentHardMax("docker_update", 1) != ComposeMaxWallTime {
		t.Errorf("docker_update agent hard max must be the compose worst case")
	}
	if BulkContainerWorstCase >= ComposeMaxWallTime {
		t.Errorf("bulk per-container worst case %v must sit below the compose worst case %v", BulkContainerWorstCase, ComposeMaxWallTime)
	}
}

func TestCL_DeadlineInvariant_BulkBudget(t *testing.T) {
	for _, n := range []int{1, 2, 10, 11, 100} {
		budget := BulkAgentBudget(n)
		if budget != ExecDeadline("docker_bulk_update", n)-5*time.Minute-30*time.Second {
			t.Errorf("BulkAgentBudget(%d) = %v, want deadline-5m30s", n, budget)
		}
		if budget < BulkContainerWorstCase {
			t.Errorf("BulkAgentBudget(%d) = %v cannot fit even one container (%v)", n, budget, BulkContainerWorstCase)
		}
	}
}

func TestCL_AC029_AgentBudgetConstants(t *testing.T) {
	cases := map[string]time.Duration{
		"AckBudget":                AckBudget,
		"DockerPrecheckBudget":     DockerPrecheckBudget,
		"ComposePullLimit":         ComposePullLimit,
		"ComposeUpLimit":           ComposeUpLimit,
		"StandalonePullLimit":      StandalonePullLimit,
		"ChildGrace":               ChildGrace,
		"StopDependentsBudget":     StopDependentsBudget,
		"VerifyWindow":             VerifyWindow,
		"VerifyBudget":             VerifyBudget,
		"DependentRestoreBudget":   DependentRestoreBudget,
		"StandaloneRecreateBudget": StandaloneRecreateBudget,
		"RollbackBudget":           RollbackBudget,
		"CleanupBudget":            CleanupBudget,
		"AgentShutdownGrace":       AgentShutdownGrace,
	}
	want := map[string]time.Duration{
		"AckBudget": 5 * time.Second, "DockerPrecheckBudget": 15 * time.Second,
		"ComposePullLimit": 15 * time.Minute, "ComposeUpLimit": 10 * time.Minute,
		"StandalonePullLimit": 15 * time.Minute, "ChildGrace": 60 * time.Second,
		"StopDependentsBudget": 20 * time.Second, "VerifyWindow": 10 * time.Second,
		"VerifyBudget": 15 * time.Second, "DependentRestoreBudget": 2 * time.Minute,
		"StandaloneRecreateBudget": 40 * time.Second, "RollbackBudget": 60 * time.Second,
		"CleanupBudget": 15 * time.Second, "AgentShutdownGrace": 20 * time.Second,
	}
	for name, got := range cases {
		if got != want[name] {
			t.Errorf("%s = %v, ADD AD-013 says %v", name, got, want[name])
		}
	}
	if AgentShutdownGrace >= 90*time.Second {
		t.Error("AgentShutdownGrace must stay below systemd's default 90s TimeoutStopSec")
	}
}

func TestCL_AC029_FormatDurationShort(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{35 * time.Minute, "35m"},
		{70 * time.Minute, "1h10m"},
		{6 * time.Hour, "6h"},
		{15 * time.Minute, "15m"},
		{40 * time.Minute, "40m"},
		{31*time.Minute + 30*time.Second, "31m30s"},
		{45 * time.Second, "45s"},
		{time.Hour, "1h"},
		{time.Hour + 5*time.Second, "1h5s"},
		{1500 * time.Millisecond, "1s"},
		{500 * time.Millisecond, "0s"},
		{0, "0s"},
		{-time.Minute, "0s"},
	}
	for _, tc := range cases {
		if got := FormatDurationShort(tc.d); got != tc.want {
			t.Errorf("FormatDurationShort(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestCL_AC029_OutboxBackoff(t *testing.T) {
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{-1, 0}, {0, 0},
		{1, 5 * time.Second}, {2, 10 * time.Second}, {3, 20 * time.Second},
		{6, 160 * time.Second}, {7, 5 * time.Minute}, {8, 5 * time.Minute}, {1000, 5 * time.Minute},
	}
	for _, tc := range cases {
		if got := OutboxBackoff(tc.attempts); got != tc.want {
			t.Errorf("OutboxBackoff(%d) = %v, want %v", tc.attempts, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Target keys (AD-004)
// ---------------------------------------------------------------------------

func TestCL_AD004_TargetKeyHelpers(t *testing.T) {
	cases := []struct{ name, got, want string }{
		{"host", TargetKeyHost, "host"},
		{"compose verbatim", TargetKeyCompose("/opt/stacks/web"), "docker:compose:/opt/stacks/web"},
		{"compose trailing slash", TargetKeyCompose("/opt/stacks/web/"), "docker:compose:/opt/stacks/web"},
		{"compose many trailing slashes", TargetKeyCompose("/opt/stacks/web//"), "docker:compose:/opt/stacks/web"},
		{"compose keeps case and spaces", TargetKeyCompose("/opt/My Stack"), "docker:compose:/opt/My Stack"},
		{"container", TargetKeyContainer("web"), "docker:container:web"},
		{"container leading slash", TargetKeyContainer("/web"), "docker:container:web"},
		{"container id", TargetKeyContainerID("abc123"), "docker:id:abc123"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestCL_AD004_TargetKeyForContainer(t *testing.T) {
	compose := map[string]string{
		LabelComposeProject: "web", LabelComposeService: "app", LabelComposeWorkingDir: "/opt/stacks/web/",
	}
	cases := []struct {
		name string
		c    ContainerInfo
		want string
	}{
		{"compose container", ContainerInfo{Name: "/web-app-1", Labels: compose}, "docker:compose:/opt/stacks/web"},
		{"standalone", ContainerInfo{Name: "/vpn"}, "docker:container:vpn"},
		{"project without working_dir", ContainerInfo{Name: "x", Labels: map[string]string{
			LabelComposeProject: "p", LabelComposeService: "s"}}, "docker:container:x"},
		{"working_dir without service", ContainerInfo{Name: "x", Labels: map[string]string{
			LabelComposeProject: "p", LabelComposeWorkingDir: "/wd"}}, "docker:container:x"},
		{"no name falls back to id", ContainerInfo{ID: "deadbeef"}, "docker:id:deadbeef"},
		{"slash-only name falls back to id", ContainerInfo{ID: "deadbeef", Name: "/"}, "docker:id:deadbeef"},
	}
	for _, tc := range cases {
		if got := TargetKeyForContainer(tc.c); got != tc.want {
			t.Errorf("%s: TargetKeyForContainer = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCL_AD004_HostActions(t *testing.T) {
	for _, a := range []string{"reboot", "shutdown", "os_update", "enable_auto_updates", "agent_update"} {
		if !IsHostAction(a) {
			t.Errorf("%s must be a host action", a)
		}
		if k := HostTargetKeys(a); len(k) != 1 || k[0] != "host" {
			t.Errorf("HostTargetKeys(%s) = %v, want [host]", a, k)
		}
	}
	for _, a := range []string{"docker_update", "docker_bulk_update", "docker_stop", "docker_restart", "fetch_logs", "agent_uninstall", "run_device_probe", ""} {
		if IsHostAction(a) {
			t.Errorf("%q must not be a host action (only docker_update/bulk and host actions carry keys)", a)
		}
		if k := HostTargetKeys(a); k != nil {
			t.Errorf("HostTargetKeys(%q) = %v, want nil", a, k)
		}
	}
}

func TestCL_AD004_NormalizeTargetKeys(t *testing.T) {
	got := NormalizeTargetKeys("b", "", "a", "b", "a")
	if strings.Join(got, ",") != "b,a" {
		t.Errorf("NormalizeTargetKeys = %v, want [b a]", got)
	}
	if empty := NormalizeTargetKeys(); empty == nil || len(empty) != 0 {
		t.Errorf("NormalizeTargetKeys() = %#v, want a non-nil empty slice", empty)
	}
	if !IsDockerTargetKey(TargetKeyCompose("/x")) || !IsDockerTargetKey(TargetKeyContainer("x")) ||
		!IsDockerTargetKey(TargetKeyContainerID("x")) || IsDockerTargetKey(TargetKeyHost) {
		t.Error("IsDockerTargetKey must hold for every docker: key and fail for host")
	}
}

// ---------------------------------------------------------------------------
// Report bounds, reasons, steps, UUID (SEC C-1 / C-7)
// ---------------------------------------------------------------------------

func TestCL_SEC001_ReportBoundConstants(t *testing.T) {
	if MaxReportStepBytes != 64 || MaxProgressMessageBytes != 1024 || MaxResultMsgBytes != 65536 ||
		MaxStderrTailBytes != 8192 || MaxReportOutputBytes != 262144 || MaxTargetKeyFilterBytes != 512 {
		t.Errorf("report bounds drifted from ADD 15.1: %d %d %d %d %d %d",
			MaxReportStepBytes, MaxProgressMessageBytes, MaxResultMsgBytes,
			MaxStderrTailBytes, MaxReportOutputBytes, MaxTargetKeyFilterBytes)
	}
	if MaxReportBodyBytes != 512*1024 || MaxAgentWSMessageBytes != 4*1024*1024 {
		t.Errorf("transport caps drifted: body %d ws %d", MaxReportBodyBytes, MaxAgentWSMessageBytes)
	}
}

func TestCL_SEC001_StepPattern(t *testing.T) {
	ok := []string{"precheck", "stop_dependents", "a", "step_1", strings.Repeat("a", 64), "0", "_"}
	bad := []string{
		"", "Pull", "pull ", " pull", "pull-now", "pull\n", "stop dependents", "résumé",
		strings.Repeat("a", 65), "pull\x00", "../x", "<img>", "pull;",
	}
	for _, s := range ok {
		if !StepPattern.MatchString(s) {
			t.Errorf("StepPattern must accept %q", s)
		}
		if SanitizeStep(s) != s {
			t.Errorf("SanitizeStep(%q) must keep a valid step", s)
		}
	}
	for _, s := range bad {
		if StepPattern.MatchString(s) {
			t.Errorf("StepPattern must reject %q", s)
		}
		if got := SanitizeStep(s); got != "" {
			t.Errorf("SanitizeStep(%q) = %q, want \"\" (stored empty, transition still applies)", s, got)
		}
	}
}

func TestCL_SEC001_StepVocabularyMatchesPattern(t *testing.T) {
	for _, s := range []string{StepPrecheck, StepResolve, StepPull, StepStopDependents, StepRecreate,
		StepVerify, StepRestartDependents, StepRollback, StepCleanup} {
		if SanitizeStep(s) != s {
			t.Errorf("vocabulary step %q does not satisfy StepPattern", s)
		}
	}
}

func TestCL_SEC001_ReasonEnum(t *testing.T) {
	want := []string{"timeout", "rolled_back", "restarted", "conflict", "shutdown", "persistence"}
	if strings.Join(ReportReasons, ",") != strings.Join(want, ",") {
		t.Errorf("ReportReasons = %v, want %v", ReportReasons, want)
	}
	for _, r := range want {
		if SanitizeReason(r) != r {
			t.Errorf("SanitizeReason(%q) must keep an enum value", r)
		}
	}
	for _, r := range []string{"", "Timeout", "timeout ", "other", "<script>", "timeout\x00"} {
		if got := SanitizeReason(r); got != "" {
			t.Errorf("SanitizeReason(%q) = %q, want \"\"", r, got)
		}
	}
}

func TestCL_SEC008_DedupeCriticalActions(t *testing.T) {
	for _, a := range []string{"reboot", "shutdown", "os_update", "agent_update"} {
		if !IsDedupeCriticalAction(a) {
			t.Errorf("%s must be dedupe-critical", a)
		}
	}
	for _, a := range []string{"docker_update", "fetch_logs", "docker_restart", "agent_uninstall", "enable_auto_updates", ""} {
		if IsDedupeCriticalAction(a) {
			t.Errorf("%q must not be dedupe-critical", a)
		}
	}
	if len(DedupeCriticalActions) != 4 {
		t.Errorf("DedupeCriticalActions has %d entries, want 4", len(DedupeCriticalActions))
	}
}

func TestCL_SEC007_IsCanonicalUUID(t *testing.T) {
	good := []string{
		"123e4567-e89b-12d3-a456-426614174000",
		"00000000-0000-0000-0000-000000000000",
		"ffffffff-ffff-ffff-ffff-ffffffffffff",
	}
	bad := map[string]string{
		"empty":             "",
		"uppercase":         "123E4567-E89B-12D3-A456-426614174000",
		"mixed case":        "123e4567-e89b-12d3-a456-42661417400A",
		"no dashes":         "123e4567e89b12d3a456426614174000",
		"braces":            "{123e4567-e89b-12d3-a456-426614174000}",
		"urn":               "urn:uuid:123e4567-e89b-12d3-a456-426614174000",
		"too short":         "123e4567-e89b-12d3-a456-42661417400",
		"too long":          "123e4567-e89b-12d3-a456-4266141740000",
		"dash misplaced":    "123e456-7e89b-12d3-a456-426614174000",
		"non-hex":           "123e4567-e89b-12d3-a456-42661417400g",
		"path traversal":    "../../telemetry",
		"trailing newline":  "123e4567-e89b-12d3-a456-426614174000\n",
		"leading space":     " 123e4567-e89b-12d3-a456-42661417400",
		"nul inside":        "123e4567-e89b-12d3-a456-42661417400\x00",
		"sql":               "x'; DROP TABLE commands;--",
		"1 MiB":             strings.Repeat("a", 1<<20),
		"multibyte 36 byte": strings.Repeat("é", 18),
		"urlencoded slash":  "123e4567-e89b-12d3-a456-4266141%2f00",
	}
	for _, g := range good {
		if !IsCanonicalUUID(g) {
			t.Errorf("IsCanonicalUUID(%q) = false, want true", g)
		}
	}
	for name, b := range bad {
		if IsCanonicalUUID(b) {
			t.Errorf("%s: IsCanonicalUUID rejected-case accepted (len %d)", name, len(b))
		}
	}
}

// ---------------------------------------------------------------------------
// Sanitizer and truncation (SEC C-2)
// ---------------------------------------------------------------------------

func TestCL_SEC002_SanitizeTextTable(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"clean ascii", "hello", "hello"},
		{"clean multibyte", "héllo wörld 日本語 🚀", "héllo wörld 日本語 🚀"},
		{"empty", "", ""},
		{"single NUL", "a\x00b", "ab"},
		{"only NULs", "\x00\x00\x00", ""},
		{"leading and trailing NUL", "\x00abc\x00", "abc"},
		{"lone continuation byte", "a\x80b", "a�b"},
		{"lone lead byte", "a\xE2b", "a�b"},
		{"truncated 3-byte sequence at end", "ok\xE2\x82", "ok�"},
		{"run of invalid bytes becomes one replacement", "a\xff\xfe\xfdb", "a�b"},
		{"overlong encoding is one invalid run", "a\xC0\x80b", "a�b"},
		{"encoded surrogate", "a\xED\xA0\x80b", "a�b"},
		{"NUL inside a valid rune is removed and the rune rejoins", "\xE2\x00\x82\xAC", "€"},
		{"NUL inside a truncated rune leaves it invalid", "\xE2\x00\x82", "�"},
		{"valid rune after invalid", "\xffé", "�é"},
		{"control chars other than NUL kept", "a\tb\nc\x01d\x7f", "a\tb\nc\x01d\x7f"},
		{"replacement char already present kept", "a�b", "a�b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeText(tc.in)
			if got != tc.want {
				t.Errorf("SanitizeText(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.ContainsRune(got, 0) {
				t.Errorf("SanitizeText(%q) still contains NUL", tc.in)
			}
			if !utf8.ValidString(got) {
				t.Errorf("SanitizeText(%q) = %q is not valid UTF-8", tc.in, got)
			}
		})
	}
}

func TestCL_SEC002_SanitizeTextIdempotent(t *testing.T) {
	for _, in := range []string{"a\x00\xff\xe2\x82b", "plain", "\xE2\x00\x82\xAC", ""} {
		once := SanitizeText(in)
		if twice := SanitizeText(once); twice != once {
			t.Errorf("SanitizeText not idempotent for %q: %q then %q", in, once, twice)
		}
	}
}

func TestCL_SEC002_TruncateHeadRuneBoundary(t *testing.T) {
	s := "ab日本語" // a b + three 3-byte runes = 11 bytes
	cases := []struct {
		n    int
		want string
	}{
		{-1, ""}, {0, ""}, {1, "a"}, {2, "ab"},
		{3, "ab"}, {4, "ab"}, {5, "ab日"}, {6, "ab日"}, {7, "ab日"}, {8, "ab日本"},
		{10, "ab日本"}, {11, s}, {12, s}, {1 << 20, s},
	}
	for _, tc := range cases {
		got := TruncateHead(s, tc.n)
		if got != tc.want {
			t.Errorf("TruncateHead(%q, %d) = %q, want %q", s, tc.n, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("TruncateHead(%q, %d) produced invalid UTF-8", s, tc.n)
		}
	}
}

func TestCL_SEC002_TruncateTailRuneBoundary(t *testing.T) {
	s := "日本語ab" // three 3-byte runes + a b = 11 bytes
	cases := []struct {
		n    int
		want string
	}{
		{-1, ""}, {0, ""}, {1, "b"}, {2, "ab"},
		{3, "ab"}, {4, "ab"}, {5, "語ab"}, {6, "語ab"}, {7, "語ab"}, {8, "本語ab"},
		{10, "本語ab"}, {11, s}, {12, s},
	}
	for _, tc := range cases {
		got := TruncateTail(s, tc.n)
		if got != tc.want {
			t.Errorf("TruncateTail(%q, %d) = %q, want %q", s, tc.n, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("TruncateTail(%q, %d) produced invalid UTF-8", s, tc.n)
		}
	}
}

func TestCL_SEC002_TruncateEveryOffsetStaysValid(t *testing.T) {
	s := strings.Repeat("aé日🚀", 50)
	for n := 0; n <= len(s)+2; n++ {
		h, tl := TruncateHead(s, n), TruncateTail(s, n)
		if !utf8.ValidString(h) || len(h) > n {
			t.Fatalf("TruncateHead n=%d: valid=%v len=%d", n, utf8.ValidString(h), len(h))
		}
		if !utf8.ValidString(tl) || len(tl) > n {
			t.Fatalf("TruncateTail n=%d: valid=%v len=%d", n, utf8.ValidString(tl), len(tl))
		}
		if !strings.HasPrefix(s, h) || !strings.HasSuffix(s, tl) {
			t.Fatalf("n=%d: head/tail are not a prefix/suffix of the input", n)
		}
		ht := TruncateHeadTail(s, n)
		if !utf8.ValidString(ht) || len(ht) > n {
			t.Fatalf("TruncateHeadTail n=%d: valid=%v len=%d", n, utf8.ValidString(ht), len(ht))
		}
	}
}

func TestCL_SEC002_EventReasonBoundaryInsideRune(t *testing.T) {
	// The CommandCompleted reason: TruncateHead(SanitizeText(msg), 200) with a
	// multi-byte rune straddling byte 200.
	msg := strings.Repeat("x", 199) + "日本語"
	got := TruncateHead(SanitizeText(msg), 200)
	if !utf8.ValidString(got) {
		t.Fatalf("200-byte cut inside a rune produced invalid UTF-8: %q", got[len(got)-4:])
	}
	if got != strings.Repeat("x", 199) {
		t.Errorf("the half-fitting rune must be dropped whole, got %d bytes", len(got))
	}
}

func TestCL_AC094_StderrTail8KiB(t *testing.T) {
	// 20 KiB of stderr: keep the last 8 KiB.
	line := "err-line-0123456789\n"
	stderr := strings.Repeat(line, (20*1024)/len(line)+1)[:20*1024]
	got := TruncateTail(stderr, MaxStderrTailBytes)
	if len(got) != MaxStderrTailBytes {
		t.Fatalf("stderr tail length = %d, want %d", len(got), MaxStderrTailBytes)
	}
	if !strings.HasSuffix(stderr, got) {
		t.Error("the kept bytes must be the LAST 8 KiB of the input")
	}
	short := "short"
	if TruncateTail(short, MaxStderrTailBytes) != short {
		t.Error("input under the bound must be returned unchanged")
	}
	exact := strings.Repeat("a", MaxStderrTailBytes)
	if TruncateTail(exact, MaxStderrTailBytes) != exact {
		t.Error("input exactly at the bound must be returned unchanged")
	}
}

func TestCL_AC094_StderrTailMultibyteBoundary(t *testing.T) {
	stderr := strings.Repeat("日", 5000) // 15000 bytes
	got := TruncateTail(stderr, MaxStderrTailBytes)
	if !utf8.ValidString(got) {
		t.Fatal("tail cut inside a rune produced invalid UTF-8")
	}
	if len(got) > MaxStderrTailBytes || len(got) < MaxStderrTailBytes-3 {
		t.Errorf("tail length %d should be within one rune of %d", len(got), MaxStderrTailBytes)
	}
}

func TestCL_SEC001_TruncateHeadTailKeepsBothEnds(t *testing.T) {
	head := strings.Repeat("H", 400)
	tail := strings.Repeat("T", 400)
	s := head + strings.Repeat("-", 5000) + tail
	got := TruncateHeadTail(s, 1000)
	if len(got) > 1000 {
		t.Fatalf("length %d exceeds the bound 1000", len(got))
	}
	if !strings.HasPrefix(got, "HHHH") || !strings.HasSuffix(got, "TTTT") {
		t.Errorf("head and tail must both survive, got prefix %q suffix %q", got[:4], got[len(got)-4:])
	}
	if !strings.Contains(got, TruncationMarker) {
		t.Error("a marker must show where output was cut")
	}
	if small := "tiny"; TruncateHeadTail(small, 1000) != small {
		t.Error("input under the bound must be returned unchanged")
	}
}

func TestCL_SEC001_TruncateHeadTailTinyBoundDegrades(t *testing.T) {
	s := strings.Repeat("x", 500)
	for _, n := range []int{0, 1, 5, len(TruncationMarker), len(TruncationMarker) + 1} {
		got := TruncateHeadTail(s, n)
		if len(got) > n {
			t.Errorf("n=%d: length %d exceeds the bound", n, len(got))
		}
	}
}

func TestCL_SEC001_TruncateHeadTailOutputBound(t *testing.T) {
	out := strings.Repeat("0123456789", 300_000) // 3 MB
	got := TruncateHeadTail(out, MaxReportOutputBytes)
	if len(got) > MaxReportOutputBytes {
		t.Errorf("output length %d exceeds %d", len(got), MaxReportOutputBytes)
	}
	if len(got) < MaxReportOutputBytes-8 {
		t.Errorf("output length %d should use nearly all of the %d budget", len(got), MaxReportOutputBytes)
	}
}

func TestCL_SEC001_ErrorCodesFrozen(t *testing.T) {
	pairs := [][2]string{
		{CodeConflict, "conflict"}, {CodeAlreadyTerminal, "already_terminal"},
		{CodeInvalidTransition, "invalid_transition"}, {CodeCommandNotFound, "command_not_found"},
		{CodeInvalidReport, "invalid_report"}, {CodeInvalidFilter, "invalid_filter"},
		{CodeRateLimited, "rate_limited"}, {CodeNotCancellable, "not_cancellable"},
	}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("error code constant is %q, the frozen contract says %q", p[0], p[1])
		}
	}
}

func TestCL_AD005_OriginsAndOutboxBounds(t *testing.T) {
	for _, o := range []string{"user", "auto_update", "auto_patch"} {
		if !IsValidOrigin(o) {
			t.Errorf("%s must be a valid origin", o)
		}
	}
	for _, o := range []string{"", "User", "cron", "auto"} {
		if IsValidOrigin(o) {
			t.Errorf("%q must not be a valid origin", o)
		}
	}
	if OutboxMaxEntries != 200 || OutboxMaxFileBytes != 1<<20 || OutboxMaxEntryBytes != 256<<10 ||
		OutboxMaxAge != 24*time.Hour || SeenMaxEntries != 1000 || SeenMaxAge != 48*time.Hour ||
		LegacyServerModeDuration != time.Hour || NotifierQueueBound != 64 {
		t.Error("agent persistence bounds drifted from ADD 5.4 / AD-010 / AD-011")
	}
}

func TestCommandPolicy_RD_AC017_RebootBudgetCoversWait(t *testing.T) {
	if DpkgLockWaitBound != 10*time.Minute {
		t.Errorf("[AC-017] DpkgLockWaitBound = %v, want 10m (BR-004)", DpkgLockWaitBound)
	}
	if got := AgentHardMax("reboot", 1); got != 12*time.Minute {
		t.Errorf("[AC-017] AgentHardMax(reboot) = %v, want 12m", got)
	}
	if AgentHardMax("reboot", 1) <= DpkgLockWaitBound {
		t.Errorf("[AC-017] the agent budget %v must exceed the wait bound %v so a full wait is never cut by cr.ctx", AgentHardMax("reboot", 1), DpkgLockWaitBound)
	}
	if got := ExecDeadline("reboot", 1); got != 20*time.Minute {
		t.Errorf("[AC-017] ExecDeadline(reboot) = %v, want 20m", got)
	}
	if got := ExecDeadlineSeconds("reboot", 1); got != 1200 {
		t.Errorf("[AC-017] ExecDeadlineSeconds(reboot) = %d, want 1200", got)
	}
	if ExecDeadline("reboot", 1) <= AgentHardMax("reboot", 1)+DeadlineSlack {
		t.Errorf("[AC-017] the server deadline %v must exceed the agent max %v + %v", ExecDeadline("reboot", 1), AgentHardMax("reboot", 1), DeadlineSlack)
	}
}

func TestCommandPolicy_RD_AC020_ShutdownBudgetUnchanged(t *testing.T) {
	if got := AgentHardMax("shutdown", 1); got != time.Minute {
		t.Errorf("[AC-020] AgentHardMax(shutdown) = %v, want 1m", got)
	}
	if got := ExecDeadline("shutdown", 1); got != 10*time.Minute {
		t.Errorf("[AC-020] ExecDeadline(shutdown) = %v, want 10m", got)
	}
	for _, action := range []string{"reboot", "shutdown"} {
		if got := QueuedTTL(action); got != 15*time.Minute {
			t.Errorf("[AC-020] QueuedTTL(%s) = %v, want 15m (unchanged)", action, got)
		}
		if got := ActionClass(action); got != ClassPower {
			t.Errorf("[AC-020] ActionClass(%s) = %v, want power (unchanged)", action, got)
		}
	}
}
