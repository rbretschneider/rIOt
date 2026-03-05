package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// NotifyRepo handles notification_channels and notification_log database operations.
type NotifyRepo struct {
	db *DB
}

func NewNotifyRepo(db *DB) *NotifyRepo {
	return &NotifyRepo{db: db}
}

// --- Notification Channels ---

// ListChannels returns all notification channels.
func (r *NotifyRepo) ListChannels(ctx context.Context) ([]models.NotificationChannel, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, name, type, enabled, config, created_at, updated_at
		 FROM notification_channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	channels := []models.NotificationChannel{}
	for rows.Next() {
		var ch models.NotificationChannel
		var configJSON []byte
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Type, &ch.Enabled, &configJSON, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		ch.Config = make(map[string]interface{})
		json.Unmarshal(configJSON, &ch.Config)
		channels = append(channels, ch)
	}
	return channels, nil
}

// ListEnabledChannels returns only enabled notification channels.
func (r *NotifyRepo) ListEnabledChannels(ctx context.Context) ([]models.NotificationChannel, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, name, type, enabled, config, created_at, updated_at
		 FROM notification_channels WHERE enabled=true ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	channels := []models.NotificationChannel{}
	for rows.Next() {
		var ch models.NotificationChannel
		var configJSON []byte
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Type, &ch.Enabled, &configJSON, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		ch.Config = make(map[string]interface{})
		json.Unmarshal(configJSON, &ch.Config)
		channels = append(channels, ch)
	}
	return channels, nil
}

// GetChannel returns a single notification channel.
func (r *NotifyRepo) GetChannel(ctx context.Context, id int64) (*models.NotificationChannel, error) {
	var ch models.NotificationChannel
	var configJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, name, type, enabled, config, created_at, updated_at
		 FROM notification_channels WHERE id=$1`, id).Scan(
		&ch.ID, &ch.Name, &ch.Type, &ch.Enabled, &configJSON, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
		return nil, err
	}
	ch.Config = make(map[string]interface{})
	json.Unmarshal(configJSON, &ch.Config)
	return &ch, nil
}

// CreateChannel inserts a new notification channel.
func (r *NotifyRepo) CreateChannel(ctx context.Context, ch *models.NotificationChannel) error {
	now := time.Now().UTC()
	ch.CreatedAt = now
	ch.UpdatedAt = now
	configJSON, err := json.Marshal(ch.Config)
	if err != nil {
		return err
	}
	return r.db.Pool.QueryRow(ctx,
		`INSERT INTO notification_channels (name, type, enabled, config, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		ch.Name, ch.Type, ch.Enabled, configJSON, ch.CreatedAt, ch.UpdatedAt).Scan(&ch.ID)
}

// UpdateChannel modifies an existing notification channel.
func (r *NotifyRepo) UpdateChannel(ctx context.Context, ch *models.NotificationChannel) error {
	ch.UpdatedAt = time.Now().UTC()
	configJSON, err := json.Marshal(ch.Config)
	if err != nil {
		return err
	}
	_, err = r.db.Pool.Exec(ctx,
		`UPDATE notification_channels SET name=$1, type=$2, enabled=$3, config=$4, updated_at=$5
		 WHERE id=$6`,
		ch.Name, ch.Type, ch.Enabled, configJSON, ch.UpdatedAt, ch.ID)
	return err
}

// DeleteChannel removes a notification channel.
func (r *NotifyRepo) DeleteChannel(ctx context.Context, id int64) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM notification_channels WHERE id=$1`, id)
	return err
}

// --- Notification Log ---

// LogNotification records a notification attempt.
func (r *NotifyRepo) LogNotification(ctx context.Context, log *models.NotificationLog) error {
	log.CreatedAt = time.Now().UTC()
	return r.db.Pool.QueryRow(ctx,
		`INSERT INTO notification_log (channel_id, event_id, alert_rule_id, status, error_msg, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		log.ChannelID, log.EventID, log.AlertRuleID, log.Status, log.ErrorMsg, log.CreatedAt).Scan(&log.ID)
}

// ListNotificationLog returns recent notification log entries.
func (r *NotifyRepo) ListNotificationLog(ctx context.Context, limit, offset int) ([]models.NotificationLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool.Query(ctx,
		`SELECT id, channel_id, event_id, alert_rule_id, status, error_msg, created_at
		 FROM notification_log ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []models.NotificationLog{}
	for rows.Next() {
		var l models.NotificationLog
		if err := rows.Scan(&l.ID, &l.ChannelID, &l.EventID, &l.AlertRuleID, &l.Status, &l.ErrorMsg, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

// PurgeNotificationLog deletes log entries older than the given time.
func (r *NotifyRepo) PurgeNotificationLog(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM notification_log WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
