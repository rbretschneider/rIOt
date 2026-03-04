package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/gorilla/websocket"
)

// terminalSession holds a single exec attach session.
type terminalSession struct {
	execID    string
	conn      io.ReadWriteCloser
	cancel    context.CancelFunc
	dockerCli *client.Client
}

// terminalSessions tracks active terminal sessions by session ID.
var terminalSessions = struct {
	sync.Mutex
	m map[string]*terminalSession
}{m: make(map[string]*terminalSession)}

type terminalResizeMsg struct {
	Cols uint `json:"cols"`
	Rows uint `json:"rows"`
}

func (a *Agent) handleTerminalStart(ctx context.Context, wsConn *websocket.Conn, msg AgentWSMessage) {
	if !a.config.Docker.TerminalEnabled {
		slog.Warn("terminal: request denied, terminal not enabled")
		return
	}

	sessionID := msg.SessionID
	containerID := msg.ContainerID
	if sessionID == "" || containerID == "" {
		return
	}

	slog.Info("terminal: starting session", "session", sessionID, "container", containerID)

	cli, err := newDockerClient(a.config.Docker.SocketPath)
	if err != nil {
		slog.Error("terminal: docker client error", "error", err)
		return
	}

	execConfig := container.ExecOptions{
		Cmd:          []string{"/bin/sh"},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	}

	execResp, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		slog.Error("terminal: exec create failed", "error", err)
		cli.Close()
		return
	}

	attachResp, err := cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		slog.Error("terminal: exec attach failed", "error", err)
		cli.Close()
		return
	}

	sessCtx, cancel := context.WithCancel(ctx)
	sess := &terminalSession{
		execID:    execResp.ID,
		conn:      attachResp.Conn,
		cancel:    cancel,
		dockerCli: cli,
	}

	terminalSessions.Lock()
	terminalSessions.m[sessionID] = sess
	terminalSessions.Unlock()

	// Relay exec stdout → server WS
	go func() {
		defer func() {
			cancel()
			terminalSessions.Lock()
			delete(terminalSessions.m, sessionID)
			terminalSessions.Unlock()
			attachResp.Close()
			cli.Close()

			// Notify server session ended
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
			n, err := attachResp.Reader.Read(buf)
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

func (a *Agent) handleTerminalInput(msg AgentWSMessage) {
	terminalSessions.Lock()
	sess := terminalSessions.m[msg.SessionID]
	terminalSessions.Unlock()
	if sess == nil {
		return
	}

	var input string
	if err := json.Unmarshal(msg.Data, &input); err != nil {
		return
	}
	sess.conn.Write([]byte(input))
}

func (a *Agent) handleTerminalResize(msg AgentWSMessage) {
	terminalSessions.Lock()
	sess := terminalSessions.m[msg.SessionID]
	terminalSessions.Unlock()
	if sess == nil {
		return
	}

	var resize terminalResizeMsg
	if err := json.Unmarshal(msg.Data, &resize); err != nil {
		return
	}

	sess.dockerCli.ContainerExecResize(context.Background(), sess.execID, container.ResizeOptions{
		Height: resize.Rows,
		Width:  resize.Cols,
	})
}

func (a *Agent) handleTerminalClose(msg AgentWSMessage) {
	terminalSessions.Lock()
	sess := terminalSessions.m[msg.SessionID]
	terminalSessions.Unlock()
	if sess == nil {
		return
	}
	sess.cancel()
}
