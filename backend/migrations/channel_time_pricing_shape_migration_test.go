package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelTimePricingCompatibilityDoesNotEmbedAutomaticDataMigration(t *testing.T) {
	for _, name := range []string{
		"234_normalize_channel_time_pricing_shape.sql",
		"235_normalize_channel_time_pricing_shape.sql",
	} {
		_, err := FS.ReadFile(name)
		require.Error(t, err, "%s must not run automatically during startup", name)
	}
}
