package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	oai "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/responses"
	"github.com/openai/openai-go/v2/shared"
)

var ErrInvalidModelOutput = errors.New("invalid model output")

type Client struct {
	api          oai.Client
	model        shared.ResponsesModel
	instructions string
	timeout      time.Duration
}

func NewClient(apiKey, model, instructions string, timeout time.Duration) *Client {
	c := oai.NewClient(option.WithAPIKey(apiKey))
	return &Client{
		api:          c,
		model:        shared.ResponsesModel(model),
		instructions: instructions,
		timeout:      timeout,
	}
}

type llmOutput struct {
	WordTranslation     string `json:"wordTranslation"`
	SentenceTranslation string `json:"sentenceTranslation"`
}

func (c *Client) TranslateContext(ctx context.Context, sourceLanguage, targetLanguage, sentence, selectedWord string) (wordTranslation, sentenceTranslation string, err error) {
	if c.timeout <= 0 {
		return "", "", errors.New("translate: timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	input := fmt.Sprintf(
		"Input:\n- Source language: %s\n- Target language: %s\n- Sentence: %s\n- Selected word: %s",
		sourceLanguage, targetLanguage, sentence, selectedWord,
	)

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"wordTranslation":     map[string]any{"type": "string"},
			"sentenceTranslation": map[string]any{"type": "string"},
		},
		"required":             []string{"wordTranslation", "sentenceTranslation"},
		"additionalProperties": false,
	}
	jsonFmt := &responses.ResponseFormatTextJSONSchemaConfigParam{
		Name:   "translate_context_output",
		Schema: schema,
		Strict: oai.Opt(true),
	}

	resp, err := c.api.Responses.New(ctx, responses.ResponseNewParams{
		Model:        c.model,
		Instructions: oai.Opt(c.instructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: oai.String(input),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: jsonFmt},
		},
	})
	if err != nil {
		return "", "", err
	}
	if resp == nil {
		return "", "", errors.New("empty response from OpenAI")
	}

	raw := strings.TrimSpace(collectOutputText(resp))
	if raw == "" {
		return "", "", fmt.Errorf("%w: empty output text from model", ErrInvalidModelOutput)
	}

	word, sentence, err := parseTranslationJSON(raw)
	if err != nil {
		return "", "", err
	}
	return word, sentence, nil
}

func collectOutputText(resp *responses.Response) string {
	if resp == nil {
		return ""
	}
	if s := strings.TrimSpace(resp.OutputText()); s != "" {
		return s
	}
	var b strings.Builder
	for _, item := range resp.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "refusal" {
				continue
			}
			t := strings.TrimSpace(part.Text)
			if t == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(part.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func parseTranslationJSON(raw string) (word, sentence string, err error) {
	jsonStr := extractJSONObject(raw)

	var out llmOutput
	if err := json.Unmarshal([]byte(jsonStr), &out); err == nil {
		out.WordTranslation = strings.TrimSpace(out.WordTranslation)
		out.SentenceTranslation = strings.TrimSpace(out.SentenceTranslation)
		if out.WordTranslation != "" && out.SentenceTranslation != "" {
			return out.WordTranslation, out.SentenceTranslation, nil
		}
	}

	type alt struct {
		WordTranslation     string `json:"word_translation"`
		SentenceTranslation string `json:"sentence_translation"`
	}
	var a alt
	if err := json.Unmarshal([]byte(jsonStr), &a); err == nil {
		a.WordTranslation = strings.TrimSpace(a.WordTranslation)
		a.SentenceTranslation = strings.TrimSpace(a.SentenceTranslation)
		if a.WordTranslation != "" && a.SentenceTranslation != "" {
			return a.WordTranslation, a.SentenceTranslation, nil
		}
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrInvalidModelOutput, err)
	}
	w := firstJSONString(m, "wordTranslation", "word_translation", "word")
	s := firstJSONString(m, "sentenceTranslation", "sentence_translation", "sentence", "translation")
	w = strings.TrimSpace(w)
	s = strings.TrimSpace(s)
	if w == "" || s == "" {
		return "", "", fmt.Errorf("%w: missing translation fields after parse", ErrInvalidModelOutput)
	}
	return w, s, nil
}

func firstJSONString(m map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	lower := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		lower[strings.ToLower(k)] = v
	}
	for _, k := range keys {
		if v, ok := lower[strings.ToLower(k)]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		s = strings.TrimSpace(s[i+3:])
		if j := strings.Index(s, "\n"); j >= 0 {
			s = strings.TrimSpace(s[j+1:])
		}
		if k := strings.LastIndex(s, "```"); k >= 0 {
			s = strings.TrimSpace(s[:k])
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}
