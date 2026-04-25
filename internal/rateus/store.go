package rateus

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Store struct {
	DB *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{DB: db}
}

type State struct {
	LastAttemptAt         time.Time
	LastAttemptAppVersion string
}

func (s *Store) Get(ctx context.Context, userID uuid.UUID) (*State, error) {
	var st State
	err := s.DB.QueryRowContext(ctx, `
		SELECT last_attempt_at, last_attempt_app_version
		FROM user_rate_prompt_state
		WHERE user_id = $1
	`, userID).Scan(&st.LastAttemptAt, &st.LastAttemptAppVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	st.LastAttemptAt = st.LastAttemptAt.UTC()
	st.LastAttemptAppVersion = strings.TrimSpace(st.LastAttemptAppVersion)
	return &st, nil
}

func (s *Store) UpsertAttempt(ctx context.Context, userID uuid.UUID, appVersion string, at time.Time) (State, error) {
	appVersion = strings.TrimSpace(appVersion)
	at = at.UTC()

	var st State
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO user_rate_prompt_state (
			user_id,
			last_attempt_at,
			last_attempt_app_version,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (user_id) DO UPDATE SET
			last_attempt_at = EXCLUDED.last_attempt_at,
			last_attempt_app_version = EXCLUDED.last_attempt_app_version,
			updated_at = now()
		RETURNING last_attempt_at, last_attempt_app_version
	`, userID, at, appVersion).Scan(&st.LastAttemptAt, &st.LastAttemptAppVersion)
	if err != nil {
		return State{}, err
	}
	st.LastAttemptAt = st.LastAttemptAt.UTC()
	st.LastAttemptAppVersion = strings.TrimSpace(st.LastAttemptAppVersion)
	return st, nil
}
