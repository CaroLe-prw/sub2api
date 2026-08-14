//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// openaiTransportAccountRepoStub records SetTempUnschedulable calls. It embeds the
// (nil) AccountRepository interface so any other method call would panic — the
// helper under test must only touch SetTempUnschedulable. tempUnschedCall is shared
// with antigravity_internal500_penalty_test.go (same package).
type openaiTransportAccountRepoStub struct {
	AccountRepository
	tempUnschedCalls []tempUnschedCall
}

func (r *openaiTransportAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempUnschedCalls = append(r.tempUnschedCalls, tempUnschedCall{accountID: id, until: until, reason: reason})
	return nil
}

func newOpenAITransportErrTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

type failingOpenAIHTTPUpstream struct {
	err   error
	calls int
}

func (u *failingOpenAIHTTPUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	return nil, u.err
}

func (u *failingOpenAIHTTPUpstream) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls++
	return nil, u.err
}

// A durable proxy/credential failure must temporarily unschedule the account,
// but the ambiguous request attempt must not be replayed.
func TestHandleOpenAIUpstreamTransportError_PersistentEvictsWithoutReplay(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 4627, Name: "proxy-expired", Platform: PlatformOpenAI}
	c, rec := newOpenAITransportErrTestContext()

	before := time.Now()
	retErr := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account,
		errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": socks connect tcp 85.255.176.68:12324->chatgpt.com:443: username/password authentication failed`), false)
	after := time.Now()

	// Failover error (handler will switch accounts), not a direct response.
	var fo *UpstreamFailoverError
	require.True(t, errors.As(retErr, &fo), "persistent error must return *UpstreamFailoverError")
	require.Equal(t, http.StatusBadGateway, fo.StatusCode)
	require.True(t, fo.BillingExposurePossible)
	require.False(t, fo.ShouldRetryNextAccount())

	// Persistent → account temporarily unscheduled for ~10min, reason carries cause.
	require.Len(t, repo.tempUnschedCalls, 1)
	require.Equal(t, int64(4627), repo.tempUnschedCalls[0].accountID)
	require.Contains(t, repo.tempUnschedCalls[0].reason, "authentication failed")
	require.True(t, repo.tempUnschedCalls[0].until.After(before.Add(openAITransportErrorTempUnschedDuration-time.Second)))
	require.True(t, repo.tempUnschedCalls[0].until.Before(after.Add(openAITransportErrorTempUnschedDuration+time.Second)))

	// Immediate in-memory effect so subsequent requests skip it before DB/cache catches up.
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))

	// Must NOT write a response body — the handler owns the (failover) response.
	require.Equal(t, 0, rec.Body.Len())
}

// A transient transport blip has no explicit upstream rejection, so it must not
// be replayed even though it does not evict the account.
func TestHandleOpenAIUpstreamTransportError_TransientStopsWithoutEviction(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 99, Name: "flaky", Platform: PlatformOpenAI}
	c, rec := newOpenAITransportErrTestContext()

	err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account,
		errors.New(`Post "https://chatgpt.com/...": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`), false)

	var fo *UpstreamFailoverError
	require.True(t, errors.As(err, &fo), "transient error must return *UpstreamFailoverError")
	require.Equal(t, http.StatusBadGateway, fo.StatusCode)
	require.True(t, fo.BillingExposurePossible)
	require.False(t, fo.ShouldRetryNextAccount())

	// Transient → do NOT evict.
	require.Empty(t, repo.tempUnschedCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.Equal(t, 0, rec.Body.Len())
}

