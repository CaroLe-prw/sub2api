package repository

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCandySchedulerMetadataPreservesEligibilityWithoutHistory(t *testing.T) {
	for _, tc := range []struct {
		name    string
		extra   map[string]any
		blocked bool
	}{
		{"untested", nil, false},
		{"blocked", map[string]any{service.CandyTestBlockedExtraKey: true}, true},
		{"cleared", map[string]any{service.CandyTestBlockedExtraKey: false}, false},
		{"legacy wrong then error", map[string]any{service.CandyTestExtraKey: service.CandyTestSnapshot{Total: 2, Failed: 1, Errors: 1, Recent: []service.CandyTestSample{{Status: "failed", ResponseText: "candy-answer-visible-only-to-admin"}, {Status: "error"}}}}, true},
		{"only errors", map[string]any{service.CandyTestExtraKey: service.CandyTestSnapshot{Total: 1, Errors: 1, Recent: []service.CandyTestSample{{Status: "error"}}}}, false},
		{"rolled over history", map[string]any{service.CandyTestExtraKey: service.CandyTestSnapshot{LastVerdict: "failed", Total: 15, Failed: 1, Errors: 14, Recent: []service.CandyTestSample{{Status: "error"}}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := service.Account{ID: 42, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Extra: tc.extra, Credentials: map[string]any{"access_token": "secret"}}
			_, payload, err := marshalSchedulerCacheAccount(account)
			require.NoError(t, err)
			var metadata service.Account
			require.NoError(t, json.Unmarshal([]byte(payload), &metadata))
			require.Equal(t, tc.blocked, account.IsCandyTestBlocked())
			require.Equal(t, tc.blocked, metadata.IsCandyTestBlocked())
			require.Equal(t, !tc.blocked, metadata.IsSchedulable())
			require.NotContains(t, metadata.Extra, service.CandyTestExtraKey)
			require.NotContains(t, payload, "candy-answer-visible-only-to-admin")
			require.Empty(t, metadata.GetCredential("access_token"))
		})
	}
}

func TestCandySchedulingUpdatesAtomicallyPublishSchedulerOutbox(t *testing.T) {
	for _, scenario := range []string{"history only", "block", "unblock", "outbox failure"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := newAccountRepositoryWithSQL(client, db, nil)
			updates := map[string]any{service.CandyTestExtraKey: service.CandyTestSnapshot{}}
			if scenario != "history only" {
				updates[service.CandyTestBlockedExtraKey] = scenario != "unblock"
				mock.ExpectBegin()
			}
			payload, err := json.Marshal(updates)
			require.NoError(t, err)
			mock.ExpectExec(regexp.QuoteMeta("UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || $1::jsonb, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL")).
				WithArgs(string(payload), int64(27)).WillReturnResult(sqlmock.NewResult(0, 1))
			if scenario != "history only" {
				expect := mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
					WithArgs(service.SchedulerOutboxEventAccountChanged, int64(27), nil, nil, sqlmock.AnyArg())
				if scenario == "outbox failure" {
					expect.WillReturnError(errors.New("outbox unavailable"))
					mock.ExpectRollback()
				} else {
					expect.WillReturnResult(sqlmock.NewResult(1, 1))
					mock.ExpectCommit()
				}
			}
			err = repo.UpdateExtra(context.Background(), 27, updates)
			if scenario == "outbox failure" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
