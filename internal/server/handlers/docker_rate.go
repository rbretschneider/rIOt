package handlers

import (
	"github.com/DesyncTheThird/rIOt/internal/models"
	"net/http"
	"sync"
	"time"
)

type dockerReadRate struct {
	tokens float64
	last   time.Time
}

func (h *Handlers) dockerEventAllowed(device string, e *models.DockerEvent) bool {
	rt := h.dockerState()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	pruneDockerRuntimeLocked(rt, h.clock())
	entries := rt.eventThrottle[device]
	if entries == nil {
		entries = map[string]time.Time{}
		rt.eventThrottle[device] = entries
	}
	now := h.clock()
	pruneDockerRuntimeLocked(rt, now)
	key := e.ContainerName + "/" + e.Action
	if t, ok := entries[key]; ok && now.Sub(t) < time.Minute {
		return false
	}
	if len(entries) >= 512 {
		oldKey := ""
		var old time.Time
		for k, t := range entries {
			if oldKey == "" || t.Before(old) {
				oldKey = k
				old = t
			}
		}
		delete(entries, oldKey)
	}
	entries[key] = now
	return true
}
func (h *Handlers) dockerEventResponse(w http.ResponseWriter) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

var terminalAdmission = struct {
	sync.Mutex
	total int
	hosts map[string]int
}{hosts: map[string]int{}}

func (h *Handlers) allowTerminal(device string) (func(), bool) {
	rt := h.dockerState()
	rt.mu.Lock()
	now := h.clock()
	key := "terminal/" + device
	b := rt.reads[key]
	if b == nil {
		b = &dockerReadRate{tokens: 30, last: now}
		rt.reads[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * .5
	if b.tokens > 30 {
		b.tokens = 30
	}
	b.last = now
	if b.tokens < 1 {
		rt.mu.Unlock()
		return nil, false
	}
	b.tokens--
	rt.mu.Unlock()
	terminalAdmission.Lock()
	defer terminalAdmission.Unlock()
	if terminalAdmission.total >= 64 || terminalAdmission.hosts[device] >= 8 {
		return nil, false
	}
	terminalAdmission.total++
	terminalAdmission.hosts[device]++
	return func() {
		terminalAdmission.Lock()
		defer terminalAdmission.Unlock()
		terminalAdmission.total--
		terminalAdmission.hosts[device]--
		if terminalAdmission.hosts[device] == 0 {
			delete(terminalAdmission.hosts, device)
		}
	}, true
}

func pruneDockerRuntimeLocked(rt *dockerRuntime, now time.Time) {
	for key, b := range rt.reads {
		if now.Sub(b.last) > 10*time.Minute {
			delete(rt.reads, key)
		}
	}
	for device, b := range rt.rate {
		if now.Sub(b.window) > 10*time.Minute {
			delete(rt.rate, device)
		}
	}
	for device, entries := range rt.eventThrottle {
		for key, t := range entries {
			if now.Sub(t) > 10*time.Minute {
				delete(entries, key)
			}
		}
		if len(entries) == 0 {
			delete(rt.eventThrottle, device)
		}
	}
}
