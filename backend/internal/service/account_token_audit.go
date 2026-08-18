package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/tiktoken-go/tokenizer"
)

const (
	tokenAuditTokenizerName    = "tiktoken-go"
	tokenAuditTokenizerVersion = "v0.8.0"
	tokenAuditEncoding         = tokenizer.O200kBase
	tokenAuditTimeout          = 90 * time.Second
)

type TokenAuditRequest struct {
	ModelID string `json:"model_id"`
}

// TokenAuditProgressEvent is deliberately limited to non-sensitive audit metadata.
type TokenAuditProgressEvent struct {
	Type        string            `json:"type"`
	Index       int               `json:"index,omitempty"`
	Total       int               `json:"total,omitempty"`
	Name        string            `json:"name,omitempty"`
	LocalTokens int               `json:"local_tokens,omitempty"`
	Completed   int               `json:"completed,omitempty"`
	Sample      *TokenAuditSample `json:"sample,omitempty"`
	Result      *TokenAuditResult `json:"result,omitempty"`
}

type TokenAuditSample struct {
	Name                  string   `json:"name"`
	Kind                  string   `json:"kind"`
	LocalTokens           int      `json:"local_tokens"`
	SHA256                string   `json:"sha256"`
	HTTPStatus            int      `json:"http_status,omitempty"`
	ElapsedMS             int64    `json:"elapsed_ms,omitempty"`
	RequestIDPresent      bool     `json:"request_id_present"`
	ResponseIDPresent     bool     `json:"response_id_present"`
	InputTokens           *int     `json:"input_tokens,omitempty"`
	DifferenceTokens      *int     `json:"difference_tokens,omitempty"`
	VariableTokens        *float64 `json:"variable_tokens,omitempty"`
	OutputTokens          *int     `json:"output_tokens,omitempty"`
	TotalTokens           *int     `json:"total_tokens,omitempty"`
	CachedTokens          *int     `json:"cached_tokens,omitempty"`
	ReasoningTokens       *int     `json:"reasoning_tokens,omitempty"`
	ErrorType             string   `json:"error_type,omitempty"`
	ErrorCode             string   `json:"error_code,omitempty"`
	AccountHeaderPresent  bool     `json:"account_header_present"`
	ChannelHeaderPresent  bool     `json:"channel_header_present"`
	UpstreamHeaderPresent bool     `json:"upstream_header_present"`
	TransportError        bool     `json:"transport_error"`
	TimedOut              bool     `json:"timed_out"`
	JSONParsed            bool     `json:"json_parsed"`
}

type TokenAuditFit struct {
	Intercept         float64 `json:"intercept"`
	Slope             float64 `json:"slope"`
	R2                float64 `json:"r2"`
	SampleCount       int     `json:"sample_count"`
	ConfidenceLimited bool    `json:"confidence_limited"`
}

type TokenAuditResult struct {
	AccountID             int64              `json:"account_id"`
	ModelID               string             `json:"model_id"`
	TokenizerName         string             `json:"tokenizer_name"`
	TokenizerVersion      string             `json:"tokenizer_version"`
	TokenizerEncoding     string             `json:"tokenizer_encoding"`
	TokenizerExactMatch   bool               `json:"tokenizer_exact_match"`
	Samples               []TokenAuditSample `json:"samples"`
	AllFit                *TokenAuditFit     `json:"all_fit,omitempty"`
	EnglishFit            *TokenAuditFit     `json:"english_fit,omitempty"`
	FixedContextEstimate  *float64           `json:"fixed_context_estimate,omitempty"`
	VariableAmplification *float64           `json:"variable_amplification,omitempty"`
	CodeExtraTokens       *float64           `json:"code_extra_tokens,omitempty"`
	ChineseExtraTokens    *float64           `json:"chinese_extra_tokens,omitempty"`
	VariableGrowthStatus  string             `json:"variable_growth_status"`
	OutputCapStatus       string             `json:"output_cap_status"`
	FixedContextStatus    string             `json:"fixed_context_status"`
	OverallStatus         string             `json:"overall_status"`
	Completed             int                `json:"completed"`
	StoppedReason         string             `json:"stopped_reason,omitempty"`
}

