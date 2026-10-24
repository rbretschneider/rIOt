package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// intentFixture is an agent with only the reboot-intent store wired up, over a
// real temp file and a fake clock.
type intentFixture struct {
	t     *testing.T
	a     *Agent
	clk   *fakeClock
	path  string
	other int // next uuidN seed for filler entries
}

func newIntentFixture(t *testing.T) *intentFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), rebootIntentFileName)
	clk := newFakeClock()
	return &intentFixture{t: t, clk: clk, path: path, other: 1000, a: &Agent{rebootIntentPath: path, clk: clk}}
}

// seed writes a v2 store whose entries are ages old (relative to the fake clock).
func (f *intentFixture) seed(ages map[string]time.Duration) {
	f.t.Helper()
	intents := map[string]time.Time{}
	for id, age := range ages {
		intents[id] = f.clk.Now().Add(-age)
	}
	data, err := json.Marshal(encodeRebootIntents(intents))
	require.NoError(f.t, err)
	require.NoError(f.t, os.WriteFile(f.path, data, 0o600))
}

// seedFiller writes n entries, all of the given age, and returns their IDs.
func (f *intentFixture) seedFiller(n int, age time.Duration) []string {
	f.t.Helper()
	ages := map[string]time.Duration{}
	var ids []string
	for i := 0; i < n; i++ {
		f.other++
		id := uuidN(f.other)
		ids = append(ids, id)
		ages[id] = age + time.Duration(i)*time.Minute // distinct, strictly increasing age
	}
	f.seed(ages)
	return ids
}

func (f *intentFixture) stored() map[string]time.Time {
	f.t.Helper()
	data, err := os.ReadFile(f.path)
	require.NoError(f.t, err)
	intents, err := decodeRebootIntents(data, f.clk.Now())
	require.NoError(f.t, err)
	return intents
}

func (f *intentFixture) fileBytes() []byte {
	f.t.Helper()
	data, err := os.ReadFile(f.path)
	require.NoError(f.t, err)
	return data
}

func (f *intentFixture) setMTime(age time.Duration) {
	f.t.Helper()
	m := f.clk.Now().Add(-age)
	require.NoError(f.t, os.Chtimes(f.path, m, m))
}

const day = 24 * time.Hour

func TestRebootIntents_PF_Bound_PrunesOlderThan30Days(t *testing.T) {
	f := newIntentFixture(t)
	old1, old2, young := uuidN(1), uuidN(2), uuidN(3)
	f.seed(map[string]time.Duration{old1: 31 * day, old2: 90 * day, young: 29 * day})

	require.Equal(t, claimOK, f.a.claimPatchReboot(idA))

	stored := f.stored()
	assert.NotContains(t, stored, old1, "an intent older than 30 days must be pruned")
	assert.NotContains(t, stored, old2)
	assert.Contains(t, stored, young)
	assert.Contains(t, stored, idA)
	assert.Len(t, stored, 2)
}

func TestRebootIntents_PF_Bound_KeepsEntriesYoungerThan30Days(t *testing.T) {
	f := newIntentFixture(t)
	f.seed(map[string]time.Duration{uuidN(1): 29*day + 23*time.Hour, uuidN(2): time.Hour})

	require.Equal(t, claimOK, f.a.claimPatchReboot(idA))

	assert.Len(t, f.stored(), 3, "intents inside the 30 day window are all kept")
}

func TestRebootIntents_PF_Bound_PrunedMapSavedEvenWhenAlreadyClaimed(t *testing.T) {
	f := newIntentFixture(t)
	old := uuidN(1)
	f.seed(map[string]time.Duration{old: 40 * day, idA: time.Hour})

	assert.Equal(t, claimAlreadyClaimed, f.a.claimPatchReboot(idA))

	stored := f.stored()
	assert.NotContains(t, stored, old, "pruning is persisted even when the claim itself is a no-op")
	assert.Contains(t, stored, idA)
}

func TestRebootIntents_PF_Bound_CapEvictsOldestFirstOnlyBeyond72h(t *testing.T) {
	f := newIntentFixture(t)
	ids := f.seedFiller(rebootIntentCap, 80*time.Hour) // ages 80h .. 80h+255m, ids[255] is the oldest

	require.Equal(t, claimOK, f.a.claimPatchReboot(idA))

	stored := f.stored()
	assert.Len(t, stored, rebootIntentCap, "the store stays at the cap")
	assert.NotContains(t, stored, ids[len(ids)-1], "the oldest intent is the one evicted")
	assert.Contains(t, stored, ids[0], "younger intents survive")
	assert.Contains(t, stored, idA)
}

