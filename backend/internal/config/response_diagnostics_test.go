package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseDiagnosticsLimitConfig(t *testing.T) {
	for _, tc := range []struct {
		name, env string
		want      int
		invalid   bool
	}{
		{"default", "", 1048576, false},
		{"environment", "2097152", 2097152, false},
		{"zero uses runtime default", "0", 0, false},
		{"negative", "-1", 0, true},
		{"too small", "100", 0, true},
		{"too large", "16777217", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv("GATEWAY_RESPONSE_DIAGNOSTICS_MAX_BODY_BYTES", tc.env)
			cfg, err := Load()
			if tc.invalid {
				require.ErrorContains(t, err, "gateway.response_diagnostics_max_body_bytes")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.Gateway.ResponseDiagnosticsMaxBodyBytes)
		})
	}
	t.Run("config file", func(t *testing.T) {
		resetViperWithJWTSecret(t)
		t.Setenv("GATEWAY_RESPONSE_DIAGNOSTICS_MAX_BODY_BYTES", "")
		file := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(file, []byte("gateway:\n  response_diagnostics_max_body_bytes: 3145728\n"), 0600))
		t.Setenv("CONFIG_FILE", file)
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, 3145728, cfg.Gateway.ResponseDiagnosticsMaxBodyBytes)
	})
}