// A transport can surface context.Canceled even while the inbound client request
// is still alive (for example an upstream/proxy cancels its own round trip). That
// is an upstream failure and must remain eligible for another account.
func TestHandleOpenAIUpstreamTransportError_InternalContextCanceled_FailsOver(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 77, Name: "healthy", Platform: PlatformOpenAI}
	c, rec := newOpenAITransportErrTestContext()

	err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account,
		context.Canceled, false)

	var fo *UpstreamFailoverError
	require.True(t, errors.As(err, &fo), "upstream-only context.Canceled must return *UpstreamFailoverError")
	require.Equal(t, http.StatusBadGateway, fo.StatusCode)

	// An ambiguous cancellation is transient: switch accounts but do not evict.
	require.Empty(t, repo.tempUnschedCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))

	// Must NOT write a response body.
	require.Equal(t, 0, rec.Body.Len())
}

func TestHandleOpenAIUpstreamTransportError_ClientContextCanceled_NoFailoverNoEviction(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 78, Name: "healthy2", Platform: PlatformOpenAI}
	c, rec := newOpenAITransportErrTestContext()
	clientCtx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(clientCtx)

	err := svc.handleOpenAIUpstreamTransportError(clientCtx, c, account, context.Canceled, false)

	var fo *UpstreamFailoverError
	require.False(t, errors.As(err, &fo), "a canceled inbound request must not be replayed")
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, repo.tempUnschedCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.Equal(t, 0, rec.Body.Len())
}

// Wrapping does not change the distinction: if the client is still connected,
// the cancellation belongs to the upstream attempt and another account may run.
func TestHandleOpenAIUpstreamTransportError_WrappedInternalContextCanceled_FailsOver(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 79, Name: "healthy3", Platform: PlatformOpenAI}
	c, _ := newOpenAITransportErrTestContext()

	wrapped := fmt.Errorf("http request failed: %w", context.Canceled)
	err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account, wrapped, false)

	var fo *UpstreamFailoverError
	require.True(t, errors.As(err, &fo), "wrapped upstream context.Canceled must return *UpstreamFailoverError")
	require.Empty(t, repo.tempUnschedCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
}

// When accountRepo is nil (no DB), in-memory block must still happen but the
// success log "openai.account_temp_unscheduled_transport" must NOT fire (it
// would be misleading: the account is only blocked in memory, not persisted).
// We verify the in-memory block occurs and no DB call is made.
func TestTempUnscheduleOpenAITransportError_NilAccountRepo_InMemoryBlockOnly(t *testing.T) {
	// nil accountRepo → no DB write.
	svc := &OpenAIGatewayService{accountRepo: nil}
	account := &Account{ID: 55, Name: "no-db", Platform: PlatformOpenAI}

	svc.tempUnscheduleOpenAITransportError(context.Background(), account, "proxy refused")

	// In-memory block must still happen.
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account),
		"in-memory block must apply even when accountRepo is nil")
}

// context.DeadlineExceeded does not prove that the upstream rejected the
// request, so it must stop without replay.
func TestHandleOpenAIUpstreamTransportError_DeadlineExceeded_StopsWithoutReplay(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 79, Name: "slow", Platform: PlatformOpenAI}
	c, _ := newOpenAITransportErrTestContext()

	err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account,
		context.DeadlineExceeded, false)

	var fo *UpstreamFailoverError
	require.True(t, errors.As(err, &fo))
	require.True(t, fo.BillingExposurePossible)
	require.False(t, fo.ShouldRetryNextAccount())
}

func TestForwardAsRawChatCompletions_TransportErrorStopsWithoutReplay(t *testing.T) {
	repo := &openaiTransportAccountRepoStub{}
	upstream := &failingOpenAIHTTPUpstream{
		err: errors.New(`Post "https://opencode.ai/zen/v1/chat/completions": EOF`),
	}
	svc := &OpenAIGatewayService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
	}
	account := &Account{
		ID:          81,
		Name:        "oc-20053",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://opencode.ai/zen/v1"},
	}
	c, rec := newOpenAITransportErrTestContext()
	body := []byte(`{"model":"deepseek-v4-flash-free","messages":[{"role":"user","content":"hello"}]}`)

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")

	require.Equal(t, 1, upstream.calls)
	var fo *UpstreamFailoverError
	require.True(t, errors.As(err, &fo), "transport error must remain protocol-compatible for handler error reporting")
	require.Equal(t, http.StatusBadGateway, fo.StatusCode)
	require.False(t, fo.ShouldRetryNextAccount())
	require.Empty(t, repo.tempUnschedCalls, "plain EOF is transient: stop without evicting the account")
	require.Equal(t, 0, rec.Body.Len(), "service must not write a hard 502 before the handler formats the terminal error")
}

