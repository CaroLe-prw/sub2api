package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestOpenAISchedulerObservabilityRecordsFailoverAndSessionMetrics(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	groupID := int64(5)
	ctx := schedulerObservabilityTestContext("request-1", &Group{ID: groupID, Name: "OpenAI 主池"})
	req := OpenAIAccountScheduleRequest{
		GroupID:         &groupID,
		SessionHash:     "private-session-id",
		StickyAccountID: 15,
		RequestedModel:  "gpt-5.6",
	}
	firstDecision := OpenAIAccountScheduleDecision{
		Layer:            openAIAccountScheduleLayerLoadBalance,
		StickySessionHit: true,
		Candidates: []OpenAISchedulerObservabilityCandidate{
			{AccountID: 15, AccountName: "account-15", Rank: 1, BaseScore: 7.5, StickyBonus: 0.75, TotalScore: 8.25},
			{AccountID: 18, AccountName: "account-18", Rank: 2, BaseScore: 7.4, TotalScore: 7.4},
		},
	}
	store.RecordSelection(ctx, req, firstDecision, &AccountSelectionResult{Account: &Account{ID: 15, Name: "account-15"}}, nil)
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{
		AccountID:      15,
		AccountName:    "account-15",
		UpstreamStatus: 429,
		Reason:         "rate_limit",
		DurationMs:     80,
	})

	secondDecision := firstDecision
	secondDecision.StickySessionHit = false
	secondDecision.StickyEscapeReason = "consecutive_errors"
	secondDecision.Candidates = []OpenAISchedulerObservabilityCandidate{
		{AccountID: 18, AccountName: "account-18", Rank: 1, BaseScore: 7.4, TotalScore: 7.4},
	}
	store.RecordSelection(ctx, req, secondDecision, &AccountSelectionResult{Account: &Account{ID: 18, Name: "account-18"}}, nil)
	firstTokenMs := 240
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{
		AccountID:           18,
		AccountName:         "account-18",
		Success:             true,
		FirstTokenMs:        &firstTokenMs,
		DurationMs:          620,
		CacheReadTokens:     80,
		CacheEligibleTokens: 100,
	})

	secondCtx := schedulerObservabilityTestContext("request-2", &Group{ID: groupID, Name: "OpenAI 主池"})
	secondReq := req
	secondReq.StickyAccountID = 18
	secondDecision.StickySessionHit = true
	store.RecordSelection(secondCtx, secondReq, secondDecision, &AccountSelectionResult{Account: &Account{ID: 18, Name: "account-18"}}, nil)
	store.RecordOutcome(secondCtx, OpenAISchedulerObservabilityOutcome{
		AccountID:           18,
		AccountName:         "account-18",
		Success:             true,
		CacheReadTokens:     60,
		CacheEligibleTokens: 100,
	})

	snapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests", Page: 1, PageSize: 20})
	sessionSnapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "sessions", Page: 1, PageSize: 20})
	require.Equal(t, "memory", snapshot.RetentionMode)
	require.Equal(t, DefaultOpenAISchedulerObservabilityMaxTraces, snapshot.RetentionMax)
	require.Equal(t, 2, snapshot.Metrics.Requests)
	require.Equal(t, 1, snapshot.Metrics.SwitchedRequests)
	require.Equal(t, 1, snapshot.Metrics.Switches)
	require.Equal(t, snapshot.Metrics, sessionSnapshot.Metrics)
	require.Len(t, snapshot.Groups, 1)
	require.Len(t, sessionSnapshot.Sessions, 1)
	require.Equal(t, 2, sessionSnapshot.Sessions[0].Turns)
	require.Equal(t, []int64{18, 18}, sessionSnapshot.Sessions[0].TurnAccounts)
	require.InDelta(t, 0.6, sessionSnapshot.Sessions[0].FollowUpCacheRate, 0.0001)

	firstTrace := snapshot.Traces[1]
	require.Equal(t, "switched", firstTrace.Status)
	require.Equal(t, "sticky_escaped_consecutive_errors", firstTrace.Summary)
	require.NotNil(t, firstTrace.SessionFingerprint)
	require.Len(t, *firstTrace.SessionFingerprint, 12)
	require.Equal(t, "session_hash", firstTrace.SessionSource)
	require.Equal(t, []OpenAISchedulerObservabilityAccount{{ID: 15, Name: "account-15"}, {ID: 18, Name: "account-18"}}, firstTrace.AccountPath)
	require.Equal(t, 1, firstTrace.SwitchCount)
	require.Equal(t, int64(80), firstTrace.CacheReadTokens)
	require.Equal(t, int64(100), firstTrace.CacheEligibleTokens)
	require.Len(t, firstTrace.Candidates, 2)
	require.Equal(t, int64(18), firstTrace.Candidates[0].AccountID)
	require.Equal(t, "selected", firstTrace.Candidates[0].State)
	require.Equal(t, int64(15), firstTrace.Candidates[1].AccountID)
	require.Equal(t, "tried", firstTrace.Candidates[1].State)
	require.Equal(t, "rate_limit", firstTrace.Candidates[1].Reason)
	require.Contains(t, schedulerObservabilityAttemptKinds(firstTrace.Attempts), "upstream_failure")
	require.Contains(t, schedulerObservabilityAttemptKinds(firstTrace.Attempts), "account_switch")
	require.Contains(t, schedulerObservabilityAttemptKinds(firstTrace.Attempts), "request_success")
	require.Contains(t, schedulerObservabilityReasonKeys(snapshot.SwitchReasons), "rate_limit")
	require.Contains(t, schedulerObservabilityReasonKeys(snapshot.SwitchReasons), "consecutive_errors")
}

