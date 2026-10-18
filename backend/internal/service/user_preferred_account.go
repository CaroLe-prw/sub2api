package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const userPreferredAccountLayer = "user_preferred"

var ErrInvalidPreferredAccount = infraerrors.BadRequest("INVALID_PREFERRED_ACCOUNT", "用户或分组不存在，或账号不属于该分组")

type UserPreferredAccountEntry struct {
	UserID        int64  `json:"user_id"`
	UserName      string `json:"user_name"`
	UserEmail     string `json:"user_email"`
	AccountID     int64  `json:"account_id"`
	AccountName   string `json:"account_name"`
	AccountStatus string `json:"account_status"`
	InGroup       bool   `json:"in_group"`
}

type UserPreferredAccountRepository interface {
	Get(context.Context, int64, int64) (int64, error)
	List(context.Context, int64) ([]UserPreferredAccountEntry, error)
	Set(context.Context, int64, int64, int64) error
	Delete(context.Context, int64, int64) error
}

type UserPreferredAccountService struct {
	repo UserPreferredAccountRepository
}

func NewUserPreferredAccountService(repo UserPreferredAccountRepository) *UserPreferredAccountService {
	return &UserPreferredAccountService{repo: repo}
}

func (s *UserPreferredAccountService) List(ctx context.Context, groupID int64) ([]UserPreferredAccountEntry, error) {
	return s.repo.List(ctx, groupID)
}

func (s *UserPreferredAccountService) Set(ctx context.Context, userID, groupID, accountID int64) error {
	if userID <= 0 || groupID <= 0 || accountID <= 0 {
		return ErrInvalidPreferredAccount
	}
	return s.repo.Set(ctx, userID, groupID, accountID)
}

func (s *UserPreferredAccountService) Delete(ctx context.Context, userID, groupID int64) error {
	return s.repo.Delete(ctx, userID, groupID)
}

type userPreferredAccountContextKey struct{}
type preferredAccountLookup struct {
	service *UserPreferredAccountService
	userID  int64
	mu      sync.Mutex
	entries map[int64]preferredAccountLookupEntry
}
type preferredAccountLookupEntry struct {
	accountID int64
	expires   time.Time
}

// Keep lookups out of the shared authentication cache. A websocket also picks
// up administrator changes within five seconds; retries reuse the same lookup.
func (s *APIKeyService) ContextWithUserAccountPreferences(ctx context.Context, userID int64) context.Context {
	if s == nil || s.preferredAccounts == nil || userID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, userPreferredAccountContextKey{}, &preferredAccountLookup{
		service: s.preferredAccounts, userID: userID, entries: make(map[int64]preferredAccountLookupEntry),
	})
}

func preferredAccountForRequest(ctx context.Context, groupID *int64, excluded map[int64]struct{}) int64 {
	if pinned, _ := ctx.Value(openAIPinnedRetryAccountContextKey{}).(int64); pinned > 0 {
		return 0
	}
	if geminiCachedContentBoundFromContext(ctx) {
		return 0
	}

	if groupID == nil || *groupID <= 0 || preferredAccountProbeID(ctx) > 0 {
		return 0
	}
	lookup, _ := ctx.Value(userPreferredAccountContextKey{}).(*preferredAccountLookup)
	if lookup == nil {
		return 0
	}
	lookup.mu.Lock()
	defer lookup.mu.Unlock()
	entry, ok := lookup.entries[*groupID]
	if !ok || time.Now().After(entry.expires) {
		lookupCtx, cancel := context.WithTimeout(ctx, time.Second)
		id, err := lookup.service.repo.Get(lookupCtx, lookup.userID, *groupID)
		cancel()
		if err != nil {
			slog.WarnContext(ctx, "preferred account lookup failed; using normal scheduling", "user_id", lookup.userID, "group_id", *groupID, "error", err)
			id = 0
		}
		entry = preferredAccountLookupEntry{accountID: id, expires: time.Now().Add(5 * time.Second)}
		lookup.entries[*groupID] = entry
	}
	if _, skip := excluded[entry.accountID]; skip {
		return 0
	}
	return entry.accountID
}

type preferredAccountProbeContextKey struct{}

func preferredAccountProbeID(ctx context.Context) int64 {
	id, _ := ctx.Value(preferredAccountProbeContextKey{}).(int64)
	return id
}

func filterPreferredAccountProbe(ctx context.Context, accounts []Account) []Account {
	id := preferredAccountProbeID(ctx)
	if id <= 0 {
		return accounts
	}
	for i := range accounts {
		if accounts[i].ID == id {
			return []Account{accounts[i]}
		}
	}
	return nil
}

// Keep an unavailable preference out of ordinary sticky/scored selection. The
// OpenAI scheduler may explicitly retry a latency-only veto after alternatives
// are exhausted, using the original caller exclusions and all hard gates.
func excludeUserPreferredAccount(ctx context.Context, groupID *int64, excluded map[int64]struct{}) map[int64]struct{} {
	id := preferredAccountForRequest(ctx, groupID, excluded)
	if id <= 0 {
		return excluded
	}
	result := cloneExcludedAccountIDs(excluded)
	if result == nil {
		result = make(map[int64]struct{})
	}
	result[id] = struct{}{}
	return result
}
