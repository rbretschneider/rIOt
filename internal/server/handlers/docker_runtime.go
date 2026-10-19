package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"sync"
	"time"
)

type dockerPending struct {
	maxBytes int
	device   string
	ch       chan json.RawMessage
}
type dockerPreview struct {
	device, actor, kind string
	expires             time.Time
	items               map[string]map[string]interface{}
}
type dockerRuntime struct {
	mu            sync.Mutex
	pending       map[string]dockerPending
	previews      map[string]dockerPreview
	streams       map[string]*dockerStream
	backfills     map[string]*dockerBackfill
	rate          map[string]*dockerRate
	viewers       int
	rpcSlots      chan struct{}
	controlMu     sync.Mutex
	controls      map[string]chan agentWSMessage
	reads         map[string]*dockerReadRate
	eventThrottle map[string]map[string]time.Time
}

var sharedDockerRuntimeOnce sync.Once
var sharedDockerRuntime *dockerRuntime

func newDockerRuntime() *dockerRuntime {
	return &dockerRuntime{pending: map[string]dockerPending{}, previews: map[string]dockerPreview{}, streams: map[string]*dockerStream{}, backfills: map[string]*dockerBackfill{}, rate: map[string]*dockerRate{}, rpcSlots: make(chan struct{}, 64), controls: map[string]chan agentWSMessage{}, reads: map[string]*dockerReadRate{}, eventThrottle: map[string]map[string]time.Time{}}
}
func (h *Handlers) dockerState() *dockerRuntime {
	h.dockerOnce.Do(func() {
		if h.agents == nil {
			sharedDockerRuntimeOnce.Do(func() { sharedDockerRuntime = newDockerRuntime() })
			h.dockerRuntime = sharedDockerRuntime
		} else {
			h.dockerRuntime = newDockerRuntime()
		}
	})
	return h.dockerRuntime
}
func dockerRandom() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func dockerActor(r *http.Request) (auth, sub, name string) {
	if c, ok := r.Context().Value(middleware.ActorClaimsKey).(jwt.MapClaims); ok {
		auth, _ = c["auth_method"].(string)
		sub, _ = c["actor_sub"].(string)
		name, _ = c["display_name"].(string)
	}
	return
}
func dockerDispatchRequest(r *http.Request, device, action string, p map[string]interface{}) commands.DispatchRequest {
	auth, sub, name := dockerActor(r)
	kind := "operator"
	if auth == "" {
		kind = "unknown"
	}
	return commands.DispatchRequest{DeviceID: device, Action: action, Params: p, ActorKind: kind, ActorAuth: auth, ActorSub: sub, ActorName: name}
}
func (h *Handlers) dockerRPC(r *http.Request, device string, data interface{}) (json.RawMessage, int) {
	rt := h.dockerState()
	select {
	case rt.rpcSlots <- struct{}{}:
		defer func() { <-rt.rpcSlots }()
	default:
		return nil, 429
	}
	ac := h.registry().get(device)
	if ac == nil {
		return nil, 502
	}
	id := dockerRandom()
	ch := make(chan json.RawMessage, 1)
	rt.mu.Lock()
	pruneDockerRuntimeLocked(rt,h.clock())
 inflight := 0
	for _, p := range rt.pending {
		if p.device == device {
			inflight++
		}
	}
	rate := rt.reads[device]
	if rate == nil {
		rate = &dockerReadRate{tokens: 30, last: h.clock()}
		rt.reads[device] = rate
	}
	elapsed := h.clock().Sub(rate.last).Seconds()
	rate.tokens += elapsed * .5
	if rate.tokens > 30 {
		rate.tokens = 30
	}
	rate.last = h.clock()
	if inflight >= 4 || rate.tokens < 1 {
		rt.mu.Unlock()
		return nil, 429
	}
	rate.tokens--
	maxBytes := 1 << 20
	if d, ok := data.(map[string]interface{}); ok && d["kind"] == "env" {
		maxBytes = 256 << 10
	}
	rt.pending[id] = dockerPending{device: device, ch: ch, maxBytes: maxBytes}
	rt.mu.Unlock()
	defer func() { rt.mu.Lock(); delete(rt.pending, id); rt.mu.Unlock() }()
	raw, _ := json.Marshal(data)
	if ac.Send(agentWSMessage{Type: "docker_read", SessionID: id, Data: raw}) != nil {
		return nil, 502
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	select {
	case raw := <-ch:
		return raw, 200
	case <-ctx.Done():
		return nil, 504
	}
}
func (h *Handlers) handleDockerAgentMessage(ac *AgentConn, msg agentWSMessage) {
	rt := h.dockerState()
	if msg.Type == "docker_read_result" {

		rt.mu.Lock()
		p, ok := rt.pending[msg.SessionID]
		rt.mu.Unlock()
		if ok && p.device == ac.DeviceID {
			if len(msg.Data) > p.maxBytes {
				msg.Data = json.RawMessage(`{"error":"too_large","reason":"too_large"}`)
			}
			select {
			case p.ch <- msg.Data:
			default:
			}
		}
		return
	}
	h.dockerRelay(ac, msg)
}
func (h *Handlers) DockerRead(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("X-Riot-Request") != "1" || (r.Header.Get("Origin") != middleware.RequestScheme(r)+"://"+r.Host && r.Header.Get("Sec-Fetch-Site") != "same-origin") {
		dockerError(w, 403, "origin_refused")
		return
	}
	h.dockerReadRequest(w, r)
}
