package readingposition

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrInvalidArgument = errors.New("invalid argument")
)

type Service struct {
	Repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{Repo: repo}
}

type LocalPosition struct {
	ChapterID        *string
	CharacterOffset  *int
	ProgressFraction *float64
	DeviceID         string
	UpdatedAt        time.Time
}

type ResolveDecision string

const (
	DecisionUseLocal  ResolveDecision = "useLocal"
	DecisionUseRemote ResolveDecision = "useRemote"
)

type ResolveResult struct {
	Decision       ResolveDecision
	RemotePosition *Position
}

// ResolvePosition decides whether the client should use local or remote position on book open.
// bookFingerprint is the stable cross-device book identity (not a device-local book id).
// Conflict rule is timestamp-based: if local is newer-or-equal, use local; else use remote.
func (s *Service) ResolvePosition(ctx context.Context, userID uuid.UUID, bookFingerprint string, local *LocalPosition) (ResolveResult, error) {
	if userID == uuid.Nil {
		return ResolveResult{}, ErrUnauthenticated
	}
	if bookFingerprint == "" {
		return ResolveResult{}, ErrInvalidArgument
	}

	remote, err := s.Repo.LatestByUserAndBookFingerprint(ctx, userID, bookFingerprint)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ResolveResult{Decision: DecisionUseLocal}, nil
		}
		return ResolveResult{}, err
	}

	if local == nil {
		return ResolveResult{Decision: DecisionUseRemote, RemotePosition: &remote}, nil
	}

	if !local.UpdatedAt.IsZero() && (local.UpdatedAt.Equal(remote.UpdatedAt) || local.UpdatedAt.After(remote.UpdatedAt)) {
		return ResolveResult{Decision: DecisionUseLocal}, nil
	}
	return ResolveResult{Decision: DecisionUseRemote, RemotePosition: &remote}, nil
}

// SavePosition upserts the latest position for user+book fingerprint, but never overwrites newer state.
func (s *Service) SavePosition(
	ctx context.Context,
	userID uuid.UUID,
	bookFingerprint string,
	chapterID string,
	characterOffset int,
	progressFraction *float64,
	deviceID string,
	updatedAt time.Time,
) (applied bool, err error) {
	if userID == uuid.Nil {
		return false, ErrUnauthenticated
	}
	if bookFingerprint == "" || chapterID == "" || deviceID == "" || updatedAt.IsZero() || characterOffset < 0 {
		return false, ErrInvalidArgument
	}
	return s.Repo.UpsertLatestByUserAndBookFingerprint(ctx, userID, bookFingerprint, chapterID, characterOffset, progressFraction, deviceID, updatedAt)
}
