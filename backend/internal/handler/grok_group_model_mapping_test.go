package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type grokGroupMappingUpstream struct {
	service.HTTPUpstream
	body        []byte
	contentType string
	response    string
}

func (u *grokGroupMappingUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	var err error
	u.body, err = io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.contentType = req.Header.Get("Content-Type")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(u.response)),
	}, nil
}

type grokGroupMappingCache struct {
	openAIStickyCredentialFailoverCache
	pending map[string][]byte
	claimed map[string]bool
}

func (c *grokGroupMappingCache) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	c.pending[key] = append([]byte(nil), payload...)
	return nil
}

func (c *grokGroupMappingCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	return append([]byte(nil), c.pending[key]...), nil
}

func (c *grokGroupMappingCache) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	if c.claimed[key] {
		return false, nil
	}
	c.claimed[key] = true
	return true, nil
}

func (c *grokGroupMappingCache) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	delete(c.claimed, key)
	return nil
}

func newGrokGroupMappingHandler(t *testing.T, target, response string, providerModel ...string) (*OpenAIGatewayHandler, *service.APIKey, *grokGroupMappingUpstream, chan *service.UsageLog) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	group := &service.Group{
		ID: 7, Hydrated: true, Platform: service.PlatformGrok, Status: service.StatusActive,
		AllowImageGeneration: true, RateMultiplier: 1, ModelMapping: map[string]string{"luna": target},
	}
	upstreamTarget := target
	if len(providerModel) > 0 {
		upstreamTarget = providerModel[0]
	}
	account := service.Account{
		ID: 9912, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "test", "base_url": "https://api.x.ai", "model_mapping": map[string]any{target: upstreamTarget}},
	}
	require.False(t, account.IsModelSupported("luna"), "the group alias must not require an account mapping")
	require.True(t, account.IsModelSupported(target))
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{account}}
	upstream := &grokGroupMappingUpstream{response: response}
	cache := &grokGroupMappingCache{pending: make(map[string][]byte), claimed: make(map[string]bool)}
	logs := make(chan *service.UsageLog, 4)
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: logs}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gateway := service.NewOpenAIGatewayService(repo, usageRepo, nil, nil, nil, nil, cache, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billingCache, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	key := &service.APIKey{ID: 1, GroupID: &group.ID, Group: group, User: &service.User{ID: 2, Status: service.StatusActive}}
	return h, key, upstream, logs
}

func grokGroupMappingContext(key *service.APIKey, path, contentType string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, key.Group))
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.User.ID})
	return c, rec
}

func grokGroupMappingMultipart(t *testing.T, fileField, fileName string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	require.NoError(t, w.WriteField("model", "luna"))
	require.NoError(t, w.WriteField("prompt", "preserve me"))
	f, err := w.CreateFormFile(fileField, fileName)
	require.NoError(t, err)
	_, err = f.Write([]byte{0, 1, 2, 255, 13, 10})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return body.Bytes(), w.FormDataContentType()
}

func requireGrokMappedMultipart(t *testing.T, upstream *grokGroupMappingUpstream, target, fileField, fileName string) {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(upstream.contentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	r := multipart.NewReader(bytes.NewReader(upstream.body), params["boundary"])
	fields := make(map[string]string)
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		payload, err := io.ReadAll(part)
		require.NoError(t, err)
		if part.FormName() == fileField {
			require.Equal(t, fileName, part.FileName())
			require.Equal(t, []byte{0, 1, 2, 255, 13, 10}, payload)
		}
		fields[part.FormName()] = string(payload)
	}
	require.Equal(t, target, fields["model"])
	require.Equal(t, "preserve me", fields["prompt"])
	require.Contains(t, fields, fileField)
}

func TestGrokMediaGroupModelMappingWithoutAccountAlias(t *testing.T) {
	t.Run("image JSON", func(t *testing.T) {
		const target = "grok-imagine-image-quality"
		h, key, upstream, logs := newGrokGroupMappingHandler(t, target, `{"data":[{"url":"https://images.example.test/generated.png"}]}`)
		c, rec := grokGroupMappingContext(key, "/v1/images/generations", "application/json", []byte(`{"model":"luna","prompt":"preserve me"}`))
		h.GrokImages(c)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, target, gjson.GetBytes(upstream.body, "model").String())
		require.Equal(t, "preserve me", gjson.GetBytes(upstream.body, "prompt").String())
		requireGrokGroupMappingUsage(t, logs, "luna→"+target)
	})
	t.Run("image multipart", func(t *testing.T) {
		const target = "grok-imagine-image-quality"
		h, key, upstream, logs := newGrokGroupMappingHandler(t, target, `{"data":[{"url":"https://images.example.test/generated.png"}]}`)
		body, contentType := grokGroupMappingMultipart(t, "image", "reference.png")
		c, rec := grokGroupMappingContext(key, "/v1/images/edits", contentType, body)
		h.GrokImages(c)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		// Image edit uploads are converted to xAI JSON after applying the group
		// mapping; their bytes must survive both transformations.
		require.Equal(t, "application/json", upstream.contentType)
		require.Equal(t, target, gjson.GetBytes(upstream.body, "model").String())
		require.Equal(t, "preserve me", gjson.GetBytes(upstream.body, "prompt").String())
		imageURL := gjson.GetBytes(upstream.body, "image.url").String()
		_, encoded, found := strings.Cut(imageURL, ";base64,")
		require.True(t, found, imageURL)
		imageBytes, err := base64.StdEncoding.DecodeString(encoded)
		require.NoError(t, err)
		require.Equal(t, []byte{0, 1, 2, 255, 13, 10}, imageBytes)
		requireGrokGroupMappingUsage(t, logs, "luna→"+target)
	})
}

