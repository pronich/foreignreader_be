package monthlycontexttranslation

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

const periodLayout = "2006-01"

// PeriodKeyUTC returns the calendar month identifier YYYY-MM for t in UTC.
func PeriodKeyUTC(t time.Time) string {
	return t.UTC().Format(periodLayout)
}

// InsertInitial creates a row for the given user, period, and monthly_limit with used_count = 0.
func InsertInitial(ctx context.Context, tx *sql.Tx, userID uuid.UUID, periodKey string, monthlyLimit int) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO monthly_context_translation_quotas (user_id, period_key, monthly_limit, used_count, created_at, updated_at)
		VALUES ($1, $2, $3, 0, now(), now())
	`, userID, periodKey, monthlyLimit)
	return err
}
