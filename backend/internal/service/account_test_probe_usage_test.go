//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountTestProbeAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *accountTestProbeAccountRepo) GetByID(_ context.Context, _ int64) (*Account, error) {
	return r.account, nil
}

type accountTestProbeUsageRepo struct {
	UsageLogRepository
	logs []*UsageLog
}

func (r *accountTestProbeUsageRepo) Create(_ context.Context, usage *UsageLog) (bool, error) {
	copy := *usage
	r.logs = append(r.logs, &copy)
	return true, nil
}

func TestAccountTestBackgroundRecordsOnlyChannelMonitorProbes(t *testing.T) {
	account := &Account{
		ID:          42,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{},
	}
	usageRepo := &accountTestProbeUsageRepo{}
	svc := &AccountTestService{
		accountRepo:  &accountTestProbeAccountRepo{account: account},
		usageLogRepo: usageRepo,
	}

	_, err := svc.RunTestBackground(context.Background(), account.ID, "claude-sonnet-4-20250514")
	require.NoError(t, err)
	require.Empty(t, usageRepo.logs)

	_, err = svc.RunChannelMonitorProbeBackground(context.Background(), account.ID, "claude-sonnet-4-20250514")
	require.NoError(t, err)
	require.Len(t, usageRepo.logs, 1)
	require.Equal(t, RequestTypeProbe, usageRepo.logs[0].RequestType)
}

func TestRecordChannelProbeUsageCapturesContextCostAndAccountAttribution(t *testing.T) {
	rateMultiplier := 1.75
	account := &Account{
		ID:             42,
		Platform:       PlatformAnthropic,
		Type:           AccountTypeAPIKey,
		RateMultiplier: &rateMultiplier,
	}
	usageRepo := &accountTestProbeUsageRepo{}
	svc := &AccountTestService{
		accountRepo:    &accountTestProbeAccountRepo{account: account},
		usageLogRepo:   usageRepo,
		billingService: NewBillingService(&config.Config{}, nil),
	}
	startedAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	ttft := int64(125)
	result := &ScheduledTestResult{
		Status:     "success",
		TTFTMs:     &ttft,
		StartedAt:  startedAt,
		FinishedAt: startedAt.Add(500 * time.Millisecond),
	}

	svc.recordChannelProbeUsage(context.Background(), account.ID, "claude-sonnet-4-20250514", result, UsageTokens{
		InputTokens:         100,
		OutputTokens:        10,
		CacheReadTokens:     20,
		CacheCreationTokens: 5,
	}, accountTestMediaUsage{})

	require.Len(t, usageRepo.logs, 1)
	log := usageRepo.logs[0]
	require.Zero(t, log.UserID)
	require.Zero(t, log.APIKeyID)
	require.Equal(t, account.ID, log.AccountID)
	require.Equal(t, RequestTypeProbe, log.RequestType)
	require.Equal(t, 100, log.InputTokens)
	require.Equal(t, 10, log.OutputTokens)
	require.Equal(t, 20, log.CacheReadTokens)
	require.Equal(t, 5, log.CacheCreationTokens)
	require.Positive(t, log.TotalCost)
	require.Zero(t, log.ActualCost)
	require.NotNil(t, log.AccountRateMultiplier)
	require.Equal(t, rateMultiplier, *log.AccountRateMultiplier)
	require.NotNil(t, log.InboundEndpoint)
	require.Equal(t, "/internal/channel-monitor/probe", *log.InboundEndpoint)
	require.NotNil(t, log.UserAgent)
	require.Equal(t, "sub2api-channel-monitor", *log.UserAgent)
}

func TestAccountTestUsageParsers(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		usage := accountTestOpenAIUsage(map[string]any{
			"prompt_tokens": 100.0, "completion_tokens": 7.0,
			"prompt_tokens_details": map[string]any{"cached_tokens": 20.0},
			"output_tokens_details": map[string]any{"image_tokens": 2.0},
		})
		require.Equal(t, UsageTokens{InputTokens: 80, OutputTokens: 7, CacheReadTokens: 20, ImageOutputTokens: 2}, usage)
	})

	t.Run("claude", func(t *testing.T) {
		usage := accountTestClaudeUsage(map[string]any{
			"input_tokens": 80.0, "output_tokens": 7.0,
			"cache_creation_input_tokens": 5.0, "cache_read_input_tokens": 20.0,
			"cache_creation": map[string]any{"ephemeral_5m_input_tokens": 3.0, "ephemeral_1h_input_tokens": 2.0},
		})
		require.Equal(t, UsageTokens{
			InputTokens: 80, OutputTokens: 7, CacheCreationTokens: 5, CacheReadTokens: 20,
			CacheCreation5mTokens: 3, CacheCreation1hTokens: 2,
		}, usage)
	})

	t.Run("gemini", func(t *testing.T) {
		usage := accountTestGeminiUsage(map[string]any{
			"promptTokenCount": 100.0, "cachedContentTokenCount": 20.0,
			"candidatesTokenCount": 5.0, "thoughtsTokenCount": 3.0,
			"candidatesTokensDetails": []any{map[string]any{"modality": "IMAGE", "tokenCount": 2.0}},
		})
		require.Equal(t, UsageTokens{InputTokens: 80, OutputTokens: 8, CacheReadTokens: 20, ImageOutputTokens: 2}, usage)
	})

	t.Run("antigravity", func(t *testing.T) {
		body := []byte("data: {\"response\":{\"usageMetadata\":{\"promptTokenCount\":100,\"cachedContentTokenCount\":20,\"candidatesTokenCount\":5,\"thoughtsTokenCount\":3,\"candidatesTokensDetails\":[{\"modality\":\"IMAGE\",\"tokenCount\":2}]}}}\n\n")
		usage := extractUsageFromAntigravitySSEResponse(body)
		require.Equal(t, UsageTokens{InputTokens: 80, OutputTokens: 8, CacheReadTokens: 20, ImageOutputTokens: 2}, usage)
	})
}

