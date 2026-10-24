package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC-036: the first updates telemetry leaves the agent at start, with no tick
// and no trigger, so it reaches the server well inside 60 seconds.
func TestTelemetryLoop_PF_AC036_FirstSendImmediateOnStart(t *testing.T) {
	fs := newFlushServer(t, always(200))
	a, _ := flushAgent(t, fs)
	a.config.Agent.PollInterval = 3600 // the periodic tick cannot be what sends it
	a.telemetryNow = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		a.telemetryLoop(ctx)
	}()

	require.Eventually(t, func() bool { return len(fs.got()) == 1 }, 2*time.Second, time.Millisecond,
		"[AC-036] the first telemetry must be sent on loop start, before any tick")
	assert.Equal(t, []string{"dev"}, fs.got())
	cancel()
	<-done
}
