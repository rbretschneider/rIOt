package collectors

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// maxBytesPerCycle is the maximum number of bytes read from the nginx access log
// per collection cycle (SEC-001). When the budget is exhausted the reader saves
// its current offset and defers remaining lines to the next cycle.
// 256 MiB is large enough to cover typical traffic spikes while bounding CPU and
// wall-clock time in the collection hot path.
const maxBytesPerCycle = 256 * 1024 * 1024 // 256 MiB

// nginxAccessLogReader tails a nginx access log file, tracking a byte offset
// across collection cycles so only new lines are processed each time.
type nginxAccessLogReader struct {
	path       string
	offsetPath string
	offset     int64
	loaded     bool // true after the offset has been initialised (either read from disk or first-run seek-to-end)
}

// newNginxAccessLogReader constructs a reader for the given access log path.
// The offset state file lives next to the agent config in /etc/riot/ (SEC-003).
func newNginxAccessLogReader(path string) *nginxAccessLogReader {
	offsetPath := offsetFilePath(path)
	r := &nginxAccessLogReader{
		path:       path,
		offsetPath: offsetPath,
	}
	r.loadOffset()
	return r
}

// offsetFilePath returns the path for the byte-offset persistence file.
// It always lives in the same directory as the global nginx-access-log.offset
// so the atomic rename (same filesystem) is guaranteed (SEC-003).
func offsetFilePath(_ string) string {
	// Replicate the logic from agent.NginxAccessLogOffsetPath inline to keep
	// the collectors package free of the agent package import.
	if runtime.GOOS == "windows" {
		programData := os.Getenv("PROGRAMDATA")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "riot", "nginx-access-log.offset")
	}
	return "/etc/riot/nginx-access-log.offset"
}

// loadOffset reads the stored byte offset from disk. If the file does not exist
// (first run) it sets loaded=false so ReadAndCount will seek to the end of the
// access log on first use rather than re-processing historical data.
func (r *nginxAccessLogReader) loadOffset() {
	data, err := os.ReadFile(r.offsetPath)
	if err != nil {
		// No offset file — first run. ReadAndCount will seek to end.
		r.offset = 0
		r.loaded = false
		return
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || n < 0 {
		r.offset = 0
		r.loaded = false
		return
	}
	r.offset = n
	r.loaded = true
}

// saveOffset atomically writes the current byte offset to disk.
// The temp file is created in the same directory as the offset file to
// guarantee atomic rename within the same filesystem (SEC-003).
func (r *nginxAccessLogReader) saveOffset() {
	dir := filepath.Dir(r.offsetPath)
	tmp, err := os.CreateTemp(dir, ".nginx-offset-*.tmp")
	if err != nil {
		slog.Warn("nginx access log: could not write offset file (degraded: offset won't survive restart)",
			"path", r.offsetPath, "error", err)
		return
	}
	tmpPath := tmp.Name()
	if _, err := fmt.Fprintf(tmp, "%d", r.offset); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		slog.Warn("nginx access log: could not write offset file", "path", r.offsetPath, "error", err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, r.offsetPath); err != nil {
		os.Remove(tmpPath)
		slog.Warn("nginx access log: could not rename offset file", "path", r.offsetPath, "error", err)
	}
}

// validatePath checks that the configured path is absolute and points to a
// regular file (not a device, FIFO, symlink to device, etc.) (SEC-002).
// Returns an error with a descriptive message if validation fails.
func validatePath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("nginx access_log path must be absolute, got %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("nginx access_log path %q is not a regular file (mode %s)", path, info.Mode())
	}
	return nil
}

