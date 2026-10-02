package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUsageBillingRepositoryApply_MissingKeyStillSettles(t *testing.T) {
	for _, billing := range []string{"balance", "subscription"} {
		for _, counters := range []string{"quota", "window", "both"} {
			t.Run(billing+"/"+counters, func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				defer func() { _ = db.Close() }()

				cmd := &service.UsageBillingCommand{
					RequestID:        "missing-key-billing",
					APIKeyID:         7,
					UserID:           42,
					AccountID:        13,
					AccountType:      service.AccountTypeAPIKey,
					AccountQuotaCost: 0.75,
				}
				mock.ExpectBegin()
				mock.ExpectQuery("INSERT INTO usage_billing_dedup").
					WithArgs(cmd.RequestID, cmd.APIKeyID, sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
				mock.ExpectQuery("SELECT request_fingerprint.*FROM usage_billing_dedup_archive").
					WithArgs(cmd.RequestID, cmd.APIKeyID).WillReturnError(sql.ErrNoRows)
				if billing == "balance" {
					cmd.BalanceCost = 1.25
					mock.ExpectQuery("(?s)UPDATE users.*SET balance = balance -.*RETURNING balance").
						WithArgs(1.25, cmd.UserID).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(98.75))
				} else {
					subscriptionID := int64(29)
					cmd.SubscriptionID = &subscriptionID
					cmd.SubscriptionCost = 1.25
					mock.ExpectExec("UPDATE user_subscriptions").WithArgs(1.25, subscriptionID).
						WillReturnResult(sqlmock.NewResult(0, 1))
				}
				if counters == "quota" || counters == "both" {
					cmd.APIKeyQuotaCost = 1.25
					mock.ExpectQuery("UPDATE api_keys").
						WithArgs(1.25, cmd.APIKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).
						WillReturnError(sql.ErrNoRows)
				}
				if counters == "window" || counters == "both" {
					cmd.APIKeyRateLimitCost = 1.25
					mock.ExpectExec("UPDATE api_keys").WithArgs(1.25, cmd.APIKeyID).
						WillReturnResult(sqlmock.NewResult(0, 0))
				}
				// Missing key bookkeeping must not skip the remaining account settlement.
				mock.ExpectQuery("UPDATE accounts SET extra").WithArgs(0.75, cmd.AccountID).
					WillReturnRows(sqlmock.NewRows([]string{
						"quota_used", "quota_limit", "quota_daily_used", "quota_daily_limit", "quota_weekly_used", "quota_weekly_limit",
					}).AddRow(10.75, 100, 0, 0, 0, 0))
				mock.ExpectCommit()

				repo := &usageBillingRepository{db: db}
				result, err := repo.Apply(context.Background(), cmd)
				require.NoError(t, err)
				require.True(t, result.Applied)
				require.False(t, result.APIKeyQuotaExhausted)
				require.False(t, result.BalanceOverdrafted)
				if billing == "balance" {
					require.NotNil(t, result.NewBalance)
					require.InDelta(t, 98.75, *result.NewBalance, 0.000001)
				} else {
					require.Nil(t, result.NewBalance)
				}
				require.NotNil(t, result.QuotaState)
				require.InDelta(t, 10.75, result.QuotaState.TotalUsed, 0.000001)

				// Committed billing is still idempotent after the key is gone.
				mock.ExpectBegin()
				mock.ExpectQuery("INSERT INTO usage_billing_dedup").
					WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).WillReturnError(sql.ErrNoRows)
				mock.ExpectQuery("SELECT request_fingerprint FROM usage_billing_dedup WHERE").
					WithArgs(cmd.RequestID, cmd.APIKeyID).
					WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
				mock.ExpectRollback()
				duplicate, err := repo.Apply(context.Background(), cmd)
				require.NoError(t, err)
				require.False(t, duplicate.Applied)
				require.Nil(t, duplicate.NewBalance)
				require.Nil(t, duplicate.QuotaState)

				// A different request cannot reuse the settled request/key pair.
				conflict := *cmd
				conflict.RequestFingerprint = "different-request-fingerprint"
				mock.ExpectBegin()
				mock.ExpectQuery("INSERT INTO usage_billing_dedup").
					WithArgs(cmd.RequestID, cmd.APIKeyID, conflict.RequestFingerprint).WillReturnError(sql.ErrNoRows)
				mock.ExpectQuery("SELECT request_fingerprint FROM usage_billing_dedup WHERE").
					WithArgs(cmd.RequestID, cmd.APIKeyID).
					WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
				mock.ExpectRollback()
				result, err = repo.Apply(context.Background(), &conflict)
				require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
				require.Nil(t, result)
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestUsageBillingRepositoryApply_KeyStorageErrorsStillRollback(t *testing.T) {
	storageErr := errors.New("database write failed")
	for _, tc := range []struct {
		name         string
		quota        bool
		rowsAffected bool
	}{
		{name: "quota database failure", quota: true},
		{name: "window database failure"},
		{name: "window affected rows failure", rowsAffected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			cmd := &service.UsageBillingCommand{RequestID: "billing-failure-test", APIKeyID: 7, UserID: 42, BalanceCost: 1.25}
			mock.ExpectBegin()
			mock.ExpectQuery("INSERT INTO usage_billing_dedup").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			mock.ExpectQuery("SELECT request_fingerprint.*FROM usage_billing_dedup_archive").WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery("(?s)UPDATE users.*SET balance = balance -.*RETURNING balance").
				WithArgs(1.25, int64(42)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(98.75))
			if tc.quota {
				cmd.APIKeyQuotaCost = 1.25
				mock.ExpectQuery("UPDATE api_keys").
					WithArgs(1.25, cmd.APIKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).
					WillReturnError(storageErr)
			} else {
				cmd.APIKeyRateLimitCost = 1.25
				query := mock.ExpectExec("UPDATE api_keys").WithArgs(1.25, cmd.APIKeyID)
				if tc.rowsAffected {
					query.WillReturnResult(sqlmock.NewErrorResult(storageErr))
				} else {
					query.WillReturnError(storageErr)
				}
			}
			mock.ExpectRollback()
			result, err := (&usageBillingRepository{db: db}).Apply(context.Background(), cmd)
			require.ErrorIs(t, err, storageErr)
			require.Nil(t, result)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
