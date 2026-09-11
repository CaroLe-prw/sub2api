//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/responsediag"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageResponseDiagnosticsInsertAndDetail(t *testing.T) {
	ctx, capture := responsediag.Start(context.Background())
	capture.WriteDownstream([]byte(`{}tail`), "application/json", false)
	value := responsediag.Snapshot(ctx)
	log := &service.UsageLog{UserID: 1, APIKeyID: 2, AccountID: 3, Model: "test", CreatedAt: time.Now()}
	var queries []string
	db, mock := newSQLCapturingMock(t, &queries)
	repo := &usageLogRepository{sql: db}
	expected := *log
	expected.ResponseDiagnostics = value
	prepared := prepareUsageLogInsert(&expected)
	mock.ExpectQuery("INSERT").WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(42), log.CreatedAt))
	inserted, err := repo.Create(ctx, log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.JSONEq(t, string(value), string(log.ResponseDiagnostics))
	requireStaticInsertMatchesArgTypes(t, queries[0])

	columns := strings.Split(usageLogSelectColumns, ", ")
	args := append([]any{int64(42)}, prepared.args...)
	mock.ExpectQuery("SELECT").WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows(columns).AddRow(anySliceToDriverValues(args)...))
	got, err := repo.GetByID(context.Background(), 42)
	require.NoError(t, err)
	require.JSONEq(t, string(value), string(got.ResponseDiagnostics))
	require.Contains(t, usageLogSelectColumns, "response_diagnostics->'summary' AS response_diagnostics")
	require.NotContains(t, queries[1], "->'summary'", "detail must load full captures")
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestUsageResponseDiagnosticsBatchAndBestEffortJSON(t *testing.T) {
	value := json.RawMessage(`{"summary":{"upstream":"extra","downstream":"ok"}}`)
	prepared := prepareUsageLogInsert(&service.UsageLog{RequestID: "test", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "test", ResponseDiagnostics: value})
	key := usageLogBatchKey("test", 2)
	query, args := buildUsageLogBatchInsertQuery([]string{key}, map[string]usageLogInsertPrepared{key: prepared})
	require.Contains(t, query, "response_diagnostics")
	require.Contains(t, args, string(value))
	query, args = buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
	require.Contains(t, query, "response_diagnostics")
	require.Contains(t, args, string(value))
}