func TestOpenAISchedulerObservabilityHandlesMissingContextAndGroupFilter(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	groupID := int64(9)
	store.RecordSelection(nil, OpenAIAccountScheduleRequest{GroupID: &groupID}, OpenAIAccountScheduleDecision{}, nil, errors.New("no account"))

	matching := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "invalid", GroupID: &groupID})
	require.Equal(t, "1h", matching.TimeRange)
	require.Len(t, matching.Traces, 1)
	require.Equal(t, "#9", matching.Traces[0].GroupName)
	require.Equal(t, "failed", matching.Traces[0].Status)
	require.NotNil(t, matching.Traces[0].AccountPath)
	require.NotNil(t, matching.Traces[0].Attempts)
	require.NotNil(t, matching.Traces[0].Candidates)

	otherGroupID := int64(10)
	filtered := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", GroupID: &otherGroupID})
	require.Empty(t, filtered.Traces)
}

func TestOpenAISchedulerObservabilityKeepsSelectedRequestPendingUntilOutcome(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	ctx := schedulerObservabilityTestContext("request-pending", nil)
	decision := OpenAIAccountScheduleDecision{Candidates: []OpenAISchedulerObservabilityCandidate{
		{AccountID: 32, AccountName: "account-32", Rank: 1},
	}}
	store.RecordSelection(ctx, OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.5"}, decision, &AccountSelectionResult{
		Account: &Account{ID: 32, Name: "account-32"},
	}, nil)

	selected := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests"}).Traces[0]
	require.Equal(t, "pending", selected.Status)
	require.Nil(t, selected.FirstTokenMs)
	require.NotContains(t, schedulerObservabilityAttemptKinds(selected.Attempts), "request_success")

	firstTokenMs := 2813
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{
		AccountID: 32, AccountName: "account-32", Success: true, FirstTokenMs: &firstTokenMs,
	})

	completed := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests"}).Traces[0]
	require.Equal(t, "success", completed.Status)
	require.Equal(t, firstTokenMs, *completed.FirstTokenMs)
	require.Contains(t, schedulerObservabilityAttemptKinds(completed.Attempts), "request_success")
}

