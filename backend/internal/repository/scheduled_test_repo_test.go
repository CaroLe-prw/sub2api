package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestScheduledTestResultRepositoryListByPlanIDReturnsEmptyJSONArray(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewScheduledTestResultRepository(db)
	mock.ExpectQuery("FROM scheduled_test_results").
		WithArgs(int64(365), 100).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "plan_id", "status", "response_text", "error_message",
			"ttft_ms", "latency_ms", "started_at", "finished_at", "created_at",
		}))

	results, err := repo.ListByPlanID(context.Background(), 365, 100)
	require.NoError(t, err)
	require.NotNil(t, results)
	require.Empty(t, results)

	payload, err := json.Marshal(results)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(payload))
	require.NoError(t, mock.ExpectationsWereMet())
}
