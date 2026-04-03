package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/entitlement"
	"foreignreader_be/internal/readingposition"
)

func registerReadingPositionRoutes(
	mux *http.ServeMux,
	store *auth.Store,
	issuer *auth.TokenIssuer,
	ent *entitlement.Store,
	svc *readingposition.Service,
) {
	resolveHandler := bearerAuthHandler(store, issuer, requireProMiddleware(ent, handleReadingPositionResolve(svc)))
	saveHandler := bearerAuthHandler(store, issuer, requireProMiddleware(ent, handleReadingPositionSave(svc)))

	mux.Handle(
		"POST /api/v1/reading-position/resolve",
		resolveHandler,
	)
	mux.Handle(
		"PUT /api/v1/reading-position",
		saveHandler,
	)

	mux.Handle(
		"POST /api/v1/me/reading-position/resolve",
		resolveHandler,
	)
	mux.Handle(
		"PUT /api/v1/me/reading-position",
		saveHandler,
	)
}

type resolveLocalPosition struct {
	ChapterID        *string      `json:"chapterId,omitempty"`
	CharacterOffset  *int         `json:"characterOffset,omitempty"`
	ProgressFraction *float64     `json:"progressFraction,omitempty"`
	DeviceID         string       `json:"deviceId"`
	UpdatedAt        FlexibleTime `json:"updatedAt"`
}

type resolveReadingPositionRequest struct {
	BookFingerprint string                `json:"bookFingerprint"`
	LocalPosition   *resolveLocalPosition `json:"localPosition,omitempty"`
}

