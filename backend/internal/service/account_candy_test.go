//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type candyAccountRepo struct {
	AccountRepository
	account     *Account
	saveErr     error
	writes      int
	lastUpdates map[string]any
}

func (r *candyAccountRepo) GetByID(context.Context, int64) (*Account, error) { return r.account, nil }
func (r *candyAccountRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.writes++
	r.lastUpdates = updates
	if r.account.Extra == nil {
		r.account.Extra = map[string]any{}
	}
	// Match the real repository's JSON round trip and merge behavior.
	b, _ := json.Marshal(updates)
	var saved map[string]any
	_ = json.Unmarshal(b, &saved)
	for k, v := range saved {
		r.account.Extra[k] = v
	}
	return nil
}
func candyAccount() *Account {
	return &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "test-token"},
		Extra: map[string]any{"keep": "unchanged"}}
}
func candyResponse(text string) *http.Response {
	chunk, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "delta": text})
	return newJSONResponse(200, "data: "+string(chunk)+"\n\ndata: {\"type\":\"response.completed\"}\n\n")
}

func TestCandyAnswerPassed(t *testing.T) {
	for _, answer := range []string{"21", "答案是21个", "**21**", "答案：\n21。", "不是 34，最少 21", "121 或 21", "21不是正确答案"} {
		t.Run(answer, func(t *testing.T) { require.True(t, candyAnswerPassed(answer)) })
	}
	for _, answer := range []string{"", "34", "121", "210", "2021", "２21", "21٣", "二十一", "2 1"} {
		t.Run(answer, func(t *testing.T) { require.False(t, candyAnswerPassed(answer)) })
	}
}

func TestRunCandyTestPersistsActualResultsAndExactModel(t *testing.T) {
	account := candyAccount()
	account.Credentials["model_mapping"] = map[string]any{"gpt-6.2-sol": "gpt-5.4"}
	repo := &candyAccountRepo{account: account}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("最少21个。"), candyResponse("34"), newJSONResponse(500, "error 21 with private-token")}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	for _, status := range []string{"passed", "failed", "error"} {
		result, err := svc.RunCandyTest(context.Background(), account.ID, 0)
		require.NoError(t, err)
		require.Equal(t, status, result.Recent[len(result.Recent)-1].Status)
	}
	result, err := readCandyTestSnapshot(account.Extra)
	require.NoError(t, err)
	require.Equal(t, 3, result.Total)
	require.Equal(t, 1, result.Passed)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, 1, result.Errors)
	require.Equal(t, defaultCandyTestModel, result.Recent[0].Model)
	require.True(t, account.Schedulable)
	require.Equal(t, "unchanged", account.Extra["keep"])
	persisted, _ := json.Marshal(result)
	require.NotContains(t, string(persisted), "private-token")
	require.Equal(t, "最少21个。", result.Recent[0].ResponseText)
	require.Equal(t, "34", result.Recent[1].ResponseText)
	require.Empty(t, result.Recent[2].ResponseText)
	require.False(t, result.Recent[0].ResponseTruncated)
	body, err := io.ReadAll(upstream.requests[0].Body)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.2-sol", gjson.GetBytes(body, "model").String())
	require.Equal(t, candyTestPrompt, gjson.GetBytes(body, "input.0.content.0.text").String())
	require.Equal(t, "medium", gjson.GetBytes(body, "reasoning.effort").String())
	require.False(t, gjson.GetBytes(body, "tools").Exists())
	require.NotContains(t, candyTestPrompt, "21")
}

func TestRunCandyTestRejectsUnsupportedAccounts(t *testing.T) {
	for _, pair := range [][2]string{{PlatformAnthropic, AccountTypeAPIKey}, {PlatformOpenAI, AccountTypeSetupToken}, {PlatformAnthropic, AccountTypeOAuth}, {PlatformGemini, AccountTypeOAuth}, {PlatformAntigravity, AccountTypeOAuth}} {
		t.Run(pair[0]+pair[1], func(t *testing.T) {
			account := candyAccount()
			account.Platform = pair[0]
			account.Type = pair[1]
			repo := &candyAccountRepo{account: account}
			svc := &AccountTestService{accountRepo: repo}
			_, err := svc.RunCandyTest(context.Background(), account.ID, 0)
			require.True(t, infraerrors.IsBadRequest(err))
			require.Zero(t, repo.writes)
		})
	}
}

