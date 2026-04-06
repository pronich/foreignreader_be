package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	oai "github.com/openai/openai-go/v2"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/entitlement"
	"foreignreader_be/internal/monthlycontexttranslation"
	"foreignreader_be/internal/onboardingsession"
	"foreignreader_be/internal/ratelimit"
	"foreignreader_be/internal/translate"
)

func registerAPIV1Routes(mux *http.ServeMux, cfg config.Config, tr *translate.Client, store *auth.Store, issuer *auth.TokenIssuer, ent *entitlement.Store, obStore *onboardingsession.Store, sessionWL, translateIPWL, translateTokWL *ratelimit.Window) {
	authenticatedTranslateHandler := func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}

		if rejectUnlessJSONContentType(w, r) {
			return
		}

		var req translateContextRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}

		isPro, err := ent.HasActivePro(r.Context(), u.ID)
		if err != nil {
			log.Printf("translate/context: pro check: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not verify entitlement")
			return
		}

		var periodKey string
		if !isPro {
			var ml, uc int
			periodKey, ml, uc, err = monthlycontexttranslation.EnsureCurrentMonthRow(r.Context(), ent.DB, u.ID, cfg.FreeContextTranslationsPerMonth)
			if err != nil {
				log.Printf("translate/context: quota ensure: %v", err)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load translation quota")
				return
			}
			rem := ml - uc
			if rem < 0 {
				rem = 0
			}
			if rem == 0 {
				writeAPIError(w, http.StatusForbidden, "context_translation_quota_exhausted", "monthly free context translation quota is exhausted")
				return
			}
		}

		word, sentence, runOK := translateContextRun(w, r, tr, req)
		if !runOK {
			return
		}

		var quota *contextQuotaPublic
		if !isPro {
			ml, uc, err := monthlycontexttranslation.IncrementUsedCount(r.Context(), ent.DB, u.ID, periodKey)
			if err != nil {
				log.Printf("translate/context: quota increment: %v", err)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not update translation quota")
				return
			}
			rem := ml - uc
			if rem < 0 {
				rem = 0
			}
			quota = &contextQuotaPublic{
				MonthlyLimit: ml,
				UsedCount:    uc,
				Remaining:    rem,
				PeriodKey:    periodKey,
			}
		}

		log.Printf("translate/context: request_id=%s openai_status=200", requestIDFromContext(r.Context()))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(translateContextResponse{
			WordTranslation:     word,
			SentenceTranslation: sentence,
			ContextQuota:        quota,
		})
	}

	mux.Handle("POST /api/v1/translate/context", bearerAuthHandler(store, issuer, authenticatedTranslateHandler))

	sessionH := handleOnboardingSession(cfg, obStore)
	sessionH = withOnboardingSessionIPRate(sessionWL, cfg.OnboardingSessionRateLimitPerIP, sessionH)
	mux.Handle("POST /api/v1/onboarding/session", sessionH)

	onboardingTranslateH := handleOnboardingTranslateContext(tr)
	onboardingTranslateH = withOnboardingTranslateTokenRate(translateTokWL, cfg.OnboardingTranslateRateLimitPerToken, onboardingTranslateH)
	onboardingTranslateH = onboardingOpaqueBearerMiddleware(obStore, onboardingTranslateH)
	onboardingTranslateH = withOnboardingTranslateIPRate(translateIPWL, cfg.OnboardingTranslateRateLimitPerIP, onboardingTranslateH)
	mux.Handle("POST /api/v1/onboarding/translate/context", onboardingTranslateH)

	mux.Handle("POST /api/v1/billing/checkout-session", bearerAuthHandler(store, issuer, handleBillingCheckoutSession(cfg, ent)))
	mux.Handle("POST /api/v1/billing/webhook", handleStripeWebhook(cfg, ent.DB, ent))
}

func serveTranslateContext(w http.ResponseWriter, r *http.Request, tr *translate.Client, req translateContextRequest) {
	word, sentence, ok := translateContextRun(w, r, tr, req)
	if !ok {
		return
	}

	log.Printf("translate/context: request_id=%s openai_status=200", requestIDFromContext(r.Context()))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(translateContextResponse{
		WordTranslation:     word,
		SentenceTranslation: sentence,
	})
}