func TestOpenAISchedulerObservabilityDistinguishesDetectedFromAdoptedSticky(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	groupID := int64(5)
	ctx := schedulerObservabilityTestContext("request-sticky-not-adopted", &Group{ID: groupID, Name: "OpenAI 主池"})
	req := OpenAIAccountScheduleRequest{
		GroupID: &groupID, SessionHash: "session-sticky-not-adopted", StickyAccountID: 15, RequestedModel: "gpt-5.6",
	}
	decision := OpenAIAccountScheduleDecision{
		Layer: openAIAccountScheduleLayerLoadBalance,
		Candidates: []OpenAISchedulerObservabilityCandidate{
			{AccountID: 18, AccountName: "account-18", Rank: 1, BaseScore: 8, TotalScore: 8},
			{AccountID: 15, AccountName: "account-15", Rank: 2, BaseScore: 7, StickyBonus: .75, TotalScore: 7.75},
		},
	}
	store.RecordSelection(ctx, req, decision, &AccountSelectionResult{Account: &Account{ID: 18, Name: "account-18"}}, nil)
	firstTokenMs := 120
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{
		AccountID: 18, AccountName: "account-18", Success: true, FirstTokenMs: &firstTokenMs,
	})

	snapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests"})
	require.Equal(t, 1, snapshot.Metrics.StickyDetectedRequests)
	require.Zero(t, snapshot.Metrics.StickyRequests)
	require.Zero(t, snapshot.Metrics.StickyHitRate)
	require.True(t, snapshot.Traces[0].StickyDetected)
	require.False(t, snapshot.Traces[0].StickyHit)
	require.Contains(t, schedulerObservabilityAttemptKinds(snapshot.Traces[0].Attempts), "sticky_detected")
	require.GreaterOrEqual(t, *snapshot.Traces[0].EndToEndFirstTokenMs, firstTokenMs)
}

func TestOpenAISchedulerObservabilityExplainsDirectSessionStickyWithoutScoreComparison(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	ctx := schedulerObservabilityTestContext("request-direct-sticky", nil)
	req := OpenAIAccountScheduleRequest{SessionHash: "session-direct-sticky", StickyAccountID: 32, RequestedModel: "gpt-5.6"}
	decision := OpenAIAccountScheduleDecision{
		Layer: openAIAccountScheduleLayerSessionSticky, StickySessionHit: true,
		Candidates: []OpenAISchedulerObservabilityCandidate{{AccountID: 32, AccountName: "account-32", Rank: 1, State: "selected"}},
	}
	store.RecordSelection(ctx, req, decision, &AccountSelectionResult{Account: &Account{ID: 32, Name: "account-32"}}, nil)
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{AccountID: 32, AccountName: "account-32", Success: true})

	trace := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests"}).Traces[0]
	require.Equal(t, "session_sticky_kept", trace.Summary)
	require.Equal(t, "sticky_short_circuit", trace.CandidateScope)
	require.Contains(t, schedulerObservabilityAttemptKinds(trace.Attempts), "sticky_selected")
}

func TestOpenAISchedulerObservabilityExplainsStickyUpstreamFailover(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	ctx := schedulerObservabilityTestContext("request-sticky-upstream-failover", nil)
	req := OpenAIAccountScheduleRequest{SessionHash: "session-upstream-failover", StickyAccountID: 32, RequestedModel: "gpt-5.6"}
	stickyDecision := OpenAIAccountScheduleDecision{
		Layer: openAIAccountScheduleLayerSessionSticky, StickySessionHit: true,
		Candidates: []OpenAISchedulerObservabilityCandidate{{AccountID: 32, AccountName: "jjhd-bt", Rank: 1, State: "selected"}},
	}
	store.RecordSelection(ctx, req, stickyDecision, &AccountSelectionResult{Account: &Account{ID: 32, Name: "jjhd-bt"}}, nil)
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{
		AccountID: 32, AccountName: "jjhd-bt", UpstreamStatus: 502, Reason: "upstream_error",
	})

	fallbackDecision := OpenAIAccountScheduleDecision{
		Layer:      openAIAccountScheduleLayerLoadBalance,
		Candidates: []OpenAISchedulerObservabilityCandidate{{AccountID: 35, AccountName: "sky-pro", Rank: 1, State: "selected"}},
	}
	store.RecordSelection(ctx, req, fallbackDecision, &AccountSelectionResult{Account: &Account{ID: 35, Name: "sky-pro"}}, nil)
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{AccountID: 35, AccountName: "sky-pro", Success: true})

	trace := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests"}).Traces[0]
	require.Equal(t, "sticky_failed_over_upstream_error", trace.Summary)
	require.Equal(t, []OpenAISchedulerObservabilityAccount{{ID: 32, Name: "jjhd-bt"}, {ID: 35, Name: "sky-pro"}}, trace.AccountPath)
}

