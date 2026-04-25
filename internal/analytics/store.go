package analytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Store struct {
	DB *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{DB: db}
}

type EventInsert struct {
	EventName   string
	AnonymousID string
	UserID      *uuid.UUID
	SessionID   string
	AppVersion  string
	Platform    string
	Properties  map[string]any
	OccurredAt  time.Time
	ReceivedAt  time.Time
}

func (s *Store) Insert(ctx context.Context, e EventInsert) error {
	props, err := json.Marshal(e.Properties)
	if err != nil {
		return err
	}

	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO analytics_events (
			event_name,
			anonymous_id,
			user_id,
			session_id,
			app_version,
			platform,
			properties,
			occurred_at,
			received_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)
	`, e.EventName, e.AnonymousID, e.UserID, e.SessionID, e.AppVersion, e.Platform, string(props), e.OccurredAt.UTC(), e.ReceivedAt.UTC())
	return err
}
