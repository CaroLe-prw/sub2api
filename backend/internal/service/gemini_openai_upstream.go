package service

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/geminiopenai"
)

// GeminiUsesOpenAI separates the account's upstream protocol from its Gemini
// platform, so group selection, quotas and model pricing remain unchanged.
func (a *Account) GeminiUsesOpenAI() bool {
	return a != nil && a.Platform == PlatformGemini && a.Type == AccountTypeAPIKey && a.GetCredential("api_protocol") == "chat_completions"
}

type geminiRequestBuilder = func(context.Context) (*http.Request, string, error)

func (s *GeminiMessagesCompatService) openAIUpstreamBuilder(account *Account, model string, body []byte, stream bool) (geminiRequestBuilder, error) {
	converted, err := geminiopenai.Request(body, model, stream)
	if err != nil {
		return nil, err
	}
	baseURL, err := s.validateUpstreamBaseURL(strings.TrimSpace(account.GetCredential("base_url")))
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (*http.Request, string, error) {
		req, err := newGeminiOpenAIRequest(ctx, baseURL, account.GetCredential("api_key"), converted)
		return req, "x-request-id", err
	}, nil
}

func newGeminiOpenAIRequest(ctx context.Context, baseURL, apiKey string, body []byte) (*http.Request, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("gemini api_key not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildOpenAIChatCompletionsURL(baseURL), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	return req, nil
}

// Keep error bodies intact for the existing retry and account error policies.
// Successful bodies are translated lazily for the existing Gemini consumers.
func adaptGeminiOpenAIResponse(account *Account, resp *http.Response, stream bool) {
	if !account.GeminiUsesOpenAI() || resp == nil || resp.StatusCode >= 400 {
		return
	}
	resp.Body = geminiopenai.Body(resp.Body, stream)
	resp.ContentLength = -1
	resp.Header = resp.Header.Clone()
	resp.Header.Del("Content-Length")
	resp.Header.Del("Content-Encoding")
	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
	}
	resp.Header.Set("Content-Type", contentType)
}
