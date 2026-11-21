package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/google/uuid"
)

const (
	defaultCandyTestModel = "gpt-6.2-sol"
	CandyTestExtraKey     = "candy_test"
	// Keep this separate from diagnostic history so only eligibility transitions
	// invalidate scheduler pools. The manual Schedulable setting is never changed.
	CandyTestBlockedExtraKey   = "candy_test_blocked"
	candyTestHistoryLimit      = 12
	candyTestResponseRuneLimit = 4096
	candyTestTimeout           = 90 * time.Second
	// The answer is deliberately NOT included in the upstream prompt.
	candyTestPrompt = `不使用任何外部工具回答以下问题：

在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）

| 形状 | 苹果味 | 桃子味 | 西瓜味 |
| --- | --- | --- | --- |
| 圆形 | 7 | 9 | 8 |
| 五角星 | 7 | 6 | 4 |`
)

type CandyTestSample struct {
	Status    string    `json:"status"`
	Model     string    `json:"model"`
	CheckedAt time.Time `json:"checked_at"`
	LatencyMS int64     `json:"latency_ms"`
	// Final visible answer only, redacted and bounded; never raw HTTP/error bodies.
	ResponseText      string `json:"response_text,omitempty"`
	ResponseTruncated bool   `json:"response_truncated,omitempty"`
}

type CandyTestSchedule struct {
	Model     string    `json:"model"`
	CheckedAt time.Time `json:"checked_at"`
}

// CandyTestSnapshot keeps bounded diagnostic history and final-answer excerpts.
// Total includes API errors; those are not wrong answers.
type CandyTestSnapshot struct {
	// Errors are inconclusive; only a complete answer can change this verdict.
	LastVerdict string                      `json:"last_verdict,omitempty"`
	Total       int                         `json:"total"`
	Passed      int                         `json:"passed"`
	Failed      int                         `json:"failed"`
	Errors      int                         `json:"errors"`
	Recent      []CandyTestSample           `json:"recent"`
	Schedules   map[int64]CandyTestSchedule `json:"schedules,omitempty"`
}

// Match the requested rule: a literal 21 with no adjacent digit. Do not inspect
// reasoning, errors, model names, or usage counters when grading the answer.
func candyAnswerPassed(answer string) bool {
	runes := []rune(answer)
	for i := 0; i+1 < len(runes); i++ {
		if runes[i] == '2' && runes[i+1] == '1' &&
			(i == 0 || !unicode.IsDigit(runes[i-1])) &&
			(i+2 == len(runes) || !unicode.IsDigit(runes[i+2])) {
			return true
		}
	}
	return false
}

// Redact before truncation so a clipped secret field cannot evade redaction.
// Grading uses the full original answer, never this display-only excerpt.
func candyTestResponseExcerpt(answer string) (string, bool) {
	answer = logredact.RedactText(answer, "api_key", "authorization")
	count := 0
	for offset := range answer {
		if count == candyTestResponseRuneLimit {
			return answer[:offset], true
		}
		count++
	}
	return answer, false
}

func readCandyTestSnapshot(extra map[string]any) (CandyTestSnapshot, error) {
	snapshot := CandyTestSnapshot{Recent: []CandyTestSample{}}
	if extra[CandyTestExtraKey] == nil {
		return snapshot, nil
	}
	data, err := json.Marshal(extra[CandyTestExtraKey])
	if err != nil {
		return snapshot, err
	}
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, err
	}
	if snapshot.Total < 0 || snapshot.Passed < 0 || snapshot.Failed < 0 || snapshot.Errors < 0 ||
		snapshot.Total != snapshot.Passed+snapshot.Failed+snapshot.Errors {
		return snapshot, fmt.Errorf("invalid candy test counters")
	}
	// Upgrade old snapshots from the most recent conclusive answer, not counters.
	if snapshot.LastVerdict != "passed" && snapshot.LastVerdict != "failed" {
		snapshot.LastVerdict = ""
		for i := len(snapshot.Recent) - 1; i >= 0; i-- {
			if snapshot.Recent[i].Status == "passed" || snapshot.Recent[i].Status == "failed" {
				snapshot.LastVerdict = snapshot.Recent[i].Status
				break
			}
		}
	}
	return snapshot, nil
}

// IsCandyTestBlocked deliberately does not conflate API failures with wrong
// answers. Legacy snapshots without a dedicated flag use their latest verdict.
func (a *Account) IsCandyTestBlocked() bool {
	if a == nil || a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth {
		return false
	}
	if blocked, ok := a.Extra[CandyTestBlockedExtraKey].(bool); ok {
		return blocked
	}
	if a.Extra[CandyTestExtraKey] == nil {
		return false
	}
	snapshot, err := readCandyTestSnapshot(a.Extra)
	return err == nil && snapshot.LastVerdict == "failed"
}

func appendCandyTestSample(snapshot *CandyTestSnapshot, sample CandyTestSample) {
	snapshot.Total++
	switch sample.Status {
	case "passed":
		snapshot.LastVerdict = "passed"
		snapshot.Passed++
	case "failed":
		snapshot.LastVerdict = "failed"
		snapshot.Failed++
	default:
		snapshot.Errors++
	}
	snapshot.Recent = append(snapshot.Recent, sample)
	if len(snapshot.Recent) > candyTestHistoryLimit {
		snapshot.Recent = snapshot.Recent[len(snapshot.Recent)-candyTestHistoryLimit:]
	}
}