func TestBedrockAccountTestResponseCapturesUsage(t *testing.T) {
	tracker := &accountTestTimingTracker{startedAt: time.Now()}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(accountTestTimingTrackerKey, tracker)

	body := map[string]any{
		"content": []any{map[string]any{"text": "ok"}},
		"usage": map[string]any{
			"input_tokens": 12, "output_tokens": 3,
			"cache_creation_input_tokens": 4, "cache_read_input_tokens": 5,
		},
	}
	encoded, err := json.Marshal(body)
	require.NoError(t, err)

	var decoded struct {
		Usage map[string]any `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	observeAccountTestUsage(c, accountTestClaudeUsage(decoded.Usage))
	tokens, _ := tracker.usageValue()
	require.Equal(t, UsageTokens{
		InputTokens: 12, OutputTokens: 3, CacheCreationTokens: 4, CacheReadTokens: 5,
	}, tokens)
}

func TestRecordChannelProbeUsageCapturesMediaAccountCost(t *testing.T) {
	rateMultiplier := 1.5
	account := &Account{
		ID:             43,
		Platform:       PlatformGrok,
		Type:           AccountTypeAPIKey,
		RateMultiplier: &rateMultiplier,
	}
	usageRepo := &accountTestProbeUsageRepo{}
	svc := &AccountTestService{
		accountRepo:    &accountTestProbeAccountRepo{account: account},
		usageLogRepo:   usageRepo,
		billingService: NewBillingService(&config.Config{}, nil),
	}
	startedAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	result := &ScheduledTestResult{Status: "success", StartedAt: startedAt, FinishedAt: startedAt.Add(time.Second)}

	svc.recordChannelProbeUsage(
		context.Background(),
		account.ID,
		"grok-imagine-video",
		result,
		UsageTokens{},
		accountTestMediaUsage{
			UpstreamModel:        "grok-imagine-video",
			VideoCount:           1,
			VideoResolution:      VideoBillingResolution480P,
			VideoDurationSeconds: 6,
		},
	)

	require.Len(t, usageRepo.logs, 1)
	log := usageRepo.logs[0]
	require.Equal(t, 1, log.VideoCount)
	require.Equal(t, VideoBillingResolution480P, *log.VideoResolution)
	require.Equal(t, 6, *log.VideoDurationSeconds)
	require.Equal(t, string(BillingModeVideo), *log.BillingMode)
	require.Positive(t, log.TotalCost)
	require.Zero(t, log.ActualCost)
	require.Equal(t, rateMultiplier, *log.AccountRateMultiplier)
}

func TestRecordChannelProbeUsageCapturesImageAccountCost(t *testing.T) {
	rateMultiplier := 1.25
	account := &Account{
		ID:             44,
		Platform:       PlatformGrok,
		Type:           AccountTypeAPIKey,
		RateMultiplier: &rateMultiplier,
	}
	usageRepo := &accountTestProbeUsageRepo{}
	svc := &AccountTestService{
		accountRepo:    &accountTestProbeAccountRepo{account: account},
		usageLogRepo:   usageRepo,
		billingService: NewBillingService(&config.Config{}, nil),
	}
	startedAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	result := &ScheduledTestResult{Status: "success", StartedAt: startedAt, FinishedAt: startedAt.Add(time.Second)}

	svc.recordChannelProbeUsage(
		context.Background(),
		account.ID,
		"image-alias",
		result,
		UsageTokens{InputTokens: 20, OutputTokens: 5},
		accountTestMediaUsage{
			UpstreamModel:      "grok-imagine-image-quality",
			ImageCount:         1,
			ImageSize:          "1K",
			ImageOutputSize:    "1024x1024",
			ImageSizeSource:    ImageSizeSourceOutput,
			ImageSizeBreakdown: map[string]int{"1K": 1},
		},
	)

	require.Len(t, usageRepo.logs, 1)
	log := usageRepo.logs[0]
	require.Equal(t, "image-alias", log.RequestedModel)
	require.Equal(t, "grok-imagine-image-quality", *log.UpstreamModel)
	require.Equal(t, 1, log.ImageCount)
	require.Equal(t, "1K", *log.ImageSize)
	require.Equal(t, "1024x1024", *log.ImageOutputSize)
	require.Equal(t, ImageSizeSourceOutput, *log.ImageSizeSource)
	require.Equal(t, map[string]int{"1K": 1}, log.ImageSizeBreakdown)
	require.Equal(t, string(BillingModeImage), *log.BillingMode)
	require.Positive(t, log.TotalCost)
	require.Zero(t, log.ActualCost)
	require.Equal(t, rateMultiplier, *log.AccountRateMultiplier)
}
