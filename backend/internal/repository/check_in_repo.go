package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type checkInRepository struct {
	db *sql.DB
}

func NewCheckInRepository(db *sql.DB) service.CheckInRepository {
	return &checkInRepository{db: db}
}

func (r *checkInRepository) Claim(ctx context.Context, userID int64, businessDate time.Time, reward float64) (record service.CheckInRecord, balance float64, created bool, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return record, 0, false, fmt.Errorf("begin check-in transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const insertQuery = `
		INSERT INTO user_check_ins (user_id, business_date, reward_amount)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, business_date) DO NOTHING
		RETURNING business_date::text, reward_amount, created_at
	`
	err = tx.QueryRowContext(ctx, insertQuery, userID, businessDate.Format("2006-01-02"), reward).
		Scan(&record.Date, &record.Reward, &record.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		created = false
		err = tx.QueryRowContext(ctx, `
			SELECT c.business_date::text, c.reward_amount, c.created_at, u.balance
			FROM user_check_ins c
			JOIN users u ON u.id = c.user_id AND u.deleted_at IS NULL
			WHERE c.user_id = $1 AND c.business_date = $2
		`, userID, businessDate.Format("2006-01-02")).Scan(&record.Date, &record.Reward, &record.CreatedAt, &balance)
		if err != nil {
			return record, 0, false, fmt.Errorf("load existing check-in: %w", err)
		}
	} else if err != nil {
		return record, 0, false, fmt.Errorf("create check-in: %w", err)
	} else {
		created = true
		err = tx.QueryRowContext(ctx, `
			UPDATE users
			SET balance = balance + $1, updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
			RETURNING balance
		`, reward, userID).Scan(&balance)
		if errors.Is(err, sql.ErrNoRows) {
			return record, 0, false, service.ErrUserNotFound
		}
		if err != nil {
			return record, 0, false, fmt.Errorf("credit check-in reward: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return record, 0, false, fmt.Errorf("commit check-in transaction: %w", err)
	}
	return record, balance, created, nil
}

func (r *checkInRepository) Overview(ctx context.Context, userID int64, monthStart, monthEnd, today time.Time) (service.CheckInRepositoryOverview, error) {
	result := service.CheckInRepositoryOverview{MonthRecords: []service.CheckInRecord{}, AllDates: []string{}}
	rows, err := r.db.QueryContext(ctx, `
		SELECT business_date::text, reward_amount, created_at
		FROM user_check_ins
		WHERE user_id = $1 AND business_date >= $2 AND business_date < $3
		ORDER BY business_date ASC
	`, userID, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"))
	if err != nil {
		return result, fmt.Errorf("list monthly check-ins: %w", err)
	}
	for rows.Next() {
		var record service.CheckInRecord
		if err := rows.Scan(&record.Date, &record.Reward, &record.CreatedAt); err != nil {
			_ = rows.Close()
			return result, fmt.Errorf("scan monthly check-in: %w", err)
		}
		result.MonthRecords = append(result.MonthRecords, record)
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	var todayRecord service.CheckInRecord
	err = r.db.QueryRowContext(ctx, `
		SELECT business_date::text, reward_amount, created_at
		FROM user_check_ins
		WHERE user_id = $1 AND business_date = $2
	`, userID, today.Format("2006-01-02")).Scan(&todayRecord.Date, &todayRecord.Reward, &todayRecord.CreatedAt)
	if err == nil {
		result.TodayRecord = &todayRecord
	} else if !errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("load today's check-in: %w", err)
	}

	err = r.db.QueryRowContext(ctx, `
		SELECT u.balance, COUNT(c.id), COALESCE(SUM(c.reward_amount), 0)
		FROM users u
		LEFT JOIN user_check_ins c ON c.user_id = u.id
		WHERE u.id = $1 AND u.deleted_at IS NULL
		GROUP BY u.id, u.balance
	`, userID).Scan(&result.Balance, &result.TotalDays, &result.TotalReward)
	if errors.Is(err, sql.ErrNoRows) {
		return result, service.ErrUserNotFound
	}
	if err != nil {
		return result, fmt.Errorf("load check-in totals: %w", err)
	}

	dateRows, err := r.db.QueryContext(ctx, `
		SELECT business_date::text
		FROM user_check_ins
		WHERE user_id = $1
		ORDER BY business_date DESC
	`, userID)
	if err != nil {
		return result, fmt.Errorf("list check-in dates: %w", err)
	}
	defer dateRows.Close()
	for dateRows.Next() {
		var date string
		if err := dateRows.Scan(&date); err != nil {
			return result, fmt.Errorf("scan check-in date: %w", err)
		}
		result.AllDates = append(result.AllDates, date)
	}
	if err := dateRows.Err(); err != nil {
		return result, err
	}
	return result, nil
}
