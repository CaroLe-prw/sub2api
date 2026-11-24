package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCandyRetentionCompareAndSetPreservesConcurrentWrites(t *testing.T) {
	before := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	old := `{"total":1,"passed":1,"recent":[{"status":"passed","checked_at":"2026-11-22T00:00:00Z","response_text":"expired"}]}`
	fresh := `{"total":1,"passed":1,"recent":[{"status":"passed","checked_at":"2026-11-23T00:00:00Z"}]}`
	for _, affected := range []int64{0, 1} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		repo := &accountRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta(listCandyTestHistorySQL)).WithArgs(int64(10), 100).
			WillReturnRows(sqlmock.NewRows([]string{"id", "candy_test"}).AddRow(11, old).AddRow(12, fresh).AddRow(13, `{"total":5}`)).RowsWillBeClosed()
		trimmed, err := service.PruneCandyTestHistory([]byte(old), before)
		require.NoError(t, err)
		mock.ExpectExec(regexp.QuoteMeta(updateCandyTestHistorySQL)).WithArgs(string(trimmed), int64(11), old).
			WillReturnResult(sqlmock.NewResult(0, affected))
		last, cleaned, err := repo.CleanupCandyTestHistory(context.Background(), before, 10, 100)
		require.NoError(t, err)
		require.Equal(t, int64(13), last, "advance over unchanged and invalid snapshots")
		require.Equal(t, affected, cleaned)
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestCandyRetentionEmptyBatchAndErrors(t *testing.T) {
	for _, scenario := range []string{"empty", "query", "rows", "update"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &accountRepository{sql: db}
			expected := mock.ExpectQuery(regexp.QuoteMeta(listCandyTestHistorySQL)).WithArgs(int64(7), 10)
			rows := sqlmock.NewRows([]string{"id", "candy_test"})
			switch scenario {
			case "query":
				expected.WillReturnError(errors.New("read failed"))
			case "rows":
				expected.WillReturnRows(rows.AddRow(8, `{}`).RowError(0, errors.New("row failed")))
			case "update":
				expected.WillReturnRows(rows.AddRow(8, `{"total":1,"passed":1,"recent":[{"status":"passed"}]}`)).RowsWillBeClosed()
				mock.ExpectExec(regexp.QuoteMeta(updateCandyTestHistorySQL)).WillReturnError(errors.New("write failed"))
			default:
				expected.WillReturnRows(rows).RowsWillBeClosed()
			}
			last, count, err := repo.CleanupCandyTestHistory(context.Background(), time.Now(), 7, 10)
			if scenario == "empty" {
				require.NoError(t, err)
				require.Equal(t, int64(7), last)
			} else {
				require.Error(t, err)
			}
			require.Zero(t, count)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCandyRetentionLogBatchAndValidation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountRepository{sql: db}
	cutoff := time.Now()
	mock.ExpectExec(regexp.QuoteMeta(cleanupAccountTestLogsSQL)).WithArgs(cutoff, 100).WillReturnResult(sqlmock.NewResult(0, 100))
	count, err := repo.CleanupAccountTestLogs(context.Background(), cutoff, 100)
	require.NoError(t, err)
	require.Equal(t, int64(100), count)
	mock.ExpectExec(regexp.QuoteMeta(cleanupAccountTestLogsSQL)).WillReturnError(errors.New("unavailable"))
	_, err = repo.CleanupAccountTestLogs(context.Background(), cutoff, 100)
	require.Error(t, err)
	for _, limit := range []int{0, -1} {
		_, _, err := repo.CleanupCandyTestHistory(context.Background(), cutoff, 0, limit)
		require.Error(t, err)
		_, err = repo.CleanupAccountTestLogs(context.Background(), cutoff, limit)
		require.Error(t, err)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}
