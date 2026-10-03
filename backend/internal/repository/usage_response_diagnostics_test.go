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
	value := responsediag.StorageSnapshot(ctx)
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
	require.NotContains(t, queries[1], "->'summary'", "detail must load all diagnostic metadata")
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestUsageResponseDiagnosticsBatchAndBestEffortJSON(t *testing.T) {
	value := json.RawMessage(`{"summary":{"upstream":"extra","downstream":"ok"}}`)
	prepared := prepareUsageLogInsert(&service.UsageLog{RequestID: "test", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "test", ResponseDiagnostics: value})
	key := usageLogBatchKey("test", 2)
	query, args := buildUsageLogBatchInsertQuery([]string{key}, map[string]usageLogInsertPrepared{key: prepared})
	require.Contains(t, query, "response_diagnostics")
	require.Contains(t, args, string(responsediag.ForStorage(value)))
	query, args = buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
	require.Contains(t, query, "response_diagnostics")
	require.Contains(t, args, string(responsediag.ForStorage(value)))
}

func TestUsageDiagnosticsNeverPersistBodies(t *testing.T) {
	value := json.RawMessage(`{"summary":{"upstream":"extra_content","downstream":"ok"},"incoming_request":{"body":"private-input"},"upstream_request":{"body":"private-forwarded"},"upstream":{"body":"private-response","status":"extra_content","bytes":9000,"complete":true,"json_documents":1,"issues":[{"kind":"trailing_content","frame":1,"json":"private-json","extra":"private-tail"}]},"downstream":{"body":"private-output","status":"ok","bytes":100,"complete":true,"json_documents":1}}`)
	prepared := prepareUsageLogInsert(&service.UsageLog{RequestID: "test", UserID: 1, APIKeyID: 2, AccountID: 3, Model: "test", ResponseDiagnostics: value})
	key := usageLogBatchKey("test", 2)
	_, batch := buildUsageLogBatchInsertQuery([]string{key}, map[string]usageLogInsertPrepared{key: prepared})
	_, bestEffort := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
	for _, args := range [][]any{prepared.args, batch, bestEffort} {
		found := false
		for _, arg := range args {
			raw, ok := arg.(string)
			if !ok || !strings.Contains(raw, `"summary"`) {
				continue
			}
			found = true
			require.NotContains(t, raw, "private-")
			require.NotContains(t, raw, `"body"`)
			require.NotContains(t, raw, `"incoming_request"`)
			require.NotContains(t, raw, `"upstream_request"`)
			require.Contains(t, raw, `"trailing_content"`)
			require.Contains(t, raw, `"bodies_omitted":true`)
		}
		require.True(t, found)
	}
}
