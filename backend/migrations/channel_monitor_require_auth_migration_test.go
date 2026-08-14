package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRequireAuthDefaultsEnabled(t *testing.T) {
	content, err := FS.ReadFile("228_channel_monitor_require_auth.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "VALUES ('channel_monitor_require_auth', 'true')")
	require.Contains(t, sql, "ON CONFLICT (key) DO NOTHING")
}
