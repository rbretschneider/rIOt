//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// handleHostTerminalStart spawns a local PTY shell for host-level terminal access.
func (a *Agent) handleHostTerminalStart(ctx context.Context, wsConn *websocket.Conn, msg AgentWSMessage) {
	if !a.config.HostTerminal.Enabled {
		slog.Warn("host terminal: request denied, host_terminal not enabled")
		return
	}

	sessionID := msg.SessionID
	if sessionID == "" {
		return
	}

	shell := a.config.HostTerminal.Shell
	if shell == "" {
		shell = defaultShell()
	}

	slog.Info("host terminal: starting session", "session", sessionID, "shell", shell)

	cmd := exec.CommandContext(ctx, shell)
	cmd.Env = os.Environ()

	ptmx, err := pty.Start(cmd)
	if err != nil {
		slog.Error("host terminal: pty start failed", "error", err)
		return
	}

	sessCtx, cancel := context.WithCancel(ctx)

	sess := &hostTerminalSession{
		ptmx:   ptmx,
		cmd:    cmd,
		cancel: cancel,
	}

	hostTerminalSessions.Lock()
	hostTerminalSessions.m[sessionID] = sess
	hostTerminalSessions.Unlock()

	// Relay PTY output → server WS
	go func() {
		defer func() {
			cancel()
			hostTerminalSessions.Lock()
			delete(hostTerminalSessions.m, sessionID)
			hostTerminalSessions.Unlock()
			ptmx.Close()
			cmd.Wait()

			if a.wsClient != nil {
				a.wsClient.send(AgentWSMessage{
					Type:      "terminal_close",
					SessionID: sessionID,
				})
			}
		}()

		buf := make([]byte, 4096)
		for {
			select {
			case <-sessCtx.Done():
				return
			default:
			}
			n, err := ptmx.Read(buf)
			if n > 0 && a.wsClient != nil {
				data, _ := json.Marshal(string(buf[:n]))
				a.wsClient.send(AgentWSMessage{
					Type:      "terminal_output",
					SessionID: sessionID,
					Data:      data,
				})
			}
			if err != nil {
				return
			}
		}
	}()
}

func defaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	if _, err := exec.LookPath("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}
