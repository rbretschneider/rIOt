package handlers

import (
	"context"
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	ws "github.com/gorilla/websocket"
	"net/http"
	"sync"
	"time"
)

type dockerViewer struct {
	deferred     map[string]*time.Timer
	boundaries   map[string]string
	pendingDrops int
	audits       map[string]int64
	mu           sync.Mutex
	conn         *ws.Conn
	q            chan []byte
	bytes        int
	closed       chan struct{}
	once         sync.Once
	dropped      time.Time
	last         map[string]time.Time
	device       string
}

func (v *dockerViewer) close() {
	v.once.Do(func() {
		close(v.closed)
		if v.conn != nil {
			v.conn.Close()
		}
	})
}
func (v *dockerViewer) push(data interface{}) {
	b, e := json.Marshal(data)
	if e != nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	select {
	case <-v.closed:
		return
	default:
	}
	if v.bytes+len(b) > 1<<20 || len(v.q) >= 64 {
		v.pendingDrops++
		if v.dropped.IsZero() {
			v.dropped = time.Now()
		}
		if time.Since(v.dropped) > 10*time.Second {
			v.close()
		}
		return
	}
	if v.pendingDrops > 0 && len(v.q) < 63 {
		marker, _ := json.Marshal(map[string]interface{}{"type": "marker", "kind": "dropped", "n": v.pendingDrops})
		if v.bytes+len(marker)+len(b) <= 1<<20 {
			v.bytes += len(marker)
			v.q <- marker
			v.pendingDrops = 0
		}
	}
	v.dropped = time.Time{}
	v.bytes += len(b)
	select {
	case v.q <- b:
	default:
		v.bytes -= len(b)
	}
}

type dockerStream struct {
	id, device, name string
	viewers          map[*dockerViewer]bool
	linger           *time.Timer
	window           time.Time
	bytes            int
}
type dockerBackfill struct {
	id, device, name, boundary string
	viewers                    map[*dockerViewer]bool
	active                     bool
	created                    time.Time
}
type dockerRate struct {
	window         time.Time
	bytes, unknown int
}

