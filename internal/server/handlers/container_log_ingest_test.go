package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func logFixture(now time.Time, name string) models.ContainerLogEntry {
	return models.ContainerLogEntry{ContainerID: "0123456789ab", ContainerName: name, Timestamp: now, Stream: "stdout", Line: "hello"}
}
func TestDUX_SEC021_FutureAndPastDatedLogRowsNotStoredOutOfWindow(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for _, ts := range []time.Time{now.Add(-8 * 24 * time.Hour), now.Add(6 * time.Minute), {}} {
		e := logFixture(ts, "web")
		out := applyLogWindow(e, now, 7, 3)
		if !out.Timestamp.Equal(now.Add(3*time.Microsecond)) || out.AgentTS == nil || !out.AgentTS.Equal(ts) {
			t.Fatalf("timestamp not preserved and restamped: %+v", out)
		}
	}
	for _, ts := range []time.Time{now.Add(-7 * 24 * time.Hour), now.Add(5 * time.Minute)} {
		e := applyLogWindow(logFixture(ts, "web"), now, 7, 0)
		if e.AgentTS != nil {
			t.Fatal("inclusive boundary restamped")
		}
	}
}
func TestDUX_SEC023_ForgedGapStreamNotStored(t *testing.T) {
	now := time.Now()
	e := logFixture(now, "web")
	e.Stream = "gap_agent"
	out, _ := newContainerLogIngest().apply("host", []models.ContainerLogEntry{e}, nil, nil, now, 7)
	if len(out) != 0 {
		t.Fatal("agent-supplied stored gap accepted")
	}
}
func TestDUX_AC111_ServerCeilingAnyAgent(t *testing.T) {
	now := time.Now()
	docker := &models.DockerInfo{Containers: []models.ContainerInfo{{Name: "web"}}}
	entries := make([]models.ContainerLogEntry, 1100)
	for i := range entries {
		entries[i] = logFixture(now, "web")
	}
	s := newContainerLogIngest()
	out, _ := s.apply("host", entries, nil, docker, now, 7)
	if len(out) != 1001 {
		t.Fatalf("got %d rows", len(out))
	}
	if out[len(out)-1].Stream != "gap_server" || out[len(out)-1].Line != "100" {
		t.Fatal("missing structured gap")
	}
	out, _ = s.apply("host", entries[:1], nil, docker, now.Add(30*time.Second), 7)
	if len(out) != 1 || out[0].Stream != "gap_server" {
		t.Fatal("second ingest bypassed rolling cap")
	}
	out, _ = s.apply("host", entries[:1], nil, docker, now.Add(time.Minute), 7)
	if len(out) != 1 || out[0].Stream != "stdout" {
		t.Fatal("rolling cap never refilled")
	}
}
func TestDUX_SEC023_GapEntriesValidatedAndBounded(t *testing.T) {
	now := time.Now()
	valid := models.ContainerLogGap{ContainerID: "0123456789ab", ContainerName: "web", Timestamp: now, Count: 2, Reason: "agent_cap"}
	invalid := valid
	invalid.Count = -1
	out, _ := newContainerLogIngest().apply("host", nil, []models.ContainerLogGap{valid, invalid, valid}, &models.DockerInfo{Containers: []models.ContainerInfo{{Name: "web"}, {Name: "db"}, {Name: "redis"}}}, now, 7)
	if len(out) != 1 || out[0].Stream != "gap_agent" {
		t.Fatalf("bad gaps accepted: %+v", out)
	}
}
func TestDUX_SEC028_MaskAfterCeiling(t *testing.T) {
	now := time.Now()
	h := &Handlers{logIngest: newContainerLogIngest()}
	e := logFixture(now, "web")
	e.Line = "password=hunter2"
	out := h.prepareContainerLogs("host", []models.ContainerLogEntry{e}, nil, nil, now)
	if len(out) != 1 || strings.Contains(out[0].Line, "hunter2") {
		t.Fatalf("unmasked stored line: %+v", out)
	}
}
func TestDUX_AC110_LineTruncationAtRuneBoundary(t *testing.T) {
	out := truncateLogLine(strings.Repeat("世界", 10000))
	if len(out) > models.StoredLogMaxLineBytes || !strings.HasSuffix(out, " [truncated]") || strings.ContainsRune(out, '\ufffd') {
		t.Fatal("invalid bounded UTF-8")
	}
}

func TestDUX_ActualMaskedBytesBoundedAndNULSafe(t *testing.T) {
	now := time.Now()
	docker := &models.DockerInfo{Containers: []models.ContainerInfo{{Name: "web", ID: "0123456789ab"}}}
	entries := make([]models.ContainerLogEntry, 1000)
	for i := range entries {
		entries[i] = logFixture(now, "web")
		entries[i].Line = strings.Repeat("password=x ", 100) + "\x00"
	}
	out, _ := newContainerLogIngest().apply("host-a", entries, nil, docker, now, 7)
	bytes := 0
	lines := 0
	for _, entry := range out {
		if entry.Stream == "stdout" {
			bytes += len(entry.Line)
			lines++
			if strings.ContainsRune(entry.Line, 0) {
				t.Fatal("NUL reached PostgreSQL COPY")
			}
			if strings.Contains(entry.Line, "password=x") {
				t.Fatal("secret was not masked")
			}
		}
	}
	if bytes > models.StoredLogMaxBytes {
		t.Fatalf("masked byte cap bypassed: %d", bytes)
	}
	if lines == 0 {
		t.Fatal("masking rejected every valid line")
	}
}
func TestDUX_PriorityClassificationSurvivesMasking(t *testing.T) {
	now := time.Now()
	e := logFixture(now, "web")
	e.Line = "PASSWORD=error"
	out, _ := newContainerLogIngest().apply("host-a", []models.ContainerLogEntry{e}, nil, nil, now, 7)
	if len(out) != 1 || !out[0].StoredPriority {
		t.Fatal("raw error priority lost while masking")
	}
	if strings.Contains(out[0].Line, "error") {
		t.Fatal("test secret was not masked")
	}
}
