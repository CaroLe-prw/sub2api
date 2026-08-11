package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

const (
	DefaultOpenAISchedulerObservabilityMaxTraces = 1000
	MinOpenAISchedulerObservabilityMaxTraces     = 100
	MaxOpenAISchedulerObservabilityMaxTraces     = 10000
	openAISchedulerObservabilityRetention        = 24 * time.Hour
)

type OpenAISchedulerObservabilityCandidate struct {
	AccountID   int64   `json:"accountId"`
	AccountName string  `json:"accountName"`
	Rank        int     `json:"rank"`
	BaseScore   float64 `json:"baseScore"`
	StickyBonus float64 `json:"stickyBonus"`
	TotalScore  float64 `json:"totalScore"`
	State       string  `json:"state"`
	Reason      string  `json:"reason,omitempty"`
}

type OpenAISchedulerObservabilityAttempt struct {
	ID                  string `json:"id"`
	Kind                string `json:"kind"`
	AccountID           int64  `json:"accountId,omitempty"`
	AccountName         string `json:"accountName,omitempty"`
	OffsetMs            int64  `json:"offsetMs"`
	UpstreamStatus      int    `json:"upstreamStatus,omitempty"`
	RetryCount          int    `json:"retryCount,omitempty"`
	RetryLimit          int    `json:"retryLimit,omitempty"`
	BudgetMs            int64  `json:"budgetMs,omitempty"`
	RemainingCandidates int    `json:"remainingCandidates,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

type OpenAISchedulerObservabilityAccount struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type OpenAISchedulerObservabilityTrace struct {
	ID                   string                                  `json:"id"`
	RequestID            string                                  `json:"requestId"`
	CreatedAt            string                                  `json:"createdAt"`
	UserID               int64                                   `json:"userId"`
	UserEmail            string                                  `json:"userEmail"`
	APIKeyID             int64                                   `json:"apiKeyId"`
	APIKeyName           string                                  `json:"apiKeyName"`
	GroupID              int64                                   `json:"groupId"`
	GroupName            string                                  `json:"groupName"`
	Model                string                                  `json:"model"`
	SessionFingerprint   *string                                 `json:"sessionFingerprint"`
	SessionSource        string                                  `json:"sessionSource"`
	SessionTurn          *int                                    `json:"sessionTurn"`
	DecisionLayer        string                                  `json:"decisionLayer"`
	CandidateScope       string                                  `json:"candidateScope"`
	Summary              string                                  `json:"summary"`
	StickyDetected       bool                                    `json:"stickyDetected"`
	StickyHit            bool                                    `json:"stickyHit"`
	AccountPath          []OpenAISchedulerObservabilityAccount   `json:"accountPath"`
	RetryCount           int                                     `json:"retryCount"`
	SwitchCount          int                                     `json:"switchCount"`
	CacheReadTokens      int64                                   `json:"cacheReadTokens"`
	CacheEligibleTokens  int64                                   `json:"cacheEligibleTokens"`
	FirstTokenMs         *int                                    `json:"firstTokenMs"`
	EndToEndFirstTokenMs *int                                    `json:"endToEndFirstTokenMs"`
	DurationMs           int64                                   `json:"durationMs"`
	Status               string                                  `json:"status"`
	Attempts             []OpenAISchedulerObservabilityAttempt   `json:"attempts"`
	Candidates           []OpenAISchedulerObservabilityCandidate `json:"candidates"`
	StickyEscapeReason   string                                  `json:"stickyEscapeReason,omitempty"`
	updatedAt            time.Time
}

type OpenAISchedulerObservabilitySession struct {
	Fingerprint       string           `json:"fingerprint"`
	Source            string           `json:"source"`
	UserID            int64            `json:"userId"`
	UserEmail         string           `json:"userEmail"`
	APIKeyName        string           `json:"apiKeyName"`
	GroupID           int64            `json:"groupId"`
	GroupName         string           `json:"groupName"`
	Model             string           `json:"model"`
	Turns             int              `json:"turns"`
	AccountIDs        []int64          `json:"accountIds"`
	AccountNames      map[int64]string `json:"accountNames"`
	SwitchCount       int              `json:"switchCount"`
	StickyHitRate     float64          `json:"stickyHitRate"`
	FollowUpCacheRate float64          `json:"followUpCacheRate"`
	LastActiveAt      string           `json:"lastActiveAt"`
	TurnAccounts      []int64          `json:"turnAccounts"`
}

type OpenAISchedulerObservabilityMetrics struct {
	Requests               int     `json:"requests"`
	StickyDetectedRequests int     `json:"stickyDetectedRequests"`
	StickyRequests         int     `json:"stickyRequests"`
	StickyHitRate          float64 `json:"stickyHitRate"`
	SwitchedRequests       int     `json:"switchedRequests"`
	Switches               int     `json:"switches"`
	SwitchRate             float64 `json:"switchRate"`
	StableSessions         int     `json:"stableSessions"`
	Sessions               int     `json:"sessions"`
	SessionStability       float64 `json:"sessionStability"`
	CacheReadTokens        int64   `json:"cacheReadTokens"`
	CacheEligibleTokens    int64   `json:"cacheEligibleTokens"`
	FollowUpCacheRate      float64 `json:"followUpCacheRate"`
}

type OpenAISchedulerObservabilityTraceCounts struct {
	All    int `json:"all"`
	Sticky int `json:"sticky"`
	Switch int `json:"switch"`
	Failed int `json:"failed"`
}

type OpenAISchedulerObservabilityReason struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type OpenAISchedulerObservabilityGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type OpenAISchedulerObservabilityFilterOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type OpenAISchedulerObservabilitySnapshot struct {
	Enabled       bool                                       `json:"enabled"`
	GeneratedAt   string                                     `json:"generatedAt"`
	TimeRange     string                                     `json:"timeRange"`
	View          string                                     `json:"view"`
	RetentionMode string                                     `json:"retentionMode"`
	RetentionMax  int                                        `json:"retentionMax"`
	RetentionDays int                                        `json:"retentionDays"`
	Pagination    OpenAISchedulerObservabilityPagination     `json:"pagination"`
	Metrics       OpenAISchedulerObservabilityMetrics        `json:"metrics"`
	TraceCounts   OpenAISchedulerObservabilityTraceCounts    `json:"traceCounts"`
	SwitchReasons []OpenAISchedulerObservabilityReason       `json:"switchReasons"`
	Groups        []OpenAISchedulerObservabilityGroup        `json:"groups"`
	Models        []string                                   `json:"models"`
	Accounts      []OpenAISchedulerObservabilityFilterOption `json:"accounts"`
	APIKeys       []OpenAISchedulerObservabilityFilterOption `json:"apiKeys"`
	Traces        []OpenAISchedulerObservabilityTrace        `json:"traces"`
	Sessions      []OpenAISchedulerObservabilitySession      `json:"sessions"`
}

type OpenAISchedulerObservabilityPagination struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
	Pages    int `json:"pages"`
}

type OpenAISchedulerObservabilityQuery struct {
	TimeRange   string
	GroupID     *int64
	Model       string
	AccountID   *int64
	APIKeyID    *int64
	View        string
	Page        int
	PageSize    int
	Search      string
	TraceFilter string
}

type OpenAISchedulerObservabilityOutcome struct {
	AccountID           int64
	AccountName         string
	Success             bool
	Canceled            bool
	UpstreamStatus      int
	Reason              string
	FirstTokenMs        *int
	DurationMs          int64
	CacheReadTokens     int64
	CacheEligibleTokens int64
}

type OpenAISchedulerObservabilityAdmissionRejection struct {
	AccountID   int64
	AccountName string
	Reason      string
}

type OpenAISchedulerObservabilityRetryDecision struct {
	Continue            bool
	Reason              string
	ElapsedMs           int64
	BudgetMs            int64
	SwitchCount         int
	SwitchLimit         int
	RemainingCandidates int
}

type OpenAISchedulerObservabilityStore struct {
	mu        sync.RWMutex
	traces    map[string]*OpenAISchedulerObservabilityTrace
	order     []string
	maxTraces int
	sequence  atomic.Uint64
	onChange  func(OpenAISchedulerObservabilityTrace)
}

func NewOpenAISchedulerObservabilityStore() *OpenAISchedulerObservabilityStore {
	return &OpenAISchedulerObservabilityStore{
		traces:    make(map[string]*OpenAISchedulerObservabilityTrace),
		maxTraces: DefaultOpenAISchedulerObservabilityMaxTraces,
	}
}

func parseOpenAISchedulerObservabilityMaxTraces(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < MinOpenAISchedulerObservabilityMaxTraces || parsed > MaxOpenAISchedulerObservabilityMaxTraces {
		return DefaultOpenAISchedulerObservabilityMaxTraces
	}
	return parsed
}

func (s *OpenAISchedulerObservabilityStore) Configure(enabled bool, maxTraces int) {
	if s == nil {
		return
	}
	if maxTraces < MinOpenAISchedulerObservabilityMaxTraces || maxTraces > MaxOpenAISchedulerObservabilityMaxTraces {
		maxTraces = DefaultOpenAISchedulerObservabilityMaxTraces
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxTraces = maxTraces
	if !enabled {
		s.traces = make(map[string]*OpenAISchedulerObservabilityTrace)
		s.order = nil
		return
	}
	s.pruneLocked(time.Now())
}

func (s *OpenAISchedulerObservabilityStore) SetObserver(observer func(OpenAISchedulerObservabilityTrace)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.onChange = observer
	s.mu.Unlock()
}

func (s *OpenAISchedulerObservabilityStore) publishLocked(trace *OpenAISchedulerObservabilityTrace) {
	if s != nil && trace != nil && s.onChange != nil {
		s.onChange(cloneSchedulerTrace(trace))
	}
}

func (s *OpenAIGatewayService) schedulerObservabilityStore() *OpenAISchedulerObservabilityStore {
	if s == nil {
		return nil
	}
	s.openaiSchedulerObservabilityOnce.Do(func() {
		s.openaiSchedulerObservability = NewOpenAISchedulerObservabilityStore()
		if s.openaiSchedulerPersistence != nil {
			s.openaiSchedulerObservability.SetObserver(s.openaiSchedulerPersistence.Enqueue)
		}
	})
	return s.openaiSchedulerObservability
}

func schedulerObservabilityRequestID(ctx context.Context, sequence *atomic.Uint64) string {
	if ctx != nil {
		if value, _ := ctx.Value(ctxkey.RequestID).(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		if value, _ := ctx.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return fmt.Sprintf("scheduler-%d-%d", time.Now().UnixMilli(), sequence.Add(1))
}

func schedulerObservabilityFingerprint(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	digest := sha256.Sum256([]byte(value))
	fingerprint := hex.EncodeToString(digest[:6])
	return &fingerprint
}

func schedulerObservabilityTimeRange(value string) (string, time.Duration) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "15m":
		return "15m", 15 * time.Minute
	case "6h":
		return "6h", 6 * time.Hour
	case "24h":
		return "24h", 24 * time.Hour
	case "7d":
		return "7d", 7 * 24 * time.Hour
	default:
		return "1h", time.Hour
	}
}

func (s *OpenAISchedulerObservabilityStore) RecordSelection(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	decision OpenAIAccountScheduleDecision,
	selection *AccountSelectionResult,
	selectionErr error,
) {
	if s == nil {
		return
	}
	now := time.Now()
	requestID := schedulerObservabilityRequestID(ctx, &s.sequence)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	trace := s.traces[requestID]
	if trace == nil {
		groupID := derefGroupID(req.GroupID)
		groupName := ""
		if ctx != nil {
			if group, _ := ctx.Value(ctxkey.Group).(*Group); group != nil {
				groupName = group.Name
			}
		}
		if groupName == "" && groupID > 0 {
			groupName = fmt.Sprintf("#%d", groupID)
		}
		fingerprint := schedulerObservabilityFingerprint(req.SessionHash)
		sessionSource := "none"
		if fingerprint != nil {
			sessionSource = "session_hash"
		} else if strings.TrimSpace(req.PreviousResponseID) != "" {
			fingerprint = schedulerObservabilityFingerprint(req.PreviousResponseID)
			sessionSource = "previous_response_id"
		}
		var sessionTurn *int
		if fingerprint != nil {
			turn := 1
			for _, existing := range s.traces {
				if existing.SessionFingerprint != nil && *existing.SessionFingerprint == *fingerprint &&
					existing.UserID == contextInt64(ctx, ctxkey.UserID) && existing.GroupID == groupID {
					turn++
				}
			}
			sessionTurn = &turn
		}
		trace = &OpenAISchedulerObservabilityTrace{
			ID:                 requestID,
			RequestID:          requestID,
			CreatedAt:          now.Format(time.RFC3339Nano),
			UserID:             contextInt64(ctx, ctxkey.UserID),
			UserEmail:          contextString(ctx, ctxkey.UserEmail),
			APIKeyID:           contextInt64(ctx, ctxkey.APIKeyID),
			APIKeyName:         contextString(ctx, ctxkey.APIKeyName),
			GroupID:            groupID,
			GroupName:          groupName,
			Model:              req.RequestedModel,
			SessionFingerprint: fingerprint,
			SessionSource:      sessionSource,
			SessionTurn:        sessionTurn,
			CandidateScope:     "scored",
			Status:             "failed",
			Summary:            "no_available_account",
			AccountPath:        make([]OpenAISchedulerObservabilityAccount, 0),
			Attempts:           make([]OpenAISchedulerObservabilityAttempt, 0),
			Candidates:         make([]OpenAISchedulerObservabilityCandidate, 0),
			updatedAt:          now,
		}
		s.traces[requestID] = trace
		s.order = append(s.order, requestID)
		s.pruneLocked(now)
	}

	trace.updatedAt = now
	trace.DurationMs = now.Sub(parseSchedulerTraceTime(trace.CreatedAt)).Milliseconds()
	trace.DecisionLayer = decision.Layer
	if trace.DecisionLayer == "" {
		trace.DecisionLayer = openAIAccountScheduleLayerLoadBalance
	}
	if decision.Layer == openAIAccountScheduleLayerSessionSticky && decision.StickySessionHit {
		trace.CandidateScope = "sticky_short_circuit"
	} else if len(decision.Candidates) > 0 {
		trace.CandidateScope = "scored"
	}
	if decision.StickyEscapeReason != "" {
		trace.StickyEscapeReason = decision.StickyEscapeReason
	}
	trace.StickyDetected = trace.StickyDetected || req.StickyAccountID > 0 || req.StickyPreviousAccountID > 0
	trace.StickyHit = trace.StickyHit || decision.StickyPreviousHit || decision.StickySessionHit
	trace.Candidates = mergeSchedulerCandidates(trace.Candidates, decision.Candidates)

	if len(trace.Attempts) == 0 {
		stickyID := req.StickyPreviousAccountID
		if stickyID <= 0 {
			stickyID = req.StickyAccountID
		}
		if stickyID > 0 {
			trace.appendAttemptLocked("sticky_detected", stickyID, schedulerCandidateName(decision.Candidates, stickyID), now, 0, "")
		}
	}
	if decision.StickyEscapeReason != "" {
		stickyID := req.StickyAccountID
		if stickyID <= 0 {
			stickyID = req.StickyPreviousAccountID
		}
		trace.appendAttemptLocked("sticky_escape", stickyID, schedulerCandidateName(decision.Candidates, stickyID), now, 0, decision.StickyEscapeReason)
		markSchedulerCandidateFailure(trace.Candidates, stickyID, decision.StickyEscapeReason, false)
	}

	if selectionErr != nil || selection == nil || selection.Account == nil {
		trace.Status = "failed"
		trace.Summary = "no_available_account"
		trace.appendAttemptLocked("upstream_failure", 0, "", now, 0, "no_available_account")
		s.publishLocked(trace)
		return
	}

	account := selection.Account
	lastAccountID := int64(0)
	if len(trace.AccountPath) > 0 {
		lastAccountID = trace.AccountPath[len(trace.AccountPath)-1].ID
	}
	if lastAccountID == account.ID {
		trace.RetryCount++
		trace.appendAttemptLocked("same_account_retry", account.ID, account.Name, now, 0, "")
	} else {
		if lastAccountID > 0 {
			trace.SwitchCount++
			trace.appendAttemptLocked("account_switch", account.ID, account.Name, now, 0, "")
		} else if schedulerTraceNeedsLocalReselection(trace) {
			trace.appendAttemptLocked("account_reselected", account.ID, account.Name, now, 0, "")
		}
		trace.AccountPath = append(trace.AccountPath, OpenAISchedulerObservabilityAccount{ID: account.ID, Name: account.Name})
	}
	selectionKind := "candidate_selected"
	if decision.Layer == openAIAccountScheduleLayerSessionSticky && decision.StickySessionHit {
		selectionKind = "sticky_selected"
	}
	trace.appendAttemptLocked(selectionKind, account.ID, account.Name, now, 0, "")
	trace.Status = "success"
	if trace.SwitchCount > 0 {
		trace.Status = "switched"
	}
	if decision.StickyEscapeReason != "" || trace.Summary == "" || trace.Summary == "no_available_account" {
		trace.Summary = schedulerObservabilitySummary(decision)
	}
	markSchedulerCandidateStates(trace.Candidates, trace.AccountPath, account.ID)
	s.publishLocked(trace)
}

// RecordAdmissionRejection records a local post-slot admission decision. No
// upstream request has been sent at this point, so the rejected account must
// not remain in the real account path or inflate failover metrics.
func (s *OpenAISchedulerObservabilityStore) RecordAdmissionRejection(
	ctx context.Context,
	rejection OpenAISchedulerObservabilityAdmissionRejection,
) {
	if s == nil || ctx == nil || rejection.AccountID <= 0 {
		return
	}
	requestID := contextString(ctx, ctxkey.RequestID)
	if requestID == "" {
		requestID = contextString(ctx, ctxkey.ClientRequestID)
	}
	if requestID == "" {
		return
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	trace := s.traces[requestID]
	if trace == nil {
		return
	}

	trace.updatedAt = now
	trace.DurationMs = now.Sub(parseSchedulerTraceTime(trace.CreatedAt)).Milliseconds()
	if rejection.AccountName == "" {
		rejection.AccountName = schedulerCandidateName(trace.Candidates, rejection.AccountID)
	}
	rejection.Reason = strings.TrimSpace(rejection.Reason)
	if rejection.Reason == "" {
		rejection.Reason = "admission_rejected"
	}

	// Selection is recorded before the slot/profit terminal check. If that
	// provisional selection created a switch/retry marker, reclassify it as a
	// local reselection and roll back the corresponding operational counter.
	selectedAttemptIndex := -1
	for index := len(trace.Attempts) - 1; index >= 0; index-- {
		attempt := trace.Attempts[index]
		if attempt.Kind == "candidate_selected" && attempt.AccountID == rejection.AccountID {
			selectedAttemptIndex = index
			break
		}
	}
	if selectedAttemptIndex > 0 {
		marker := &trace.Attempts[selectedAttemptIndex-1]
		if marker.AccountID == rejection.AccountID {
			switch marker.Kind {
			case "account_switch":
				marker.Kind = "account_reselected"
				if trace.SwitchCount > 0 {
					trace.SwitchCount--
				}
			case "same_account_retry":
				marker.Kind = "account_reselected"
				if trace.RetryCount > 0 {
					trace.RetryCount--
				}
			}
		}
	}
	if last := len(trace.AccountPath) - 1; last >= 0 && trace.AccountPath[last].ID == rejection.AccountID {
		trace.AccountPath = trace.AccountPath[:last]
	}
	if trace.SwitchCount > 0 {
		trace.Status = "switched"
	} else {
		trace.Status = "success"
	}
	markSchedulerCandidateAdmissionRejected(trace.Candidates, rejection.AccountID, rejection.Reason)
	trace.appendAttemptLocked("admission_rejected", rejection.AccountID, rejection.AccountName, now, 0, rejection.Reason)
	s.publishLocked(trace)
}

func (s *OpenAISchedulerObservabilityStore) RecordOutcome(ctx context.Context, outcome OpenAISchedulerObservabilityOutcome) {
	if s == nil || ctx == nil {
		return
	}
	requestID := contextString(ctx, ctxkey.RequestID)
	if requestID == "" {
		requestID = contextString(ctx, ctxkey.ClientRequestID)
	}
	if requestID == "" {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	trace := s.traces[requestID]
	if trace == nil {
		return
	}
	s.recordOutcomeLocked(trace, outcome, now)
	s.publishLocked(trace)
}

func (s *OpenAISchedulerObservabilityStore) RecordRetryDecision(ctx context.Context, decision OpenAISchedulerObservabilityRetryDecision) {
	if s == nil || ctx == nil {
		return
	}
	requestID := contextString(ctx, ctxkey.RequestID)
	if requestID == "" {
		requestID = contextString(ctx, ctxkey.ClientRequestID)
	}
	if requestID == "" {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	trace := s.traces[requestID]
	if trace == nil {
		return
	}
	kind := "retry_stopped"
	if decision.Continue {
		kind = "retry_continued"
	} else {
		trace.Status = "failed"
	}
	trace.updatedAt = now
	trace.DurationMs = now.Sub(parseSchedulerTraceTime(trace.CreatedAt)).Milliseconds()
	trace.appendAttemptLocked(kind, 0, "", now, 0, decision.Reason)
	attempt := &trace.Attempts[len(trace.Attempts)-1]
	attempt.OffsetMs = decision.ElapsedMs
	attempt.RetryCount = decision.SwitchCount
	attempt.RetryLimit = decision.SwitchLimit
	attempt.BudgetMs = decision.BudgetMs
	attempt.RemainingCandidates = decision.RemainingCandidates
	s.publishLocked(trace)
}

func (s *OpenAISchedulerObservabilityStore) RecordTurnOutcome(ctx context.Context, turn int, outcome OpenAISchedulerObservabilityOutcome) {
	if turn <= 1 {
		s.RecordOutcome(ctx, outcome)
		return
	}
	if s == nil || ctx == nil {
		return
	}
	requestID := contextString(ctx, ctxkey.RequestID)
	if requestID == "" {
		requestID = contextString(ctx, ctxkey.ClientRequestID)
	}
	if requestID == "" {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	parent := s.traces[requestID]
	if parent == nil {
		return
	}
	turnID := fmt.Sprintf("%s-turn-%d", requestID, turn)
	trace := s.traces[turnID]
	if trace == nil {
		cloned := cloneSchedulerTrace(parent)
		cloned.ID = turnID
		cloned.RequestID = turnID
		cloned.CreatedAt = now.Format(time.RFC3339Nano)
		cloned.AccountPath = []OpenAISchedulerObservabilityAccount{{ID: outcome.AccountID, Name: outcome.AccountName}}
		cloned.RetryCount = 0
		cloned.SwitchCount = 0
		cloned.CacheReadTokens = 0
		cloned.CacheEligibleTokens = 0
		cloned.FirstTokenMs = nil
		cloned.DurationMs = 0
		cloned.Status = "success"
		cloned.Attempts = nil
		cloned.updatedAt = now
		if parent.SessionTurn != nil {
			value := *parent.SessionTurn + turn - 1
			cloned.SessionTurn = &value
		}
		cloned.appendAttemptLocked("candidate_selected", outcome.AccountID, outcome.AccountName, now, 0, "")
		markSchedulerCandidateStates(cloned.Candidates, cloned.AccountPath, outcome.AccountID)
		trace = &cloned
		s.traces[turnID] = trace
		s.order = append(s.order, turnID)
		s.pruneLocked(now)
	}
	s.recordOutcomeLocked(trace, outcome, now)
	s.publishLocked(trace)
}

func (s *OpenAIGatewayService) AttachOpenAISchedulerObservabilityPersistence(persistence *OpenAISchedulerObservabilityPersistence) {
	if s == nil {
		return
	}
	s.openaiSchedulerPersistence = persistence
	if store := s.schedulerObservabilityStore(); store != nil && persistence != nil {
		store.SetObserver(persistence.Enqueue)
	}
}

func (s *OpenAIGatewayService) StopOpenAISchedulerObservabilityPersistence() {
	if s != nil && s.openaiSchedulerPersistence != nil {
		s.openaiSchedulerPersistence.Stop()
	}
}

func (s *OpenAISchedulerObservabilityStore) recordOutcomeLocked(trace *OpenAISchedulerObservabilityTrace, outcome OpenAISchedulerObservabilityOutcome, now time.Time) {
	trace.updatedAt = now
	// DurationMs is the whole scheduler chain, not only the final upstream attempt.
	trace.DurationMs = now.Sub(parseSchedulerTraceTime(trace.CreatedAt)).Milliseconds()
	if outcome.FirstTokenMs != nil {
		value := *outcome.FirstTokenMs
		trace.FirstTokenMs = &value
		endToEnd := value
		for index := len(trace.Attempts) - 1; index >= 0; index-- {
			attempt := trace.Attempts[index]
			if (attempt.Kind == "candidate_selected" || attempt.Kind == "sticky_selected") && attempt.AccountID == outcome.AccountID {
				endToEnd += int(attempt.OffsetMs)
				break
			}
		}
		trace.EndToEndFirstTokenMs = &endToEnd
	}
	if outcome.CacheReadTokens >= 0 {
		trace.CacheReadTokens = outcome.CacheReadTokens
	}
	if outcome.CacheEligibleTokens >= 0 {
		trace.CacheEligibleTokens = outcome.CacheEligibleTokens
	}
	if outcome.Canceled {
		trace.Status = "canceled"
		reason := strings.TrimSpace(outcome.Reason)
		if reason == "" {
			reason = "client_disconnected"
		}
		trace.appendAttemptLocked("request_canceled", outcome.AccountID, outcome.AccountName, now, 0, reason)
		return
	}
	if outcome.Success {
		trace.Status = "success"
		if trace.SwitchCount > 0 {
			trace.Status = "switched"
			if trace.StickyDetected && trace.StickyEscapeReason == "" {
				trace.Summary = "sticky_failed_over_upstream_error"
			}
		}
		trace.appendAttemptLocked("request_success", outcome.AccountID, outcome.AccountName, now, 0, "")
		return
	}
	trace.Status = "failed"
	markSchedulerCandidateFailure(trace.Candidates, outcome.AccountID, outcome.Reason, true)
	trace.appendAttemptLocked("upstream_failure", outcome.AccountID, outcome.AccountName, now, outcome.UpstreamStatus, outcome.Reason)
}

func (s *OpenAIGatewayService) schedulerObservabilityStoreForRequest(ctx context.Context) *OpenAISchedulerObservabilityStore {
	if s == nil {
		return nil
	}
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	store := s.schedulerObservabilityStore()
	if store == nil {
		return nil
	}
	store.Configure(settings.schedulerObservabilityEnabled, settings.schedulerObservabilityMaxTraces)
	if s.openaiSchedulerPersistence != nil {
		s.openaiSchedulerPersistence.Configure(settings.schedulerObservabilityEnabled, settings.schedulerObservabilityRetentionDays)
	}
	if !settings.schedulerObservabilityEnabled {
		return nil
	}
	return store
}

func (s *OpenAIGatewayService) RecordOpenAISchedulerObservabilitySelection(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	decision OpenAIAccountScheduleDecision,
	selection *AccountSelectionResult,
	selectionErr error,
) {
	if store := s.schedulerObservabilityStoreForRequest(ctx); store != nil {
		store.RecordSelection(ctx, req, decision, selection, selectionErr)
	}
}

func (s *OpenAIGatewayService) RecordOpenAISchedulerObservabilityOutcome(ctx context.Context, outcome OpenAISchedulerObservabilityOutcome) {
	if store := s.schedulerObservabilityStoreForRequest(ctx); store != nil {
		store.RecordOutcome(ctx, outcome)
	}
}

func (s *OpenAIGatewayService) RecordOpenAISchedulerObservabilityRetryDecision(ctx context.Context, decision OpenAISchedulerObservabilityRetryDecision) {
	if store := s.schedulerObservabilityStoreForRequest(ctx); store != nil {
		store.RecordRetryDecision(ctx, decision)
	}
}

func (s *OpenAIGatewayService) RecordOpenAISchedulerObservabilityAdmissionRejection(
	ctx context.Context,
	rejection OpenAISchedulerObservabilityAdmissionRejection,
) {
	if store := s.schedulerObservabilityStoreForRequest(ctx); store != nil {
		store.RecordAdmissionRejection(ctx, rejection)
	}
}

func (s *OpenAIGatewayService) RecordOpenAISchedulerObservabilityTurnOutcome(ctx context.Context, turn int, outcome OpenAISchedulerObservabilityOutcome) {
	if store := s.schedulerObservabilityStoreForRequest(ctx); store != nil {
		store.RecordTurnOutcome(ctx, turn, outcome)
	}
}

func (s *OpenAIGatewayService) GetOpenAISchedulerObservabilitySnapshot(ctx context.Context, query OpenAISchedulerObservabilityQuery) OpenAISchedulerObservabilitySnapshot {
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	if store := s.schedulerObservabilityStore(); store != nil {
		store.Configure(settings.schedulerObservabilityEnabled, settings.schedulerObservabilityMaxTraces)
		if s.openaiSchedulerPersistence != nil {
			s.openaiSchedulerPersistence.Configure(settings.schedulerObservabilityEnabled, settings.schedulerObservabilityRetentionDays)
		}
		if !settings.schedulerObservabilityEnabled {
			return emptyOpenAISchedulerObservabilitySnapshot(query, false, settings.schedulerObservabilityMaxTraces)
		}
		if persisted, ok := s.openaiSchedulerPersistence.Load(ctx, time.Now().Add(-schedulerObservabilityQueryDuration(query.TimeRange)), query.GroupID); ok {
			return store.SnapshotHybrid(query, persisted, settings.schedulerObservabilityRetentionDays)
		}
		return store.Snapshot(query)
	}
	return emptyOpenAISchedulerObservabilitySnapshot(query, settings.schedulerObservabilityEnabled, settings.schedulerObservabilityMaxTraces)
}

func schedulerObservabilityQueryDuration(value string) time.Duration {
	_, duration := schedulerObservabilityTimeRange(value)
	return duration
}

func emptyOpenAISchedulerObservabilitySnapshot(query OpenAISchedulerObservabilityQuery, enabled bool, retentionMax int) OpenAISchedulerObservabilitySnapshot {
	rangeName, _ := schedulerObservabilityTimeRange(query.TimeRange)
	view, page, pageSize := normalizeSchedulerObservabilityPagination(query)
	return OpenAISchedulerObservabilitySnapshot{
		Enabled: enabled, GeneratedAt: time.Now().Format(time.RFC3339Nano), TimeRange: rangeName, View: view,
		RetentionMode: "memory", RetentionMax: retentionMax,
		Pagination:    OpenAISchedulerObservabilityPagination{Page: page, PageSize: pageSize},
		SwitchReasons: make([]OpenAISchedulerObservabilityReason, 0),
		Groups:        make([]OpenAISchedulerObservabilityGroup, 0),
		Models:        make([]string, 0),
		Accounts:      make([]OpenAISchedulerObservabilityFilterOption, 0),
		APIKeys:       make([]OpenAISchedulerObservabilityFilterOption, 0),
		Traces:        make([]OpenAISchedulerObservabilityTrace, 0),
		Sessions:      make([]OpenAISchedulerObservabilitySession, 0),
	}
}

func (s *OpenAISchedulerObservabilityStore) Snapshot(query OpenAISchedulerObservabilityQuery) OpenAISchedulerObservabilitySnapshot {
	return s.snapshot(query, OpenAISchedulerObservabilityPersistentData{}, false, 0)
}

func (s *OpenAISchedulerObservabilityStore) SnapshotHybrid(query OpenAISchedulerObservabilityQuery, persisted OpenAISchedulerObservabilityPersistentData, retentionDays int) OpenAISchedulerObservabilitySnapshot {
	return s.snapshot(query, persisted, true, retentionDays)
}

func (s *OpenAISchedulerObservabilityStore) snapshot(query OpenAISchedulerObservabilityQuery, persisted OpenAISchedulerObservabilityPersistentData, hybrid bool, retentionDays int) OpenAISchedulerObservabilitySnapshot {
	rangeName, duration := schedulerObservabilityTimeRange(query.TimeRange)
	view, page, pageSize := normalizeSchedulerObservabilityPagination(query)
	cutoff := time.Now().Add(-duration)
	s.mu.RLock()
	retentionMax := s.maxTraces
	traces := make([]OpenAISchedulerObservabilityTrace, 0, len(s.order))
	groupsByID := make(map[int64]string)
	for index := len(s.order) - 1; index >= 0; index-- {
		trace := s.traces[s.order[index]]
		if trace == nil || trace.updatedAt.Before(cutoff) {
			continue
		}
		if trace.GroupID > 0 {
			groupsByID[trace.GroupID] = trace.GroupName
		}
		if query.GroupID != nil && trace.GroupID != *query.GroupID {
			continue
		}
		traces = append(traces, cloneSchedulerTrace(trace))
	}
	s.mu.RUnlock()
	if hybrid {
		seen := make(map[string]struct{}, len(traces))
		for _, trace := range traces {
			seen[trace.RequestID] = struct{}{}
		}
		for _, trace := range persisted.Traces {
			if _, exists := seen[trace.RequestID]; exists {
				continue
			}
			traces = append(traces, trace)
			seen[trace.RequestID] = struct{}{}
		}
		for _, group := range persisted.Groups {
			groupsByID[group.ID] = group.Name
		}
		sort.SliceStable(traces, func(i, j int) bool { return traces[i].CreatedAt > traces[j].CreatedAt })
	}

	models, accounts, apiKeys := buildSchedulerObservabilityFilterOptions(traces)
	filteredByDimension := filterSchedulerTracesByDimensions(traces, query)
	allSessions := buildSchedulerSessions(filteredByDimension)
	metrics, reasons := buildSchedulerMetrics(filteredByDimension, allSessions)
	if hybrid && !schedulerObservabilityHasDimensionFilter(query) {
		metrics.Requests = persisted.Metrics.Requests
		metrics.StickyDetectedRequests = persisted.Metrics.StickyDetectedRequests
		metrics.StickyRequests = persisted.Metrics.StickyRequests
		metrics.StickyHitRate = persisted.Metrics.StickyHitRate
		metrics.SwitchedRequests = persisted.Metrics.SwitchedRequests
		metrics.Switches = persisted.Metrics.Switches
		metrics.SwitchRate = persisted.Metrics.SwitchRate
		metrics.CacheReadTokens = persisted.Metrics.CacheReadTokens
		metrics.CacheEligibleTokens = persisted.Metrics.CacheEligibleTokens
		metrics.FollowUpCacheRate = persisted.Metrics.FollowUpCacheRate
	}
	traceCounts := buildSchedulerTraceCounts(filteredByDimension)
	groups := make([]OpenAISchedulerObservabilityGroup, 0, len(groupsByID))
	for id, name := range groupsByID {
		groups = append(groups, OpenAISchedulerObservabilityGroup{ID: id, Name: name})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })

	pagination := OpenAISchedulerObservabilityPagination{Page: page, PageSize: pageSize}
	pagedTraces := make([]OpenAISchedulerObservabilityTrace, 0)
	pagedSessions := make([]OpenAISchedulerObservabilitySession, 0)
	if view == "sessions" {
		filtered := filterSchedulerSessions(allSessions, query.Search)
		pagination.Total = len(filtered)
		pagination.Pages = schedulerObservabilityPageCount(pagination.Total, pageSize)
		pagination.Page, pagedSessions = paginateSchedulerItems(filtered, page, pageSize)
	} else {
		filtered := filterSchedulerTraces(filteredByDimension, query.Search, query.TraceFilter)
		pagination.Total = len(filtered)
		pagination.Pages = schedulerObservabilityPageCount(pagination.Total, pageSize)
		pagination.Page, pagedTraces = paginateSchedulerItems(filtered, page, pageSize)
	}

	retentionMode := "memory"
	if hybrid {
		retentionMode = "hybrid"
	}
	return OpenAISchedulerObservabilitySnapshot{
		Enabled:       true,
		GeneratedAt:   time.Now().Format(time.RFC3339Nano),
		TimeRange:     rangeName,
		View:          view,
		RetentionMode: retentionMode,
		RetentionMax:  retentionMax,
		RetentionDays: retentionDays,
		Pagination:    pagination,
		Metrics:       metrics,
		TraceCounts:   traceCounts,
		SwitchReasons: reasons,
		Groups:        groups,
		Models:        models,
		Accounts:      accounts,
		APIKeys:       apiKeys,
		Traces:        pagedTraces,
		Sessions:      pagedSessions,
	}
}

func schedulerObservabilityHasDimensionFilter(query OpenAISchedulerObservabilityQuery) bool {
	return strings.TrimSpace(query.Model) != "" || query.AccountID != nil || query.APIKeyID != nil
}

func filterSchedulerTracesByDimensions(traces []OpenAISchedulerObservabilityTrace, query OpenAISchedulerObservabilityQuery) []OpenAISchedulerObservabilityTrace {
	if !schedulerObservabilityHasDimensionFilter(query) {
		return traces
	}
	model := strings.TrimSpace(query.Model)
	filtered := make([]OpenAISchedulerObservabilityTrace, 0, len(traces))
	for _, trace := range traces {
		if model != "" && !strings.EqualFold(strings.TrimSpace(trace.Model), model) {
			continue
		}
		if query.APIKeyID != nil && trace.APIKeyID != *query.APIKeyID {
			continue
		}
		if query.AccountID != nil && !schedulerTraceHasAccount(trace, *query.AccountID) {
			continue
		}
		filtered = append(filtered, trace)
	}
	return filtered
}

func schedulerTraceHasAccount(trace OpenAISchedulerObservabilityTrace, accountID int64) bool {
	for _, account := range trace.AccountPath {
		if account.ID == accountID {
			return true
		}
	}
	for _, attempt := range trace.Attempts {
		if attempt.AccountID == accountID {
			return true
		}
	}
	return false
}

func buildSchedulerObservabilityFilterOptions(traces []OpenAISchedulerObservabilityTrace) ([]string, []OpenAISchedulerObservabilityFilterOption, []OpenAISchedulerObservabilityFilterOption) {
	modelsByName := make(map[string]string)
	accountsByID := make(map[int64]string)
	apiKeysByID := make(map[int64]string)
	for _, trace := range traces {
		if model := strings.TrimSpace(trace.Model); model != "" {
			modelsByName[strings.ToLower(model)] = model
		}
		if trace.APIKeyID > 0 {
			apiKeysByID[trace.APIKeyID] = schedulerObservabilityOptionName(trace.APIKeyName, trace.APIKeyID)
		}
		for _, account := range trace.AccountPath {
			if account.ID > 0 {
				accountsByID[account.ID] = schedulerObservabilityOptionName(account.Name, account.ID)
			}
		}
		for _, attempt := range trace.Attempts {
			if attempt.AccountID > 0 {
				accountsByID[attempt.AccountID] = schedulerObservabilityOptionName(attempt.AccountName, attempt.AccountID)
			}
		}
	}
	models := make([]string, 0, len(modelsByName))
	for _, model := range modelsByName {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return strings.ToLower(models[i]) < strings.ToLower(models[j]) })
	accounts := schedulerObservabilityFilterOptionsFromMap(accountsByID)
	apiKeys := schedulerObservabilityFilterOptionsFromMap(apiKeysByID)
	return models, accounts, apiKeys
}

func schedulerObservabilityFilterOptionsFromMap(values map[int64]string) []OpenAISchedulerObservabilityFilterOption {
	options := make([]OpenAISchedulerObservabilityFilterOption, 0, len(values))
	for id, name := range values {
		options = append(options, OpenAISchedulerObservabilityFilterOption{ID: id, Name: name})
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].Name != options[j].Name {
			return strings.ToLower(options[i].Name) < strings.ToLower(options[j].Name)
		}
		return options[i].ID < options[j].ID
	})
	return options
}

func schedulerObservabilityOptionName(name string, id int64) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return fmt.Sprintf("#%d", id)
}

func buildSchedulerTraceCounts(traces []OpenAISchedulerObservabilityTrace) OpenAISchedulerObservabilityTraceCounts {
	counts := OpenAISchedulerObservabilityTraceCounts{All: len(traces)}
	for _, trace := range traces {
		if trace.StickyHit {
			counts.Sticky++
		}
		if trace.SwitchCount > 0 {
			counts.Switch++
		}
		if trace.Status == "failed" {
			counts.Failed++
		}
	}
	return counts
}

func normalizeSchedulerObservabilityPagination(query OpenAISchedulerObservabilityQuery) (string, int, int) {
	view := "requests"
	if strings.EqualFold(strings.TrimSpace(query.View), "sessions") {
		view = "sessions"
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 20
	}
	return view, page, pageSize
}

func schedulerObservabilityPageCount(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}

func paginateSchedulerItems[T any](items []T, page, pageSize int) (int, []T) {
	pages := schedulerObservabilityPageCount(len(items), pageSize)
	if pages == 0 {
		return 1, make([]T, 0)
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * pageSize
	end := min(start+pageSize, len(items))
	return page, items[start:end]
}

func filterSchedulerTraces(traces []OpenAISchedulerObservabilityTrace, search, filter string) []OpenAISchedulerObservabilityTrace {
	query := strings.ToLower(strings.TrimSpace(search))
	filter = strings.ToLower(strings.TrimSpace(filter))
	filtered := make([]OpenAISchedulerObservabilityTrace, 0, len(traces))
	for _, trace := range traces {
		switch filter {
		case "sticky":
			if !trace.StickyHit {
				continue
			}
		case "switch":
			if trace.SwitchCount == 0 {
				continue
			}
		case "failed":
			if trace.Status != "failed" {
				continue
			}
		}
		if query != "" && !schedulerTraceMatchesSearch(trace, query) {
			continue
		}
		filtered = append(filtered, trace)
	}
	return filtered
}

func schedulerTraceMatchesSearch(trace OpenAISchedulerObservabilityTrace, query string) bool {
	values := []string{trace.RequestID, trace.UserEmail, trace.APIKeyName, trace.Model}
	if trace.SessionFingerprint != nil {
		values = append(values, *trace.SessionFingerprint)
	}
	for _, account := range trace.AccountPath {
		values = append(values, strconv.FormatInt(account.ID, 10), account.Name)
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func filterSchedulerSessions(sessions []OpenAISchedulerObservabilitySession, search string) []OpenAISchedulerObservabilitySession {
	query := strings.ToLower(strings.TrimSpace(search))
	if query == "" {
		return sessions
	}
	filtered := make([]OpenAISchedulerObservabilitySession, 0, len(sessions))
	for _, session := range sessions {
		values := []string{session.Fingerprint, session.UserEmail, session.APIKeyName, session.Model}
		for _, accountID := range session.AccountIDs {
			values = append(values, strconv.FormatInt(accountID, 10))
		}
		matched := false
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), query) {
				matched = true
				break
			}
		}
		if matched {
			filtered = append(filtered, session)
		}
	}
	return filtered
}

func (s *OpenAISchedulerObservabilityStore) pruneLocked(now time.Time) {
	cutoff := now.Add(-openAISchedulerObservabilityRetention)
	kept := s.order[:0]
	for _, requestID := range s.order {
		trace := s.traces[requestID]
		if trace == nil || trace.updatedAt.Before(cutoff) {
			delete(s.traces, requestID)
			continue
		}
		kept = append(kept, requestID)
	}
	s.order = kept
	maxTraces := s.maxTraces
	if maxTraces <= 0 {
		maxTraces = DefaultOpenAISchedulerObservabilityMaxTraces
	}
	for len(s.order) > maxTraces {
		delete(s.traces, s.order[0])
		s.order = s.order[1:]
	}
}

func (t *OpenAISchedulerObservabilityTrace) appendAttemptLocked(kind string, accountID int64, accountName string, now time.Time, status int, reason string) {
	if t == nil {
		return
	}
	createdAt := parseSchedulerTraceTime(t.CreatedAt)
	attempt := OpenAISchedulerObservabilityAttempt{
		ID:             fmt.Sprintf("%s-%d", t.ID, len(t.Attempts)+1),
		Kind:           kind,
		AccountID:      accountID,
		AccountName:    accountName,
		OffsetMs:       now.Sub(createdAt).Milliseconds(),
		UpstreamStatus: status,
		Reason:         reason,
	}
	if kind == "same_account_retry" {
		attempt.RetryCount = t.RetryCount
	}
	t.Attempts = append(t.Attempts, attempt)
}

func contextString(ctx context.Context, key ctxkey.Key) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(key).(string)
	return strings.TrimSpace(value)
}

func contextInt64(ctx context.Context, key ctxkey.Key) int64 {
	if ctx == nil {
		return 0
	}
	value, _ := ctx.Value(key).(int64)
	return value
}

func parseSchedulerTraceTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Now()
	}
	return parsed
}

func schedulerObservabilitySummary(decision OpenAIAccountScheduleDecision) string {
	switch decision.StickyEscapeReason {
	case "consecutive_errors", "error_rate", "ttft":
		return "sticky_escaped_consecutive_errors"
	case "concurrency_full":
		return "sticky_escaped_concurrency"
	}
	if decision.StickyPreviousHit && decision.Layer == openAIAccountScheduleLayerPreviousResponse {
		return "previous_response_kept"
	}
	if decision.StickySessionHit && decision.Layer == openAIAccountScheduleLayerSessionSticky {
		return "session_sticky_kept"
	}
	if decision.StickyPreviousHit || decision.StickySessionHit {
		return "weighted_sticky_kept"
	}
	return "load_balance_best_score"
}

func schedulerCandidateName(candidates []OpenAISchedulerObservabilityCandidate, accountID int64) string {
	for _, candidate := range candidates {
		if candidate.AccountID == accountID {
			return candidate.AccountName
		}
	}
	return ""
}

func cloneSchedulerCandidates(in []OpenAISchedulerObservabilityCandidate) []OpenAISchedulerObservabilityCandidate {
	return append([]OpenAISchedulerObservabilityCandidate{}, in...)
}

func mergeSchedulerCandidates(previous, current []OpenAISchedulerObservabilityCandidate) []OpenAISchedulerObservabilityCandidate {
	merged := cloneSchedulerCandidates(current)
	seen := make(map[int64]struct{}, len(current))
	for _, candidate := range current {
		seen[candidate.AccountID] = struct{}{}
	}
	for _, candidate := range previous {
		if _, exists := seen[candidate.AccountID]; exists {
			continue
		}
		merged = append(merged, candidate)
	}
	for index := range merged {
		merged[index].Rank = index + 1
	}
	return merged
}

func markSchedulerCandidateFailure(candidates []OpenAISchedulerObservabilityCandidate, accountID int64, reason string, overwriteReason bool) {
	if accountID <= 0 {
		return
	}
	for index := range candidates {
		if candidates[index].AccountID != accountID {
			continue
		}
		candidates[index].State = "tried"
		if strings.TrimSpace(reason) != "" && (overwriteReason || candidates[index].Reason == "") {
			candidates[index].Reason = strings.TrimSpace(reason)
		}
		return
	}
}

func markSchedulerCandidateAdmissionRejected(candidates []OpenAISchedulerObservabilityCandidate, accountID int64, reason string) {
	if accountID <= 0 {
		return
	}
	for index := range candidates {
		if candidates[index].AccountID != accountID {
			continue
		}
		candidates[index].State = "rejected"
		candidates[index].Reason = strings.TrimSpace(reason)
		return
	}
}

func markSchedulerCandidateStates(candidates []OpenAISchedulerObservabilityCandidate, path []OpenAISchedulerObservabilityAccount, selectedID int64) {
	tried := make(map[int64]struct{}, len(path))
	for _, account := range path {
		tried[account.ID] = struct{}{}
	}
	for index := range candidates {
		switch {
		case candidates[index].AccountID == selectedID:
			candidates[index].State = "selected"
		case candidates[index].State == "rejected" || candidates[index].State == "excluded":
			// Local admission rejections and sticky-escape exclusions are not
			// upstream attempts. Preserve them after another candidate is selected.
		case containsSchedulerAccount(tried, candidates[index].AccountID):
			candidates[index].State = "tried"
		default:
			candidates[index].State = "eligible"
		}
	}
}

func schedulerTraceNeedsLocalReselection(trace *OpenAISchedulerObservabilityTrace) bool {
	if trace == nil {
		return false
	}
	for index := len(trace.Attempts) - 1; index >= 0; index-- {
		switch trace.Attempts[index].Kind {
		case "admission_rejected":
			return true
		case "candidate_selected", "upstream_failure", "request_success":
			return false
		}
	}
	return false
}

func containsSchedulerAccount(accounts map[int64]struct{}, id int64) bool {
	_, ok := accounts[id]
	return ok
}

func cloneSchedulerTrace(trace *OpenAISchedulerObservabilityTrace) OpenAISchedulerObservabilityTrace {
	cloned := *trace
	cloned.AccountPath = append([]OpenAISchedulerObservabilityAccount{}, trace.AccountPath...)
	cloned.Attempts = append([]OpenAISchedulerObservabilityAttempt{}, trace.Attempts...)
	cloned.Candidates = cloneSchedulerCandidates(trace.Candidates)
	if trace.SessionFingerprint != nil {
		value := *trace.SessionFingerprint
		cloned.SessionFingerprint = &value
	}
	if trace.SessionTurn != nil {
		value := *trace.SessionTurn
		cloned.SessionTurn = &value
	}
	if trace.FirstTokenMs != nil {
		value := *trace.FirstTokenMs
		cloned.FirstTokenMs = &value
	}
	if trace.EndToEndFirstTokenMs != nil {
		value := *trace.EndToEndFirstTokenMs
		cloned.EndToEndFirstTokenMs = &value
	}
	return cloned
}

type schedulerSessionAccumulator struct {
	session        OpenAISchedulerObservabilitySession
	sticky         int
	stickyDetected int
	cacheRead      int64
	cacheEligible  int64
}

func buildSchedulerSessions(traces []OpenAISchedulerObservabilityTrace) []OpenAISchedulerObservabilitySession {
	byKey := make(map[string]*schedulerSessionAccumulator)
	chronological := append([]OpenAISchedulerObservabilityTrace(nil), traces...)
	sort.SliceStable(chronological, func(i, j int) bool {
		if chronological[i].CreatedAt != chronological[j].CreatedAt {
			return chronological[i].CreatedAt < chronological[j].CreatedAt
		}
		leftTurn, rightTurn := 0, 0
		if chronological[i].SessionTurn != nil {
			leftTurn = *chronological[i].SessionTurn
		}
		if chronological[j].SessionTurn != nil {
			rightTurn = *chronological[j].SessionTurn
		}
		return leftTurn < rightTurn
	})
	for _, trace := range chronological {
		if trace.SessionFingerprint == nil || len(trace.AccountPath) == 0 {
			continue
		}
		key := fmt.Sprintf("%d:%d:%s", trace.UserID, trace.GroupID, *trace.SessionFingerprint)
		acc := byKey[key]
		if acc == nil {
			acc = &schedulerSessionAccumulator{session: OpenAISchedulerObservabilitySession{
				Fingerprint:  *trace.SessionFingerprint,
				Source:       trace.SessionSource,
				UserID:       trace.UserID,
				UserEmail:    trace.UserEmail,
				APIKeyName:   trace.APIKeyName,
				GroupID:      trace.GroupID,
				GroupName:    trace.GroupName,
				Model:        trace.Model,
				AccountNames: make(map[int64]string),
			}}
			byKey[key] = acc
		}
		finalAccountID := trace.AccountPath[len(trace.AccountPath)-1].ID
		finalAccountName := trace.AccountPath[len(trace.AccountPath)-1].Name
		acc.session.AccountNames[finalAccountID] = schedulerObservabilityOptionName(finalAccountName, finalAccountID)
		if len(acc.session.TurnAccounts) > 0 && acc.session.TurnAccounts[len(acc.session.TurnAccounts)-1] != finalAccountID {
			acc.session.SwitchCount++
		}
		acc.session.TurnAccounts = append(acc.session.TurnAccounts, finalAccountID)
		if !containsInt64(acc.session.AccountIDs, finalAccountID) {
			acc.session.AccountIDs = append(acc.session.AccountIDs, finalAccountID)
		}
		acc.session.Turns++
		if trace.StickyHit {
			acc.sticky++
		}
		if trace.StickyDetected {
			acc.stickyDetected++
		}
		if acc.session.Turns > 1 {
			acc.cacheRead += trace.CacheReadTokens
			acc.cacheEligible += trace.CacheEligibleTokens
		}
		acc.session.LastActiveAt = trace.CreatedAt
		acc.session.Model = trace.Model
		acc.session.APIKeyName = trace.APIKeyName
	}
	sessions := make([]OpenAISchedulerObservabilitySession, 0, len(byKey))
	for _, acc := range byKey {
		if acc.stickyDetected > 0 {
			acc.session.StickyHitRate = float64(acc.sticky) / float64(acc.stickyDetected)
		}
		if acc.cacheEligible > 0 {
			acc.session.FollowUpCacheRate = float64(acc.cacheRead) / float64(acc.cacheEligible)
		}
		sessions = append(sessions, acc.session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].LastActiveAt > sessions[j].LastActiveAt })
	return sessions
}

func buildSchedulerMetrics(traces []OpenAISchedulerObservabilityTrace, sessions []OpenAISchedulerObservabilitySession) (OpenAISchedulerObservabilityMetrics, []OpenAISchedulerObservabilityReason) {
	metrics := OpenAISchedulerObservabilityMetrics{Requests: len(traces), Sessions: len(sessions)}
	reasonCounts := make(map[string]int)
	for _, trace := range traces {
		if trace.StickyDetected {
			metrics.StickyDetectedRequests++
		}
		if trace.StickyHit {
			metrics.StickyRequests++
		}
		if trace.SwitchCount > 0 {
			metrics.SwitchedRequests++
			metrics.Switches += trace.SwitchCount
		}
		metrics.CacheReadTokens += trace.CacheReadTokens
		metrics.CacheEligibleTokens += trace.CacheEligibleTokens
		for _, attempt := range trace.Attempts {
			if attempt.Kind != "sticky_escape" && attempt.Kind != "upstream_failure" {
				continue
			}
			reason := strings.TrimSpace(attempt.Reason)
			if reason == "" || reason == "no_available_account" {
				continue
			}
			reasonCounts[reason]++
		}
	}
	for _, session := range sessions {
		if session.SwitchCount == 0 {
			metrics.StableSessions++
		}
	}
	if metrics.Requests > 0 {
		metrics.SwitchRate = float64(metrics.SwitchedRequests) / float64(metrics.Requests)
	}
	if metrics.StickyDetectedRequests > 0 {
		metrics.StickyHitRate = float64(metrics.StickyRequests) / float64(metrics.StickyDetectedRequests)
	}
	if metrics.Sessions > 0 {
		metrics.SessionStability = float64(metrics.StableSessions) / float64(metrics.Sessions)
	}
	if metrics.CacheEligibleTokens > 0 {
		metrics.FollowUpCacheRate = float64(metrics.CacheReadTokens) / float64(metrics.CacheEligibleTokens)
	}
	reasons := make([]OpenAISchedulerObservabilityReason, 0, len(reasonCounts))
	for key, count := range reasonCounts {
		reasons = append(reasons, OpenAISchedulerObservabilityReason{Key: key, Count: count})
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].Count != reasons[j].Count {
			return reasons[i].Count > reasons[j].Count
		}
		return reasons[i].Key < reasons[j].Key
	})
	return metrics, reasons
}
