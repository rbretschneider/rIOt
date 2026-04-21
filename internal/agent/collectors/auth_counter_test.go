package collectors

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// [AC-005] After Add but before MarkReady, IsReady returns false.
func TestAC005_AuthCounter_NotReadyBeforeMarkReady(t *testing.T) {
	c := &authFailureCounter{}
	c.Add(5)
	assert.False(t, c.IsReady(), "[AC-005] counter must not be ready before MarkReady is called")
}

// [AC-005] After MarkReady, IsReady returns true.
func TestAC005_AuthCounter_ReadyAfterMarkReady(t *testing.T) {
	c := &authFailureCounter{}
	c.MarkReady()
	assert.True(t, c.IsReady(), "[AC-005] counter must be ready after MarkReady")
}

// [AC-005] MarkReady is idempotent — calling it multiple times does not panic or reset.
func TestAC005_AuthCounter_MarkReadyIsIdempotent(t *testing.T) {
	c := &authFailureCounter{}
	c.MarkReady()
	c.MarkReady()
	c.MarkReady()
	assert.True(t, c.IsReady(), "[AC-005] repeated MarkReady calls must leave counter ready")
}

// Drain returns the accumulated value and resets to zero.
func TestAuthCounter_DrainReturnsValueAndResetsToZero(t *testing.T) {
	c := &authFailureCounter{}
	c.Add(3)
	c.Add(7)
	v := c.Drain()
	assert.Equal(t, 10, v, "drain must return sum of all Add calls")
	v2 := c.Drain()
	assert.Equal(t, 0, v2, "drain must reset counter to zero")
}

// Add increments the counter by the given delta.
func TestAuthCounter_AddAccumulates(t *testing.T) {
	c := &authFailureCounter{}
	c.Add(1)
	c.Add(1)
	c.Add(1)
	assert.Equal(t, 3, c.Drain(), "Add must accumulate across calls")
}

// [AC-007] Concurrent Add calls are race-free and do not lose counts.
func TestAC007_AuthCounter_ConcurrentAddIsRaceFree(t *testing.T) {
	c := &authFailureCounter{}
	const goroutines = 50
	const addsPerGoroutine = 100

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < addsPerGoroutine; j++ {
				c.Add(1)
			}
		}()
	}
	wg.Wait()

	total := c.Drain()
	assert.Equal(t, goroutines*addsPerGoroutine, total,
		"[AC-007] concurrent Add must not lose counts — expected %d got %d",
		goroutines*addsPerGoroutine, total)
}

// Drain after zero Adds returns zero.
func TestAuthCounter_DrainOnEmptyCounterReturnsZero(t *testing.T) {
	c := &authFailureCounter{}
	assert.Equal(t, 0, c.Drain(), "fresh counter drain must return zero")
}