func TestOpenAISchedulerObservabilityClientCancellationDoesNotMarkAccountFailed(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	ctx := schedulerObservabilityTestContext("request-client-canceled", nil)
	decision := OpenAIAccountScheduleDecision{Layer: openAIAccountScheduleLayerLoadBalance, Candidates: []OpenAISchedulerObservabilityCandidate{
		{AccountID: 32, AccountName: "account-32", Rank: 1, State: "selected"},
	}}
	store.RecordSelection(ctx, OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.6"}, decision, &AccountSelectionResult{Account: &Account{ID: 32, Name: "account-32"}}, nil)
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{
		AccountID: 32, AccountName: "account-32", Canceled: true, Reason: "client_disconnected",
	})

	snapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests"})
	trace := snapshot.Traces[0]
	require.Equal(t, "canceled", trace.Status)
	require.Equal(t, "selected", trace.Candidates[0].State)
	require.Zero(t, snapshot.TraceCounts.Failed)
	require.Contains(t, schedulerObservabilityAttemptKinds(trace.Attempts), "request_canceled")
	require.NotContains(t, schedulerObservabilityAttemptKinds(trace.Attempts), "upstream_failure")
}

func TestOpenAISchedulerObservabilitySeparatesLocalAdmissionRejectionFromUpstreamSwitch(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	groupID := int64(5)
	ctx := schedulerObservabilityTestContext("request-admission-reselection", &Group{ID: groupID, Name: "OpenAI 主池"})
	req := OpenAIAccountScheduleRequest{GroupID: &groupID, RequestedModel: "gpt-5.6"}
	firstDecision := OpenAIAccountScheduleDecision{
		Layer: openAIAccountScheduleLayerLoadBalance,
		Candidates: []OpenAISchedulerObservabilityCandidate{
			{AccountID: 32, AccountName: "account-32", Rank: 1, BaseScore: 5.18, TotalScore: 5.18},
			{AccountID: 11, AccountName: "account-11", Rank: 2, BaseScore: 5.16, TotalScore: 5.16},
		},
	}
	store.RecordSelection(ctx, req, firstDecision, &AccountSelectionResult{Account: &Account{ID: 32, Name: "account-32"}}, nil)
	store.RecordAdmissionRejection(ctx, OpenAISchedulerObservabilityAdmissionRejection{
		AccountID: 32, AccountName: "account-32", Reason: "profit_veto",
	})

	secondDecision := firstDecision
	secondDecision.Candidates = []OpenAISchedulerObservabilityCandidate{
		{AccountID: 11, AccountName: "account-11", Rank: 1, BaseScore: 5.16, TotalScore: 5.16},
	}
	store.RecordSelection(ctx, req, secondDecision, &AccountSelectionResult{Account: &Account{ID: 11, Name: "account-11"}}, nil)
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{AccountID: 11, AccountName: "account-11", Success: true})

	snapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests"})
	require.Len(t, snapshot.Traces, 1)
	trace := snapshot.Traces[0]
	require.Equal(t, "success", trace.Status)
	require.Zero(t, trace.SwitchCount)
	require.Zero(t, snapshot.Metrics.Switches)
	require.Zero(t, snapshot.Metrics.SwitchedRequests)
	require.Equal(t, []OpenAISchedulerObservabilityAccount{{ID: 11, Name: "account-11"}}, trace.AccountPath)
	require.Equal(t, []string{
		"candidate_selected", "admission_rejected", "account_reselected", "candidate_selected", "request_success",
	}, schedulerObservabilityAttemptKinds(trace.Attempts))

	require.Len(t, trace.Candidates, 2)
	require.Equal(t, int64(11), trace.Candidates[0].AccountID)
	require.Equal(t, "selected", trace.Candidates[0].State)
	require.Equal(t, int64(32), trace.Candidates[1].AccountID)
	require.Equal(t, "rejected", trace.Candidates[1].State)
	require.Equal(t, "profit_veto", trace.Candidates[1].Reason)
}

