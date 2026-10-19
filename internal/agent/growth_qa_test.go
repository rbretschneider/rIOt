package agent

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer: adversarial probes of the GROWTH-001 PR-C agent bounds.

// Covers AC-060 (byte cap) and the NFR-001 bounded-growth rule under concurrent
// pushes. Several goroutines store, read, delete and compact at once; the
// in-memory counters must stay equal to the table, the caps must hold, and the
// file must not outgrow the cap by more than SQLite's own overhead.
func TestGROWTH_QA_AC060_ConcurrentStoreKeepsByteAndItemCap(t *testing.T) {
	captureLogs(t)
	const capBytes = 2 << 20
	path := filepath.Join(t.TempDir(), "buffer.db")
	b := openTestBuffer(t, path, capBytes)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				if err := b.Store(bufSnap(fmt.Sprintf("w%d-%d", w, i), 90_000)); err != nil {
					t.Errorf("store: %v", err)
					return
				}
				if bytes := b.Bytes(); bytes > capBytes {
					t.Errorf("[AC-060] queued payload %d exceeded the cap %d during concurrent stores", bytes, capBytes)
				}
				if n := b.Len(); n > maxBuffered {
					t.Errorf("[AC-060] %d items exceeded the item cap", n)
				}
			}
		}(w)
	}
	// A reader/deleter/compactor races the writers, as a flush does in production.
	stop := make(chan struct{})
	var bg sync.WaitGroup
	bg.Add(1)
	go func() {
		defer bg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if item, err := b.Oldest(); err == nil && item != nil {
				_ = b.Delete(item.ID)
				b.Compact()
			}
		}
	}()
	wg.Wait()
	close(stop)
	bg.Wait()

	var rows, sum int64
	require.NoError(t, b.db.QueryRow("SELECT COUNT(*), COALESCE(SUM(LENGTH(CAST(payload AS BLOB))),0) FROM buffer").Scan(&rows, &sum))
	assert.Equal(t, int(rows), b.Len(), "[AC-060] the in-memory item count must equal the table")
	assert.Equal(t, sum, b.Bytes(), "[AC-060] the in-memory byte total must equal the table")
	assert.LessOrEqual(t, sum, int64(capBytes), "[AC-060] the stored payload is within the cap")
	b.Compact()
	assert.LessOrEqual(t, fileSize(t, path), int64(capBytes)+(256<<10),
		"[AC-060] the file must not exceed the cap by more than SQLite's page overhead")
}

// Covers AC-061 / FR-067 and NFR-001. If the buffer cannot delete (read-only or
// failing disk), an acknowledged item is sent again on the next tick, but the
// failure must not loop inside one flush, must not grow the queue, and the item
// cap must still hold when eviction also fails.
func TestGROWTH_QA_AC061_ItemThatFailsDeleteRepeatedlyDoesNotLoopOrGrow(t *testing.T) {
	fs := newFlushServer(t, always(http.StatusOK))
	a, _ := flushAgent(t, fs, "stuck", "behind")
	_, err := a.buffer.db.Exec(`CREATE TRIGGER qa_block_delete BEFORE DELETE ON buffer
		BEGIN SELECT RAISE(ABORT, 'qa: delete refused'); END`)
	require.NoError(t, err)

	for tick := 1; tick <= 5; tick++ {
		a.flushBuffer(context.Background())
		assert.Equal(t, tick, fs.count("stuck"),
			"one send per tick: a failing Delete must end the flush, not spin on the same item")
		assert.Equal(t, 0, fs.count("behind"), "the item behind a stuck one waits")
		assert.Equal(t, 2, a.buffer.Len(), "the queue must not grow or shrink while Delete fails")
	}

	// Fill to the item cap, then push more: eviction fails too, so Store must
	// return an error and the queue must stay at the cap.
	for i := 0; i < maxBuffered+20; i++ {
		_ = a.buffer.Store(bufSnap(fmt.Sprintf("n%d", i), 10))
	}
	assert.LessOrEqual(t, a.buffer.Len(), maxBuffered, "[NFR-001] the item cap must hold even when eviction fails")
	var rows int
	require.NoError(t, a.buffer.db.QueryRow("SELECT COUNT(*) FROM buffer").Scan(&rows))
	assert.LessOrEqual(t, rows, maxBuffered, "[NFR-001] the table, not just the counter, stays within the cap")
}

