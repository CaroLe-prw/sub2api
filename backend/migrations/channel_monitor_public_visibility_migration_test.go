package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorPublicVisibilityDefaultsClosed(t *testing.T) {
	content, err := FS.ReadFile("225_channel_monitor_public_visibility.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "public_visible BOOLEAN NOT NULL DEFAULT FALSE")
}
