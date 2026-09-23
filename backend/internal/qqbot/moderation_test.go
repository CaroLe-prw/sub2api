package qqbot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const supplierAd = "兄弟们，GPT 官方key，100并发左右，找稳定上游，倍率能给0.22以下的来私聊"

func TestQQAdRulesMatchExamplesWithoutGenericKeywordBans(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{supplierAd, "advertisement"},
		{"火爆AI OPC创业项目，ai算力，ai token词元超市，ai工具超市，ai短剧漫剧制作及海外发行平台。支持代理贴牌。", "advertisement"},
		{"挖 矿 ， 好 玩 还 能 赚 现 金 ! 长 按 图 片 识 别 关注 首 月 可 挖 300-1000 元", "advertisement"},
		{"GPT 的 100 并发和倍率 0.22 是什么意思？", "allowed"},
		{"这是登录二维码，请扫码登录", "allowed"},
		{"AI 算力和 token 价格讨论", "allowed"},
		{"GPT 调用报错，联系我看看", "suspected"},
		{"有人发广告：" + supplierAd, "suspected"},
		{"不要相信这个挖矿赚钱广告，不要扫码", "suspected"},
		{"渠道状态 OpenAI", "allowed"},
	} {
		t.Run(tc.text, func(t *testing.T) { require.Equal(t, tc.want, EvaluateAdText(tc.text).Verdict) })
	}
}

type moderationTestGuard struct{ testGuard }

func (g *moderationTestGuard) ClaimModeration(ctx context.Context, id string) (bool, error) {
	return g.Claim(ctx, "moderation:"+id, "", false)
}

type moderationTestSource struct {
	testSource
	records []ModerationRecord
}

func (s *moderationTestSource) ReviewMessage(_ context.Context, _ Config, msg Message) (AdDecision, error) {
	return EvaluateAdText(msg.Content), nil
}
func (s *moderationTestSource) RecordModeration(_ context.Context, record ModerationRecord) error {
	s.records = append(s.records, record)
	return nil
}

func TestQQModerationPermissionsDeduplicationAndRecall(t *testing.T) {
	for _, tc := range []struct {
		name, role, text, group         string
		enabled, observe, trusted, fail bool
		deletes, records                int
	}{
		{"clear", "member", supplierAd, "g", true, false, false, false, 1, 1},
		{"observe", "member", supplierAd, "g", true, true, false, false, 0, 1},
		{"suspected", "member", "GPT 联系我排查", "g", true, false, false, false, 0, 1},
		{"ordinary", "member", "GPT 并发是什么意思", "g", true, false, false, false, 0, 0},
		{"owner", "owner", supplierAd, "g", true, false, false, false, 0, 0},
		{"admin", "admin", supplierAd, "g", true, false, false, false, 0, 0},
		{"unknown-role", "", supplierAd, "g", true, false, false, false, 0, 1},
		{"trusted", "member", supplierAd, "g", true, false, true, false, 0, 0},
		{"other-group", "member", supplierAd, "other", true, false, false, false, 0, 0},
		{"disabled", "member", supplierAd, "g", false, false, false, false, 0, 0},
		{"no-permission", "member", supplierAd, "g", true, false, false, true, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{AppID: "app", Groups: []string{"g"}, Moderation: ModerationConfig{Enabled: tc.enabled, ObserveOnly: tc.observe}}
			if tc.trusted {
				cfg.Moderation.TrustedMembers = []string{"sender"}
			}
			source := &moderationTestSource{}
			guard := &moderationTestGuard{testGuard{seen: map[string]bool{}}}
			r := New(cfg, "secret", source, guard, nil)
			deletes := 0
			r.api.http.Transport = mediaTransport(func(req *http.Request) (*http.Response, error) {
				body := `{"access_token":"token","expires_in":7200}`
				if req.Method == http.MethodDelete {
					require.Equal(t, "/v2/groups/g/messages/ad-id", req.URL.Path)
					deletes++
					body = ""
					if tc.fail {
						body = `{"code":40062003}`
					}
				} else {
					require.Equal(t, "/app/getAppAccessToken", req.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			msg := Message{ID: "ad-id", Group: tc.group, Content: tc.text, Timestamp: time.Now()}
			msg.Author.ID = "sender"
			msg.Author.Role = tc.role
			data, err := json.Marshal(msg)
			require.NoError(t, err)
			// Moderation must work even when the unrelated no-mention query toggle is off.
			require.NoError(t, r.dispatchGroupEvent(context.Background(), payload{Type: "GROUP_MESSAGE_CREATE", Data: data}))
			r.wg.Wait()
			require.NoError(t, r.dispatchGroupEvent(context.Background(), payload{Type: "GROUP_AT_MESSAGE_CREATE", Data: data}))
			r.wg.Wait()
			require.Equal(t, tc.deletes, deletes)
			require.Len(t, source.records, tc.records)
			if tc.records > 0 {
				if tc.fail {
					require.Equal(t, "failed", source.records[0].Action)
				} else if tc.deletes > 0 {
					require.Equal(t, "recalled", source.records[0].Action)
				} else {
					require.Equal(t, "recorded", source.records[0].Action)
				}
			}
		})
	}
}