// Covers AC-063 / AD-017: an absurd Retry-After never becomes an unbounded or
// overflowing hold. A date a millennium away clamps to one hour; a number too
// large for int64 is unusable and falls back to the 30 s doubling schedule.
func TestGROWTH_QA_AC063_AbsurdRetryAfterIsBounded(t *testing.T) {
	t.Run("an HTTP date in the year 9999 holds for at most one hour", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "429 Fri, 31 Dec 9999 23:59:59 GMT", "200")
		c, clk := newBackoffClient(t, srv)
		_ = sendOne(c)
		clk.advance(time.Hour - time.Second)
		require.ErrorIs(t, sendOne(c), ErrBackoff)
		clk.advance(time.Second)
		require.NoError(t, sendOne(c), "[AC-063] the hold must end after one hour whatever the server asked for")
	})
	t.Run("a number beyond int64 falls back to the 30 s schedule", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "429 99999999999999999999999", "200")
		c, clk := newBackoffClient(t, srv)
		_ = sendOne(c)
		clk.advance(30 * time.Second)
		require.NoError(t, sendOne(c), "[AC-063] an unparsable Retry-After must not hold telemetry for ever")
	})
	t.Run("a 5xx with a huge Retry-After is clamped too", func(t *testing.T) {
		srv := newScriptedTelemetryServer(t, "503 86400", "200")
		c, clk := newBackoffClient(t, srv)
		_ = sendOne(c)
		clk.advance(time.Hour)
		require.NoError(t, sendOne(c), "[AC-063] one hour is the ceiling for every status")
	})
}

// Covers AC-068: odd YAML scalars for every limit key still resolve inside the
// range and never stop the load. Null and tilde are the same as an unset key.
func TestGROWTH_QA_AC068_OddYAMLScalarsStayInRange(t *testing.T) {
	values := []string{"~", "null", ".inf", "-.inf", ".nan", "0x20", "0o17", "1e3", "'12'", `"abc"`, "[1, 2]", "{a: 1}", "+50", "' 7 '", "1_000", "-0"}
	for key, spec := range agentLimitSpecs {
		for _, v := range values {
			t.Run(key+"/"+v, func(t *testing.T) {
				cfg, _ := loadYAML(t, yamlForKey(key, v))
				got := resolvedFor(cfg.Limits(), key)
				assert.GreaterOrEqual(t, got, spec.min, "[AC-068] %s=%s below range", key, v)
				assert.LessOrEqual(t, got, spec.max, "[AC-068] %s=%s above range", key, v)
			})
		}
	}
}

// Covers AC-066 / FR-072. A server that announces more than it sends (a lying
// Content-Length) and hangs up must end in an error and leave no partial file.
func TestGROWTH_QA_AC066_ShortBodyUnderLyingContentLengthLeavesNoPartial(t *testing.T) {
	captureLogs(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadString('\n')
		// Announces 10 MiB, sends 10 bytes, closes.
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 10485760\r\nConnection: close\r\n\r\n0123456789"))
	}()
	p, paths := newDownloadPipeline(t)

	_, err = p.downloadBinary(context.Background(), "http://"+ln.Addr().String())

	require.Error(t, err, "[AC-066] a truncated download must fail")
	_, statErr := os.Stat(paths.binary)
	assert.True(t, os.IsNotExist(statErr), "[AC-066] no partial file may remain after a short body")
}

// Covers AC-066: a Content-Length that lies low and a body that is really huge.
// net/http stops the read at the announced length, so the cap is not the only
// defence, but the staged file must still never exceed what was announced.
func TestGROWTH_QA_AC066_LyingLowContentLengthCannotExceedAnnounced(t *testing.T) {
	captureLogs(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 16\r\nConnection: close\r\n\r\n"))
		chunk := make([]byte, 64<<10)
		for i := 0; i < 64; i++ { // 4 MiB of surplus
			if _, err := conn.Write(chunk); err != nil {
				return
			}
		}
	}()
	p, paths := newDownloadPipeline(t)

	_, err = p.downloadBinary(context.Background(), "http://"+ln.Addr().String())

	require.NoError(t, err, "a short but complete body is a legal download (the checksum step rejects it later)")
	info, statErr := os.Stat(paths.binary)
	require.NoError(t, statErr)
	assert.LessOrEqual(t, info.Size(), int64(16), "[AC-066] the staged file must not exceed the announced length")
}

// Covers AC-053 / FR-057 on the real file system (the other tests inject a fake):
// when the ID path cannot be created at all (its parent is a regular file), the
// agent never reaches Register.
func TestGROWTH_QA_AC053_RealFilesystemUnwritableIdentityNeverRegisters(t *testing.T) {
	captureLogs(t)
	fx := newIdentityFixture(t)
	fx.agent.fsys = nil // the real file system
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	fx.agent.idPath = filepath.Join(blocker, "id")
	fx.agent.pause = func(ctx context.Context, d time.Duration) bool {
		fx.cancel()
		return false
	}

	err := fx.agent.register(fx.ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, fx.srv.registrations(), "[AC-053] an identity that cannot be created must never reach Register")
	assert.Empty(t, fx.agent.config.Agent.DeviceID)
}
