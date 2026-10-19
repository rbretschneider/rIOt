package agent

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC-066 (FR-056): a lock is released when the holder fails or panics, so the
// next command for the same target acquires it.
func TestCL_AC066_LockReleasedAfterFailureAndPanic(t *testing.T) {
	locks := newTargetLocks()
	key := "docker:container:web"

	// A holder that panics and is recovered by its caller, with the release
	// deferred the way runAction does it.
	func() {
		defer func() { _ = recover() }()
		_, _, ok := locks.TryLockAll([]string{key}, "cmd-1", "docker_update")
		require.True(t, ok, "[AC-066] the first command must acquire a free lock")
		defer locks.Unlock([]string{key}, "cmd-1")
		panic("boom")
	}()

	_, _, ok := locks.TryLockAll([]string{key}, "cmd-2", "docker_update")
	assert.True(t, ok, "[AC-066] the next command must acquire the lock after a panic")
	locks.Unlock([]string{key}, "cmd-2")

	_, _, ok = locks.TryLockAll([]string{key}, "cmd-3", "docker_update")
	assert.True(t, ok, "[AC-066] the lock must also be free after a normal release")
}

// AC-063 / AC-064 (FR-054): TryLockAll is all-or-nothing and names the holder.
func TestCL_AC063_TryLockAllIsAllOrNothing(t *testing.T) {
	locks := newTargetLocks()
	_, _, ok := locks.TryLockAll([]string{"docker:container:db"}, "cmd-1", "docker_update")
	require.True(t, ok)

	key, holder, ok := locks.TryLockAll([]string{"docker:container:web", "docker:container:db"}, "cmd-2", "docker_bulk_update")

	assert.False(t, ok, "[AC-063] a held key must refuse the whole set")
	assert.Equal(t, "docker:container:db", key)
	assert.Equal(t, lockHolder{CmdID: "cmd-1", Action: "docker_update"}, holder)
	_, free := locks.Holder("docker:container:web")
	assert.False(t, free, "[AC-063] a refused set must not leave partial locks behind")
	assert.Equal(t, "update already in progress for db (command cmd-1)", conflictMessage(key, holder),
		"[AC-063] the conflict message must name the target and the holding command")
}

// AC-065 (FR-054): different targets never conflict.
func TestCL_AC065_DifferentKeysDoNotConflict(t *testing.T) {
	locks := newTargetLocks()
	_, _, okWeb := locks.TryLockAll([]string{"docker:container:web"}, "cmd-1", "docker_update")
	_, _, okDB := locks.TryLockAll([]string{"docker:container:db"}, "cmd-2", "docker_update")
	_, _, okHost := locks.TryLockAll([]string{"host"}, "cmd-3", "reboot")

	assert.True(t, okWeb && okDB && okHost, "[AC-065] distinct targets must all be lockable at once")
}

// AC-066: an Unlock by a different command must not free someone else's lock,
// and re-locking by the same command is a no-op success (bulk inner updates).
func TestCL_AC066_UnlockIgnoresForeignHolderAndRelockIsIdempotent(t *testing.T) {
	locks := newTargetLocks()
	_, _, ok := locks.TryLockAll([]string{"host"}, "cmd-1", "reboot")
	require.True(t, ok)

	locks.Unlock([]string{"host"}, "cmd-2")
	_, held := locks.Holder("host")
	assert.True(t, held, "[AC-066] a late release by another command must not free the lock")

	_, _, again := locks.TryLockAll([]string{"host"}, "cmd-1", "reboot")
	assert.True(t, again, "[AC-066] the holder may re-acquire its own key")
}

// SEC C-4(c): HeldWithPrefix finds any docker lock, deterministically.
func TestCL_SEC004_HeldWithPrefixFindsDockerLocks(t *testing.T) {
	locks := newTargetLocks()
	_, _, _ = locks.TryLockAll([]string{"host"}, "cmd-h", "reboot")

	_, _, found := locks.HeldWithPrefix("docker:")
	assert.False(t, found, "[SEC-004] the host key is not a docker lock")

	_, _, _ = locks.TryLockAll([]string{"docker:container:web", "docker:compose:/srv/app"}, "cmd-1", "docker_update")
	key, holder, found := locks.HeldWithPrefix("docker:")
	require.True(t, found)
	assert.Equal(t, "docker:compose:/srv/app", key, "[SEC-004] with several matches the smallest key is reported")
	assert.Equal(t, "cmd-1", holder.CmdID)
}

// NFR-010: concurrent lockers see exactly one winner per key (run with -race
// on Linux CI).
func TestCL_AC063_ConcurrentLockersHaveOneWinner(t *testing.T) {
	locks := newTargetLocks()
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := uuidN(i + 1)
			if _, _, ok := locks.TryLockAll([]string{"docker:container:web"}, id, "docker_update"); ok {
				winners.Add(1)
			}
		}(i)
	}
	wg.Wait()
	assert.Equal(t, int32(1), winners.Load(), "[AC-063] exactly one concurrent command may take the lock")
}
