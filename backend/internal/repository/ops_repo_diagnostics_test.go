package repository

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOpsRequestDiagnosticsReadFromDetail(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
	})
	snapshot := `{"client_request":{"body":"{}","bytes":2},"upstream_attempts":[]}`
	values := []driver.Value{
		int64(1), time.Now(), "request", "api_error", "client", "client_request", "error", 400, "openai", "test", false, nil, nil, "cid", "rid", "invalid content", "{}", 400, "", "", "[]", false, nil, "", nil, nil, "", nil, "", nil, "/v1/responses", false, "", "", "", "", nil, "", nil, nil, nil, nil, nil, "", "", nil, snapshot,
	}
	columns := make([]string, len(values))
	for i := range columns {
		columns[i] = fmt.Sprintf("column_%d", i)
	}
	mock.ExpectQuery(`COALESCE\(e.request_diagnostics::text, ''\)`).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))
	got, err := NewOpsRepository(db).GetErrorLogByID(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, snapshot, got.RequestDiagnostics)
	require.NoError(t, mock.ExpectationsWereMet())
}
