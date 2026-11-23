package service

import (
	"sort"
	"time"
)

type openAIOAuthCreationGroup struct {
	priority    int
	compactTier int
}

func openAIOAuthCreationGroupFor(account *Account, groupID *int64, requireCompact bool) (openAIOAuthCreationGroup, bool) {
	// Missing timestamps are not evidence of age; retain their existing policy.
	if account == nil || !account.IsOpenAIOAuth() || account.CreatedAt.IsZero() {
		return openAIOAuthCreationGroup{}, false
	}
	key := openAIOAuthCreationGroup{priority: openAIAccountSchedulingPriority(account, groupID)}
	if requireCompact {
		key.compactTier = openAICompactSupportTier(account)
	}
	return key, true
}

func needsOpenAIOAuthCreationOrder[T any](items []T, accountOf func(T) *Account, groupID *int64, requireCompact bool) bool {
	first := make(map[openAIOAuthCreationGroup]time.Time)
	for _, item := range items {
		account := accountOf(item)
		key, ok := openAIOAuthCreationGroupFor(account, groupID, requireCompact)
		if !ok {
			continue
		}
		if created, exists := first[key]; exists {
			if !created.Equal(account.CreatedAt) {
				return true
			}
		} else {
			first[key] = account.CreatedAt
		}
	}
	return false
}

// Reorder only the slots belonging to same-priority OpenAI OAuth accounts.
// A pairwise "OAuth age else score" comparator is not transitive in mixed
// OAuth/API Key pools. Slot replacement preserves other account types and
// priority/capability tiers without introducing inconsistent sort comparisons.
func orderOpenAIOAuthByCreation[T any](items []T, accountOf func(T) *Account, groupID *int64, requireCompact bool) {
	groups := make(map[openAIOAuthCreationGroup][]int)
	for i, item := range items {
		if key, ok := openAIOAuthCreationGroupFor(accountOf(item), groupID, requireCompact); ok {
			groups[key] = append(groups[key], i)
		}
	}
	for _, positions := range groups {
		if len(positions) < 2 {
			continue
		}
		ordered := make([]T, len(positions))
		for i, position := range positions {
			ordered[i] = items[position]
		}
		sort.SliceStable(ordered, func(i, j int) bool {
			return accountOf(ordered[i]).CreatedAt.Before(accountOf(ordered[j]).CreatedAt)
		})
		for i, position := range positions {
			items[position] = ordered[i]
		}
	}
}

func openAICreationOrderAccount(account *Account) *Account { return account }

func openAICreationOrderLoadedAccount(item accountWithLoad) *Account { return item.account }