func TestOpenAISchedulerObservabilityRollsBackProvisionalSwitchWhenAdmissionRejects(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	ctx := schedulerObservabilityTestContext("request-provisional-switch", nil)
	req := OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.6"}
	decision := OpenAIAccountScheduleDecision{Candidates: []OpenAISchedulerObservabilityCandidate{
		{AccountID: 32, AccountName: "account-32", Rank: 1},
		{AccountID: 11, AccountName: "account-11", Rank: 2},
		{AccountID: 29, AccountName: "account-29", Rank: 3},
	}}

	store.RecordSelection(ctx, req, decision, &AccountSelectionResult{Account: &Account{ID: 32, Name: "account-32"}}, nil)
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{AccountID: 32, AccountName: "account-32", UpstreamStatus: 502, Reason: "upstream_error"})
	store.RecordSelection(ctx, req, decision, &AccountSelectionResult{Account: &Account{ID: 11, Name: "account-11"}}, nil)
	store.RecordAdmissionRejection(ctx, OpenAISchedulerObservabilityAdmissionRejection{AccountID: 11, AccountName: "account-11", Reason: "profit_veto"})
	store.RecordSelection(ctx, req, decision, &AccountSelectionResult{Account: &Account{ID: 29, Name: "account-29"}}, nil)
	store.RecordOutcome(ctx, OpenAISchedulerObservabilityOutcome{AccountID: 29, AccountName: "account-29", Success: true})

	trace := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests"}).Traces[0]
	require.Equal(t, 1, trace.SwitchCount)
	require.Equal(t, []OpenAISchedulerObservabilityAccount{{ID: 32, Name: "account-32"}, {ID: 29, Name: "account-29"}}, trace.AccountPath)
	require.Equal(t, 1, countSchedulerObservabilityAttemptKind(trace.Attempts, "account_switch"))
	require.Equal(t, 1, countSchedulerObservabilityAttemptKind(trace.Attempts, "account_reselected"))
}

func TestOpenAISchedulerObservabilityRecordsWebSocketTurnsAsSessionRequests(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	groupID := int64(5)
	ctx := WithRequestTypeContext(schedulerObservabilityTestContext("ws-request", &Group{ID: groupID, Name: "OpenAI 主池"}), RequestTypeWSV2)
	req := OpenAIAccountScheduleRequest{GroupID: &groupID, SessionHash: "ws-session", RequestedModel: "gpt-5.6"}
	decision := OpenAIAccountScheduleDecision{Layer: openAIAccountScheduleLayerLoadBalance}
	selection := &AccountSelectionResult{Account: &Account{ID: 18, Name: "account-18"}}
	store.RecordSelection(ctx, req, decision, selection, nil)
	store.RecordTurnOutcome(ctx, 1, OpenAISchedulerObservabilityOutcome{AccountID: 18, AccountName: "account-18", Success: true})
	store.RecordTurnOutcome(ctx, 2, OpenAISchedulerObservabilityOutcome{
		AccountID: 18, AccountName: "account-18", Success: true, CacheReadTokens: 90, CacheEligibleTokens: 100,
	})

	snapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests"})
	sessionSnapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "sessions"})
	require.Len(t, snapshot.Traces, 2)
	require.Equal(t, "ws-request-turn-2", snapshot.Traces[0].RequestID)
	require.Equal(t, "ws_v2", snapshot.Traces[0].RequestType)
	require.Equal(t, "ws_v2", snapshot.Traces[1].RequestType)
	require.Len(t, sessionSnapshot.Sessions, 1)
	require.Equal(t, 2, sessionSnapshot.Sessions[0].Turns)
	require.InDelta(t, 0.9, sessionSnapshot.Sessions[0].FollowUpCacheRate, 0.0001)
}

