package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Added by QA Engineer (PATCH-FLOW-001).

// Covers C-3: only a run that has written its terminal record is excused. A
// helper that is active and still running this same command is real work, so a
// reboot must not start under it. Mutation testing showed the developer tests
// accepted "same command id" alone as the excuse.
func TestMaintenanceBlockerFor_QA_C3_OwnRunStillInProgressIsStillBusy(t *testing.T) {
	a := &Agent{maintenanceClient: ownHelperFixture(idA, false)} // idA's record is not terminal

	assert.Equal(t, maintenanceRunning, a.maintenanceBlockerFor(context.Background(), idA),
		"[C-3] an unfinished run of the same command still blocks a reboot")
}
