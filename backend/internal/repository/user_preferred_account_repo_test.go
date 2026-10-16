package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserPreferredAccountRepositorySetValidatesMembership(t *testing.T) {
	for _, affected := range []int64{0, 1} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		repo := NewUserPreferredAccountRepository(db)
		mock.ExpectExec(`INSERT INTO user_group_preferred_accounts .*JOIN account_groups .*ON CONFLICT \(user_id, group_id\) DO UPDATE`).WithArgs(int64(7), int64(10), int64(1)).WillReturnResult(sqlmock.NewResult(0, affected))
		err = repo.Set(context.Background(), 7, 10, 1)
		if affected == 0 {
			require.ErrorIs(t, err, service.ErrInvalidPreferredAccount)
		} else {
			require.NoError(t, err)
		}
		require.NoError(t, mock.ExpectationsWereMet())
		mock.ExpectClose()
		require.NoError(t, db.Close())
	}
}

func TestUserPreferredAccountRepositoryLookupMissingIsOptional(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(`SELECT p.account_id .*JOIN account_groups .*a.deleted_at IS NULL`).WithArgs(int64(7), int64(10)).WillReturnRows(sqlmock.NewRows([]string{"account_id"}))
	id, err := NewUserPreferredAccountRepository(db).Get(context.Background(), 7, 10)
	require.NoError(t, err)
	require.Zero(t, id)
	require.NoError(t, mock.ExpectationsWereMet())
}
