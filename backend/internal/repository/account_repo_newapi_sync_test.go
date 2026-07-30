package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestUpdateNewAPISyncResultCommitsRatioStateAndOutboxAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)`+regexp.QuoteMeta("SELECT rate_multiplier")+`.*`+regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(17), service.NewAPISyncIdentityExtraKey).
		WillReturnRows(sqlmock.NewRows([]string{"rate_multiplier", "identity", "base_url", "api_key"}).
			AddRow(0.04, "identity", "", "sk-account")).
		RowsWillBeClosed()
	mock.ExpectExec(`(?s)`+regexp.QuoteMeta("UPDATE accounts")+`.*`+
		regexp.QuoteMeta("SET rate_multiplier = $1")+`.*`+
		regexp.QuoteMeta("COALESCE(extra ->> $4, '') = $5")).
		WithArgs(0.0325, sqlmock.AnyArg(), int64(17), service.NewAPISyncIdentityExtraKey, "identity").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(17), nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	ratio := 0.0325
	userGroup := "GPT Lite"
	tokenGroup := "GPT Lite大户组"
	actualGroup := tokenGroup
	source := service.NewAPIRatioSourceConfiguredGroup
	crossRetry := false
	repo := newAccountRepositoryWithSQL(client, db, nil)
	result, err := repo.UpdateNewAPISyncResult(context.Background(), &service.NewAPISyncWrite{
		AccountID:                 17,
		ExpectedIdentity:          "identity",
		ExpectedAccountAPIKeyHash: hashNewAPIAccountAPIKey("sk-account"),
		AttemptedAt:               time.Date(2026, 7, 30, 3, 0, 0, 0, time.UTC),
		Status:                    service.NewAPISyncStatusOK,
		UserGroup:                 &userGroup,
		TokenGroup:                &tokenGroup,
		ActualGroup:               &actualGroup,
		RatioSource:               &source,
		CrossGroupRetry:           &crossRetry,
		Ratio:                     &ratio,
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, 0.04, result.OldRatio)
	require.Equal(t, 0.0325, result.NewRatio)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateNewAPISyncResultFailureDoesNotWriteRatioOrOutbox(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)`+regexp.QuoteMeta("SELECT rate_multiplier")+`.*`+regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(18), service.NewAPISyncIdentityExtraKey).
		WillReturnRows(sqlmock.NewRows([]string{"rate_multiplier", "identity", "base_url", "api_key"}).
			AddRow(0.04, "identity", "", "sk-account")).
		RowsWillBeClosed()
	mock.ExpectExec(`(?s)`+regexp.QuoteMeta("UPDATE accounts")+`.*`+
		regexp.QuoteMeta("SET extra = COALESCE(extra, '{}'::jsonb) || $1::jsonb")).
		WithArgs(sqlmock.AnyArg(), int64(18), service.NewAPISyncIdentityExtraKey, "identity").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	result, err := repo.UpdateNewAPISyncResult(context.Background(), &service.NewAPISyncWrite{
		AccountID:                 18,
		ExpectedIdentity:          "identity",
		ExpectedAccountAPIKeyHash: hashNewAPIAccountAPIKey("sk-account"),
		AttemptedAt:               time.Now(),
		Status:                    service.NewAPISyncStatusFailed,
	})
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.Equal(t, 0.04, result.NewRatio)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateNewAPISyncResultUnchangedRatioRefreshesSchedulingSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)`+regexp.QuoteMeta("SELECT rate_multiplier")+`.*`+regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(20), service.NewAPISyncIdentityExtraKey).
		WillReturnRows(sqlmock.NewRows([]string{"rate_multiplier", "identity", "base_url", "api_key"}).
			AddRow(0.0325, "identity", "", "sk-account")).
		RowsWillBeClosed()
	mock.ExpectExec(`(?s)`+regexp.QuoteMeta("UPDATE accounts")+`.*`+
		regexp.QuoteMeta("SET extra = COALESCE(extra, '{}'::jsonb) || $1::jsonb")).
		WithArgs(sqlmock.AnyArg(), int64(20), service.NewAPISyncIdentityExtraKey, "identity").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(20), nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	ratio := 0.0325
	now := time.Now().UTC()
	repo := newAccountRepositoryWithSQL(client, db, nil)
	result, err := repo.UpdateNewAPISyncResult(context.Background(), &service.NewAPISyncWrite{
		AccountID:                 20,
		ExpectedIdentity:          "identity",
		ExpectedAccountAPIKeyHash: hashNewAPIAccountAPIKey("sk-account"),
		AttemptedAt:               now,
		Status:                    service.NewAPISyncStatusOK,
		Ratio:                     &ratio,
		SchedulingSnapshot: &service.UpstreamBillingProbeSnapshot{
			Status:        service.UpstreamBillingProbeStatusOK,
			LastAttemptAt: now,
			NextProbeAt:   now.Add(30 * time.Minute),
		},
	})
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.Equal(t, ratio, result.NewRatio)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateNewAPISyncResultRejectsChangedIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)`+regexp.QuoteMeta("SELECT rate_multiplier")+`.*`+regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(19), service.NewAPISyncIdentityExtraKey).
		WillReturnRows(sqlmock.NewRows([]string{"rate_multiplier", "identity", "base_url", "api_key"}).
			AddRow(0.04, "new-identity", "", "sk-account")).
		RowsWillBeClosed()
	mock.ExpectRollback()

	ratio := 0.0325
	repo := newAccountRepositoryWithSQL(client, db, nil)
	_, err = repo.UpdateNewAPISyncResult(context.Background(), &service.NewAPISyncWrite{
		AccountID:                 19,
		ExpectedIdentity:          "old-identity",
		ExpectedAccountAPIKeyHash: hashNewAPIAccountAPIKey("sk-account"),
		AttemptedAt:               time.Now(),
		Status:                    service.NewAPISyncStatusOK,
		Ratio:                     &ratio,
	})
	require.ErrorIs(t, err, service.ErrNewAPISyncIdentityChanged)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateNewAPISyncResultRejectsChangedInheritedAccountEndpoint(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)`+regexp.QuoteMeta("SELECT rate_multiplier")+`.*`+regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(21), service.NewAPISyncIdentityExtraKey).
		WillReturnRows(sqlmock.NewRows([]string{"rate_multiplier", "identity", "base_url", "api_key"}).
			AddRow(0.04, "identity", "https://new-endpoint.example.test", "sk-account")).
		RowsWillBeClosed()
	mock.ExpectRollback()

	ratio := 0.0325
	expectedBaseURL := "https://old-endpoint.example.test"
	repo := newAccountRepositoryWithSQL(client, db, nil)
	_, err = repo.UpdateNewAPISyncResult(context.Background(), &service.NewAPISyncWrite{
		AccountID:                 21,
		ExpectedIdentity:          "identity",
		ExpectedAccountBaseURL:    &expectedBaseURL,
		ExpectedAccountAPIKeyHash: hashNewAPIAccountAPIKey("sk-account"),
		AttemptedAt:               time.Now(),
		Status:                    service.NewAPISyncStatusOK,
		Ratio:                     &ratio,
	})
	require.ErrorIs(t, err, service.ErrNewAPISyncIdentityChanged)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateNewAPISyncResultRejectsChangedAccountAPIKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)`+regexp.QuoteMeta("SELECT rate_multiplier")+`.*`+regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(22), service.NewAPISyncIdentityExtraKey).
		WillReturnRows(sqlmock.NewRows([]string{"rate_multiplier", "identity", "base_url", "api_key"}).
			AddRow(0.04, "identity", "", "sk-new")).
		RowsWillBeClosed()
	mock.ExpectRollback()

	ratio := 0.0325
	repo := newAccountRepositoryWithSQL(client, db, nil)
	_, err = repo.UpdateNewAPISyncResult(context.Background(), &service.NewAPISyncWrite{
		AccountID:                 22,
		ExpectedIdentity:          "identity",
		ExpectedAccountAPIKeyHash: hashNewAPIAccountAPIKey("sk-old"),
		AttemptedAt:               time.Now(),
		Status:                    service.NewAPISyncStatusOK,
		Ratio:                     &ratio,
	})
	require.ErrorIs(t, err, service.ErrNewAPISyncIdentityChanged)
	require.NoError(t, mock.ExpectationsWereMet())
}