func TestRunCandyTestIncompleteOrEmptyResponseIsAPIError(t *testing.T) {
	for _, body := range []string{"data: {\"type\":\"response.output_text.delta\",\"delta\":\"21\"}\n\n", "data: {\"type\":\"response.completed\"}\n\n"} {
		repo := &candyAccountRepo{account: candyAccount()}
		svc := &AccountTestService{accountRepo: repo, httpUpstream: &queuedHTTPUpstream{responses: []*http.Response{newJSONResponse(200, body)}}}
		got, err := svc.RunCandyTest(context.Background(), 42, 0)
		require.NoError(t, err)
		require.Equal(t, 1, got.Errors)
		require.Zero(t, got.Passed)
		require.Empty(t, got.Recent[0].ResponseText, "incomplete answers must not be presented as completed replies")
	}
}

func TestRunCandyTestSaveFailureAndDuplicate(t *testing.T) {
	repo := &candyAccountRepo{account: candyAccount(), saveErr: errors.New("database unavailable")}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: &queuedHTTPUpstream{responses: []*http.Response{candyResponse("21")}}}
	got, err := svc.RunCandyTest(context.Background(), 42, 0)
	require.ErrorContains(t, err, "save candy test history")
	require.Nil(t, got)
	svc.candyTestsInFlight.Store(int64(42), struct{}{})
	_, err = svc.RunCandyTest(context.Background(), 42, 0)
	require.True(t, infraerrors.IsConflict(err))
}

func TestCandyHistoryIsBoundedAndCountersSurviveJSON(t *testing.T) {
	result := CandyTestSnapshot{}
	for i := 0; i < 30; i++ {
		appendCandyTestSample(&result, CandyTestSample{Status: "passed", CheckedAt: time.Unix(int64(i), 0)})
	}
	require.Equal(t, 30, result.Total)
	require.Len(t, result.Recent, 12)
	require.Equal(t, time.Unix(18, 0), result.Recent[0].CheckedAt)
	got, err := readCandyTestSnapshot(map[string]any{CandyTestExtraKey: result})
	require.NoError(t, err)
	require.Equal(t, 30, got.Passed)
}

func TestCandyTestGroupModelResolution(t *testing.T) {
	account := candyAccount()
	model, err := resolveCandyTestConfig(account, 0)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.2-sol", model.Model)
	account.Groups = []*Group{{ID: 1, Platform: PlatformOpenAI, CandyTestModel: " gpt-6.1-sol "}}
	model, err = resolveCandyTestConfig(account, 0)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", model.Model)
	account.Groups = append(account.Groups, &Group{ID: 2, Platform: PlatformOpenAI})
	_, err = resolveCandyTestConfig(account, 0)
	require.True(t, infraerrors.IsBadRequest(err))
	model, err = resolveCandyTestConfig(account, 1)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", model.Model)
	model, err = resolveCandyTestConfig(account, 2)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.2-sol", model.Model)
	_, err = resolveCandyTestConfig(account, 99)
	require.True(t, infraerrors.IsBadRequest(err))
	account.Groups[1].CandyTestModel = "gpt-6.1-sol"
	model, err = resolveCandyTestConfig(account, 0)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", model.Model)
}

func TestCandyTestUsesSelectedGroupModel(t *testing.T) {
	account := candyAccount()
	account.Groups = []*Group{{ID: 7, Platform: PlatformOpenAI, CandyTestModel: "gpt-6.1-sol"}}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("21")}}
	svc := &AccountTestService{accountRepo: &candyAccountRepo{account: account}, httpUpstream: upstream}
	got, err := svc.RunCandyTest(context.Background(), 42, 7)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", got.Recent[0].Model)
	body, _ := io.ReadAll(upstream.requests[0].Body)
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(body, "model").String())
}

