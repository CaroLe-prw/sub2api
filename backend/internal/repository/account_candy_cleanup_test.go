package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestDeleteAccountCandyCleanupIsAtomic(t *testing.T) {
	for _, fail := range []string{"", "history", "logs"} {
		t.Run("failure_"+fail, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := newAccountRepositoryWithSQL(client, db, nil)
			mock.ExpectQuery(`SELECT .* FROM "account_groups" WHERE .*account_id.*`).WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"id", "account_id", "group_id"}))
			mock.ExpectBegin()
			mock.ExpectExec(`DELETE FROM "account_groups" WHERE .*account_id.*`).WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(regexp.QuoteMeta("DELETE FROM scheduled_test_plans WHERE account_id = $1")).WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(`UPDATE "accounts" SET .*deleted_at.* WHERE .*id.*`).WillReturnResult(sqlmock.NewResult(0, 1))
			history := mock.ExpectExec(regexp.QuoteMeta(deleteAccountCandyExtraSQL)).WithArgs(int64(42))
			if fail == "history" {
				history.WillReturnError(errors.New("history unavailable"))
			} else {
				history.WillReturnResult(sqlmock.NewResult(0, 1))
				logs := mock.ExpectExec(regexp.QuoteMeta(deleteAccountTestLogsSQL)).WithArgs(int64(42))
				if fail == "logs" {
					logs.WillReturnError(errors.New("logs unavailable"))
				} else {
					logs.WillReturnResult(sqlmock.NewResult(0, 2))
				}
			}
			if fail != "" {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
				mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnResult(sqlmock.NewResult(1, 1))
			}
			err = repo.Delete(context.Background(), 42)
			if fail != "" {
				require.ErrorContains(t, err, "clear deleted account")
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestDeletedAccountCandyHistoryCannotBeRecreated(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE accounts SET extra = .* WHERE id = \$2 AND deleted_at IS NULL`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	err = repo.UpdateExtra(context.Background(), 42, map[string]any{service.CandyTestExtraKey: service.CandyTestSnapshot{}, service.CandyTestBlockedExtraKey: true})
	require.ErrorIs(t, err, service.ErrAccountNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBatchInsertAccountTestLogsSkipsDeletedButKeepsBusinessLogs(t *testing.T) {
	for _, onlyBusiness := range []bool{false, true} {
		t.Run(map[bool]string{false: "mixed", true: "business_only"}[onlyBusiness], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := NewOpsRepository(db)
			active, deleted := int64(41), int64(42)
			at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
			business := &service.OpsInsertSystemLogInput{CreatedAt: at, Level: "warn", Component: "openai.gateway", Message: "upstream.failed", AccountID: &deleted}
			audit := &service.OpsInsertSystemLogInput{CreatedAt: at, Level: "info", Component: "audit", Message: "account.deleted", AccountID: &deleted}
			accepted := []*service.OpsInsertSystemLogInput{business, audit}
			inputs := append([]*service.OpsInsertSystemLogInput{nil}, accepted...)
			mock.ExpectBegin()
			if !onlyBusiness {
				liveTest := &service.OpsInsertSystemLogInput{CreatedAt: at, Level: "warn", Component: "service.account_test", Message: "account_test.failed", AccountID: &active}
				deadTest := &service.OpsInsertSystemLogInput{CreatedAt: at, Level: "warn", Component: "service.account_test", Message: "account_test.failed", AccountID: &deleted}
				inputs = append(inputs, deadTest, liveTest, deadTest)
				accepted = append(accepted, liveTest)
				mock.ExpectQuery(regexp.QuoteMeta(lockActiveAccountTestLogAccountsSQL)).WithArgs("{41,42}").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(active)).RowsWillBeClosed()
			}
			copySQL := pq.CopyIn("ops_system_logs", "created_at", "host", "level", "component", "message", "request_id", "client_request_id", "user_id", "api_key_id", "account_id", "platform", "model", "extra")
			stmt := mock.ExpectPrepare(regexp.QuoteMeta(copySQL)).WillBeClosed()
			for _, row := range accepted {
				stmt.ExpectExec().WithArgs(at, nil, row.Level, row.Component, row.Message, nil, nil, nil, nil, *row.AccountID, nil, nil, "{}").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			stmt.ExpectExec().WithArgs().WillReturnResult(sqlmock.NewResult(0, int64(len(accepted))))
			mock.ExpectCommit()
			inserted, err := repo.BatchInsertSystemLogs(context.Background(), inputs)
			require.NoError(t, err)
			require.Equal(t, int64(len(accepted)), inserted)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAccountTestLogGuardRollsBackOnLookupFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewOpsRepository(db)
	id := int64(42)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(lockActiveAccountTestLogAccountsSQL)).WithArgs("{42}").WillReturnError(errors.New("lookup unavailable"))
	mock.ExpectRollback()
	inserted, err := repo.BatchInsertSystemLogs(context.Background(), []*service.OpsInsertSystemLogInput{{Level: "warn", Component: "service.account_test", Message: "account_test.failed", AccountID: &id}})
	require.ErrorContains(t, err, "lookup unavailable")
	require.Zero(t, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountTestLogCleanupMatchesOnlyScopedFailures(t *testing.T) {
	id := int64(42)
	for _, tc := range []struct {
		component, message string
		accountID          *int64
		match              bool
	}{
		{"service.account_test", "account_test.failed", &id, true},
		{" service.account_test ", " account_test.failed ", &id, true},
		{"service.account_test", "account_test.failed", nil, false},
		{"audit", "account_test.failed", &id, false},
		{"service.account_test", "different.event", &id, false},
	} {
		require.Equal(t, tc.match, isAccountTestFailureLog(&service.OpsInsertSystemLogInput{Component: tc.component, Message: tc.message, AccountID: tc.accountID}))
	}
	require.False(t, isAccountTestFailureLog(nil))
}
