package readingposition

import (
	"time"

	"github.com/google/uuid"
)

// Position is the latest synced reading position for one user + one book.
// Restore anchor is ChapterID + CharacterOffset. ProgressFraction is metadata only.
type Position struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	BookID           string
	ChapterID        string
	CharacterOffset  int
	ProgressFraction *float64
	DeviceID         string
	UpdatedAt        time.Time
	CreatedAt        time.Time
}
