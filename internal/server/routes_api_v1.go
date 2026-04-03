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
	"foreignreader_be/internal/entitlement"
	"foreignreader_be/internal/translate"
)

func registerAPIV1Routes(mux *http.ServeMux, tr *translate.Client, store *auth.Store, issuer *auth.TokenIssuer, ent *entitlement.Store) {
	translateHandler := func(w http.ResponseWriter, r *http.Request) {
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

		if strings.TrimSpace(req.SourceLanguage) == "" ||
			strings.TrimSpace(req.TargetLanguage) == "" ||
			strings.TrimSpace(req.Sentence) == "" ||
			strings.TrimSpace(req.SelectedWord) == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "missing or empty required fields")
			return
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
				return
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				reason := "context_deadline_exceeded"
				if errors.Is(err, context.Canceled) {
					reason = "context_cancelled"
				}
				log.Printf("translate/context: request_id=%s reason=%s", rid, reason)
				writeAPIError(w, http.StatusBadGateway, "translation_failed", "translation request failed")
				return
			}

			var apiErr *oai.Error
			if errors.As(err, &apiErr) {
				logOpenAITranslateFailure(rid, apiErr, err)
				clientStatus := mapOpenAIHTTPStatusToClient(apiErr.StatusCode)
				code, msg := translateClientErrorCodeAndMessage(clientStatus)
				writeAPIError(w, clientStatus, code, msg)
				return
			}

			log.Printf("translate/context: request_id=%s reason=upstream_error err=%v", rid, err)
			writeAPIError(w, http.StatusBadGateway, "translation_failed", "translation request failed")
			return
		}

		log.Printf("translate/context: request_id=%s openai_status=200", rid)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(translateContextResponse{
			WordTranslation:     word,
			SentenceTranslation: sentence,
		})
	}

	mux.Handle("POST /api/v1/translate/context", bearerAuthHandler(store, issuer, requireProMiddleware(ent, translateHandler)))
}

type translateContextRequest struct {
	SourceLanguage string `json:"sourceLanguage"`
	TargetLanguage string `json:"targetLanguage"`
	Sentence       string `json:"sentence"`
	SelectedWord   string `json:"selectedWord"`
}

type translateContextResponse struct {
	WordTranslation     string `json:"wordTranslation"`
	SentenceTranslation string `json:"sentenceTranslation"`
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
