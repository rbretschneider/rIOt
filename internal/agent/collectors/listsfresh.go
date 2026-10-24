package collectors

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Package-list freshness limits (PATCH-FLOW-001 AD-003, NFR-004). The deadline
// is checked between directory chunks, not inside a single syscall.
const (
	freshnessEntryBudget = 1000
	freshnessDeadline    = 2 * time.Second
	freshnessClockSkew   = 5 * time.Minute
	freshnessReadChunk   = 128

	aptListsRel     = "var/lib/apt/lists"
	aptStampRel     = "var/lib/apt/periodic/update-success-stamp"
	packagesListTag = "_Packages"
)

// listsFreshness decides whether apt's package lists are older than the apt
// configuration (FR-029..FR-031). It examines at most freshnessEntryBudget
// directory entries in total, stops at deadline, creates no files, and returns
// the reason only for models.ListsUnknown. root "" means "/".
//
// The lists directory mtime is the refresh time: apt renames list files into
// it on every refresh that changes a list. The Ubuntu update-success-stamp and
// the maintenance helper's stamp cover refreshes that change nothing.
func listsFreshness(root string, now, deadline time.Time) (models.ListsFreshness, string) {
	if root == "" {
		root = string(filepath.Separator)
	}
	refreshedAt, state, reason := listsRefreshedAt(root)
	if state != "" {
		return state, reason
	}
	scan := freshnessScan{deadline: deadline, remaining: freshnessEntryBudget}
	found, err := scan.hasPackageList(filepath.Join(root, filepath.FromSlash(aptListsRel)))
	if err != nil {
		return models.ListsUnknown, "scan package lists: " + err.Error()
	}
	if !found {
		return models.ListsStale, ""
	}
	entries, err := maintenance.AptSourceEntriesUntil(root, scan.remaining, scan.expired)
	if err != nil {
		return models.ListsUnknown, "scan apt configuration: " + err.Error()
	}
	return decideFreshness(entries, refreshedAt, now)
}

// listsRefreshedAt returns the last refresh time. A non-empty state is a final
// answer (missing lists dir is stale, any other stat error is unknown).
func listsRefreshedAt(root string) (time.Time, models.ListsFreshness, string) {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(aptListsRel)))
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, models.ListsStale, ""
	}
	if err != nil {
		return time.Time{}, models.ListsUnknown, "stat lists dir: " + err.Error()
	}
	refreshed := info.ModTime()
	if stamp, stampErr := os.Stat(filepath.Join(root, filepath.FromSlash(aptStampRel))); stampErr == nil && stamp.ModTime().After(refreshed) {
		refreshed = stamp.ModTime()
	}
	return refreshed, "", ""
}

func decideFreshness(entries []maintenance.AptSourceEntry, refreshedAt, now time.Time) (models.ListsFreshness, string) {
	stale := false
	for _, entry := range entries {
		if entry.ModTime.After(now.Add(freshnessClockSkew)) {
			return models.ListsUnknown, "clock-skewed apt configuration file"
		}
		if entry.ModTime.After(refreshedAt) {
			stale = true
		}
	}
	if stale {
		return models.ListsStale, ""
	}
	return models.ListsFresh, ""
}

// freshnessScan tracks the shared entry budget and deadline for one check.
type freshnessScan struct {
	deadline  time.Time
	remaining int
}

func (s *freshnessScan) expired() bool {
	return !time.Now().Before(s.deadline)
}

var (
	errFreshnessBudget   = errors.New("entry budget reached")
	errFreshnessDeadline = errors.New("deadline reached")
)

// hasPackageList reports whether dir holds a regular file whose name contains
// "_Packages", stopping at the first hit. The read is chunked and spends the
// shared budget.
func (s *freshnessScan) hasPackageList(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()
	for {
		if s.expired() {
			return false, errFreshnessDeadline
		}
		chunk := freshnessReadChunk
		if s.remaining < chunk {
			chunk = s.remaining
		}
		if chunk <= 0 {
			return false, errFreshnessBudget
		}
		entries, readErr := f.ReadDir(chunk)
		s.remaining -= len(entries)
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.Contains(entry.Name(), packagesListTag) {
				return true, nil
			}
		}
		if errors.Is(readErr, io.EOF) {
			return false, nil
		}
		if readErr != nil {
			return false, readErr
		}
	}
}

// applyListsFreshness sets info.ListsFreshness from the apt tree under
// c.aptRoot. It logs one debug line, and only when the result is unknown.
func (c *UpdatesCollector) applyListsFreshness(info *models.UpdateInfo) {
	now := time.Now()
	if c.listsNow != nil {
		now = c.listsNow()
	}
	state, reason := listsFreshness(c.aptRoot, now, time.Now().Add(freshnessDeadline))
	info.ListsFreshness = state
	if state == models.ListsUnknown {
		slog.Debug("updates: package-list freshness unknown", "reason", reason)
	}
}