// Control writes are ordered per agent and never block the agent read loop.
func (h *Handlers) dockerSend(device, kind string, data interface{}) bool {
	if !h.registry().connected(device) {
		return false
	}
	rt := h.dockerState()
	raw, e := json.Marshal(data)
	if e != nil {
		return false
	}
	msg := agentWSMessage{Type: kind, Data: raw}
	rt.controlMu.Lock()
	defer rt.controlMu.Unlock()
	q := rt.controls[device]
	if q == nil {
		q = make(chan agentWSMessage, 32)
		rt.controls[device] = q
		go h.dockerControlWriter(device, q)
	}
	select {
	case q <- msg:
		return true
	default:
		return false
	}
}
func (h *Handlers) dockerControlWriter(device string, q chan agentWSMessage) {
	idle := time.NewTimer(time.Minute)
	defer idle.Stop()
	for {
		select {
		case msg := <-q:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(time.Minute)
			if ac := h.registry().get(device); ac != nil {
				ac.Send(msg)
			}
		case <-idle.C:
			rt := h.dockerState()
			rt.controlMu.Lock()
			if len(q) == 0 && rt.controls[device] == q {
				delete(rt.controls, device)
				rt.controlMu.Unlock()
				return
			}
			rt.controlMu.Unlock()
			idle.Reset(time.Minute)
		}
	}
}
func (h *Handlers) DockerLogsWS(w http.ResponseWriter, r *http.Request) {
	device := chi.URLParam(r, "deviceId")
	if !models.IsCanonicalUUID(device) {
		dockerError(w, 400, "invalid_identifier")
		return
	}
	if r.Header.Get("Origin") == "" || r.Header.Get("Origin") != middleware.RequestScheme(r)+"://"+r.Host {
		dockerError(w, 403, "origin_refused")
		return
	}
	if !h.dockerCap(w, r, device, models.DockerCapLogsLive) {
		return
	}
	if !h.registry().connected(device) {
		dockerError(w, 502, "agent_offline")
		return
	}
	expiry := h.clock().Add(4 * time.Hour)
	if c, ok := r.Context().Value(middleware.ActorClaimsKey).(jwt.MapClaims); ok {
		if t, e := c.GetExpirationTime(); e == nil && t != nil && t.Time.Before(expiry) {
			expiry = t.Time
		}
	}
	up := ws.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, e := up.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	v := &dockerViewer{conn: conn, q: make(chan []byte, 64), closed: make(chan struct{}), last: map[string]time.Time{}, device: device}
	defer v.close()
	defer h.dockerDetach(v, nil)
	conn.SetReadLimit(4096)
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(90 * time.Second)) })
	go func() {
		ping := time.NewTicker(30 * time.Second)
		keep := time.NewTicker(time.Minute)
		defer ping.Stop()
		defer keep.Stop()
		timer := time.NewTimer(time.Until(expiry))
		defer timer.Stop()
		for {
			select {
			case b := <-v.q:
				v.mu.Lock()
				v.bytes -= len(b)
				v.mu.Unlock()
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if conn.WriteMessage(ws.TextMessage, b) != nil {
					v.close()
					return
				}
			case <-ping.C:
				if conn.WriteControl(ws.PingMessage, nil, time.Now().Add(10*time.Second)) != nil {
					v.close()
					return
				}
			case <-keep.C:
				rt := h.dockerState()
				rt.mu.Lock()
				ids := []string{}
				for _, s := range rt.streams {
					if s.device == device && len(s.viewers) > 0 {
						ids = append(ids, s.id)
					}
				}
				rt.mu.Unlock()
				if len(ids) > 0 {
					h.dockerSend(device, "logs_keep", map[string]interface{}{"stream_ids": ids})
				}
			case <-timer.C:
				conn.SetWriteDeadline(time.Now().Add(time.Second))
				conn.WriteJSON(map[string]string{"type": "error", "code": "session_expired"})
				v.close()
				return
			case <-v.closed:
				return
			}
		}
	}()
	v.push(map[string]interface{}{"type": "session", "state": "connecting", "server_now": h.clock()})
	for {
		var msg struct {
			Type       string          `json:"type"`
			Containers json.RawMessage `json:"containers"`
			Merged     bool            `json:"merged"`
		}
		if conn.ReadJSON(&msg) != nil {
			return
		}
		if !h.clock().Before(expiry) {
			return
		}
		switch msg.Type {
		case "attach":
			var targets []struct {
				Name     string `json:"name"`
				Boundary string `json:"boundary"`
			}
			if json.Unmarshal(msg.Containers, &targets) != nil || len(targets) > 25 {
				v.push(map[string]string{"type": "error", "code": "invalid_request"})
				continue
			}
			for _, t := range targets {
				h.dockerAttach(r, v, t.Name, t.Boundary)
			}
		case "detach":
			var names []string
			if len(msg.Containers) > 0 && json.Unmarshal(msg.Containers, &names) != nil {
				continue
			}
			h.dockerDetach(v, names)
		default:
			v.push(map[string]string{"type": "error", "code": "invalid_request"})
		}
	}
}
func (h *Handlers) dockerAttach(r *http.Request, v *dockerViewer, name, boundary string) {
	if !dockerName.MatchString(name) {
		v.push(map[string]string{"type": "error", "container": name, "code": "invalid_request"})
		return
	}
	snap, ok := h.dockerSnapshot(r, v.device)
	found := false
	if ok {
		for _, c := range snap.Data.Docker.Containers {
			if c.Name == name || c.Name == "/"+name {
				found = true
			}
		}
	}
	if !found {
		v.push(map[string]string{"type": "error", "container": name, "code": "not_found"})
		return
	}
	rt := h.dockerState()
	rt.mu.Lock()
	key := v.device + "/" + name
	s := rt.streams[key]
	fresh := false
	if s == nil {
		count := 0
		for _, st := range rt.streams {
			if st.device == v.device {
				count++
			}
		}
		if count >= 8 {
			rt.mu.Unlock()
			v.push(map[string]string{"type": "error", "container": name, "code": "too_many_streams"})
			return
		}
		s = &dockerStream{id: dockerRandom(), device: v.device, name: name, viewers: map[*dockerViewer]bool{}}
		rt.streams[key] = s
		fresh = true
	}
	if !s.viewers[v] && (rt.viewers >= 64 || len(s.viewers) >= 8) {
		if fresh {
			delete(rt.streams, key)
		}
		rt.mu.Unlock()
		v.push(map[string]string{"type": "error", "container": name, "code": "too_many_viewers"})
		return
	}
	if s.linger != nil {
		s.linger.Stop()
		s.linger = nil
	}
	if !s.viewers[v] {
		s.viewers[v] = true
		rt.viewers++
	}
	if fresh {
		h.dockerSend(v.device, "logs_follow", map[string]string{"stream_id": s.id, "container": name})
	}
	rt.mu.Unlock()
	v.push(map[string]interface{}{"type": "session", "state": "live", "server_now": h.clock()})
	days := h.containerLogRetentionDays
	if days < 1 {
		days = 7
	}
	if t, e := time.Parse(time.RFC3339Nano, boundary); e != nil || t.Before(h.clock().Add(-time.Duration(days)*24*time.Hour)) || t.After(h.clock().Add(5*time.Minute)) {
		boundary = ""
	}

	v.mu.Lock()
	last := v.last[name]
	if h.clock().Sub(last) < 10*time.Second {
		if v.deferred == nil {
			v.deferred = map[string]*time.Timer{}
			v.boundaries = map[string]string{}
		}
		v.boundaries[name] = boundary
		if v.deferred[name] == nil {
			wait := 10*time.Second - h.clock().Sub(last)
			v.deferred[name] = time.AfterFunc(wait, func() {
				v.mu.Lock()
				b := v.boundaries[name]
				delete(v.deferred, name)
				delete(v.boundaries, name)
				v.last[name] = h.clock()
				v.mu.Unlock()
				select {
				case <-v.closed:
					return
				default:
				}
				rt := h.dockerState()
				rt.mu.Lock()
				stream := rt.streams[v.device+"/"+name]
				attached := stream != nil && stream.viewers[v]
				rt.mu.Unlock()
				if attached {
					h.dockerBackfill(v, name, b)
				}
			})
		}
		v.mu.Unlock()
		return
	}
	if len(v.last) >= 25 {
		oldName := ""
		var oldest time.Time
		for key, t := range v.last {
			if oldName == "" || t.Before(oldest) {
				oldName = key
				oldest = t
			}
		}
		delete(v.last, oldName)
	}
	v.last[name] = h.clock()
	v.mu.Unlock()
	if h.dockerRepo != nil {
		_, sub, _ := dockerActor(r)
		id, e := h.dockerRepo.StartLogAudit(r.Context(), v.device, name, sub, middleware.ClientIP(r))
		if e == nil {
			v.mu.Lock()
			if v.audits == nil {
				v.audits = map[string]int64{}
			}
			v.audits[name] = id
			v.mu.Unlock()
		}
	}
	h.dockerBackfill(v, name, boundary)
}
func (h *Handlers) dockerBackfill(v *dockerViewer, name, boundary string) {
	rt := h.dockerState()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	active, total := 0, 0
	for _, b := range rt.backfills {
		if b.device == v.device {
			total++
			if b.active {
				active++
			}
			if b.name == name && b.boundary == boundary {
				b.viewers[v] = true
				return
			}
		}
	}
	if total >= 18 {
		v.push(map[string]interface{}{"type": "marker", "container": name, "kind": "dropped"})
		return
	}
	b := &dockerBackfill{id: dockerRandom(), device: v.device, name: name, boundary: boundary, viewers: map[*dockerViewer]bool{v: true}, created: h.clock(), active: active < 2}
	rt.backfills[b.id] = b
	if b.active {
		h.dockerStartBackfill(b)
	}
	time.AfterFunc(20*time.Second, func() { h.dockerEndBackfill(b.id) })
}
func (h *Handlers) dockerStartBackfill(b *dockerBackfill) {
	h.dockerSend(b.device, "logs_backfill", map[string]interface{}{"request_id": b.id, "container": b.name, "after": b.boundary, "limit": 5000})
}
func (h *Handlers) dockerEndBackfill(id string) {
	rt := h.dockerState()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	b := rt.backfills[id]
	if b == nil {
		return
	}
	delete(rt.backfills, id)
	if !b.active {
		return
	}
	var next *dockerBackfill
	for _, candidate := range rt.backfills {
		if candidate.device == b.device && !candidate.active && (next == nil || candidate.created.Before(next.created)) {
			next = candidate
		}
	}
	if next != nil {
		next.active = true
		h.dockerStartBackfill(next)
	}
}
func (h *Handlers) dockerDetach(v *dockerViewer, names []string) {
	v.mu.Lock()
	for name, timer := range v.deferred {
		match := len(names) == 0
		for _, n := range names {
			if n == name {
				match = true
			}
		}
		if match {
			timer.Stop()
			delete(v.deferred, name)
			delete(v.boundaries, name)
		}
	}
	v.mu.Unlock()

	defer func() {
		if h.dockerRepo == nil {
			return
		}
		v.mu.Lock()
		ids := []int64{}
		for name, id := range v.audits {
			match := len(names) == 0
			for _, n := range names {
				if n == name {
					match = true
				}
			}
			if match {
				ids = append(ids, id)
				delete(v.audits, name)
			}
		}
		v.mu.Unlock()
		for _, id := range ids {
			h.dockerRepo.EndLogAudit(context.Background(), id)
		}
	}()

	rt := h.dockerState()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for key, s := range rt.streams {
		if s.device != v.device || !s.viewers[v] {
			continue
		}
		match := len(names) == 0
		for _, n := range names {
			if n == s.name {
				match = true
			}
		}
		if !match {
			continue
		}
		delete(s.viewers, v)
		rt.viewers--
		if len(s.viewers) == 0 && s.linger == nil {
			key, stream := key, s
			s.linger = time.AfterFunc(2*time.Second, func() {
				rt.mu.Lock()
				defer rt.mu.Unlock()
				if rt.streams[key] == stream && len(stream.viewers) == 0 {
					h.dockerSend(stream.device, "logs_unfollow", map[string]string{"stream_id": stream.id})
					delete(rt.streams, key)
				}
			})
		}
	}
	for id, b := range rt.backfills {
		delete(b.viewers, v)
		if len(b.viewers) == 0 && !b.active {
			delete(rt.backfills, id)
		}
	}
}
func (h *Handlers) dockerRelay(ac *AgentConn, msg agentWSMessage) {
	if len(msg.Data) > 64<<10 {
		return
	}
	var frame struct {
		StreamID  string `json:"stream_id"`
		RequestID string `json:"request_id"`
		Lines     []struct {
			S string    `json:"s"`
			T time.Time `json:"t"`
			L string    `json:"l"`
			X bool      `json:"x"`
		} `json:"lines"`
		Kind      string `json:"kind"`
		N         int    `json:"n"`
		Skipped   int    `json:"skipped"`
		SkippedLB bool   `json:"skipped_lb"`
	}
	if json.Unmarshal(msg.Data, &frame) != nil || len(frame.Lines) > 256 {
		return
	}
	rt := h.dockerState()
	rt.mu.Lock()
	now := h.clock()
	rate := rt.rate[ac.DeviceID]
	if rate == nil {
		rate = &dockerRate{}
		rt.rate[ac.DeviceID] = rate
	}
	if now.Sub(rate.window) >= time.Second {
		rate.window = now
		rate.bytes = 0
		rate.unknown = 0
	}
	rate.bytes += len(msg.Data)
	if rate.bytes > 3<<20 {
		rt.mu.Unlock()
		return
	}
	var viewers map[*dockerViewer]bool
	name, src := "", "live"
	if frame.RequestID != "" {
		b := rt.backfills[frame.RequestID]
		if b != nil && b.device == ac.DeviceID {
			viewers = b.viewers
			name = b.name
			src = "backfill"
		}
	} else {
		for _, s := range rt.streams {
			if s.id == frame.StreamID && s.device == ac.DeviceID {
				if now.Sub(s.window) >= time.Second {
					s.window = now
					s.bytes = 0
				}
				s.bytes += len(msg.Data)
				if s.bytes > 768<<10 {
					rt.mu.Unlock()
					return
				}
				viewers = s.viewers
				name = s.name
				break
			}
		}
	}
	if viewers == nil {
		if frame.StreamID != "" && len(frame.StreamID) == 32 && rate.unknown < 4 {
			rate.unknown++
			h.dockerSend(ac.DeviceID, "logs_unfollow", map[string]string{"stream_id": frame.StreamID})
		}
		rt.mu.Unlock()
		return
	}
	targets := []*dockerViewer{}
	for v := range viewers {
		targets = append(targets, v)
	}
	rt.mu.Unlock()
	if msg.Type == "logs_data" {
		lines := []map[string]interface{}{}
		days := h.containerLogRetentionDays
		if days < 1 {
			days = 7
		}
		for _, line := range frame.Lines {
			if line.S != "o" && line.S != "e" {
				continue
			}
			text := models.TruncateHead(models.SanitizeText(line.L), 16<<10)
			text, _ = redact.MaskLine(text)
			entry := map[string]interface{}{"t": line.T.Truncate(time.Microsecond), "s": line.S, "l": text}
			if line.X || len(line.L) > 16<<10 {
				entry["x"] = true
			}
			if line.T.Before(now.Add(-time.Duration(days)*24*time.Hour)) || line.T.After(now.Add(5*time.Minute)) {
				entry["agent_t"] = line.T
				entry["t"] = now.Truncate(time.Microsecond)
			}
			lines = append(lines, entry)
		}
		for _, v := range targets {
			v.push(map[string]interface{}{"type": "lines", "container": name, "src": src, "lines": lines})
		}
	} else {
		switch frame.Kind {
		case "backfill_end", "dropped", "exited", "restarted":
			for _, v := range targets {
				v.push(map[string]interface{}{"type": "marker", "container": name, "kind": frame.Kind, "n": frame.N, "skipped": frame.Skipped, "skipped_lb": frame.SkippedLB})
			}
		case "unavailable", "ended":
			for _, v := range targets {
				v.push(map[string]string{"type": "session", "state": "unavailable"})
			}
		}
		if frame.Kind == "backfill_end" || frame.Kind == "ended" || frame.Kind == "unavailable" {
			h.dockerEndBackfill(frame.RequestID)
		}
	}
}

func (h *Handlers) dockerAgentConnected(device string) {
	rt := h.dockerState()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, s := range rt.streams {
		if s.device == device && len(s.viewers) > 0 {
			h.dockerSend(device, "logs_follow", map[string]string{"stream_id": s.id, "container": s.name})
			for v := range s.viewers {
				v.push(map[string]interface{}{"type": "session", "state": "live", "server_now": h.clock()})
			}
		}
	}
}
func (h *Handlers) dockerAgentGone(device string) {
	rt := h.dockerState()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, s := range rt.streams {
		if s.device == device {
			for v := range s.viewers {
				v.push(map[string]string{"type": "session", "state": "reconnecting"})
			}
		}
	}
}
