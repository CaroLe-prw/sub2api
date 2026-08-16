package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// GetLatestSuccessfulRealTrafficAt returns the latest user-owned successful
// request for an upstream account. Usage rows are written only after a request
// reaches the billable success path; probe and cyber-policy rows are excluded.
func (r *usageLogRepository) GetLatestSuccessfulRealTrafficAt(ctx context.Context, accountID int64) (*time.Time, error) {
	var latest sql.NullTime
	err := scanSingleRow(ctx, r.sql, `
		SELECT MAX(created_at)
		FROM usage_logs
		WHERE account_id = $1
		  AND user_id IS NOT NULL
		  AND request_type NOT IN ($2, $3)
	`, []any{accountID, int16(service.RequestTypeCyberBlocked), int16(service.RequestTypeProbe)}, &latest)
	if err != nil {
		return nil, err
	}
	if !latest.Valid {
		return nil, nil
	}
	value := latest.Time
	return &value, nil
}