func TestRebootIntents_PF_Bound_CapNeverEvictsYoungerThan72hFailsClosed(t *testing.T) {
	f := newIntentFixture(t)
	f.seedFiller(rebootIntentCap, 10*time.Hour)
	before := f.fileBytes()

	assert.Equal(t, claimStoreFull, f.a.claimPatchReboot(idA), "256 intents all inside the 72h floor leave no room")

	assert.True(t, bytes.Equal(before, f.fileBytes()), "a refused claim must leave the store unchanged")
}

func TestRebootIntents_PF_Bound_ClockBackwardsSkipsPruneAndEvict(t *testing.T) {
	t.Run("prune skipped", func(t *testing.T) {
		f := newIntentFixture(t)
		old := uuidN(1)
		future := uuidN(2)
		f.seed(map[string]time.Duration{old: 60 * day, future: -10 * day})

		require.Equal(t, claimOK, f.a.claimPatchReboot(idA))

		stored := f.stored()
		assert.Contains(t, stored, old, "a backwards clock must not prune: live intents would look old")
		assert.Contains(t, stored, future)
	})
	t.Run("eviction skipped", func(t *testing.T) {
		f := newIntentFixture(t)
		ages := map[string]time.Duration{uuidN(1): -10 * day}
		for i := 0; i < rebootIntentCap-1; i++ {
			ages[uuidN(2000+i)] = 100 * time.Hour
		}
		f.seed(ages)

		assert.Equal(t, claimStoreFull, f.a.claimPatchReboot(idA), "eviction is skipped while the clock looks wrong")
	})
}

func TestRebootIntents_PF_Bound_LegacyV1LoadedAsNowAndRewrittenV2(t *testing.T) {
	f := newIntentFixture(t)
	a1, a2, a3 := uuidN(11), uuidN(12), uuidN(13)
	legacy := `{"` + a1 + `":true,"` + a2 + `":true,"` + a3 + `":true,"not-a-uuid":true,"` + uuidN(14) + `":false}`
	require.NoError(t, os.WriteFile(f.path, []byte(legacy), 0o600))

	require.Equal(t, claimOK, f.a.claimPatchReboot(idA))

	var file rebootIntentFile
	require.NoError(t, json.Unmarshal(f.fileBytes(), &file))
	assert.Equal(t, rebootIntentVersion, file.Version, "the file is rewritten in the v2 format")
	assert.Len(t, file.Intents, 4, "3 valid legacy intents + the new claim; the invalid ones are dropped")
	assert.Equal(t, f.clk.Now().UTC().Format(time.RFC3339), file.Intents[a1], "a legacy intent is dated at load time")
}

func TestRebootIntents_PF_Bound_LegacyV1EntryNotPrunedBefore30Days(t *testing.T) {
	f := newIntentFixture(t)
	legacyID := uuidN(11)
	require.NoError(t, os.WriteFile(f.path, []byte(`{"`+legacyID+`":true}`), 0o600))
	require.Equal(t, claimOK, f.a.claimPatchReboot(idA)) // rewrites as v2 at "now"

	f.clk.Advance(29 * day)
	assert.Equal(t, claimAlreadyClaimed, f.a.claimPatchReboot(legacyID), "a legacy intent is kept for a further 29 days")

	f.clk.Advance(2 * day)
	assert.Equal(t, claimOK, f.a.claimPatchReboot(legacyID), "after 31 days it is pruned, so the ID can be claimed again")
}

func TestRebootIntents_PF_Bound_InvalidKeysAndValuesDropped(t *testing.T) {
	f := newIntentFixture(t)
	good := uuidN(21)
	content := `{"version":2,"intents":{"` + good + `":"2026-10-17T11:00:00Z","` + uuidN(22) + `":"yesterday","not-a-uuid":"2026-10-17T11:00:00Z","` + uuidN(23) + `":""}}`
	require.NoError(t, os.WriteFile(f.path, []byte(content), 0o600))

	require.Equal(t, claimOK, f.a.claimPatchReboot(idA))

	stored := f.stored()
	assert.Len(t, stored, 2)
	assert.Contains(t, stored, good)
	assert.Contains(t, stored, idA)
}

