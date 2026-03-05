//go:build !windows

package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// hostTerminalSession holds a single host PTY session.
type hostTerminalSession struct {
	ptmx   *os.File
	cmd    *exec.Cmd
	cancel func()
}

var hostTerminalSessions = struct {
	sync.Mutex
	m map[string]*hostTerminalSession
}{m: make(map[string]*hostTerminalSession)}

func (a *Agent) handleHostTerminalInput(msg AgentWSMessage) {
	hostTerminalSessions.Lock()
	sess := hostTerminalSessions.m[msg.SessionID]
	hostTerminalSessions.Unlock()
	if sess == nil {
		return
	}
	var input string
	if err := json.Unmarshal(msg.Data, &input); err != nil {
		return
	}
	sess.ptmx.Write([]byte(input))
}

func (a *Agent) handleHostTerminalResize(msg AgentWSMessage) {
	hostTerminalSessions.Lock()
	sess := hostTerminalSessions.m[msg.SessionID]
	hostTerminalSessions.Unlock()
	if sess == nil {
		return
	}
	var resize terminalResizeMsg
	if err := json.Unmarshal(msg.Data, &resize); err != nil {
		return
	}
	pty.Setsize(sess.ptmx, &pty.Winsize{
		Rows: uint16(resize.Rows),
		Cols: uint16(resize.Cols),
	})
}

func (a *Agent) handleHostTerminalClose(msg AgentWSMessage) {
	hostTerminalSessions.Lock()
	sess := hostTerminalSessions.m[msg.SessionID]
	hostTerminalSessions.Unlock()
	if sess == nil {
		return
	}
	sess.cancel()
}
