package server

import (
	"context"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"log/slog"
	"time"
)

func (s *Server) storageQuotaWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			flushCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			for _, transition := range s.StorageQuota.DrainTransitions(now) {
				event := &models.Event{DeviceID: transition.DeviceID, Type: "storage_quota_exceeded", Severity: "warning", Message: "Storage quota entered degraded mode: " + transition.Class, CreatedAt: now}
				if err := s.EventRepo.Create(flushCtx, event); err == nil && s.Hub != nil {
					s.Hub.BroadcastEvent(event)
				}
			}
			if err := s.StorageQuota.Flush(flushCtx, now); err != nil {
				slog.Warn("storage quota flush failed")
			}
			_, err := s.DB.Pool.Exec(flushCtx, `DELETE FROM device_storage_usage WHERE day < $1::date`, now.UTC().AddDate(0, 0, -max(s.EffectiveContainerLogRetention()+1, 7)))
			if err != nil {
				slog.Warn("storage usage retention failed")
			}
			cancel()
			if s.StorageGuard != nil {
				if err := s.StorageGuard.Check(ctx, now); err != nil {
					slog.Warn("storage guard check failed")
				}
			}
		}
	}
}
