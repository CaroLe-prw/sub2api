package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type lotteryRepository struct {
	db *sql.DB
}

type lotteryRowScanner interface {
	Scan(dest ...any) error
}

func NewLotteryRepository(db *sql.DB) service.LotteryRepository {
	return &lotteryRepository{db: db}
}

func (r *lotteryRepository) GetCurrentRound(ctx context.Context) (*service.LotteryRound, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT r.id, r.starts_at, r.ends_at,
		       r.first_prize_name, r.first_prize_reward, r.first_prize_weight,
		       r.second_prize_name, r.second_prize_reward, r.second_prize_weight,
		       r.third_prize_name, r.third_prize_reward, r.third_prize_weight,
		       r.settled_at, r.created_at,
		       (SELECT COUNT(*) FROM lottery_entries e WHERE e.round_id = r.id)
		FROM lottery_rounds r
		WHERE r.settled_at IS NULL AND r.cancelled_at IS NULL
		ORDER BY r.created_at DESC
		LIMIT 1
	`)
	round, err := scanLotteryRound(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load current lottery round: %w", err)
	}
	return &round, nil
}

func (r *lotteryRepository) ConfigureRound(ctx context.Context, input service.LotteryRound) (round service.LotteryRound, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return round, fmt.Errorf("begin lottery configuration: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	existing, scanErr := scanLotteryRound(tx.QueryRowContext(ctx, `
		SELECT r.id, r.starts_at, r.ends_at,
		       r.first_prize_name, r.first_prize_reward, r.first_prize_weight,
		       r.second_prize_name, r.second_prize_reward, r.second_prize_weight,
		       r.third_prize_name, r.third_prize_reward, r.third_prize_weight,
		       r.settled_at, r.created_at,
		       (SELECT COUNT(*) FROM lottery_entries e WHERE e.round_id = r.id)
		FROM lottery_rounds r
		WHERE r.settled_at IS NULL AND r.cancelled_at IS NULL
		ORDER BY r.created_at DESC
		LIMIT 1
		FOR UPDATE
	`))
	first, second, third, prizeErr := lotteryPrizeColumns(input.Prizes)
	if prizeErr != nil {
		return round, prizeErr
	}

	switch {
	case errors.Is(scanErr, sql.ErrNoRows):
		round, err = scanLotteryRound(tx.QueryRowContext(ctx, `
			INSERT INTO lottery_rounds (
				starts_at, ends_at,
				first_prize_name, first_prize_reward, first_prize_weight,
				second_prize_name, second_prize_reward, second_prize_weight,
				third_prize_name, third_prize_reward, third_prize_weight
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id, starts_at, ends_at,
			          first_prize_name, first_prize_reward, first_prize_weight,
			          second_prize_name, second_prize_reward, second_prize_weight,
			          third_prize_name, third_prize_reward, third_prize_weight,
			          settled_at, created_at, 0
		`, input.StartsAt, input.EndsAt,
			first.Name, first.Reward, first.Weight,
			second.Name, second.Reward, second.Weight,
			third.Name, third.Reward, third.Weight))
	case scanErr != nil:
		return round, fmt.Errorf("lock current lottery round: %w", scanErr)
	case existing.ParticipantCount > 0:
		return round, service.ErrLotteryRoundLocked
	default:
		round, err = scanLotteryRound(tx.QueryRowContext(ctx, `
			UPDATE lottery_rounds
			SET starts_at = $1, ends_at = $2,
			    first_prize_name = $3, first_prize_reward = $4, first_prize_weight = $5,
			    second_prize_name = $6, second_prize_reward = $7, second_prize_weight = $8,
			    third_prize_name = $9, third_prize_reward = $10, third_prize_weight = $11,
			    updated_at = NOW()
			WHERE id = $12
			RETURNING id, starts_at, ends_at,
			          first_prize_name, first_prize_reward, first_prize_weight,
			          second_prize_name, second_prize_reward, second_prize_weight,
			          third_prize_name, third_prize_reward, third_prize_weight,
			          settled_at, created_at, 0
		`, input.StartsAt, input.EndsAt,
			first.Name, first.Reward, first.Weight,
			second.Name, second.Reward, second.Weight,
			third.Name, third.Reward, third.Weight, existing.ID))
	}
	if err != nil {
		return round, fmt.Errorf("save lottery round: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return round, fmt.Errorf("commit lottery configuration: %w", err)
	}
	return round, nil
}

func (r *lotteryRepository) Enter(ctx context.Context, userID int64, now time.Time) (round service.LotteryRound, entry service.LotteryEntry, created bool, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return round, entry, false, fmt.Errorf("begin lottery entry: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	round, err = scanLotteryRound(tx.QueryRowContext(ctx, `
		SELECT r.id, r.starts_at, r.ends_at,
		       r.first_prize_name, r.first_prize_reward, r.first_prize_weight,
		       r.second_prize_name, r.second_prize_reward, r.second_prize_weight,
		       r.third_prize_name, r.third_prize_reward, r.third_prize_weight,
		       r.settled_at, r.created_at,
		       (SELECT COUNT(*) FROM lottery_entries e WHERE e.round_id = r.id)
		FROM lottery_rounds r
		WHERE r.settled_at IS NULL AND r.cancelled_at IS NULL
		  AND r.starts_at <= $1 AND r.ends_at > $1
		ORDER BY r.created_at DESC
		LIMIT 1
		FOR UPDATE
	`, now))
	if errors.Is(err, sql.ErrNoRows) {
		return round, entry, false, service.ErrLotteryNotOpen
	}
	if err != nil {
		return round, entry, false, fmt.Errorf("load open lottery round: %w", err)
	}

	entry, err = scanLotteryEntry(tx.QueryRowContext(ctx, `
		INSERT INTO lottery_entries (round_id, user_id, entered_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (round_id, user_id) DO NOTHING
		RETURNING id, round_id, entered_at, $4::timestamptz, $5::timestamptz,
		          prize_tier, COALESCE(prize_name, ''), COALESCE(reward_amount, 0), balance_after, settled_at,
		          NULL::timestamptz
	`, round.ID, userID, now, round.StartsAt, round.EndsAt))
	if errors.Is(err, sql.ErrNoRows) {
		created = false
		entry, err = scanLotteryEntry(tx.QueryRowContext(ctx, `
			SELECT e.id, e.round_id, e.entered_at, r.starts_at, r.ends_at,
			       e.prize_tier, COALESCE(e.prize_name, ''), COALESCE(e.reward_amount, 0), e.balance_after, e.settled_at,
			       r.cancelled_at
			FROM lottery_entries e
			JOIN lottery_rounds r ON r.id = e.round_id
			WHERE e.round_id = $1 AND e.user_id = $2
		`, round.ID, userID))
	} else {
		created = err == nil
	}
	if err != nil {
		return round, entry, false, fmt.Errorf("save lottery entry: %w", err)
	}
	if created {
		round.ParticipantCount++
	}
	if err = tx.Commit(); err != nil {
		return round, entry, false, fmt.Errorf("commit lottery entry: %w", err)
	}
	return round, entry, created, nil
}

func (r *lotteryRepository) ListUserEntries(ctx context.Context, userID int64, limit int) ([]service.LotteryEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.id, e.round_id, e.entered_at, r.starts_at, r.ends_at,
		       e.prize_tier, COALESCE(e.prize_name, ''), COALESCE(e.reward_amount, 0), e.balance_after, e.settled_at,
		       r.cancelled_at
		FROM lottery_entries e
		JOIN lottery_rounds r ON r.id = e.round_id
		WHERE e.user_id = $1
		ORDER BY e.entered_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list user lottery entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	entries := make([]service.LotteryEntry, 0)
	for rows.Next() {
		entry, err := scanLotteryEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user lottery entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (r *lotteryRepository) AdminListResults(ctx context.Context, page, pageSize int) ([]service.LotteryAdminResult, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM lottery_entries WHERE prize_tier IS NOT NULL`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin lottery results: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.id, e.round_id, e.user_id, COALESCE(u.email, ''), COALESCE(u.username, ''),
		       e.entered_at, e.settled_at, e.prize_tier, COALESCE(e.prize_name, ''),
		       COALESCE(e.reward_amount, 0), COALESCE(e.balance_after, 0)
		FROM lottery_entries e
		LEFT JOIN users u ON u.id = e.user_id
		WHERE e.prize_tier IS NOT NULL
		ORDER BY e.settled_at DESC, e.round_id DESC, e.prize_tier ASC, e.id ASC
		LIMIT $1 OFFSET $2
	`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list admin lottery results: %w", err)
	}
	defer func() { _ = rows.Close() }()
	results := make([]service.LotteryAdminResult, 0)
	for rows.Next() {
		var result service.LotteryAdminResult
		if err := rows.Scan(
			&result.EntryID, &result.RoundID, &result.UserID, &result.Email, &result.Username,
			&result.EnteredAt, &result.SettledAt, &result.PrizeTier, &result.PrizeName,
			&result.Reward, &result.BalanceAfter,
		); err != nil {
			return nil, 0, fmt.Errorf("scan admin lottery result: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

func (r *lotteryRepository) FindDueRoundIDs(ctx context.Context, now time.Time, limit int) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id
		FROM lottery_rounds
		WHERE settled_at IS NULL AND cancelled_at IS NULL AND ends_at <= $1
		ORDER BY ends_at ASC
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list due lottery rounds: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *lotteryRepository) CancelCurrentRound(ctx context.Context, now time.Time) (bool, error) {
	var roundID int64
	err := r.db.QueryRowContext(ctx, `
		UPDATE lottery_rounds
		SET cancelled_at = $1, updated_at = NOW()
		WHERE settled_at IS NULL AND cancelled_at IS NULL AND ends_at > $1
		RETURNING id
	`, now).Scan(&roundID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cancel current lottery round: %w", err)
	}
	return true, nil
}

func (r *lotteryRepository) SettleRound(ctx context.Context, roundID int64, now time.Time, picker service.LotteryPrizePicker) (result service.LotterySettlementSummary, err error) {
	result.RoundID = roundID
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin lottery settlement: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	round, err := scanLotteryRound(tx.QueryRowContext(ctx, `
		SELECT r.id, r.starts_at, r.ends_at,
		       r.first_prize_name, r.first_prize_reward, r.first_prize_weight,
		       r.second_prize_name, r.second_prize_reward, r.second_prize_weight,
		       r.third_prize_name, r.third_prize_reward, r.third_prize_weight,
		       r.settled_at, r.created_at,
		       (SELECT COUNT(*) FROM lottery_entries e WHERE e.round_id = r.id)
		FROM lottery_rounds r
		WHERE r.id = $1 AND r.settled_at IS NULL AND r.cancelled_at IS NULL
		FOR UPDATE
	`, roundID))
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("lock lottery round for settlement: %w", err)
	}
	if now.Before(round.EndsAt) {
		_ = tx.Rollback()
		return result, nil
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, user_id
		FROM lottery_entries
		WHERE round_id = $1 AND prize_tier IS NULL
		ORDER BY id ASC
		FOR UPDATE
	`, roundID)
	if err != nil {
		return result, fmt.Errorf("lock lottery entries: %w", err)
	}
	type unsettledEntry struct{ id, userID int64 }
	entries := make([]unsettledEntry, 0, round.ParticipantCount)
	for rows.Next() {
		var entry unsettledEntry
		if err := rows.Scan(&entry.id, &entry.userID); err != nil {
			_ = rows.Close()
			return result, fmt.Errorf("scan unsettled lottery entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Close(); err != nil {
		return result, fmt.Errorf("close unsettled lottery entries: %w", err)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	for _, entry := range entries {
		prize, pickErr := picker(round.Prizes)
		if pickErr != nil {
			return result, fmt.Errorf("pick lottery prize: %w", pickErr)
		}
		var balance float64
		if err := tx.QueryRowContext(ctx, `
			UPDATE users
			SET balance = balance + $1, updated_at = NOW()
			WHERE id = $2
			RETURNING balance
		`, prize.Reward, entry.userID).Scan(&balance); err != nil {
			return result, fmt.Errorf("credit lottery reward: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE lottery_entries
			SET prize_tier = $1, prize_name = $2, reward_amount = $3,
			    balance_after = $4, settled_at = $5
			WHERE id = $6
		`, prize.Tier, prize.Name, prize.Reward, balance, now, entry.id); err != nil {
			return result, fmt.Errorf("save lottery result: %w", err)
		}
		result.Entrants++
		result.TotalAward += prize.Reward
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE lottery_rounds
		SET settled_at = $1, updated_at = NOW()
		WHERE id = $2
	`, now, roundID); err != nil {
		return result, fmt.Errorf("mark lottery round settled: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return result, fmt.Errorf("commit lottery settlement: %w", err)
	}
	result.Settled = true
	return result, nil
}

func scanLotteryRound(scanner lotteryRowScanner) (service.LotteryRound, error) {
	var round service.LotteryRound
	var first, second, third service.LotteryPrize
	var settledAt sql.NullTime
	first.Tier, second.Tier, third.Tier = 1, 2, 3
	err := scanner.Scan(
		&round.ID, &round.StartsAt, &round.EndsAt,
		&first.Name, &first.Reward, &first.Weight,
		&second.Name, &second.Reward, &second.Weight,
		&third.Name, &third.Reward, &third.Weight,
		&settledAt, &round.CreatedAt, &round.ParticipantCount,
	)
	if err != nil {
		return round, err
	}
	if settledAt.Valid {
		round.SettledAt = &settledAt.Time
	}
	round.Prizes = []service.LotteryPrize{first, second, third}
	return round, nil
}

func scanLotteryEntry(scanner lotteryRowScanner) (service.LotteryEntry, error) {
	var entry service.LotteryEntry
	var tier sql.NullInt64
	var balance sql.NullFloat64
	var settledAt sql.NullTime
	var cancelledAt sql.NullTime
	if err := scanner.Scan(
		&entry.ID, &entry.RoundID, &entry.EnteredAt, &entry.RoundStartsAt, &entry.RoundEndsAt,
		&tier, &entry.PrizeName, &entry.Reward, &balance, &settledAt, &cancelledAt,
	); err != nil {
		return entry, err
	}
	if tier.Valid {
		value := int(tier.Int64)
		entry.PrizeTier = &value
	}
	if balance.Valid {
		entry.BalanceAfter = &balance.Float64
	}
	if settledAt.Valid {
		entry.SettledAt = &settledAt.Time
	}
	if cancelledAt.Valid {
		entry.CancelledAt = &cancelledAt.Time
	}
	return entry, nil
}

func lotteryPrizeColumns(prizes []service.LotteryPrize) (service.LotteryPrize, service.LotteryPrize, service.LotteryPrize, error) {
	var first, second, third service.LotteryPrize
	for _, prize := range prizes {
		switch prize.Tier {
		case 1:
			first = prize
		case 2:
			second = prize
		case 3:
			third = prize
		}
	}
	if first.Tier == 0 || second.Tier == 0 || third.Tier == 0 {
		return first, second, third, service.ErrLotteryInvalid
	}
	return first, second, third, nil
}
