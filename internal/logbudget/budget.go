// Package logbudget prevents diagnostics from becoming an unbounded storage path.
package logbudget

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type state struct {
	mu sync.Mutex
	keys map[string]time.Time
	hour,timeSummary time.Time
	count,suppressed int
	maxPerHour int
}
type Handler struct { next slog.Handler; shared *state; attrs []slog.Attr; now func()time.Time }

func New(next slog.Handler,maxPerHour int)*Handler {
	return &Handler{next:next,shared:&state{keys:map[string]time.Time{},maxPerHour:maxPerHour},now:time.Now}
}
func(h *Handler) Enabled(ctx context.Context,level slog.Level)bool{return h.next.Enabled(ctx,level)}
func Relevant(message string)bool {
	m:=strings.ToLower(message)
	for _,word:=range []string{"docker","container log","partition","storage","ingest","buffer","quota","telemetry"}{if strings.Contains(m,word){return true}}
	return false
}
func(h *Handler) Handle(ctx context.Context,r slog.Record)error {
	if r.Level<slog.LevelWarn || !Relevant(r.Message){return h.next.Handle(ctx,r)}
	now:=h.now();device:="server"
	read:=func(a slog.Attr)bool{if a.Key=="device"||a.Key=="device_id"{device=a.Value.String()};return true}
	for _,a:=range h.attrs{read(a)};r.Attrs(read)
	message:=r.Message;if len(message)>256{message=message[:256]};if len(device)>128{device=device[:128]};key:=device+":"+message
	s:=h.shared;s.mu.Lock()
	for k,at:=range s.keys{if now.Sub(at)>=time.Minute{delete(s.keys,k)}}
	if s.hour.IsZero()||now.Sub(s.hour)>=time.Hour{s.hour=now;s.count=0}
	summary:=0
	if s.timeSummary.IsZero(){s.timeSummary=now}else if now.Sub(s.timeSummary)>=time.Minute{summary=s.suppressed;s.suppressed=0;s.timeSummary=now}
	_,seen:=s.keys[key];allow:=!seen&&s.count<s.maxPerHour
	if allow {if len(s.keys)>=4096{allow=false}else{s.keys[key]=now;s.count++}}
	if !allow{s.suppressed++};s.mu.Unlock()
	if summary>0{record:=slog.NewRecord(now,slog.LevelInfo,"diagnostic log records suppressed",0);record.AddAttrs(slog.Int("count",summary));if err:=h.next.Handle(ctx,record);err!=nil{return err}}
	if allow{return h.next.Handle(ctx,r)};return nil
}
func(h *Handler) WithAttrs(attrs []slog.Attr)slog.Handler {copy:=*h;copy.next=h.next.WithAttrs(attrs);copy.attrs=append(append([]slog.Attr(nil),h.attrs...),attrs...);return &copy}
func(h *Handler) WithGroup(name string)slog.Handler {copy:=*h;copy.next=h.next.WithGroup(name);return &copy}