type remotePositionPublic struct {
	ChapterID        string    `json:"chapterId"`
	CharacterOffset  int       `json:"characterOffset"`
	ProgressFraction *float64  `json:"progressFraction,omitempty"`
	DeviceID         string    `json:"deviceId"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type resolveReadingPositionResponse struct {
	Decision       string                `json:"decision"`
	RemotePosition *remotePositionPublic `json:"remotePosition,omitempty"`
}

func handleReadingPositionResolve(svc *readingposition.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}

		if rejectUnlessJSONContentType(w, r) {
			return
		}

		var req resolveReadingPositionRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			log.Printf("reading_position: resolve: request_id=%s reason=json_decode_failed err=%v", requestIDFromContext(r.Context()), err)
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}

		bookFingerprint := strings.TrimSpace(req.BookFingerprint)
		if bookFingerprint == "" {
			log.Printf("reading_position: resolve: request_id=%s reason=missing_book_fingerprint", requestIDFromContext(r.Context()))
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "bookFingerprint is required")
			return
		}

		var local *readingposition.LocalPosition
		if req.LocalPosition != nil {
			lp := req.LocalPosition
			if strings.TrimSpace(lp.DeviceID) == "" {
				log.Printf("reading_position: resolve: request_id=%s book_fingerprint=%s reason=missing_local_device_id", requestIDFromContext(r.Context()), bookFingerprint)
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "localPosition.deviceId is required")
				return
			}
			if lp.UpdatedAt.IsZero() {
				log.Printf("reading_position: resolve: request_id=%s book_fingerprint=%s reason=missing_local_updated_at", requestIDFromContext(r.Context()), bookFingerprint)
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "localPosition.updatedAt is required")
				return
			}
			if lp.CharacterOffset != nil && *lp.CharacterOffset < 0 {
				log.Printf("reading_position: resolve: request_id=%s book_fingerprint=%s reason=negative_local_character_offset value=%d", requestIDFromContext(r.Context()), bookFingerprint, *lp.CharacterOffset)
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "localPosition.characterOffset must be >= 0")
				return
			}
			if lp.ChapterID != nil {
				s := strings.TrimSpace(*lp.ChapterID)
				lp.ChapterID = &s
			}
			local = &readingposition.LocalPosition{
				ChapterID:        lp.ChapterID,
				CharacterOffset:  lp.CharacterOffset,
				ProgressFraction: lp.ProgressFraction,
				DeviceID:         strings.TrimSpace(lp.DeviceID),
				UpdatedAt:        lp.UpdatedAt.UTC(),
			}
		}

		res, err := svc.ResolvePosition(r.Context(), u.ID, bookFingerprint, local)
		if err != nil {
			log.Printf("reading_position: resolve: request_id=%s user_id=%s book_fingerprint=%s err=%v",
				requestIDFromContext(r.Context()), u.ID.String(), bookFingerprint, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not resolve reading position")
			return
		}

		rid := requestIDFromContext(r.Context())
		var localUpdatedAt string
		if local != nil {
			localUpdatedAt = local.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		var remoteUpdatedAt string
		if res.RemotePosition != nil {
			remoteUpdatedAt = res.RemotePosition.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		log.Printf("reading_position: resolve: request_id=%s user_id=%s book_fingerprint=%s decision=%s local_updated_at=%s remote_updated_at=%s",
			rid, u.ID.String(), bookFingerprint, res.Decision, localUpdatedAt, remoteUpdatedAt)

		out := resolveReadingPositionResponse{Decision: string(res.Decision)}
		if res.Decision == readingposition.DecisionUseRemote && res.RemotePosition != nil {
			out.RemotePosition = &remotePositionPublic{
				ChapterID:        res.RemotePosition.ChapterID,
				CharacterOffset:  res.RemotePosition.CharacterOffset,
				ProgressFraction: res.RemotePosition.ProgressFraction,
				DeviceID:         res.RemotePosition.DeviceID,
				UpdatedAt:        res.RemotePosition.UpdatedAt.UTC(),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(out)
	}
}

type saveReadingPositionRequest struct {
	BookFingerprint string `json:"bookFingerprint"`
	// Flat root fields or nested `position` (older clients).
	Position *saveReadingPositionPosition `json:"position,omitempty"`

	ChapterID        string       `json:"chapterId,omitempty"`
	CharacterOffset  int          `json:"characterOffset,omitempty"`
	ProgressFraction *float64     `json:"progressFraction,omitempty"`
	DeviceID         string       `json:"deviceId,omitempty"`
	UpdatedAt        FlexibleTime `json:"updatedAt,omitempty"`
}

type saveReadingPositionPosition struct {
	ChapterID        string       `json:"chapterId"`
	CharacterOffset  int          `json:"characterOffset"`
	ProgressFraction *float64     `json:"progressFraction,omitempty"`
	DeviceID         string       `json:"deviceId"`
	UpdatedAt        FlexibleTime `json:"updatedAt"`
}

type saveReadingPositionResponse struct {
	Applied bool `json:"applied"`
}

func handleReadingPositionSave(svc *readingposition.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}

		if rejectUnlessJSONContentType(w, r) {
			return
		}

		var req saveReadingPositionRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			log.Printf("reading_position: save: request_id=%s reason=json_decode_failed err=%v", requestIDFromContext(r.Context()), err)
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}

		bookFingerprint := strings.TrimSpace(req.BookFingerprint)
		var chapterID string
		var characterOffset int
		var progressFraction *float64
		var deviceID string
		var updatedAt time.Time

		if req.Position != nil {
			chapterID = req.Position.ChapterID
			characterOffset = req.Position.CharacterOffset
			progressFraction = req.Position.ProgressFraction
			deviceID = req.Position.DeviceID
			updatedAt = req.Position.UpdatedAt.UTC()
		} else {
			chapterID = req.ChapterID
			characterOffset = req.CharacterOffset
			progressFraction = req.ProgressFraction
			deviceID = req.DeviceID
			updatedAt = req.UpdatedAt.UTC()
		}

		chapterID = strings.TrimSpace(chapterID)
		deviceID = strings.TrimSpace(deviceID)
		if bookFingerprint == "" {
			log.Printf("reading_position: save: request_id=%s reason=missing_book_fingerprint", requestIDFromContext(r.Context()))
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "bookFingerprint is required")
			return
		}
		if chapterID == "" {
			log.Printf("reading_position: save: request_id=%s book_fingerprint=%s reason=missing_chapter_id", requestIDFromContext(r.Context()), bookFingerprint)
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "chapterId is required")
			return
		}
		if characterOffset < 0 {
			log.Printf("reading_position: save: request_id=%s book_fingerprint=%s reason=negative_character_offset value=%d", requestIDFromContext(r.Context()), bookFingerprint, characterOffset)
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "characterOffset must be >= 0")
			return
		}
		if deviceID == "" {
			log.Printf("reading_position: save: request_id=%s book_fingerprint=%s reason=missing_device_id", requestIDFromContext(r.Context()), bookFingerprint)
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "deviceId is required")
			return
		}
		if updatedAt.IsZero() {
			log.Printf("reading_position: save: request_id=%s book_fingerprint=%s reason=missing_updated_at", requestIDFromContext(r.Context()), bookFingerprint)
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "updatedAt is required")
			return
		}

		applied, err := svc.SavePosition(
			r.Context(),
			u.ID,
			bookFingerprint,
			chapterID,
			characterOffset,
			progressFraction,
			deviceID,
			updatedAt.UTC(),
		)
		if err != nil {
			if errors.Is(err, readingposition.ErrInvalidArgument) {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid reading position")
				return
			}
			log.Printf("reading_position: save: request_id=%s user_id=%s book_fingerprint=%s err=%v",
				requestIDFromContext(r.Context()), u.ID.String(), bookFingerprint, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not save reading position")
			return
		}

		log.Printf("reading_position: save: request_id=%s user_id=%s book_fingerprint=%s applied=%t incoming_updated_at=%s",
			requestIDFromContext(r.Context()),
			u.ID.String(),
			bookFingerprint,
			applied,
			updatedAt.UTC().Format(time.RFC3339Nano),
		)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(saveReadingPositionResponse{Applied: applied})
	}
}
