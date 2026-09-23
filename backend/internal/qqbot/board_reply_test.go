package qqbot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQQBoardMentionUsesMarkdownAndPreservesImageBudget(t *testing.T) {
	for _, failMention := range []bool{false, true} {
		t.Run(map[bool]string{false: "mention-and-images", true: "mention-failure-still-sends-images"}[failMention], func(t *testing.T) {
			var messages []map[string]any
			var status Status
			q := &client{baseURL: "https://api.bot.qq.com", http: httpClient()}
			q.http.Transport = mediaTransport(func(req *http.Request) (*http.Response, error) {
				result := "{}"
				if req.URL.Host != "storage.qq.com" {
					var body map[string]any
					require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
					switch req.URL.Path {
					case "/app/getAppAccessToken":
						result = `{"access_token":"token","expires_in":7200}`
					case "/v2/groups/group-a/upload_prepare":
						result = `{"upload_id":"u","block_size":5,"parts":[{"index":1,"presigned_url":"https://storage.qq.com/image","block_size":5}]}`
					case "/v2/groups/group-a/upload_part_finish":
					case "/v2/groups/group-a/files":
						require.Equal(t, false, body["srv_send_msg"])
						result = `{"file_info":"file-info"}`
					case "/v2/groups/group-a/messages":
						messages = append(messages, body)
						result = `{"id":"reply"}`
						if failMention && len(messages) == 1 {
							result = `{"code":40034001}`
						}
					default:
						t.Fatalf("unexpected API request: %s", req.URL.Path)
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(result))}, nil
			})
			r := &Runtime{api: q, report: func(s Status) { status = s }}
			msg := Message{ID: "request-message", Group: "group-a"}
			msg.Author.ID = "requesting-member"
			r.sendBoardImages(context.Background(), msg, Board{}, [][]byte{[]byte("image"), []byte("image"), []byte("image"), []byte("image")})
			require.Len(t, messages, 5, "one mention + four images must fit QQ's passive reply limit")
			for i, message := range messages {
				require.Equal(t, "request-message", message["msg_id"])
				require.EqualValues(t, i+1, message["msg_seq"])
				require.NotContains(t, message, "content", "never put mention markup in plain/native-image text")
				if i == 0 {
					require.EqualValues(t, 2, message["msg_type"])
					markdown, ok := message["markdown"].(map[string]any)
					require.True(t, ok)
					require.Equal(t, `<qqbot-at-user id="requesting-member" /> 已为你查询渠道状态，见下方图片。`, markdown["content"])
				} else {
					require.EqualValues(t, 7, message["msg_type"])
					require.NotContains(t, message, "markdown")
				}
			}
			if failMention {
				require.Equal(t, "mention_error", status.State)
			} else {
				require.Equal(t, "online", status.State)
			}
		})
	}
}

func TestQQBoardMentionRejectsInvalidRecipient(t *testing.T) {
	q := &client{http: httpClient()}
	q.http.Transport = mediaTransport(func(_ *http.Request) (*http.Response, error) {
		t.Fatal("invalid recipients must not send requests")
		return nil, nil
	})
	for _, invalid := range []string{"", `member" /><qqbot-at-everyone />`, "@everyone", "昵称", strings.Repeat("x", 129)} {
		require.Error(t, q.replyBoardMention(context.Background(), "group", "message", invalid))
	}
}
