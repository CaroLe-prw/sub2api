//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUsageBillingRepositoryApply_SettlesDeletedKey(t *testing.T) {
	for _, subscriptionBilling := range []bool{false, true} {
		for _, limit := range []string{"quota", "window", "both"} {
			t.Run(fmt.Sprintf("subscription=%t/%s", subscriptionBilling, limit), func(t *testing.T) {
				ctx := context.Background()
				client := testEntClient(t)
				user := mustCreateUser(t, client, &service.User{
					Email: "deleted-key-bill-" + uuid.NewString() + "@example.com", PasswordHash: "hash", Balance: 100,
				})
				keyRepo := NewAPIKeyRepository(client, integrationDB)
				key := &service.APIKey{UserID: user.ID, Key: "sk-deleted-key-bill-" + uuid.NewString(), Name: "in-flight", Status: service.StatusAPIKeyActive}
				if limit != "window" {
					key.Quota = 1
				}
				if limit != "quota" {
					key.RateLimit5h, key.RateLimit1d, key.RateLimit7d = 10, 20, 30
				}
				require.NoError(t, keyRepo.Create(ctx, key))
				cmd := &service.UsageBillingCommand{
					RequestID: uuid.NewString(), APIKeyID: key.ID, UserID: user.ID, BalanceCost: 1.25,
				}
				if limit != "window" {
					cmd.APIKeyQuotaCost = 1.25
				}
				if limit != "quota" {
					cmd.APIKeyRateLimitCost = 1.25
				}
				if subscriptionBilling {
					group := mustCreateGroup(t, client, &service.Group{
						Name: "deleted-key-sub-" + uuid.NewString(), Platform: service.PlatformAnthropic,
						SubscriptionType: service.SubscriptionTypeSubscription,
					})
					sub := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, GroupID: group.ID})
					cmd.SubscriptionID = &sub.ID
					cmd.SubscriptionCost, cmd.BalanceCost = 1.25, 0
				}

				// The request was admitted before deletion. Reusing its key text must
				// not move the old request's usage onto the replacement credential.
				require.NoError(t, keyRepo.DeleteWithAudit(ctx, key.ID))
				var tombstone string
				var deletedAt time.Time
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT key, deleted_at FROM api_keys WHERE id = $1", key.ID).Scan(&tombstone, &deletedAt))
				require.NotEqual(t, key.Key, tombstone)
				replacement := &service.APIKey{UserID: user.ID, Key: key.Key, Name: "replacement", Status: service.StatusAPIKeyActive, Quota: 1, RateLimit5h: 10}
				require.NoError(t, keyRepo.Create(ctx, replacement))
				require.NotEqual(t, key.ID, replacement.ID)

				repo := NewUsageBillingRepository(client, integrationDB)
				result, err := repo.Apply(ctx, cmd)
				require.NoError(t, err, "a deleted key must not roll back an already incurred charge")
				require.True(t, result.Applied)
				require.False(t, result.APIKeyQuotaExhausted, "tombstones must not trigger live-key state transitions")
				result, err = repo.Apply(ctx, cmd)
				require.NoError(t, err)
				require.False(t, result.Applied, "billing retries must remain idempotent")

				var balance float64
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
				require.InDelta(t, 100-cmd.BalanceCost, balance, 1e-8)
				if subscriptionBilling {
					var daily, weekly, monthly float64
					require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd, weekly_usage_usd, monthly_usage_usd FROM user_subscriptions WHERE id = $1", *cmd.SubscriptionID).Scan(&daily, &weekly, &monthly))
					require.Equal(t, []float64{1.25, 1.25, 1.25}, []float64{daily, weekly, monthly})
				}
				var used, used5h, used1d, used7d float64
				var status, storedKey string
				var storedDeletedAt sql.NullTime
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used, usage_5h, usage_1d, usage_7d, status, key, deleted_at FROM api_keys WHERE id = $1", key.ID).Scan(&used, &used5h, &used1d, &used7d, &status, &storedKey, &storedDeletedAt))
				require.InDelta(t, cmd.APIKeyQuotaCost, used, 1e-8)
				require.Equal(t, []float64{cmd.APIKeyRateLimitCost, cmd.APIKeyRateLimitCost, cmd.APIKeyRateLimitCost}, []float64{used5h, used1d, used7d})
				require.Equal(t, service.StatusAPIKeyActive, status)
				require.Equal(t, tombstone, storedKey)
				require.True(t, storedDeletedAt.Valid)
				require.True(t, deletedAt.Equal(storedDeletedAt.Time))
				_, err = keyRepo.GetByID(ctx, key.ID)
				require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
				liveKey, err := keyRepo.GetByKey(ctx, key.Key)
				require.NoError(t, err)
				require.Equal(t, replacement.ID, liveKey.ID)
				require.True(t, liveKey.IsActive())
				require.Zero(t, liveKey.QuotaUsed)
				require.Zero(t, liveKey.Usage5h)
			})
		}
	}
}
