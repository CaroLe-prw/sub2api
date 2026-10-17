//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyDeviceConvergenceSeedAtomicLifecycle(t *testing.T) {
	ctx := context.Background()
	fixtures := []struct {
		platform string
		typ      string
		extra    string
	}{
		{service.PlatformOpenAI, service.AccountTypeAPIKey, `{}`},
		{service.PlatformOpenAI, service.AccountTypeAPIKey, `{"codex_fingerprint_seed":"invalid"}`},
		{service.PlatformOpenAI, service.AccountTypeAPIKey, `{"codex_fingerprint_seed":"11111111-1111-4111-8111-111111111111"}`},
		{service.PlatformOpenAI, service.AccountTypeOAuth, `{}`},
		{service.PlatformAnthropic, service.AccountTypeAPIKey, `{}`},
	}
	ids := make([]int64, 0, len(fixtures))
	for _, fixture := range fixtures {
		var id int64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ($1, $2, $3, $4::jsonb) RETURNING id
`, "apikey-device-"+uuid.NewString(), fixture.platform, fixture.typ, fixture.extra).Scan(&id))
		ids = append(ids, id)
	}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM scheduler_outbox WHERE account_id = ANY($1)`, pq.Array(ids))
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = ANY($1)`, pq.Array(ids))
	})
	readSeed := func(id int64) string {
		t.Helper()
		var seed string
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COALESCE(extra->>'codex_fingerprint_seed', '') FROM accounts WHERE id = $1`, id).Scan(&seed))
		return seed
	}
	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	// A copied legacy mode and string-valued "true" must not activate API keys.
	require.NoError(t, repo.UpdateExtra(ctx, ids[0], map[string]any{
		"codex_fingerprint_mode":                   "full",
		"openai_apikey_device_convergence_enabled": "true",
	}))
	require.Empty(t, readSeed(ids[0]))
	require.NoError(t, repo.UpdateExtra(ctx, ids[0], map[string]any{
		"openai_apikey_device_convergence_enabled": true,
		"codex_fingerprint_seed":                   "22222222-2222-4222-8222-222222222222",
	}))
	firstSeed := readSeed(ids[0])
	requireCanonicalUUIDString(t, firstSeed)
	require.NotEqual(t, "22222222-2222-4222-8222-222222222222", firstSeed)

	updates := service.AccountBulkUpdate{
		Extra: map[string]any{
			"openai_apikey_device_convergence_enabled": true,
			"codex_fingerprint_seed":                   "22222222-2222-4222-8222-222222222222",
		},
		EnsureCodexFingerprintSeed: true,
	}
	rows, err := repo.BulkUpdate(ctx, ids, updates)
	require.NoError(t, err)
	require.Equal(t, int64(len(ids)), rows)
	seeds := make([]string, len(ids))
	for i, id := range ids {
		seeds[i] = readSeed(id)
	}
	require.Equal(t, firstSeed, seeds[0])
	requireCanonicalUUIDString(t, seeds[1])
	require.NotEqual(t, firstSeed, seeds[1], "each account gets its own random seed")
	require.Equal(t, "11111111-1111-4111-8111-111111111111", seeds[2])
	require.Empty(t, seeds[3], "the API-key switch must not enable OAuth convergence")
	require.Empty(t, seeds[4], "the switch is OpenAI-only")

	_, err = repo.BulkUpdate(ctx, ids, service.AccountBulkUpdate{Extra: map[string]any{"openai_apikey_device_convergence_enabled": false}})
	require.NoError(t, err)
	for i, id := range ids {
		require.Equal(t, seeds[i], readSeed(id), "disabling must retain seeds")
	}
	_, err = repo.BulkUpdate(ctx, ids, updates)
	require.NoError(t, err)
	for i, id := range ids {
		require.Equal(t, seeds[i], readSeed(id), "re-enabling must retain seeds")
	}
}