func TestNormalizeCandyTestModel(t *testing.T) {
	model, err := normalizeCandyTestModel(PlatformOpenAI, " gpt-6.2-sol ")
	require.NoError(t, err)
	require.Equal(t, "gpt-6.2-sol", model)
	model, err = normalizeCandyTestModel(PlatformAnthropic, "gpt-6.2-sol")
	require.NoError(t, err)
	require.Empty(t, model)
	for _, bad := range []string{"gpt-6.2\tsol", strings.Repeat("a", 101), "gpt-image-1"} {
		_, err = normalizeCandyTestModel(PlatformOpenAI, bad)
		require.True(t, infraerrors.IsBadRequest(err))
	}
}

func TestRunCandyTestCompletedOnlyAnswer(t *testing.T) {
	repo := &candyAccountRepo{account: candyAccount()}
	body := `data: {"type":"response.completed","response":{"output":[{"type":"reasoning","summary":[{"text":"121"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"21"}]}]}}` + "\n\n"
	svc := &AccountTestService{accountRepo: repo, httpUpstream: &queuedHTTPUpstream{responses: []*http.Response{newJSONResponse(200, body)}}}
	result, err := svc.RunCandyTest(context.Background(), 42, 0)
	require.NoError(t, err)
	require.Equal(t, 1, result.Passed)
}

func TestCandyAnswerDetailsPersistAndGradeFullResponse(t *testing.T) {
	for _, tc := range []struct {
		name, answer, expected, status string
		truncated                      bool
	}{
		{"wrong answer", "最少需要 34 个糖果。", "最少需要 34 个糖果。", "failed", false},
		{"passing answer", "最少需要 21 个糖果。", "最少需要 21 个糖果。", "passed", false},
		{"exact boundary", strings.Repeat("糖", candyTestResponseRuneLimit-2) + "21", strings.Repeat("糖", candyTestResponseRuneLimit-2) + "21", "passed", false},
		{"answer beyond excerpt", strings.Repeat("糖", candyTestResponseRuneLimit) + "21", strings.Repeat("糖", candyTestResponseRuneLimit), "passed", true},
		{"unicode excerpt", strings.Repeat("🍬", candyTestResponseRuneLimit+1), strings.Repeat("🍬", candyTestResponseRuneLimit), "failed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := automaticCandyAccount()
			repo := &candyAccountRepo{account: account}
			svc := &AccountTestService{accountRepo: repo, httpUpstream: &queuedHTTPUpstream{responses: []*http.Response{candyResponse(tc.answer)}}}
			result, err := svc.RunCandyTest(context.Background(), account.ID, 0)
			require.NoError(t, err)
			sample := result.Recent[0]
			require.Equal(t, tc.status, sample.Status)
			require.Equal(t, tc.expected, sample.ResponseText)
			require.Equal(t, tc.truncated, sample.ResponseTruncated)
			require.True(t, utf8.ValidString(sample.ResponseText))
			require.LessOrEqual(t, utf8.RuneCountInString(sample.ResponseText), candyTestResponseRuneLimit)
			require.False(t, account.IsCandyTestBlocked(), "one wrong answer must not block scheduling")
			reloaded, err := readCandyTestSnapshot(account.Extra)
			require.NoError(t, err)
			require.Equal(t, sample.ResponseText, reloaded.Recent[0].ResponseText)
			require.Equal(t, sample.ResponseTruncated, reloaded.Recent[0].ResponseTruncated)
		})
	}
}

func TestCandyAnswerDetailsRedactSensitiveFieldsBeforePersisting(t *testing.T) {
	account := candyAccount()
	repo := &candyAccountRepo{account: account}
	answer := `{"answer":"34","access_token":"private-token","authorization":"Bearer private-auth","api_key":"private-key"}`
	svc := &AccountTestService{accountRepo: repo, httpUpstream: &queuedHTTPUpstream{responses: []*http.Response{candyResponse(answer)}}}
	result, err := svc.RunCandyTest(context.Background(), account.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "failed", result.Recent[0].Status)
	require.Contains(t, result.Recent[0].ResponseText, `"answer":"34"`)
	persisted, err := json.Marshal(account.Extra)
	require.NoError(t, err)
	for _, secret := range []string{"private-token", "private-auth", "private-key"} {
		require.NotContains(t, string(persisted), secret)
		require.NotContains(t, result.Recent[0].ResponseText, secret)
	}
}