func TestOpenAISchedulerObservabilityFiltersRequestTypeAndMarksCyberResult(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	streamCtx := WithRequestTypeContext(schedulerObservabilityTestContext("stream-request", nil), RequestTypeStream)
	wsCtx := WithRequestTypeContext(schedulerObservabilityTestContext("ws-request-filter", nil), RequestTypeWSV2)

	store.RecordSelection(streamCtx, OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.6"}, OpenAIAccountScheduleDecision{}, nil, errors.New("no account"))
	store.MarkCyberBlocked(streamCtx)
	store.RecordSelection(wsCtx, OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.6"}, OpenAIAccountScheduleDecision{}, nil, errors.New("no account"))

	snapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests", RequestType: "stream"})
	require.Len(t, snapshot.Traces, 1)
	require.Equal(t, "stream", snapshot.Traces[0].RequestType)
	require.True(t, snapshot.Traces[0].CyberBlocked)
	require.Equal(t, 1, snapshot.Metrics.Requests)
}

func TestOpenAISchedulerObservabilityTreatsLegacyTraceAsSync(t *testing.T) {
	legacy := OpenAISchedulerObservabilityTrace{RequestType: ""}
	require.Equal(t, "sync", schedulerObservabilityTraceRequestType(legacy.RequestType))
}

func TestOpenAISchedulerObservabilityTurnCyberResultDoesNotMutateFirstTurn(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	ctx := WithRequestTypeContext(schedulerObservabilityTestContext("ws-cyber-turn", nil), RequestTypeWSV2)
	store.RecordSelection(ctx, OpenAIAccountScheduleRequest{SessionHash: "ws-cyber", RequestedModel: "gpt-5.6"}, OpenAIAccountScheduleDecision{}, &AccountSelectionResult{Account: &Account{ID: 18, Name: "account-18"}}, nil)
	store.RecordTurnOutcome(ctx, 1, OpenAISchedulerObservabilityOutcome{AccountID: 18, AccountName: "account-18", Success: true})
	store.RecordTurnOutcome(ctx, 2, OpenAISchedulerObservabilityOutcome{AccountID: 18, AccountName: "account-18", CyberBlocked: true})

	snapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests"})
	require.Len(t, snapshot.Traces, 2)
	require.True(t, snapshot.Traces[0].CyberBlocked)
	require.False(t, snapshot.Traces[1].CyberBlocked)
}

func TestOpenAISchedulerObservabilityPaginationDoesNotChangeAggregates(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	for index := 1; index <= 25; index++ {
		ctx := schedulerObservabilityTestContext(fmt.Sprintf("request-%02d", index), nil)
		store.RecordSelection(ctx, OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.6"}, OpenAIAccountScheduleDecision{}, nil, errors.New("no account"))
	}

	snapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{
		TimeRange: "1h", View: "requests", Page: 2, PageSize: 10, TraceFilter: "failed",
	})
	require.Equal(t, 25, snapshot.Metrics.Requests)
	require.Equal(t, 25, snapshot.TraceCounts.Failed)
	require.Equal(t, 25, snapshot.Pagination.Total)
	require.Equal(t, 3, snapshot.Pagination.Pages)
	require.Equal(t, 2, snapshot.Pagination.Page)
	require.Len(t, snapshot.Traces, 10)
}

func TestOpenAISchedulerObservabilityFiltersByModelAccountAndAPIKey(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	groupID := int64(5)
	firstCtx := schedulerObservabilityTestContext("filtered-request-1", &Group{ID: groupID, Name: "OpenAI 主池"})
	firstDecision := OpenAIAccountScheduleDecision{Candidates: []OpenAISchedulerObservabilityCandidate{
		{AccountID: 11, AccountName: "account-eleven", Rank: 1},
	}}
	store.RecordSelection(firstCtx, OpenAIAccountScheduleRequest{
		GroupID: &groupID, SessionHash: "filter-session", RequestedModel: "gpt-5.6-sol",
	}, firstDecision, &AccountSelectionResult{Account: &Account{ID: 11, Name: "account-eleven"}}, nil)
	store.RecordOutcome(firstCtx, OpenAISchedulerObservabilityOutcome{AccountID: 11, AccountName: "account-eleven", Success: true})

	secondCtx := schedulerObservabilityTestContext("filtered-request-2", &Group{ID: groupID, Name: "OpenAI 主池"})
	secondCtx = context.WithValue(secondCtx, ctxkey.APIKeyID, int64(8))
	secondCtx = context.WithValue(secondCtx, ctxkey.APIKeyName, "Secondary key")
	secondDecision := OpenAIAccountScheduleDecision{Candidates: []OpenAISchedulerObservabilityCandidate{
		{AccountID: 22, AccountName: "account-twenty-two", Rank: 1},
	}}
	store.RecordSelection(secondCtx, OpenAIAccountScheduleRequest{
		GroupID: &groupID, RequestedModel: "gpt-5.6-terra",
	}, secondDecision, &AccountSelectionResult{Account: &Account{ID: 22, Name: "account-twenty-two"}}, nil)
	store.RecordOutcome(secondCtx, OpenAISchedulerObservabilityOutcome{AccountID: 22, AccountName: "account-twenty-two", Success: true})

	accountID := int64(11)
	apiKeyID := int64(8)
	byModel := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", Model: "gpt-5.6-sol", PageSize: 20})
	byAccount := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", AccountID: &accountID, PageSize: 20})
	byAPIKey := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", APIKeyID: &apiKeyID, PageSize: 20})

	require.Equal(t, []string{"gpt-5.6-sol", "gpt-5.6-terra"}, byModel.Models)
	require.Len(t, byModel.Accounts, 2)
	require.Len(t, byModel.APIKeys, 2)
	require.Equal(t, 1, byModel.Metrics.Requests)
	require.Equal(t, "filtered-request-1", byModel.Traces[0].RequestID)
	require.Equal(t, "filtered-request-1", byAccount.Traces[0].RequestID)
	require.Equal(t, "filtered-request-2", byAPIKey.Traces[0].RequestID)

	sessions := store.Snapshot(OpenAISchedulerObservabilityQuery{
		TimeRange: "1h", View: "sessions", AccountID: &accountID, PageSize: 20,
	})
	require.Len(t, sessions.Sessions, 1)
	require.Equal(t, "account-eleven", sessions.Sessions[0].AccountNames[11])
}

func TestOpenAISchedulerObservabilityConfigureBoundsAndClearsMemory(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	store.Configure(true, 100)
	for index := 1; index <= 101; index++ {
		ctx := schedulerObservabilityTestContext(fmt.Sprintf("bounded-%03d", index), nil)
		store.RecordSelection(ctx, OpenAIAccountScheduleRequest{}, OpenAIAccountScheduleDecision{}, nil, errors.New("no account"))
	}

	snapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{View: "requests", PageSize: 100})
	require.Equal(t, 100, snapshot.RetentionMax)
	require.Equal(t, 100, snapshot.Metrics.Requests)

	store.Configure(false, 100)
	cleared := store.Snapshot(OpenAISchedulerObservabilityQuery{View: "requests", PageSize: 100})
	require.Zero(t, cleared.Metrics.Requests)
	require.Empty(t, cleared.Traces)
}

func TestOpenAISchedulerObservabilityHybridUsesPersistedAggregatesAndMergesAbnormalDetails(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	ctx := schedulerObservabilityTestContext("live-request", nil)
	store.RecordSelection(ctx, OpenAIAccountScheduleRequest{}, OpenAIAccountScheduleDecision{}, nil, errors.New("no account"))
	persistedTrace := OpenAISchedulerObservabilityTrace{
		ID: "persisted-switch", RequestID: "persisted-switch", CreatedAt: "2026-08-10T12:00:00Z",
		Status: "switched", SwitchCount: 1, AccountPath: []OpenAISchedulerObservabilityAccount{{ID: 3, Name: "account-3"}},
		Attempts: []OpenAISchedulerObservabilityAttempt{}, Candidates: []OpenAISchedulerObservabilityCandidate{},
	}
	snapshot := store.SnapshotHybrid(OpenAISchedulerObservabilityQuery{TimeRange: "7d", PageSize: 20}, OpenAISchedulerObservabilityPersistentData{
		Metrics: OpenAISchedulerObservabilityMetrics{Requests: 120, StickyRequests: 90, StickyHitRate: 0.75, SwitchedRequests: 8, Switches: 9, CacheReadTokens: 400, CacheEligibleTokens: 800, FollowUpCacheRate: 0.5},
		Groups:  []OpenAISchedulerObservabilityGroup{{ID: 5, Name: "main"}},
		Traces:  []OpenAISchedulerObservabilityTrace{persistedTrace},
	}, 7)

	require.Equal(t, "hybrid", snapshot.RetentionMode)
	require.Equal(t, 7, snapshot.RetentionDays)
	require.Equal(t, 120, snapshot.Metrics.Requests)
	require.Equal(t, 0.5, snapshot.Metrics.FollowUpCacheRate)
	require.Len(t, snapshot.Traces, 2)
	require.Len(t, snapshot.Groups, 1)
}

func TestSchedulerObservabilityContributionDeltaCorrectsIntermediateFailure(t *testing.T) {
	failed := schedulerObservabilityContributionFromTrace(OpenAISchedulerObservabilityTrace{
		CreatedAt: "2026-08-10T12:01:10Z", Status: "failed", CacheEligibleTokens: 100,
	})
	succeeded := schedulerObservabilityContributionFromTrace(OpenAISchedulerObservabilityTrace{
		CreatedAt: "2026-08-10T12:01:10Z", Status: "success", CacheReadTokens: 80, CacheEligibleTokens: 100,
	})
	initial := schedulerObservabilityContributionDelta(failed, schedulerObservabilityContribution{})
	correction := schedulerObservabilityContributionDelta(succeeded, failed)

	require.Equal(t, int64(1), initial.Requests)
	require.Equal(t, int64(1), initial.FailedRequests)
	require.Zero(t, correction.Requests)
	require.Equal(t, int64(-1), correction.FailedRequests)
	require.Equal(t, int64(80), correction.CacheReadTokens)
}

func TestMarkSchedulerCandidateStatesPreservesStickyEscapeExclusion(t *testing.T) {
	candidates := []OpenAISchedulerObservabilityCandidate{
		{AccountID: 32, AccountName: "jjhd-bt", State: "excluded", Reason: "consecutive_errors"},
		{AccountID: 35, AccountName: "sky-pro", State: "eligible"},
	}

	markSchedulerCandidateStates(candidates, []OpenAISchedulerObservabilityAccount{{ID: 35}}, 35)

	require.Equal(t, "excluded", candidates[0].State)
	require.Equal(t, "consecutive_errors", candidates[0].Reason)
	require.Equal(t, "selected", candidates[1].State)
}

func TestOpenAISchedulerObservabilityRecordsRetryBudgetDecisions(t *testing.T) {
	store := NewOpenAISchedulerObservabilityStore()
	ctx := schedulerObservabilityTestContext("retry-budget", nil)
	store.RecordSelection(ctx, OpenAIAccountScheduleRequest{}, OpenAIAccountScheduleDecision{}, &AccountSelectionResult{
		Account: &Account{ID: 326, Name: "account-326"},
	}, nil)
	store.RecordRetryDecision(ctx, OpenAISchedulerObservabilityRetryDecision{
		Continue: true, Reason: "within_retry_budget", ElapsedMs: 17_800, BudgetMs: 75_000,
		SwitchCount: 4, SwitchLimit: 5, RemainingCandidates: 4,
	})
	store.RecordRetryDecision(ctx, OpenAISchedulerObservabilityRetryDecision{
		Continue: false, Reason: "max_account_switches", ElapsedMs: 23_000, BudgetMs: 75_000,
		SwitchCount: 5, SwitchLimit: 5, RemainingCandidates: 2,
	})

	snapshot := store.Snapshot(OpenAISchedulerObservabilityQuery{View: "requests", PageSize: 10})
	require.Len(t, snapshot.Traces, 1)
	attempts := snapshot.Traces[0].Attempts
	require.Len(t, attempts, 3)
	require.Equal(t, "retry_continued", attempts[1].Kind)
	require.Equal(t, int64(75_000), attempts[1].BudgetMs)
	require.Equal(t, 4, attempts[1].RemainingCandidates)
	require.Equal(t, "retry_stopped", attempts[2].Kind)
	require.Equal(t, "max_account_switches", attempts[2].Reason)
	require.Equal(t, "failed", snapshot.Traces[0].Status)
}

func schedulerObservabilityTestContext(requestID string, group *Group) context.Context {
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, requestID)
	ctx = context.WithValue(ctx, ctxkey.UserID, int64(42))
	ctx = context.WithValue(ctx, ctxkey.UserEmail, "operator@example.com")
	ctx = context.WithValue(ctx, ctxkey.APIKeyID, int64(7))
	ctx = context.WithValue(ctx, ctxkey.APIKeyName, "Codex Team")
	return context.WithValue(ctx, ctxkey.Group, group)
}

func schedulerObservabilityAttemptKinds(attempts []OpenAISchedulerObservabilityAttempt) []string {
	kinds := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		kinds = append(kinds, attempt.Kind)
	}
	return kinds
}

func countSchedulerObservabilityAttemptKind(attempts []OpenAISchedulerObservabilityAttempt, kind string) int {
	count := 0
	for _, attempt := range attempts {
		if attempt.Kind == kind {
			count++
		}
	}
	return count
}

func schedulerObservabilityReasonKeys(reasons []OpenAISchedulerObservabilityReason) []string {
	keys := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		keys = append(keys, reason.Key)
	}
	return keys
}
