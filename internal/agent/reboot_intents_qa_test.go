package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer (PATCH-FLOW-001).
//
// The developer tests pin the 72 hour floor with entries that are 10 hours old
// (cap) and a corrupt file that is 73 hours old (self-heal). A mutation run
// showed that moving rebootIntentMinKeep anywhere between 24 h and 71.5 h still
// passed every test, so the floor was not actually pinned at 72 h. These tests
// sit one minute either side of the boundary.

const (
	justUnderFloor = 72*time.Hour - time.Minute
	justOverFloor  = 72*time.Hour + time.Minute
)

// seedUniform writes n entries that all have exactly the given age.
func (f *intentFixture) seedUniform(n int, age time.Duration) {
	f.t.Helper()
	ages := make(map[string]time.Duration, n)
	for i := 0; i < n; i++ {
		ages[uuidN(2000+i)] = age
	}
	f.seed(ages)
}

// Covers AC-021, NFR-006: the cap never evicts an intent younger than 72 h.
func TestRebootIntents_QA_AC021_CapNeverEvictsAnIntentOneMinuteUnder72h(t *testing.T) {
	f := newIntentFixture(t)
	f.seedUniform(rebootIntentCap, justUnderFloor)
	before := f.fileBytes()

	assert.Equal(t, claimStoreFull, f.a.claimPatchReboot(idA),
		"[AC-021] a full store whose every intent is 71h59m old must fail closed, not evict one")

	assert.Equal(t, before, f.fileBytes(), "a refused claim leaves the store untouched")
}

// Covers AC-021, NFR-006: the other side of the same boundary, so the floor is
// exactly 72 h and not "never evict".
func TestRebootIntents_QA_AC021_CapEvictsAnIntentOneMinuteOver72h(t *testing.T) {
	f := newIntentFixture(t)
	f.seedUniform(rebootIntentCap, justOverFloor)

	assert.Equal(t, claimOK, f.a.claimPatchReboot(idA),
		"[AC-021] intents older than 72h are evictable, so the claim succeeds")

	stored := f.stored()
	assert.Len(t, stored, rebootIntentCap, "exactly one entry was evicted to make room")
	assert.Contains(t, stored, idA)
}

// Covers AC-021: a mix. Only the entry past the floor may go; the 71h59m ones stay.
func TestRebootIntents_QA_AC021_OnlyTheEntryPastTheFloorIsEvicted(t *testing.T) {
	f := newIntentFixture(t)
	ages := make(map[string]time.Duration, rebootIntentCap)
	for i := 0; i < rebootIntentCap-1; i++ {
		ages[uuidN(3000+i)] = justUnderFloor
	}
	old := uuidN(9999)
	ages[old] = justOverFloor
	f.seed(ages)

	require.Equal(t, claimOK, f.a.claimPatchReboot(idA))

	stored := f.stored()
	assert.NotContains(t, stored, old, "the only evictable intent is the one past the floor")
	assert.Len(t, stored, rebootIntentCap)
	for i := 0; i < rebootIntentCap-1; i++ {
		assert.Contains(t, stored, uuidN(3000+i), "an intent younger than 72h must survive the cap")
	}
}

// Covers C-1: a corrupt store last written 71h59m ago may still be protecting a
// live intent, so it fails closed and is not set aside.
func TestRebootIntents_QA_C1_CorruptStoreOneMinuteUnder72hStaysFailClosed(t *testing.T) {
	f := newIntentFixture(t)
	require.NoError(t, os.WriteFile(f.path, []byte("{not json"), 0o600))
	f.setMTime(justUnderFloor)

	assert.Equal(t, claimPersistFailed, f.a.claimPatchReboot(idA))

	_, err := os.Stat(f.path + rebootIntentAsideSuffix)
	assert.True(t, os.IsNotExist(err), "no set-aside copy is made while the store could still hold a live intent")
}

// Covers C-1: one minute past the floor the store heals.
func TestRebootIntents_QA_C1_CorruptStoreOneMinuteOver72hSelfHeals(t *testing.T) {
	f := newIntentFixture(t)
	require.NoError(t, os.WriteFile(f.path, []byte("{not json"), 0o600))
	f.setMTime(justOverFloor)

	assert.Equal(t, claimOK, f.a.claimPatchReboot(idA))
}

// Covers C-1, NFR-004 style bound: a store far larger than the read bound is not
// read whole and still only costs one bounded set-aside copy once it is old.
func TestRebootIntents_QA_C1_HugeOldStoreSetAsideCopyIsBounded(t *testing.T) {
	f := newIntentFixture(t)
	huge := strings.Repeat("a", 3*rebootIntentMaxFileBytes)
	require.NoError(t, os.WriteFile(f.path, []byte(huge), 0o600))
	f.setMTime(200 * time.Hour)

	require.Equal(t, claimOK, f.a.claimPatchReboot(idA))

	info, err := os.Stat(f.path + rebootIntentAsideSuffix)
	require.NoError(t, err)
	assert.LessOrEqual(t, info.Size(), int64(rebootIntentAsideMaxBytes), "the set-aside copy never exceeds its bound")
}
