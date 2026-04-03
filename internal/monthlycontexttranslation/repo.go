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

// EnsureCurrentMonthRow returns the quota row for the user's current UTC calendar month, inserting it if missing.
// On insert, monthly_limit is defaultMonthlyLimit and used_count is 0. Existing rows are left unchanged except updated_at.
func EnsureCurrentMonthRow(ctx context.Context, db *sql.DB, userID uuid.UUID, defaultMonthlyLimit int) (periodKey string, monthlyLimit, usedCount int, err error) {
	pk := PeriodKeyUTC(time.Now())
	err = db.QueryRowContext(ctx, `
		INSERT INTO monthly_context_translation_quotas (user_id, period_key, monthly_limit, used_count, created_at, updated_at)
		VALUES ($1, $2, $3, 0, now(), now())
		ON CONFLICT (user_id, period_key) DO UPDATE SET updated_at = monthly_context_translation_quotas.updated_at
		RETURNING period_key, monthly_limit, used_count
	`, userID, pk, defaultMonthlyLimit).Scan(&periodKey, &monthlyLimit, &usedCount)
	if err != nil {
		return "", 0, 0, err
	}
	return periodKey, monthlyLimit, usedCount, nil
}

// IncrementUsedCount increments used_count by 1 for the user's row for periodKey and returns the updated monthly_limit and used_count.
func IncrementUsedCount(ctx context.Context, db *sql.DB, userID uuid.UUID, periodKey string) (monthlyLimit, usedCount int, err error) {
	err = db.QueryRowContext(ctx, `
		UPDATE monthly_context_translation_quotas
		SET used_count = used_count + 1, updated_at = now()
		WHERE user_id = $1 AND period_key = $2
		RETURNING monthly_limit, used_count
	`, userID, periodKey).Scan(&monthlyLimit, &usedCount)
	return monthlyLimit, usedCount, err
}
