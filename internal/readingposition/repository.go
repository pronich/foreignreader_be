package readingposition

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("reading position not found")

type Repository struct {
	DB *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{DB: db}
}

func (r *Repository) LatestByUserAndBookFingerprint(ctx context.Context, userID uuid.UUID, bookFingerprint string) (Position, error) {
	var p Position
	var progress sql.NullFloat64
	err := r.DB.QueryRowContext(ctx, `
		SELECT id, user_id, book_fingerprint, chapter_id, character_offset, progress_fraction, device_id, updated_at, created_at
		FROM reading_positions
		WHERE user_id = $1 AND book_fingerprint = $2
		LIMIT 1
	`, userID, bookFingerprint).Scan(
		&p.ID,
		&p.UserID,
		&p.BookFingerprint,
		&p.ChapterID,
		&p.CharacterOffset,
		&progress,
		&p.DeviceID,
		&p.UpdatedAt,
		&p.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Position{}, ErrNotFound
		}
		return Position{}, err
	}
	if progress.Valid {
		v := progress.Float64
		p.ProgressFraction = &v
	}
	return p, nil
}

// UpsertLatestByUserAndBookFingerprint writes the incoming position iff incomingUpdatedAt is newer-or-equal
// to the currently stored updated_at (newest write wins). Returns applied=true when the row was
// inserted/updated, applied=false when the write was ignored (stale).
func (r *Repository) UpsertLatestByUserAndBookFingerprint(
	ctx context.Context,
	userID uuid.UUID,
	bookFingerprint string,
	chapterID string,
	characterOffset int,
	progressFraction *float64,
	deviceID string,
	incomingUpdatedAt time.Time,
) (applied bool, err error) {
	if r.DB == nil {
		return false, errors.New("database not configured")
	}

	var pf any
	if progressFraction != nil {
		pf = *progressFraction
	} else {
		pf = nil
	}

	res, err := r.DB.ExecContext(ctx, `
		INSERT INTO reading_positions (user_id, book_fingerprint, chapter_id, character_offset, progress_fraction, device_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, book_fingerprint) DO UPDATE
		SET chapter_id = EXCLUDED.chapter_id,
		    character_offset = EXCLUDED.character_offset,
		    progress_fraction = EXCLUDED.progress_fraction,
		    device_id = EXCLUDED.device_id,
		    updated_at = EXCLUDED.updated_at
		WHERE EXCLUDED.updated_at >= reading_positions.updated_at
	`, userID, bookFingerprint, chapterID, characterOffset, pf, deviceID, incomingUpdatedAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