func TestRebootIntents_PF_Bound_ParseErrorFailsClosed(t *testing.T) {
	f := newIntentFixture(t)
	require.NoError(t, os.WriteFile(f.path, []byte(`{"version":2,"intents":`), 0o600))
	f.setMTime(time.Hour)
	before := f.fileBytes()

	assert.Equal(t, claimPersistFailed, f.a.claimPatchReboot(idA), "a corrupt store written within 72h could still hold a live intent")

	assert.True(t, bytes.Equal(before, f.fileBytes()), "a fail-closed claim must not touch the store")
	assert.NoFileExists(t, f.path+rebootIntentAsideSuffix)
}

func TestRebootIntents_PF_C1_CorruptStoreSelfHealsAfter72h(t *testing.T) {
	f := newIntentFixture(t)
	corrupt := []byte(`{"version":2,"intents":` + strings.Repeat("x", 50))
	require.NoError(t, os.WriteFile(f.path, corrupt, 0o600))
	f.setMTime(73 * time.Hour)

	require.Equal(t, claimOK, f.a.claimPatchReboot(idA), "a corrupt store older than the floor must self-heal")

	aside, err := os.ReadFile(f.path + rebootIntentAsideSuffix)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(corrupt, aside), "the bad content is kept as the set-aside copy")
	assert.Equal(t, map[string]bool{idA: true}, idsOf(f.stored()), "a fresh v2 store holds only the new claim")

	matches, err := filepath.Glob(f.path + "*")
	require.NoError(t, err)
	var copies int
	for _, m := range matches {
		if strings.HasSuffix(m, rebootIntentAsideSuffix) {
			copies++
		}
	}
	assert.Equal(t, 1, copies, "exactly one set-aside copy exists")
}

func TestRebootIntents_PF_C1_SecondHealOverwritesTheSingleAsideCopy(t *testing.T) {
	f := newIntentFixture(t)
	require.NoError(t, os.WriteFile(f.path, []byte("first corruption"), 0o600))
	f.setMTime(100 * time.Hour)
	require.Equal(t, claimOK, f.a.claimPatchReboot(idA))

	require.NoError(t, os.WriteFile(f.path, []byte("second corruption"), 0o600))
	f.setMTime(100 * time.Hour)
	require.Equal(t, claimOK, f.a.claimPatchReboot(idB))

	aside, err := os.ReadFile(f.path + rebootIntentAsideSuffix)
	require.NoError(t, err)
	assert.Equal(t, "second corruption", string(aside), "the set-aside copy is overwritten, never accumulated")
}

func TestRebootIntents_PF_C1_FutureMtimeStaysFailClosed(t *testing.T) {
	f := newIntentFixture(t)
	require.NoError(t, os.WriteFile(f.path, []byte("garbage"), 0o600))
	f.setMTime(-5 * day) // five days in the future

	assert.Equal(t, claimPersistFailed, f.a.claimPatchReboot(idA), "a future write time counts as young")
}

func TestRebootIntents_PF_C1_OversizedFileCountsAsCorruptAndIsNotReadWhole(t *testing.T) {
	big := bytes.Repeat([]byte("a"), 3*rebootIntentMaxFileBytes)
	t.Run("young oversized store fails closed", func(t *testing.T) {
		f := newIntentFixture(t)
		require.NoError(t, os.WriteFile(f.path, big, 0o600))
		f.setMTime(time.Hour)
		assert.Equal(t, claimPersistFailed, f.a.claimPatchReboot(idA))
	})
	t.Run("old oversized store heals with a bounded aside copy", func(t *testing.T) {
		f := newIntentFixture(t)
		require.NoError(t, os.WriteFile(f.path, big, 0o600))
		f.setMTime(100 * time.Hour)
		require.Equal(t, claimOK, f.a.claimPatchReboot(idA))
		info, err := os.Stat(f.path + rebootIntentAsideSuffix)
		require.NoError(t, err)
		assert.LessOrEqual(t, info.Size(), int64(rebootIntentAsideMaxBytes), "the aside copy is bounded")
	})
}

