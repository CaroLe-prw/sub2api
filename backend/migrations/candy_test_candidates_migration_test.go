package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCandyCandidatesMigrationPreservesExistingModels(t *testing.T) {
	content, err := FS.ReadFile("259_group_candy_test_model_candidates.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ALTER TABLE groups ALTER COLUMN candy_test_model TYPE VARCHAR(2048)")
	require.NotContains(t, sql, "UPDATE groups")
	require.NotContains(t, sql, "DROP")
	require.NotContains(t, sql, "DELETE")
}