type tokenAuditSample struct {
	name string
	kind string
	text string
}

func buildTokenAuditSamples(codec tokenizer.Codec) ([]tokenAuditSample, error) {
	englishUnit := "The audit measures input token usage consistently."
	code := "function collect(value) {\n  const values = [];\n" +
		"  const offset = 0;\n  if (value === undefined) value = 0;\n" +
		"  values.push(String(value));\n  values.push({ value });\n"
	for i := 0; i < 42; i++ {
		code += "  values.push(value);\n"
	}
	code += "  return values;\n}\n"
	zh := strings.Join([]string{
		"本次审计使用固定语义句子，比较本地分词数量与接口返回的输入数量。",
		"本次审计使用固定语义句子，比较本地分词数量与接口返回的输入数量。",
		"本次审计使用固定语义句子，比较本地分词数量与接口返回的输入数量。",
		"本次审计使用固定语义句子，比较本地分词数量与接口返回的输入数量。",
		"本次审计使用固定语义句子，比较本地分词数量与接口返回的输入数量。",
		"本次审计使用固定语义句子，比较本地分词数量与接口返回的输入数量。",
		"本次审计使用固定语义句子，比较本地分词数量与接口返回的输入数量。",
		"每个请求都使用相同模型，并将输出上限固定为一个令牌。",
		"每个请求都使用相同模型，并将输出上限固定为一个令牌。",
		"每个请求都使用相同模型，并将输出上限固定为一个令牌。",
		"每个请求都使用相同模型，并将输出上限固定为一个令牌。",
		"这些句子用于稳定地观察输入计量变化。",
	}, "\n")
	samples := []tokenAuditSample{
		{"english-64", "english", strings.TrimSpace(strings.Repeat(englishUnit+" ", 8))},
		{"english-256", "english", strings.TrimSpace(strings.Repeat(englishUnit+" ", 32))},
		{"english-1024", "english", strings.TrimSpace(strings.Repeat(englishUnit+" ", 128))},
		{"english-4096", "english", strings.TrimSpace(strings.Repeat(englishUnit+" ", 512))},
		{"code-256", "code", code},
		{"chinese-256", "chinese", zh},
	}
	expected := []int{64, 256, 1024, 4096, 256, 256}
	for i, sample := range samples {
		count, err := codec.Count(sample.text)
		if err != nil {
			return nil, fmt.Errorf("count %s: %w", sample.name, err)
		}
		if count != expected[i] {
			return nil, fmt.Errorf("count %s: got %d, want %d", sample.name, count, expected[i])
		}
	}
	return samples, nil
}

func tokenAuditHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

func tokenAuditInt(value int) *int { return &value }

func parseTokenAuditUsage(body []byte) (*TokenAuditSample, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	usage, _ := payload["usage"].(map[string]any)
	if usage == nil {
		return nil, nil
	}
	result := &TokenAuditSample{}
	readInt := func(value any) *int {
		switch v := value.(type) {
		case float64:
			n := int(v)
			return &n
		case json.Number:
			n, _ := v.Int64()
			return tokenAuditInt(int(n))
		default:
			return nil
		}
	}
	result.InputTokens = readInt(usage["input_tokens"])
	result.OutputTokens = readInt(usage["output_tokens"])
	result.TotalTokens = readInt(usage["total_tokens"])
	if details, ok := usage["input_tokens_details"].(map[string]any); ok {
		result.CachedTokens = readInt(details["cached_tokens"])
	}
	if details, ok := usage["output_tokens_details"].(map[string]any); ok {
		result.ReasoningTokens = readInt(details["reasoning_tokens"])
	}
	return result, nil
}

