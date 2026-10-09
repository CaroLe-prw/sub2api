package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountModelStateWritesOnlyOneModelAndRefreshesScheduler(t *testing.T) {
	for _, operation := range []string{"cooldown", "error", "recover"} {
		t.Run(operation, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			repo := newAccountRepositoryWithSQL(client, db, nil)
			if operation == "recover" {
				mock.ExpectExec(regexp.QuoteMeta("UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) #- ARRAY['model_rate_limits', $2]::text[]")).WithArgs(int64(42), "gpt-6-astra").WillReturnResult(sqlmock.NewResult(0, 1))
			} else {
				mock.ExpectExec(`(?s)UPDATE accounts SET.*ARRAY\['model_rate_limits', \$1\].*CASE WHEN.*status.*THEN.*ELSE \$2::jsonb.*WHERE id = \$3`).WithArgs("gpt-6-astra", sqlmock.AnyArg(), int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).WithArgs(service.SchedulerOutboxEventAccountChanged, int64(42), nil, nil, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
			ctx := context.Background()
			switch operation {
			case "recover":
				err = repo.ClearModelRateLimit(ctx, 42, "gpt-6-astra")
			case "error":
				err = repo.SetModelError(ctx, 42, "gpt-6-astra", "timeout")
			default:
				err = repo.SetModelRateLimit(ctx, 42, "gpt-6-astra", time.Now().Add(time.Minute), "timeout")
			}
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
