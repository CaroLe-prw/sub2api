//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Execute the complete statement against PostgreSQL so malformed SQL in any
// branch fails before it can stall the production aggregation watermark.
func TestChannelMonitorV2ErrorAggregationSQLExecutes(t *testing.T) {
	tx := testTx(t)
	end := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	_, err := tx.ExecContext(
		context.Background(),
		channelMonitorV2ErrorAggregationSQL,
		end.Add(-10*time.Minute),
		end,
	)
	require.NoError(t, err)
}