// translateContextRun validates input, calls the translation provider, and maps errors to HTTP responses.
// On success it returns word, sentence, and ok=true (caller writes JSON). On failure it writes the response and returns ok=false.
func translateContextRun(w http.ResponseWriter, r *http.Request, tr *translate.Client, req translateContextRequest) (word, sentence string, ok bool) {
	if strings.TrimSpace(req.SourceLanguage) == "" ||
		strings.TrimSpace(req.TargetLanguage) == "" ||
		strings.TrimSpace(req.Sentence) == "" ||
		strings.TrimSpace(req.SelectedWord) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "missing or empty required fields")
		return "", "", false
	}

	rid := requestIDFromContext(r.Context())

	word, sentence, err := tr.TranslateContext(
		r.Context(),
		strings.TrimSpace(req.SourceLanguage),
		strings.TrimSpace(req.TargetLanguage),
		strings.TrimSpace(req.Sentence),
		strings.TrimSpace(req.SelectedWord),
	)
	if err != nil {
		if errors.Is(err, translate.ErrInvalidModelOutput) {
			log.Printf("translate/context: request_id=%s reason=invalid_model_output err=%v", rid, err)
			writeAPIError(w, http.StatusInternalServerError, "invalid_model_output", "could not parse model response")
			return "", "", false
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			reason := "context_deadline_exceeded"
			if errors.Is(err, context.Canceled) {
				reason = "context_cancelled"
			}
			log.Printf("translate/context: request_id=%s reason=%s", rid, reason)
			writeAPIError(w, http.StatusBadGateway, "translation_failed", "translation request failed")
			return "", "", false
		}

		var apiErr *oai.Error
		if errors.As(err, &apiErr) {
			logOpenAITranslateFailure(rid, apiErr, err)
			clientStatus := mapOpenAIHTTPStatusToClient(apiErr.StatusCode)
			code, msg := translateClientErrorCodeAndMessage(clientStatus)
			writeAPIError(w, clientStatus, code, msg)
			return "", "", false
		}

		log.Printf("translate/context: request_id=%s reason=upstream_error err=%v", rid, err)
		writeAPIError(w, http.StatusBadGateway, "translation_failed", "translation request failed")
		return "", "", false
	}

	return word, sentence, true
}

type translateContextRequest struct {
	SourceLanguage string `json:"sourceLanguage"`
	TargetLanguage string `json:"targetLanguage"`
	Sentence       string `json:"sentence"`
	SelectedWord   string `json:"selectedWord"`
}

type translateContextResponse struct {
	WordTranslation     string              `json:"wordTranslation"`
	SentenceTranslation string              `json:"sentenceTranslation"`
	ContextQuota        *contextQuotaPublic `json:"contextQuota,omitempty"`
}

func mapOpenAIHTTPStatusToClient(code int) int {
	switch {
	case code == http.StatusTooManyRequests:
		return http.StatusTooManyRequests
	case code >= 400 && code <= 499:
		return http.StatusBadRequest
	case code >= 500 && code <= 599:
		return http.StatusBadGateway
	default:
		return http.StatusBadGateway
	}
}

func translateClientErrorCodeAndMessage(clientStatus int) (code, message string) {
	if clientStatus == http.StatusTooManyRequests {
		return "rate_limited", "translation rate limited"
	}
	if clientStatus == http.StatusBadRequest {
		return "translation_failed", "translation request rejected"
	}
	return "translation_failed", "translation request failed"
}

func logOpenAITranslateFailure(rid string, apiErr *oai.Error, err error) {
	if apiErr == nil {
		log.Printf("translate/context: request_id=%s openai_status=0 err=%v", rid, err)
		return
	}
	httpStatus := apiErr.StatusCode

	var body string
	if apiErr.Response != nil && apiErr.Response.Body != nil {
		b, readErr := io.ReadAll(io.LimitReader(apiErr.Response.Body, 1<<20))
		_ = apiErr.Response.Body.Close()
		if readErr == nil {
			body = strings.TrimSpace(string(b))
		}
	}
	if body == "" {
		if raw := strings.TrimSpace(apiErr.RawJSON()); raw != "" {
			body = raw
		}
	}
	if len(body) > 16384 {
		body = body[:16384] + "...(truncated)"
	}

	if httpStatus != http.StatusOK {
		log.Printf("translate/context: request_id=%s openai_status=%d code=%s type=%s openai_message=%q openai_body=%q err=%v",
			rid, httpStatus, apiErr.Code, apiErr.Type, apiErr.Message, body, err)
		return
	}
	log.Printf("translate/context: request_id=%s openai_status=%d code=%s type=%s openai_message=%q err=%v",
		rid, httpStatus, apiErr.Code, apiErr.Type, apiErr.Message, err)
}

type apiErrorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorResponse{
		Error: apiError{Code: code, Message: message},
	})
}