func TestCandyAnswerDetailsDoNotFabricateLegacyAnswers(t *testing.T) {
	legacy := map[string]any{CandyTestExtraKey: map[string]any{
		"total": 1, "passed": 0, "failed": 1, "errors": 0,
		"recent": []any{map[string]any{"status": "failed", "model": "gpt-6.2-sol"}},
	}}
	snapshot, err := readCandyTestSnapshot(legacy)
	require.NoError(t, err)
	require.Equal(t, "failed", snapshot.LastVerdict)
	require.Empty(t, snapshot.Recent[0].ResponseText)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "response_text")
	require.NotContains(t, string(encoded), "response_truncated")
}

// Assert the actual outbound request, not only the internal options, for both
// candy entry points. Ordinary connectivity probes must retain their defaults.
func TestCandyTestMediumReasoningIsScopedToDiagnostics(t *testing.T) {
	for _, mode := range []string{"manual", "scheduled", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			account := automaticCandyAccount()
			upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("21")}}
			svc := &AccountTestService{accountRepo: &candyAccountRepo{account: account}, httpUpstream: upstream}
			ctx := context.Background()
			switch mode {
			case "manual":
				result, err := svc.RunCandyTest(ctx, account.ID, 7)
				require.NoError(t, err)
				require.Equal(t, 1, result.Passed)
			case "scheduled":
				result, err := svc.RunScheduledCandyTest(ctx, account.ID, 7)
				require.NoError(t, err)
				require.Equal(t, 1, result.Passed)
			case "ordinary":
				result, err := svc.runTestBackgroundWithPrompt(ctx, account.ID, defaultCandyTestModel, "hi", false)
				require.NoError(t, err)
				require.Equal(t, "success", result.Status)
			}
			require.Len(t, upstream.requests, 1)
			body, err := io.ReadAll(upstream.requests[0].Body)
			require.NoError(t, err)
			if mode == "ordinary" {
				require.False(t, gjson.GetBytes(body, "reasoning").Exists())
			} else {
				require.Equal(t, "medium", gjson.GetBytes(body, "reasoning.effort").String())
			}
		})
	}
}

func TestCandyTestUnsupportedMediumIsAPIErrorNotWrongAnswer(t *testing.T) {
	account := candyAccount()
	upstream := &queuedHTTPUpstream{responses: []*http.Response{newJSONResponse(400, `{"error":{"message":"Unsupported reasoning effort: medium"}}`)}}
	svc := &AccountTestService{accountRepo: &candyAccountRepo{account: account}, httpUpstream: upstream}
	result, err := svc.RunCandyTest(context.Background(), account.ID, 0)
	require.NoError(t, err)
	require.Equal(t, 1, result.Errors)
	require.Zero(t, result.Failed)
	require.False(t, account.IsCandyTestBlocked())
	require.True(t, account.Schedulable)
	require.Len(t, upstream.requests, 1, "do not silently retry with lower reasoning")
}

func TestRunCandyTestDoesNotRestoreExpiredHistory(t *testing.T) {
	account := candyAccount()
	old := time.Now().Add(-48 * time.Hour)
	account.Extra[CandyTestExtraKey] = CandyTestSnapshot{
		Total: 2, Failed: 2, LastVerdict: "failed", ConsecutiveFailures: 2,
		Recent: []CandyTestSample{{Status: "failed", CheckedAt: old, ResponseText: "expired answer"}},
	}
	repo := &candyAccountRepo{account: account}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: &queuedHTTPUpstream{responses: []*http.Response{newJSONResponse(500, "failed")}}}
	result, err := svc.RunCandyTest(context.Background(), account.ID, 0)
	require.NoError(t, err)
	require.Len(t, result.Recent, 1)
	require.Equal(t, "error", result.Recent[0].Status)
	require.Equal(t, 3, result.Total)
	require.Equal(t, 2, result.ConsecutiveFailures, "expiration cannot reset the scheduling gate")
	require.True(t, account.IsCandyTestBlocked())
	raw, err := json.Marshal(repo.lastUpdates)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "expired answer")
}