func TestHandleOpenAIUpstreamTransportError_RecordsOllamaActivityOnly(t *testing.T) {
	deferred := NewDeferredService(nil, nil, time.Second)
	svc := &OpenAIGatewayService{
		accountRepo:     &openaiTransportAccountRepoStub{},
		deferredService: deferred,
	}
	ollama := &Account{
		ID: 501, Name: "ollama-cloud", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "k-ollama", "base_url": "https://ollama.com"},
	}
	other := &Account{
		ID: 502, Name: "openai-official", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "k-openai", "base_url": "https://api.openai.com"},
	}
	c, _ := newOpenAITransportErrTestContext()

	_ = svc.handleOpenAIUpstreamTransportError(context.Background(), c, ollama, errors.New("connection reset"), false)
	_ = svc.handleOpenAIUpstreamTransportError(context.Background(), c, other, errors.New("connection reset"), false)

	_, ok := deferred.lastUsedUpdates.Load(int64(501))
	require.True(t, ok, "Ollama Cloud transport error must schedule last_used activity")
	_, ok = deferred.lastUsedUpdates.Load(int64(502))
	require.False(t, ok, "non-Ollama transport error must not schedule Ollama activity")
}

func TestHandleOpenAIUpstreamTransportError_ContextCanceledSkipsOllamaActivity(t *testing.T) {
	deferred := NewDeferredService(nil, nil, time.Second)
	svc := &OpenAIGatewayService{
		accountRepo:     &openaiTransportAccountRepoStub{},
		deferredService: deferred,
	}
	ollama := &Account{
		ID: 503, Name: "ollama-canceled", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "k-ollama", "base_url": "https://ollama.com"},
	}
	c, _ := newOpenAITransportErrTestContext()
	clientCtx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(clientCtx)

	err := svc.handleOpenAIUpstreamTransportError(clientCtx, c, ollama, context.Canceled, false)

	require.ErrorIs(t, err, context.Canceled)
	_, ok := deferred.lastUsedUpdates.Load(int64(503))
	require.False(t, ok, "context.Canceled is client disconnect before a fault; do not count as Ollama activity")
}

func TestHandleOpenAIAccountUpstreamError_RecordsOllamaActivityOnly(t *testing.T) {
	deferred := NewDeferredService(nil, nil, time.Second)
	svc := &OpenAIGatewayService{deferredService: deferred}
	ollama := &Account{
		ID: 504, Name: "ollama-429", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "k-ollama", "base_url": "https://ollama.com"},
	}
	other := &Account{
		ID: 505, Name: "openai-429", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "k-openai", "base_url": "https://api.openai.com"},
	}

	_ = svc.handleOpenAIAccountUpstreamError(context.Background(), ollama, http.StatusTooManyRequests, http.Header{}, []byte(`{"error":{"message":"rate"}}`), "gpt-test")
	_ = svc.handleOpenAIAccountUpstreamError(context.Background(), other, http.StatusTooManyRequests, http.Header{}, []byte(`{"error":{"message":"rate"}}`), "gpt-test")

	_, ok := deferred.lastUsedUpdates.Load(int64(504))
	require.True(t, ok, "Ollama Cloud non-2xx must schedule last_used activity")
	_, ok = deferred.lastUsedUpdates.Load(int64(505))
	require.False(t, ok, "non-Ollama non-2xx must not schedule Ollama activity")
}