// ReadAndCount reads new lines from the nginx access log since the last stored
// offset, counts HTTP status codes, updates and persists the offset, and returns
// aggregate metrics. It always returns a non-nil *NginxAccessMetrics; on error
// the returned metrics contain all-zero counts.
//
// Memory characteristics: uses bufio.Scanner with default 64 KiB buffer (SEC-004),
// five int counters, and no per-line allocations. The scanner buffer size is NOT
// increased beyond the default MaxScanTokenSize.
func (r *nginxAccessLogReader) ReadAndCount() (*models.NginxAccessMetrics, error) {
	metrics := &models.NginxAccessMetrics{}

	// SEC-002: validate that the path is absolute and a regular file.
	if err := validatePath(r.path); err != nil {
		slog.Debug("nginx access log: path validation failed", "path", r.path, "error", err)
		return metrics, err
	}

	info, err := os.Stat(r.path)
	if err != nil {
		slog.Debug("nginx access log: stat failed", "path", r.path, "error", err)
		return metrics, err
	}

	fileSize := info.Size()

	// First-run: no offset file exists yet. Seek to end so we don't process
	// historical data on first start (per FRD Section 7 state transitions).
	if !r.loaded {
		r.offset = fileSize
		r.loaded = true
		r.saveOffset()
		slog.Debug("nginx access log: first run, seeking to end", "path", r.path, "offset", r.offset)
		return metrics, nil
	}

	// Log rotation detection: if the file is smaller than our stored offset
	// the file has been replaced. Reset to beginning of the new file.
	if fileSize < r.offset {
		slog.Debug("nginx access log: rotation detected, resetting offset", "path", r.path,
			"stored_offset", r.offset, "file_size", fileSize)
		r.offset = 0
	}

	// Nothing new to read.
	if r.offset == fileSize {
		return metrics, nil
	}

	f, err := os.Open(r.path)
	if err != nil {
		slog.Debug("nginx access log: open failed", "path", r.path, "error", err)
		return metrics, err
	}
	defer f.Close()

	if _, err := f.Seek(r.offset, 0); err != nil {
		slog.Debug("nginx access log: seek failed", "path", r.path, "offset", r.offset, "error", err)
		return metrics, err
	}

	// Use bufio.Scanner with default buffer — do NOT increase MaxScanTokenSize (SEC-004).
	scanner := bufio.NewScanner(f)

	var bytesRead int64
	var linesProcessed int64

	for scanner.Scan() {
		// SEC-001: enforce per-cycle byte budget.
		bytesRead += int64(len(scanner.Bytes())) + 1 // +1 for the newline
		if bytesRead > maxBytesPerCycle {
			slog.Debug("nginx access log: per-cycle byte budget exhausted, deferring remaining lines",
				"path", r.path, "bytes_read", bytesRead, "lines_processed", linesProcessed)
			break
		}

		status := parseStatusCode(scanner.Bytes())
		if status == 0 {
			// Malformed line — silently skip (NFR-006).
			continue
		}

		metrics.TotalRequests++
		linesProcessed++

		switch {
		case status >= 200 && status <= 299:
			metrics.Status2xx++
		case status >= 300 && status <= 399:
			metrics.Status3xx++
		case status >= 400 && status <= 499:
			metrics.Status4xx++
		case status >= 500 && status <= 599:
			metrics.Status5xx++
		}
	}

	// Update offset to the current file position after scanning.
	// Seek to current position to get precise offset.
	if pos, err := f.Seek(0, 1); err == nil {
		r.offset = pos
	}

	slog.Debug("nginx access log: processed lines", "path", r.path,
		"lines", linesProcessed, "offset", r.offset)

	r.saveOffset()
	return metrics, nil
}

// parseStatusCode extracts the HTTP status code from a nginx combined log format line.
// Works on the raw []byte from bufio.Scanner to avoid per-line string allocations.
// Returns 0 if the line is malformed or does not match the expected format.
//
// Combined log format:
//
//	$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
//
// Strategy: find the closing quote of the "$request" field (the sequence `" `),
// then read the next 3 ASCII digit bytes as the status code.
func parseStatusCode(line []byte) int {
	// The request field ends with `" ` (closing double-quote followed by space).
	// We search for this pattern scanning from the left. The first `" ` we find
	// after the time bracket `]` section is the end of the request field.
	//
	// To avoid matching the opening `"` of the request field, we look for the
	// pattern where `"` is followed immediately by ` ` and then a digit (status
	// codes are always 3 digits, so we can verify the char after the space is 1-5).

	n := len(line)
	// Minimum viable combined log line is short; anything under 20 bytes is garbage.
	if n < 20 {
		return 0
	}

	// Walk through the line looking for `" ` where the next character is a digit.
	// We start from position 1 so we can check line[i-1] safely.
	for i := 1; i < n-3; i++ {
		if line[i] == '"' && line[i+1] == ' ' {
			// Check that the three bytes after the space are ASCII digits
			b0 := line[i+2]
			if b0 < '1' || b0 > '5' {
				// Status codes are 100-599; first digit must be 1-5.
				continue
			}
			if i+4 >= n {
				continue
			}
			b1 := line[i+3]
			b2 := line[i+4]
			if b1 < '0' || b1 > '9' || b2 < '0' || b2 > '9' {
				continue
			}
			// Verify that position i+5 is a space or end of line (not part of a longer number).
			if i+5 < n && line[i+5] != ' ' && line[i+5] != '\r' {
				continue
			}
			return int(b0-'0')*100 + int(b1-'0')*10 + int(b2-'0')
		}
	}

	return 0
}

