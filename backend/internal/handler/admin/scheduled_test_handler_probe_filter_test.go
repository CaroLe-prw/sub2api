package admin

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSchedulerProbeAccountIDs(t *testing.T) {
	t.Run("empty means all accounts", func(t *testing.T) {
		accountIDs, err := parseSchedulerProbeAccountIDs("  ")
		require.NoError(t, err)
		require.Nil(t, accountIDs)
	})

	t.Run("deduplicates while preserving order", func(t *testing.T) {
		accountIDs, err := parseSchedulerProbeAccountIDs("35, 31,35")
		require.NoError(t, err)
		require.Equal(t, []int64{35, 31}, accountIDs)
	})

	for _, input := range []string{"0", "-1", "abc", "1,"} {
		t.Run("rejects "+input, func(t *testing.T) {
			_, err := parseSchedulerProbeAccountIDs(input)
			require.Error(t, err)
		})
	}

	t.Run("rejects more than 200 accounts", func(t *testing.T) {
		parts := make([]string, 201)
		for index := range parts {
			parts[index] = strconv.Itoa(index + 1)
		}
		_, err := parseSchedulerProbeAccountIDs(strings.Join(parts, ","))
		require.Error(t, err)
	})
}