func TestRebootIntents_PF_C1_UnreadableStoreFailsClosed(t *testing.T) {
	// A store that cannot be read at all (here: the path is a
	// directory) is corrupt, and a store with unusable write-time evidence fails closed.
	f := newIntentFixture(t)
	require.NoError(t, os.MkdirAll(f.path, 0o700))
	assert.Equal(t, claimPersistFailed, f.a.claimPatchReboot(idA))
}

func TestRebootIntents_PF_Bound_RebootsResumeAfter256OldIntents(t *testing.T) {
	f := newIntentFixture(t)
	f.seedFiller(rebootIntentCap, 31*day)

	assert.Equal(t, claimOK, f.a.claimPatchReboot(idA),
		"today's permanent suppression after 256 reboots must be gone: old intents age out")
	assert.Len(t, f.stored(), 1)
}

func TestRebootIntents_PF_AC021_ClaimedIDInsideWindowNeverPruned(t *testing.T) {
	f := newIntentFixture(t)
	require.Equal(t, claimOK, f.a.claimPatchReboot(idA))
	// 255 other intents that are older than the claim but inside 30 days.
	ages := map[string]time.Duration{}
	for i := 0; i < rebootIntentCap-1; i++ {
		ages[uuidN(3000+i)] = 20*day + time.Duration(i)*time.Minute
	}
	stored := f.stored()
	for id, claimedAt := range stored {
		ages[id] = f.clk.Now().Sub(claimedAt)
	}
	f.seed(ages)

	f.clk.Advance(71 * time.Hour)

	assert.Equal(t, claimAlreadyClaimed, f.a.claimPatchReboot(idA), "an ID inside the redelivery window must never be forgotten")
}

func TestEvictForCap_PF_Bound_TieBreaksByID(t *testing.T) {
	now := newFakeClock().Now()
	at := now.Add(-100 * time.Hour)
	intents := map[string]time.Time{}
	for i := 0; i < rebootIntentCap; i++ {
		intents[uuidN(5000+i)] = at
	}
	out, ok := evictForCap(intents, now)
	require.True(t, ok)
	assert.Len(t, out, rebootIntentCap-1)
	assert.NotContains(t, out, uuidN(5000), "with equal times the smallest ID goes first")
	assert.Len(t, intents, rebootIntentCap, "the input map is not mutated")
}

func TestPruneRebootIntents_PF_Bound_BoundaryIsInclusive(t *testing.T) {
	now := newFakeClock().Now()
	exactly := uuidN(1)
	justOver := uuidN(2)
	intents := map[string]time.Time{exactly: now.Add(-rebootIntentMaxAge), justOver: now.Add(-rebootIntentMaxAge - time.Second)}
	out, removed := pruneRebootIntents(intents, now)
	assert.Equal(t, 1, removed)
	assert.Contains(t, out, exactly, "exactly 30 days old is still kept")
}

func TestRebootIntents_PF_Bound_InfoLogOnlyWhenPruned(t *testing.T) {
	t.Run("pruning logs exactly one info line", func(t *testing.T) {
		logs := captureLogs(t)
		f := newIntentFixture(t)
		f.seed(map[string]time.Duration{uuidN(1): 40 * day, uuidN(2): 50 * day})
		require.Equal(t, claimOK, f.a.claimPatchReboot(idA))
		assert.Equal(t, 1, strings.Count(logs.String(), `msg="reboot intents pruned"`))
	})
	t.Run("nothing pruned logs nothing", func(t *testing.T) {
		logs := captureLogs(t)
		f := newIntentFixture(t)
		f.seed(map[string]time.Duration{uuidN(1): time.Hour})
		require.Equal(t, claimOK, f.a.claimPatchReboot(idA))
		assert.Zero(t, strings.Count(logs.String(), `msg="reboot intents pruned"`))
	})
}

func TestRebootIntents_PF_Bound_NonCanonicalIDRejected(t *testing.T) {
	f := newIntentFixture(t)
	assert.Equal(t, claimPersistFailed, f.a.claimPatchReboot("../../etc/passwd"))
	assert.NoFileExists(t, f.path, "a rejected ID must not create the store")
}

func idsOf(intents map[string]time.Time) map[string]bool {
	out := map[string]bool{}
	for id := range intents {
		out[id] = true
	}
	return out
}