func fitTokenAudit(samples []TokenAuditSample, englishOnly bool) *TokenAuditFit {
	var xs, ys []float64
	for _, sample := range samples {
		if englishOnly && sample.Kind != "english" {
			continue
		}
		if sample.InputTokens == nil {
			continue
		}
		xs = append(xs, float64(sample.LocalTokens))
		ys = append(ys, float64(*sample.InputTokens))
	}
	if len(xs) < 2 {
		return nil
	}
	var xm, ym float64
	for i := range xs {
		xm += xs[i]
		ym += ys[i]
	}
	xm /= float64(len(xs))
	ym /= float64(len(ys))
	var sxx, sxy float64
	for i := range xs {
		dx, dy := xs[i]-xm, ys[i]-ym
		sxx += dx * dx
		sxy += dx * dy
	}
	if sxx == 0 {
		return nil
	}
	slope := sxy / sxx
	intercept := ym - slope*xm
	var residual, total float64
	for i := range xs {
		pred := intercept + slope*xs[i]
		residual += (ys[i] - pred) * (ys[i] - pred)
		total += (ys[i] - ym) * (ys[i] - ym)
	}
	r2 := 1.0
	if total > 0 {
		r2 = 1 - residual/total
	}
	return &TokenAuditFit{Intercept: intercept, Slope: slope, R2: r2, SampleCount: len(xs), ConfidenceLimited: len(xs) < 6 || residual == 0}
}

func (s *AccountTestService) RunTokenAudit(ctx context.Context, accountID int64, modelID string) (*TokenAuditResult, error) {
	return s.RunTokenAuditProgress(ctx, accountID, modelID, nil)
}

