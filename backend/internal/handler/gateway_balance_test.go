package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type balanceUserRepository struct {
	service.UserRepository
	user *service.User
	err  error
}

func (r *balanceUserRepository) GetByID(_ context.Context, id int64) (*service.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.user == nil || r.user.ID != id {
		return nil, errors.New("unexpected user")
	}
	copy := *r.user
	return &copy, nil
}

func (r *balanceUserRepository) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

type balanceAPIKeyRepository struct {
	service.APIKeyRepository
	data *service.APIKeyRateLimitData
	err  error
}

func (r *balanceAPIKeyRepository) GetRateLimitData(context.Context, int64) (*service.APIKeyRateLimitData, error) {
	return r.data, r.err
}

func TestGatewayBalanceAmounts(t *testing.T) {
	now := time.Now()
	expiredWindow := now.Add(-6 * time.Hour)
	dailyLimit, weeklyLimit := 10.0, 40.0
	for _, tc := range []struct {
		name         string
		key          service.APIKey
		subscription *service.UserSubscription
		rateData     *service.APIKeyRateLimitData
		rateErr      error
		wallet       float64
		walletErr    error
		want         float64
		status       int
	}{
		{name: "fresh wallet", wallet: 23.75, want: 23.75},
		{name: "empty wallet", wallet: 0, want: 0},
		{name: "key quota instead of owner wallet", key: service.APIKey{Quota: 100, QuotaUsed: 30}, wallet: 999, want: 70},
		{name: "exhausted key", key: service.APIKey{Status: service.StatusAPIKeyQuotaExhausted, Quota: 100, QuotaUsed: 105}, wallet: 999, want: 0},
		{
			name: "subscription tightest window",
			key: service.APIKey{Group: &service.Group{
				SubscriptionType: service.SubscriptionTypeSubscription,
				DailyLimitUSD:    &dailyLimit, WeeklyLimitUSD: &weeklyLimit,
			}},
			subscription: &service.UserSubscription{DailyUsageUSD: 6, WeeklyUsageUSD: 39},
			want:         1,
		},
		{
			name: "subscription without active plan",
			key:  service.APIKey{Group: &service.Group{SubscriptionType: service.SubscriptionTypeSubscription}},
			want: 0,
		},
		{
			name:         "unlimited subscription retains usage sentinel",
			key:          service.APIKey{Group: &service.Group{SubscriptionType: service.SubscriptionTypeSubscription}},
			subscription: &service.UserSubscription{},
			want:         -1,
		},
		{
			name: "rate-only key tightest window",
			key:  service.APIKey{RateLimit5h: 10, RateLimit1d: 20},
			rateData: &service.APIKeyRateLimitData{
				Usage5h: 8, Usage1d: 13, Window5hStart: &now, Window1dStart: &now,
			},
			wallet: 999, want: 2,
		},
		{
			name: "expired rate window",
			key:  service.APIKey{RateLimit5h: 10},
			rateData: &service.APIKeyRateLimitData{
				Usage5h: 9, Window5hStart: &expiredWindow,
			},
			want: 10,
		},
		{name: "rate lookup failure is not zero balance", key: service.APIKey{RateLimit5h: 10}, rateErr: errors.New("private database detail"), status: http.StatusServiceUnavailable},
		{name: "wallet lookup failure", walletErr: errors.New("private database detail"), status: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			key := tc.key
			key.ID, key.UserID = 7, 42
			if key.Status == "" {
				key.Status = service.StatusAPIKeyActive
			}
			// The cached authentication snapshot is intentionally different from
			// the wallet repository: balance probes must read the current balance.
			key.User = &service.User{ID: 42, Status: service.StatusActive, Balance: 9999}
			h := &GatewayHandler{
				userService: service.NewUserService(&balanceUserRepository{
					user: &service.User{ID: 42, Status: service.StatusActive, Balance: tc.wallet}, err: tc.walletErr,
				}, nil, nil, nil),
				apiKeyService: service.NewAPIKeyService(&balanceAPIKeyRepository{
					data: tc.rateData, err: tc.rateErr,
				}, nil, nil, nil, nil, nil, nil),
				// A balance request must not call any historical usage repository.
				usageService: &service.UsageService{},
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/user/balance", nil)
			c.Set(string(middleware.ContextKeyAPIKey), &key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})
			if tc.subscription != nil {
				c.Set(string(middleware.ContextKeySubscription), tc.subscription)
			}

			h.Balance(c)

			status := tc.status
			if status == 0 {
				status = http.StatusOK
			}
			require.Equal(t, status, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			if status != http.StatusOK {
				require.NotContains(t, body, "balance")
				require.NotContains(t, w.Body.String(), "private database detail")
				return
			}
			require.Equal(t, map[string]any{"is_active": true, "balance": tc.want, "unit": "USD"}, body)
		})
	}
}

func TestGatewayBalanceRequiresAuthContext(t *testing.T) {
	for _, keyPresent := range []bool{false, true} {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/user/balance", nil)
		if keyPresent {
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Status: service.StatusAPIKeyActive})
		}
		(&GatewayHandler{}).Balance(c)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.NotContains(t, w.Body.String(), "\"balance\"")
	}
}

func TestGatewayUsageWalletResponseKeepsExistingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &GatewayHandler{userService: service.NewUserService(&balanceUserRepository{
		user: &service.User{ID: 42, Status: service.StatusActive, Balance: 12.5},
	}, nil, nil, nil)}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 7, UserID: 42, Status: service.StatusAPIKeyActive})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})

	h.Usage(c)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, map[string]any{
		"mode": "unrestricted", "isValid": true, "planName": "钱包余额",
		"remaining": 12.5, "balance": 12.5, "unit": "USD",
	}, body)
}
