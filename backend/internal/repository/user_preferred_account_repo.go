package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type userPreferredAccountRepository struct{ sql *sql.DB }

func NewUserPreferredAccountRepository(db *sql.DB) service.UserPreferredAccountRepository {
	return &userPreferredAccountRepository{sql: db}
}

func (r *userPreferredAccountRepository) Get(ctx context.Context, userID, groupID int64) (int64, error) {
	var id int64
	err := r.sql.QueryRowContext(ctx, `SELECT p.account_id FROM user_group_preferred_accounts p
		JOIN account_groups ag ON ag.account_id = p.account_id AND ag.group_id = p.group_id
		JOIN accounts a ON a.id = p.account_id AND a.deleted_at IS NULL
		WHERE p.user_id = $1 AND p.group_id = $2`, userID, groupID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (r *userPreferredAccountRepository) List(ctx context.Context, groupID int64) ([]service.UserPreferredAccountEntry, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT p.user_id, COALESCE(u.username, ''), u.email,
		p.account_id, a.name, a.status,
		EXISTS (SELECT 1 FROM account_groups ag WHERE ag.account_id = p.account_id AND ag.group_id = p.group_id)
		FROM user_group_preferred_accounts p
		JOIN users u ON u.id = p.user_id AND u.deleted_at IS NULL
		JOIN accounts a ON a.id = p.account_id AND a.deleted_at IS NULL
		WHERE p.group_id = $1 ORDER BY p.user_id`, groupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	entries := make([]service.UserPreferredAccountEntry, 0)
	for rows.Next() {
		var e service.UserPreferredAccountEntry
		if err := rows.Scan(&e.UserID, &e.UserName, &e.UserEmail, &e.AccountID, &e.AccountName, &e.AccountStatus, &e.InGroup); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *userPreferredAccountRepository) Set(ctx context.Context, userID, groupID, accountID int64) error {
	result, err := r.sql.ExecContext(ctx, `INSERT INTO user_group_preferred_accounts (user_id, group_id, account_id)
		SELECT u.id, g.id, a.id FROM users u
		JOIN groups g ON g.id = $2 AND g.deleted_at IS NULL
		JOIN account_groups ag ON ag.group_id = g.id AND ag.account_id = $3
		JOIN accounts a ON a.id = ag.account_id AND a.deleted_at IS NULL
		WHERE u.id = $1 AND u.deleted_at IS NULL
		ON CONFLICT (user_id, group_id) DO UPDATE SET account_id = EXCLUDED.account_id, updated_at = NOW()`, userID, groupID, accountID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrInvalidPreferredAccount
	}
	return nil
}

func (r *userPreferredAccountRepository) Delete(ctx context.Context, userID, groupID int64) error {
	_, err := r.sql.ExecContext(ctx, `DELETE FROM user_group_preferred_accounts WHERE user_id = $1 AND group_id = $2`, userID, groupID)
	return err
}