// RunTokenAuditProgress runs the six fixed samples serially. A sample-level
// transport/upstream failure is recorded and does not stop later samples.
func (s *AccountTestService) RunTokenAuditProgress(ctx context.Context, accountID int64, modelID string, onProgress func(TokenAuditProgressEvent) error) (*TokenAuditResult, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return nil, errors.New("token audit currently supports OpenAI API key accounts only")
	}
	if !openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return nil, errors.New("token audit requires the Responses API mode")
	}
	if strings.TrimSpace(modelID) == "" {
		modelID = "gpt-5.6-sol"
	}
	modelID = account.GetMappedModel(modelID)
	codec, err := tokenizer.Get(tokenAuditEncoding)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}
	samples, err := buildTokenAuditSamples(codec)
	if err != nil {
		return nil, err
	}
	result := &TokenAuditResult{
		AccountID:            accountID,
		ModelID:              modelID,
		TokenizerName:        tokenAuditTokenizerName,
		TokenizerVersion:     tokenAuditTokenizerVersion,
		TokenizerEncoding:    "o200k_base",
		TokenizerExactMatch:  false,
		VariableGrowthStatus: "insufficient_evidence",
		FixedContextStatus:   "insufficient_evidence",
		OutputCapStatus:      "insufficient_evidence",
		OverallStatus:        "insufficient_evidence",
	}
	if onProgress != nil {
		if err := onProgress(TokenAuditProgressEvent{Type: "started", Total: len(samples)}); err != nil {
			return nil, err
		}
	}
	for index, sample := range samples {
		localCount, _ := codec.Count(sample.text)
		record := TokenAuditSample{Name: sample.name, Kind: sample.kind, LocalTokens: localCount, SHA256: tokenAuditHash(sample.text)}
		if onProgress != nil {
			if err := onProgress(TokenAuditProgressEvent{Type: "sample_started", Index: index, Total: len(samples), Name: sample.name, LocalTokens: localCount, Completed: result.Completed}); err != nil {
				return nil, err
			}
		}
		start := time.Now()
		record, stop, stopReason := s.runTokenAuditSample(ctx, account, modelID, sample, record)
		record.ElapsedMS = time.Since(start).Milliseconds()
		result.Samples = append(result.Samples, record)
		if record.HTTPStatus >= 200 && record.HTTPStatus < 300 && !record.TransportError && !record.TimedOut {
			result.Completed++
		}
		if onProgress != nil {
			if err := onProgress(TokenAuditProgressEvent{Type: "sample_finished", Index: index, Total: len(samples), Name: sample.name, Completed: result.Completed, Sample: &result.Samples[len(result.Samples)-1]}); err != nil {
				return nil, err
			}
		}
		if stop {
			result.StoppedReason = stopReason
			break
		}
	}
	result.AllFit = fitTokenAudit(result.Samples, false)
	result.EnglishFit = fitTokenAudit(result.Samples, true)
	if result.AllFit != nil {
		result.FixedContextEstimate = &result.AllFit.Intercept
		result.VariableAmplification = &result.AllFit.Slope
		for i := range result.Samples {
			sample := &result.Samples[i]
			if sample.InputTokens == nil {
				continue
			}
			difference := *sample.InputTokens - sample.LocalTokens
			sample.DifferenceTokens = &difference
			variable := float64(*sample.InputTokens) - result.AllFit.Intercept
			sample.VariableTokens = &variable
		}
		if result.AllFit.Slope >= 0.9 && result.AllFit.Slope <= 1.1 && result.AllFit.R2 >= 0.98 {
			result.VariableGrowthStatus = "normal"
		} else {
			result.VariableGrowthStatus = "suspicious"
		}
		result.FixedContextStatus = "evidence_only"
	}
	var english256 *int
	for i := range result.Samples {
		sample := &result.Samples[i]
		if sample.Kind == "english" && sample.LocalTokens == 256 && sample.InputTokens != nil {
			english256 = sample.InputTokens
			break
		}
	}
	if english256 != nil {
		for i := range result.Samples {
			sample := &result.Samples[i]
			if sample.InputTokens == nil || sample.LocalTokens != 256 {
				continue
			}
			extra := float64(*sample.InputTokens - *english256)
			switch sample.Kind {
			case "code":
				result.CodeExtraTokens = &extra
			case "chinese":
				result.ChineseExtraTokens = &extra
			}
		}
	}
	for _, sample := range result.Samples {
		if sample.OutputTokens != nil && *sample.OutputTokens > 1 {
			result.OutputCapStatus = "suspicious"
			result.OverallStatus = "suspicious"
			break
		}
	}
	if result.OverallStatus != "suspicious" && result.VariableGrowthStatus == "normal" && result.Completed == len(samples) {
		result.OverallStatus = "normal"
	}
	if onProgress != nil {
		if err := onProgress(TokenAuditProgressEvent{Type: "completed", Total: len(samples), Completed: result.Completed, Result: result}); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// RunTokenAuditSample runs exactly one named fixed sample. It never retries.
func (s *AccountTestService) RunTokenAuditSample(ctx context.Context, accountID int64, modelID, sampleName string) (*TokenAuditSample, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return nil, errors.New("token audit currently supports OpenAI API key accounts only")
	}
	if !openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return nil, errors.New("token audit requires the Responses API mode")
	}
	if strings.TrimSpace(modelID) == "" {
		modelID = "gpt-5.6-sol"
	}
	modelID = account.GetMappedModel(modelID)
	codec, err := tokenizer.Get(tokenAuditEncoding)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}
	samples, err := buildTokenAuditSamples(codec)
	if err != nil {
		return nil, err
	}
	for _, sample := range samples {
		if sample.name != sampleName {
			continue
		}
		localCount, _ := codec.Count(sample.text)
		record := TokenAuditSample{Name: sample.name, Kind: sample.kind, LocalTokens: localCount, SHA256: tokenAuditHash(sample.text)}
		start := time.Now()
		record, _, _ = s.runTokenAuditSample(ctx, account, modelID, sample, record)
		record.ElapsedMS = time.Since(start).Milliseconds()
		return &record, nil
	}
	return nil, fmt.Errorf("unknown token audit sample: %s", sampleName)
}

