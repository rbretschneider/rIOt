package logbudget

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)
func TestDUX_AC141_DiagnosticBudget(t *testing.T){
	var out bytes.Buffer;now:=time.Unix(100000,0);h:=New(slog.NewJSONHandler(&out,nil),2);h.now=func()time.Time{return now};logger:=slog.New(h)
	for i:=0;i<100;i++{logger.Warn("container log ceiling applied","device","host-a")}
	logger.Error("partition drop failed");logger.Error("Docker inventory failed")
	if strings.Count(out.String(),"\n")!=2{t.Fatalf("budget did not bind: %s",out.String())}
	now=now.Add(time.Minute);logger.Warn("container log ceiling applied","device","host-a")
	if !strings.Contains(out.String(),"diagnostic log records suppressed"){t.Fatal("missing suppression summary")}
	if strings.Count(out.String(),"\n")!=3{t.Fatal("hourly budget reset early")}
	now=now.Add(time.Hour);logger.Log(context.Background(),slog.LevelWarn,"container log ceiling applied");if strings.Count(out.String(),"\n")<4{t.Fatal("hourly budget never refilled")}
}
