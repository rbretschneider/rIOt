//go:build !linux

package agent

import (
	"context"
	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/maintenance"
)

func maintenanceReadiness(context.Context, collectors.CommandRunner) error {
	return maintenance.ErrUnsupported
}