func TestGrokVoiceGroupModelMappingWithoutAccountAlias(t *testing.T) {
	for _, endpoint := range []string{"tts", "stt"} {
		t.Run(endpoint, func(t *testing.T) {
			const target = "grok-voice-latest"
			const provider = "provider-voice"
			h, key, upstream, logs := newGrokGroupMappingHandler(t, target, `{"text":"transcribed","duration":3}`, provider)
			body, contentType := []byte(`{"model":"luna","input":"hello"}`), "application/json"
			if endpoint == "stt" {
				body, contentType = grokGroupMappingMultipart(t, "file", "speech.mp3")
			}
			c, rec := grokGroupMappingContext(key, "/v1/"+endpoint, contentType, body)
			h.GrokVoice(c, endpoint)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			if endpoint == "stt" {
				requireGrokMappedMultipart(t, upstream, provider, "file", "speech.mp3")
			} else {
				require.Equal(t, provider, gjson.GetBytes(upstream.body, "model").String())
				require.Equal(t, "hello", gjson.GetBytes(upstream.body, "input").String())
			}
			requireGrokGroupMappingUsage(t, logs, "luna→"+target+"→"+provider)
		})
	}
}

func requireGrokGroupMappingUsage(t *testing.T, logs <-chan *service.UsageLog, chain string) {
	t.Helper()
	select {
	case log := <-logs:
		require.Equal(t, "luna", log.RequestedModel)
		require.NotNil(t, log.ModelMappingChain)
		require.Equal(t, chain, *log.ModelMappingChain)
	case <-time.After(time.Second):
		t.Fatal("mapped usage was not recorded")
	}
}

func TestGrokVideoGroupModelMappingPersistsCreateTimeTarget(t *testing.T) {
	const target = "grok-imagine-video"
	const provider = "provider-video"
	h, key, upstream, logs := newGrokGroupMappingHandler(t, target, `{"request_id":"video-mapped"}`, provider)
	c, rec := grokGroupMappingContext(key, "/v1/videos/generations", "application/json", []byte(`{"model":"luna","prompt":"ocean","resolution":"720p","duration":10}`))
	h.GrokVideoGeneration(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, provider, gjson.GetBytes(upstream.body, "model").String())
	pending, err := h.gatewayService.LoadGrokVideoPendingBilling(c.Request.Context(), "video-mapped", key.User.ID, key.ID)
	require.NoError(t, err)
	require.NotNil(t, pending)
	require.NotNil(t, pending.ModelMappingUsage)
	require.True(t, pending.ModelMappingUsage.GroupMapped)
	require.Equal(t, "luna", pending.ModelMappingUsage.OriginalModel)
	require.Equal(t, target, pending.ModelMappingUsage.ChannelMappedModel)
	require.Equal(t, "luna→"+target+"→"+provider, pending.ModelMappingUsage.ModelMappingChain)
	require.Equal(t, "luna", pending.OriginalModel)

	// A later poll sees refreshed group configuration; billing must use the
	// persisted target from creation, even when status names another model.
	key.Group.ModelMapping["luna"] = "changed-after-create"
	statusResult := &service.OpenAIForwardResult{
		Model: "status-provider-model", BillingModel: "status-provider-model", UpstreamModel: "status-provider-model",
		ResponseID: "video-mapped", VideoCount: 1, VideoDurationSeconds: 12,
	}
	result := prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, middleware.AuthSubject{UserID: key.User.ID}, "video-mapped", statusResult)
	require.NotNil(t, result)
	require.Equal(t, target, result.BillingModel)
	require.NotNil(t, result.ModelMappingUsage)
	require.Equal(t, "luna", result.ModelMappingUsage.OriginalModel)
	require.Equal(t, target, result.ModelMappingUsage.ChannelMappedModel)
	require.Equal(t, service.BillingModelSourceChannelMapped, result.ModelMappingUsage.BillingModelSource)
	require.Equal(t, "720p", result.VideoResolution)
	require.Equal(t, 12, result.VideoDurationSeconds)
	require.Equal(t, "grok-video:video-mapped", result.RequestID)
	account := &service.Account{ID: 9912, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey}
	recordGrokMediaUsage(c, h, zap.NewNop(), key, middleware.AuthSubject{UserID: key.User.ID}, nil, account, result, result.Model, nil, "video-mapped")
	requireGrokGroupMappingUsage(t, logs, "luna→"+target+"→"+provider)
	require.Nil(t, prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, middleware.AuthSubject{UserID: key.User.ID}, "video-mapped", statusResult), "later polls must not duplicate billing")
}
