//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestCandyFailureStreakMigrationReconcilesOnlyChangedGates(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newAccountRepositoryWithSQL(tx.Client(), tx, nil)
	migration, err := migrations.FS.ReadFile("258_candy_test_consecutive_failures.sql")
	require.NoError(t, err)
	for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		for _, tc := range []struct {
			name                                                       string
			statuses                                                   []string
			oldBlock, omitFlag, existingStreak, deleted, otherPlatform bool
			streak, events                                             int
			rolledOver                                                 bool
		}{
			{name: "one wrong", statuses: []string{"failed"}, oldBlock: true, streak: 1, events: 1},
			{name: "two wrong", statuses: []string{"failed", "failed"}, oldBlock: true, streak: 2},
			{name: "error neutral", statuses: []string{"failed", "error", "failed"}, oldBlock: true, streak: 2},
			{name: "pass resets", statuses: []string{"failed", "passed", "failed"}, oldBlock: true, streak: 1, events: 1},
			{name: "passed", statuses: []string{"failed", "failed", "passed"}},
			{name: "only errors", statuses: []string{"error", "error"}},
			{name: "legacy no flag single", statuses: []string{"failed"}, omitFlag: true, streak: 1, events: 1},
			{name: "legacy no flag double", statuses: []string{"failed", "failed"}, omitFlag: true, streak: 2},
			{name: "lost history", statuses: []string{"error"}, oldBlock: true, rolledOver: true, streak: 1, events: 1},
			{name: "new format untouched", statuses: []string{"failed", "failed"}, oldBlock: true, existingStreak: true, streak: 2},
			{name: "deleted untouched", statuses: []string{"failed"}, oldBlock: true, deleted: true},
			{name: "other platform untouched", statuses: []string{"failed"}, oldBlock: true, otherPlatform: true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				passed, failed, apiErrors, verdict := 0, 0, 0, ""
				recent := []map[string]any{}
				for _, status := range tc.statuses {
					recent = append(recent, map[string]any{"status": status, "response_text": "preserve-original-answer"})
					switch status {
					case "passed":
						passed++
						verdict = status
					case "failed":
						failed++
						verdict = status
					default:
						apiErrors++
					}
				}
				if tc.rolledOver {
					failed++
					verdict = "failed"
				}
				snapshot := map[string]any{"last_verdict": verdict, "total": passed + failed + apiErrors, "passed": passed, "failed": failed, "errors": apiErrors, "recent": recent}
				if tc.existingStreak {
					snapshot["consecutive_failures"] = tc.streak
				}
				extra := map[string]any{service.CandyTestExtraKey: snapshot, "unrelated_setting": "keep"}
				if !tc.omitFlag {
					extra[service.CandyTestBlockedExtraKey] = tc.oldBlock
				}
				platform := service.PlatformOpenAI
				if tc.otherPlatform {
					platform = service.PlatformAnthropic
				}
				account := mustCreateAccount(t, tx.Client(), &service.Account{Name: "streak-" + kind + tc.name, Platform: platform, Type: kind, Extra: extra})
				if tc.deleted {
					_, err = tx.ExecContext(ctx, "UPDATE accounts SET deleted_at=NOW() WHERE id=$1", account.ID)
					require.NoError(t, err)
				}
				for repeat := 0; repeat < 2; repeat++ {
					_, err = tx.ExecContext(ctx, string(migration))
					require.NoError(t, err)
					rows, err := tx.QueryContext(ctx, "SELECT extra, schedulable, (SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1 AND event_type='account_changed') FROM accounts WHERE id=$1", account.ID)
					require.NoError(t, err)
					require.True(t, rows.Next())
					var data []byte
					var schedulable bool
					var events int
					require.NoError(t, rows.Scan(&data, &schedulable, &events))
					require.NoError(t, rows.Close())
					require.True(t, schedulable, "migration must not touch manual switch")
					require.Equal(t, tc.events, events, "only an eligibility transition should invalidate scheduler pools, once")
					var stored map[string]any
					require.NoError(t, json.Unmarshal(data, &stored))
					require.Equal(t, "keep", stored["unrelated_setting"])
					got := stored[service.CandyTestExtraKey].(map[string]any)
					require.EqualValues(t, passed+failed+apiErrors, got["total"])
					require.Equal(t, "preserve-original-answer", got["recent"].([]any)[0].(map[string]any)["response_text"])
					if tc.deleted || tc.otherPlatform {
						require.NotContains(t, got, "consecutive_failures")
						require.Equal(t, true, stored[service.CandyTestBlockedExtraKey])
					} else {
						require.EqualValues(t, tc.streak, got["consecutive_failures"])
						require.Equal(t, tc.streak >= 2, stored[service.CandyTestBlockedExtraKey])
						loaded, err := repo.GetByID(ctx, account.ID)
						require.NoError(t, err)
						require.Equal(t, tc.streak >= 2, loaded.IsCandyTestBlocked())
					}
				}
			})
		}
	}
}
