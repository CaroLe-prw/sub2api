package service

import (
	"context"
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// groupOAuthOnlyFilter is a request-scoped account-type admission policy.  The
// persisted field historically prevented new API-key bindings, but an existing
// binding (or a stale scheduler snapshot) could still be selected at runtime.
// Keeping the policy in the request context makes every candidate path,
// including sticky-session rechecks, use the same rule.
type groupOAuthOnlyFilter struct {
	groupID  int64
	platform string
	enabled  bool
}

type groupOAuthOnlyFilterContextKey struct{}

func groupRequiresOAuthOnlyFilter(group *Group) bool {
	return group != nil && group.RequireOAuthOnly && groupSupportsOAuthOnlyFilter(group.Platform)
}

func withGroupOAuthOnlyFilter(ctx context.Context, group *Group) context.Context {
	if group == nil || group.ID <= 0 {
		return ctx
	}
	filter := groupOAuthOnlyFilter{
		groupID:  group.ID,
		platform: group.Platform,
		enabled:  groupRequiresOAuthOnlyFilter(group),
	}
	if existing, ok := ctx.Value(groupOAuthOnlyFilterContextKey{}).(groupOAuthOnlyFilter); ok && existing == filter {
		return ctx
	}
	return context.WithValue(ctx, groupOAuthOnlyFilterContextKey{}, filter)
}

// accountAllowedByGroupOAuthOnlyFilter intentionally follows the existing
// product contract: require_oauth_only excludes API-key accounts.  It does not
// reject setup-token or other non-apikey account types.
func accountAllowedByGroupOAuthOnlyFilter(ctx context.Context, account *Account) bool {
	if account == nil {
		return false
	}
	filter, ok := ctx.Value(groupOAuthOnlyFilterContextKey{}).(groupOAuthOnlyFilter)
	return !ok || !filter.enabled || account.Type != AccountTypeAPIKey
}

func resolveGroupOAuthOnlyFilter(ctx context.Context, groupID *int64, snapshot *SchedulerSnapshotService, repo GroupRepository) context.Context {
	if groupID == nil || *groupID <= 0 {
		return ctx
	}
	if existing, ok := ctx.Value(groupOAuthOnlyFilterContextKey{}).(groupOAuthOnlyFilter); ok && existing.groupID == *groupID {
		if existing.enabled {
			return ctx
		}
		if group, ok := ctx.Value(ctxkey.Group).(*Group); ok && IsGroupContextValid(group) && group.ID == *groupID {
			return ctx
		}
	}
	if group, ok := ctx.Value(ctxkey.Group).(*Group); ok && IsGroupContextValid(group) && group.ID == *groupID {
		return withGroupOAuthOnlyFilter(ctx, group)
	}

	var (
		group *Group
		err   error
	)
	if snapshot != nil {
		group, err = snapshot.GetGroupByIDLite(ctx, *groupID)
	}
	// A force-platform request can have a snapshot service whose group reader is
	// unavailable (for example during startup), while the owning service still
	// has a repository. Use that repository as a fallback before deciding that
	// the policy is disabled.
	if (group == nil || err != nil) && repo != nil {
		fallbackGroup, fallbackErr := repo.GetByIDLite(ctx, *groupID)
		if fallbackErr == nil {
			group, err = fallbackGroup, nil
		} else if err == nil {
			err = fallbackErr
		}
	}
	if err != nil {
		// This mirrors the other group policy lookups on the scheduling path:
		// keep availability during a transient configuration-store failure, but
		// make the resulting fail-open decision visible to operators.
		slog.Warn("group_oauth_only_filter_load_failed", "group_id", *groupID, "error", err)
		return ctx
	}
	return withGroupOAuthOnlyFilter(ctx, group)
}

func (s *OpenAIGatewayService) withGroupOAuthOnlyFilter(ctx context.Context, groupID *int64) context.Context {
	if s == nil {
		return ctx
	}
	var repo GroupRepository
	if s.channelService != nil {
		repo = s.channelService.groupRepo
	}
	return resolveGroupOAuthOnlyFilter(ctx, groupID, s.schedulerSnapshot, repo)
}

func (s *GeminiMessagesCompatService) withGroupOAuthOnlyFilter(ctx context.Context, groupID *int64) context.Context {
	if s == nil {
		return ctx
	}
	return resolveGroupOAuthOnlyFilter(ctx, groupID, s.schedulerSnapshot, s.groupRepo)
}
