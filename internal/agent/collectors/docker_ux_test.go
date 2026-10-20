package collectors

import (
	"bytes"
	"encoding/binary"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func logFrame(stream byte, s string) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(s)))
	return append(h, []byte(s)...)
}
func TestDUX_LogReaderTTYAndMultiplexed(t *testing.T) {
	stamp := "2026-10-19T10:11:12.123456789Z"
	for _, tty := range []bool{false, true} {
		raw := []byte(stamp + " first\n" + stamp + " second\n")
		if !tty {
			raw = logFrame(2, string(raw))
		}
		var got []models.ContainerLogEntry
		ReadDockerLogs(bytes.NewReader(raw), tty, func(e models.ContainerLogEntry) bool { got = append(got, e); return true })
		if len(got) != 2 || got[0].Line != "first" || got[1].Line != "second" {
			t.Fatalf("tty=%v got=%+v", tty, got)
		}
		if got[0].Timestamp.Nanosecond() != 123456000 {
			t.Fatal("timestamp must match storage microseconds")
		}
		if !tty && got[0].Stream != "stderr" {
			t.Fatal("lost stderr stream")
		}
	}
}
func TestDUX_LogReaderOversizeFrameContinues(t *testing.T) {
	raw := append(logFrame(1, strings.Repeat("a", 2<<20)+"\n"), logFrame(2, "2026-10-19T10:00:00Z final\n")...)
	var got []models.ContainerLogEntry
	ReadDockerLogs(bytes.NewReader(raw), false, func(e models.ContainerLogEntry) bool { got = append(got, e); return true })
	if len(got) != 2 || got[1].Line != "final" || len(got[0].Line) > 16400 {
		t.Fatalf("oversized frame lost following data: %d", len(got))
	}
}
func TestDUX_CapRingNewestAndBytes(t *testing.T) {
	r := containerLogCapRing{maxLines: 3, maxBytes: 8}
	for _, s := range []string{"old", "next", "new"} {
		r.add(models.ContainerLogEntry{Line: s, Timestamp: time.Now()})
	}
	if r.dropped != 1 || len(r.entries) != 2 || r.entries[0].Line != "next" || r.bytes != 7 {
		t.Fatalf("unexpected ring %+v", r)
	}
	r.add(models.ContainerLogEntry{Line: strings.Repeat("界", 20)})
	if r.dropped != 2 || len(r.entries) != 2 || !utf8.ValidString(r.entries[0].Line) {
		t.Fatal("byte cap must preserve UTF-8")
	}
}
func TestDUX_IORatesResetAndInterval(t *testing.T) {
	now := time.Now()
	prev := ioSample{ID: "one", At: now, Rx: 100, Tx: 200}
	cur := ioSample{ID: "one", At: now.Add(2 * time.Second), Rx: 300, Tx: 600}
	got := ioRates(prev, cur)
	if got == nil || got.Rx != 100 || got.Tx != 200 {
		t.Fatalf("wrong rates %+v", got)
	}
	cur.ID = "other"
	if ioRates(prev, cur) != nil {
		t.Fatal("recreation must reset rates")
	}
	cur.ID = "one"
	cur.Rx = 1
	if ioRates(prev, cur) != nil {
		t.Fatal("counter reset must not underflow")
	}
}
func TestDUX_InventoryOptIn(t *testing.T) {
	r := NewRegistry()
	r.Register(&DockerInventoryCollector{})
	r.Register(&SystemCollector{})
	r.FilterEnabled(nil)
	for _, c := range r.Collectors() {
		if c.Name() == "docker_inventory" {
			t.Fatal("inventory is opt-in")
		}
	}
}

func TestDUX_StoredLogCursorKeepsNanosecondProgress(t *testing.T) {
	raw := logFrame(1, "2026-10-19T10:00:00.123456789Z last\n")
	var entry models.ContainerLogEntry
	readDockerLogs(bytes.NewReader(raw), false, false, func(e models.ContainerLogEntry) bool { entry = e; return true })
	cursor := entry.Timestamp
	if cursor.Nanosecond() != 123456789 {
		t.Fatal("cursor precision would lose different lines in the same microsecond")
	}
	entries := []models.ContainerLogEntry{entry}
	MaskLogEntries(entries)
	if entries[0].Timestamp.Nanosecond() != 123456000 {
		t.Fatal("stored line identity must match microsecond precision")
	}
}
