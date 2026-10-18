//go:build windows

package agent

import (
	"context"
	"log/slog"
)

// Host terminal is not supported on Windows.

type hostTerminalSession struct{}

func (a *Agent) handleHostTerminalStart(ctx context.Context, msg AgentWSMessage) {
	slog.Warn("host terminal: not supported on Windows")
}

func (a *Agent) handleHostTerminalInput(msg AgentWSMessage)  {}
func (a *Agent) handleHostTerminalResize(msg AgentWSMessage) {}
func (a *Agent) handleHostTerminalClose(msg AgentWSMessage)  {}
