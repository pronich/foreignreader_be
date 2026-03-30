package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	oai "github.com/openai/openai-go/v2"

	"foreignreader_be/internal/translate"
)

func registerAPIV1Routes(mux *http.ServeMux, tr *translate.Client) {
	mux.HandleFunc("POST /api/v1/translate/context", func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
			writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_request", "expected Content-Type: application/json")
			return
		}

		var req translateContextRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20)) // 1 MiB
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

		word, sentence, err := tr.TranslateContext(
			r.Context(),
			strings.TrimSpace(req.SourceLanguage),
			strings.TrimSpace(req.TargetLanguage),
			strings.TrimSpace(req.Sentence),
			strings.TrimSpace(req.SelectedWord),
		)
		if err != nil {
			if errors.Is(err, translate.ErrInvalidModelOutput) {
				writeAPIError(w, http.StatusInternalServerError, "invalid_model_output", "could not parse model response")
				return
			}
			var apiErr *oai.Error
			if errors.As(err, &apiErr) {
				log.Printf("translate/context: OpenAI error status=%d code=%s type=%s message=%s",
					apiErr.StatusCode, apiErr.Code, apiErr.Type, apiErr.Message)
			} else {
				log.Printf("translate/context: %v", err)
			}
			writeAPIError(w, http.StatusBadGateway, "translation_failed", "translation request failed")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(translateContextResponse{
			WordTranslation:     word,
			SentenceTranslation: sentence,
		})
	})
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
