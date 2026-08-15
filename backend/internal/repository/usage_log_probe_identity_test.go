//go:build unit

package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPrepareUsageLogInsertNullsSystemIdentityOnlyForProbe(t *testing.T) {
	probe := prepareUsageLogInsert(&service.UsageLog{
		AccountID:   42,
		Model:       "claude-sonnet-4-20250514",
		RequestType: service.RequestTypeProbe,
		CreatedAt:   time.Now(),
	})
	require.Nil(t, probe.args[0])
	require.Nil(t, probe.args[1])

	ordinary := prepareUsageLogInsert(&service.UsageLog{
		AccountID:   42,
		Model:       "claude-sonnet-4-20250514",
		RequestType: service.RequestTypeStream,
		CreatedAt:   time.Now(),
	})
	require.Equal(t, int64(0), ordinary.args[0])
	require.Equal(t, int64(0), ordinary.args[1])
}
