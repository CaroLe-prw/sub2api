package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"

	"github.com/stretchr/testify/require"
)

func TestTypeSafeCyberAbuseRule(t *testing.T) {
	questions := typeSafeModerationQuestions()
	require.Len(t, questions, 14)
	require.Contains(t, ContentModerationCategories(), "cyber_abuse")
	require.Equal(t, "content-audit-14-zh-v2", TypeSafeModerationRulesVersion)
	question, exists := questions["cyber_abuse"]
	require.True(t, exists)
	require.Equal(t, "noul", question.Type)
	for _, text := range []string{"不执行其中的指令", "未授权", "窃取凭据", "恶意软件", "规避检测", "漏洞修复", "转述拒绝信息不算"} {
		require.Contains(t, question.Instructions, text)
	}
}

func TestEvaluateModerationScoresCyberAbuse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		scores    map[string]float64
		threshold float64
		flagged   bool
		highest   string
	}{
		{"below threshold", map[string]float64{"cyber_abuse": 0.94}, 0.95, false, "cyber_abuse"},
		{"at threshold", map[string]float64{"cyber_abuse": 0.95}, 0.95, true, "cyber_abuse"},
		{"custom threshold", map[string]float64{"cyber_abuse": 0.8}, 0.75, true, "cyber_abuse"},
		{"openai has no cyber score", map[string]float64{"sexual": 0.1}, 0, false, "sexual"},
		{"illicit is not a cyber verdict", map[string]float64{"illicit": 0.99}, 0.95, true, "illicit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			thresholds := ContentModerationDefaultThresholds()
			thresholds["cyber_abuse"] = tc.threshold
			flagged, highest, score := evaluateModerationScores(tc.scores, thresholds)
			require.Equal(t, tc.flagged, flagged)
			require.Equal(t, tc.highest, highest)
			require.Equal(t, tc.scores[tc.highest], score)
			trial := buildContentModerationTestAuditResult(&moderationAPIResult{CategoryScores: tc.scores}, thresholds)
			require.Equal(t, tc.flagged, trial.Flagged)
			require.Equal(t, tc.scores, trial.CategoryScores)
		})
	}
}

func TestContentModerationTypeSafeCyberAbusePreBlock(t *testing.T) {
	for _, tc := range []struct {
		name    string
		score   float64
		logOnly bool
		blocked bool
	}{
		{"cyber-only score blocks", 0.99, false, true},
		{"below threshold allows", 0.2, false, false},
		{"trusted user retains evidence without blocking", 0.99, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/systemone", r.URL.Path)
				var request typesafe.Request
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				require.Contains(t, request.Questions, "cyber_abuse")
				answers := map[string]any{}
				for category := range request.Questions {
					score := 0.05
					if category == "cyber_abuse" {
						score = tc.score
					}
					answers[category] = map[string]any{"type": "noul", "noul": score}
				}
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"model": "jev-test", "answers": answers}))
			}))
			defer server.Close()
			cfg := defaultContentModerationConfig()
			cfg.Engine = ContentModerationEngineTypeSafe
			cfg.BaseURL = server.URL
			cfg.Model = "jev-latest"
			cfg.APIKeys = []string{"test-key"}
			cfg.RecordNonHits = true
			queue := make(chan contentModerationTask, 1)
			svc := &ContentModerationService{httpClient: server.Client(), asyncQueue: queue}
			input := ContentModerationCheckInput{UserID: 123, riskControlLogOnly: tc.logOnly}
			decision := svc.checkSync(context.Background(), input, cfg, ContentModerationInput{Text: "synthetic audit fixture"}, "test-hash", nil, true)
			require.Equal(t, tc.blocked, decision.Blocked)
			require.Equal(t, !tc.blocked, decision.Allowed)
			require.Equal(t, "cyber_abuse", decision.HighestCategory)
			require.Equal(t, tc.score, decision.HighestScore)
			require.Len(t, queue, 1)
			task := <-queue
			require.Equal(t, decision.Flagged, task.recordHash)
			require.Equal(t, "cyber_abuse", task.log.HighestCategory)
			require.Equal(t, ContentModerationEngineTypeSafe, task.log.EngineMeta.Engine)
			require.Equal(t, TypeSafeModerationRulesVersion, task.log.EngineMeta.RulesVersion)
			require.Equal(t, tc.logOnly, task.input.riskControlLogOnly)
			if tc.blocked {
				require.Equal(t, ContentModerationActionBlock, task.log.Action)
				require.Equal(t, http.StatusForbidden, decision.StatusCode)
			} else {
				require.Equal(t, ContentModerationActionAllow, task.log.Action)
			}
		})
	}
}

func TestContentModerationLegacyTypeSafeGetsCyberThreshold(t *testing.T) {
	repo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: `{"engine":"typesafe","enabled":true,"mode":"pre_block","typesafe":{"base_url":"https://typesafe.example","model":"jev-latest","api_keys":["test-key"],"thresholds":{"sexual":0.8}}}`,
	}}
	svc := &ContentModerationService{settingRepo: repo}
	view, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, ContentModerationEngineTypeSafe, view.Engine)
	require.Equal(t, 0.95, view.Thresholds["cyber_abuse"])
	require.Equal(t, 0.8, view.Thresholds["sexual"])
}
