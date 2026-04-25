package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/rateus"
)

func registerRateUsRoutes(mux *http.ServeMux, store *auth.Store, issuer *auth.TokenIssuer, rateStore *rateus.Store) {
	mux.Handle("POST /api/v1/me/rate-us/attempt", bearerAuthHandler(store, issuer, handleRateUsAttempt(rateStore)))
}

type rateUsAttemptRequest struct {
	AppVersion string `json:"appVersion"`
}

type rateUsAttemptResponse struct {
	RateUs rateUsPublic `json:"rateUs"`
}

func handleRateUsAttempt(rateStore *rateus.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}

		if rejectUnlessJSONContentType(w, r) {
			return
		}

		var req rateUsAttemptRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<14))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}

		appV := strings.TrimSpace(req.AppVersion)
		if appV == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "appVersion is required and must be a non-empty string")
			return
		}

		now := time.Now().UTC()
		st, err := rateStore.UpsertAttempt(r.Context(), u.ID, appV, now)
		if err != nil {
			log.Printf("rate_us: attempt upsert user_id=%s err=%v", u.ID, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not record rate us attempt")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(rateUsAttemptResponse{
			RateUs: rateUsPublic{
				LastAttemptAt:         st.LastAttemptAt.UTC(),
				LastAttemptAppVersion: st.LastAttemptAppVersion,
			},
		})
	}
}
