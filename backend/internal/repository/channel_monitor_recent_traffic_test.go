package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryGetLatestSuccessfulRealTrafficAt(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := newUsageLogRepositoryWithSQL(nil, db)
	want := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT MAX(created_at)")).
		WithArgs(int64(42), int16(service.RequestTypeCyberBlocked), int16(service.RequestTypeProbe)).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(want))

	got, err := repo.GetLatestSuccessfulRealTrafficAt(context.Background(), 42)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, want, *got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetLatestSuccessfulRealTrafficAtReturnsNilWithoutTraffic(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := newUsageLogRepositoryWithSQL(nil, db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT MAX(created_at)")).
		WithArgs(int64(42), int16(service.RequestTypeCyberBlocked), int16(service.RequestTypeProbe)).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(nil))

	got, err := repo.GetLatestSuccessfulRealTrafficAt(context.Background(), 42)
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}
