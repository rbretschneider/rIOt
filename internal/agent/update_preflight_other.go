//go:build !linux

package agent

import (
	"context"
	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
)

func updateInstallPreflight(collectors.CommandRunner) func(context.Context) error { return nil }