func (s *AccountTestService) runTokenAuditSample(ctx context.Context, account *Account, modelID string, sample tokenAuditSample, record TokenAuditSample) (TokenAuditSample, bool, string) {
	apiKey := account.GetOpenAIApiKey()
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	normalized, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		record.TransportError = true
		record.ErrorType = "invalid_base_url"
		return record, true, "invalid_base_url"
	}
	payload := map[string]any{"model": modelID, "input": sample.text, "max_output_tokens": 1, "store": false}
	body, _ := json.Marshal(payload)
	requestCtx, cancel := context.WithTimeout(ctx, tokenAuditTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, buildOpenAIResponsesURL(normalized), bytes.NewReader(body))
	if err != nil {
		record.TransportError = true
		record.ErrorType = "request_build"
		return record, true, "request_build"
	}
	// This audit must be one network attempt per sample: prevent redirects and
	// make the request non-replayable so net/http cannot retry a broken POST.
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req = req.WithContext(WithHTTPUpstreamRedirectsDisabled(req.Context()))
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	applyOpenAICodexProbeHeaders(req.Header)
	account.ApplyHeaderOverrides(req.Header)
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		record.TransportError = true
		record.ErrorType = "network_error"
		if errors.Is(err, context.DeadlineExceeded) {
			record.TimedOut = true
			record.ErrorType = "timeout"
		}
		// Only cancellation of the parent audit stops subsequent samples. A
		// per-sample timeout/network failure is isolated to this row.
		if ctx.Err() != nil {
			return record, true, record.ErrorType
		}
		return record, false, ""
	}
	defer resp.Body.Close()
	record.HTTPStatus = resp.StatusCode
	record.RequestIDPresent = resp.Header.Get("x-request-id") != "" || resp.Header.Get("request-id") != "" || resp.Header.Get("openai-request-id") != ""
	for key := range resp.Header {
		lower := strings.ToLower(key)
		record.AccountHeaderPresent = record.AccountHeaderPresent || strings.Contains(lower, "account") || strings.Contains(lower, "credential")
		record.ChannelHeaderPresent = record.ChannelHeaderPresent || strings.Contains(lower, "channel") || strings.Contains(lower, "group") || strings.Contains(lower, "provider") || strings.Contains(lower, "route")
		record.UpstreamHeaderPresent = record.UpstreamHeaderPresent || strings.Contains(lower, "upstream") || strings.Contains(lower, "backend") || strings.Contains(lower, "origin") || strings.Contains(lower, "target")
	}
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		record.TransportError = true
		record.ErrorType = "response_read"
		if ctx.Err() != nil {
			return record, true, "response_read"
		}
		return record, false, ""
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		record.ErrorType = "http_error"
		var errorPayload struct {
			Error struct {
				Type string `json:"type"`
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(responseBody, &errorPayload) == nil {
			record.ErrorType = errorPayload.Error.Type
			record.ErrorCode = errorPayload.Error.Code
		}
		return record, false, ""
	}
	parsed, parseErr := parseTokenAuditUsage(responseBody)
	if parseErr != nil {
		record.ErrorType = "invalid_json"
		return record, false, ""
	}
	record.JSONParsed = true
	if parsed == nil {
		record.ErrorType = "usage_missing"
		return record, false, ""
	}
	record.InputTokens = parsed.InputTokens
	record.OutputTokens = parsed.OutputTokens
	record.TotalTokens = parsed.TotalTokens
	record.CachedTokens = parsed.CachedTokens
	record.ReasoningTokens = parsed.ReasoningTokens
	var identity struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(responseBody, &identity) == nil {
		record.ResponseIDPresent = identity.ID != ""
	}
	return record, false, ""
}