// SetCandyTestLeaderLock coordinates manual and automatic checks across instances.
func (s *AccountTestService) SetCandyTestLeaderLock(cache LeaderLockCache, db *sql.DB) {
	s.candyTestLockCache, s.candyTestLockDB = cache, db
}

// RunScheduledCandyTest reloads configuration and persisted due time under the
// same per-account lock as manual tests. Stale queued work cannot run a disabled
// or removed group, or double-charge when a peer has just completed a check.
func (s *AccountTestService) RunScheduledCandyTest(ctx context.Context, accountID, groupID int64) (*CandyTestSnapshot, error) {
	return s.runCandyTest(ctx, accountID, groupID, true)
}

// RunCandyTest performs one manual test without changing the scheduler switch.
func (s *AccountTestService) RunCandyTest(ctx context.Context, accountID int64, groupID int64) (*CandyTestSnapshot, error) {
	return s.runCandyTest(ctx, accountID, groupID, false)
}

func (s *AccountTestService) runCandyTest(ctx context.Context, accountID int64, groupID int64, automatic bool) (*CandyTestSnapshot, error) {
	ctx, cancelRun := context.WithTimeout(ctx, 100*time.Second)
	defer cancelRun()
	if _, running := s.candyTestsInFlight.LoadOrStore(accountID, struct{}{}); running {
		return nil, infraerrors.Conflict("CANDY_TEST_RUNNING", "A candy test is already running for this account")
	}
	defer s.candyTestsInFlight.Delete(accountID)
	lockCtx, cancelLock := context.WithTimeout(ctx, 2*time.Second)
	release, acquired := tryAcquireSingletonLeaderLock(lockCtx, s.candyTestLockCache, s.candyTestLockDB,
		fmt.Sprintf("candy-test:account:%d", accountID), uuid.NewString(), 2*time.Minute)
	cancelLock()
	if !acquired {
		return nil, infraerrors.Conflict("CANDY_TEST_RUNNING", "A candy test is already running for this account")
	}
	defer release()

	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.IsSyntheticUITest() {
		return nil, infraerrors.BadRequest("CANDY_TEST_OAUTH_ONLY", "Candy tests require a real OpenAI OAuth account")
	}
	var scheduledGroup *Group
	if automatic {
		if !candyAutomaticAccountEligible(account, time.Now()) {
			return nil, nil
		}
		for _, group := range account.Groups {
			if group != nil && group.ID == groupID && group.Platform == PlatformOpenAI && group.IsActive() && group.CandyTestIntervalSeconds > 0 {
				scheduledGroup = group
				break
			}
		}
		if scheduledGroup == nil {
			return nil, nil
		}
	}
	model, err := resolveCandyTestModel(account, groupID)
	if err != nil {
		return nil, err
	}
	snapshot, err := readCandyTestSnapshot(account.Extra)
	if err != nil {
		return nil, fmt.Errorf("read candy test history: %w", err)
	}

	if automatic && candyScheduleDueAt(snapshot, scheduledGroup).After(time.Now()) {
		return nil, nil
	}

	testCtx, cancel := context.WithTimeout(ctx, candyTestTimeout)
	defer cancel()
	result, err := s.runTestBackgroundWithPrompt(testCtx, accountID, model, candyTestPrompt, false, AccountTestOptions{ExactModel: true, PreserveAccountState: true})
	if err != nil {
		return nil, err
	}
	// A canceled client must not be recorded as a model failure.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	sample := CandyTestSample{Status: "error", Model: firstNonEmptyString(result.UpstreamModel, model),
		CheckedAt: result.FinishedAt, LatencyMS: result.LatencyMs}
	if result.Status == "success" && strings.TrimSpace(result.ResponseText) != "" {
		sample.Status = "failed"
		if candyAnswerPassed(result.ResponseText) {
			sample.Status = "passed"
		}
		sample.ResponseText, sample.ResponseTruncated = candyTestResponseExcerpt(result.ResponseText)
	}
	appendCandyTestSample(&snapshot, sample)
	if groupID > 0 {
		if snapshot.Schedules == nil {
			snapshot.Schedules = make(map[int64]CandyTestSchedule)
		}
		snapshot.Schedules[groupID] = CandyTestSchedule{Model: model, CheckedAt: sample.CheckedAt}
	}
	// Drop obsolete bindings; persisted schedule size stays bounded by memberships.
	attached := make(map[int64]bool, len(account.Groups))
	for _, group := range account.Groups {
		if group != nil && group.Platform == PlatformOpenAI {
			attached[group.ID] = true
		}
	}
	for id := range snapshot.Schedules {
		if !attached[id] {
			delete(snapshot.Schedules, id)
		}
	}

	// Use the parent context so a probe timeout can still be persisted. Never
	// swallow a database error and report an unpersisted success to the client.
	updates := map[string]any{CandyTestExtraKey: snapshot}
	blocked := snapshot.LastVerdict == "failed"
	current, recorded := account.Extra[CandyTestBlockedExtraKey].(bool)
	if !recorded || current != blocked {
		// One atomic extra write publishes the verdict and the scheduler gate. The
		// non-neutral flag creates an outbox event and refreshes cached eligibility.
		updates[CandyTestBlockedExtraKey] = blocked
	}
	if err := s.accountRepo.UpdateExtra(ctx, accountID, updates); err != nil {
		return nil, fmt.Errorf("save candy test history: %w", err)
	}
	return &snapshot, nil
}
