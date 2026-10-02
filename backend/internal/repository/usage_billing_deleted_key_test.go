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

func TestUsageBillingRepositoryApply_KeyBookkeepingErrorsStillRollback(t *testing.T) {
	storageErr := errors.New("database write failed")
	for _, tc := range []struct {
		name  string
		quota bool
		err   error
	}{
		{"missing quota row", true, service.ErrAPIKeyNotFound},
		{"missing window row", false, service.ErrAPIKeyNotFound},
		{"quota database failure", true, storageErr},
		{"window database failure", false, storageErr},
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
				query := mock.ExpectQuery("UPDATE api_keys")
				if tc.err == service.ErrAPIKeyNotFound {
					query.WillReturnError(sql.ErrNoRows)
				} else {
					query.WillReturnError(tc.err)
				}
			} else {
				cmd.APIKeyRateLimitCost = 1.25
				query := mock.ExpectExec("UPDATE api_keys")
				if tc.err == service.ErrAPIKeyNotFound {
					query.WillReturnResult(sqlmock.NewResult(0, 0))
				} else {
					query.WillReturnError(tc.err)
				}
			}
			mock.ExpectRollback()
			result, err := (&usageBillingRepository{db: db}).Apply(context.Background(), cmd)
			require.ErrorIs(t, err, tc.err)
			require.Nil(t, result)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
