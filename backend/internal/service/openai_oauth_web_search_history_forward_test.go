//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// TestForward_OAuthWebSearchHistoryDeclaresTool drives the Codex compaction
// shape (web_search_call history, tools:[]) through Forward and asserts the
// body reaching chatgpt.com declares web_search (#7927): top-level for the
// standard wire, input additional_tools for Responses Lite. Native compaction
// uses the full Responses contract even when the inbound Lite header is set.
func TestForward_OAuthWebSearchHistoryDeclaresTool(t *testing.T) {
	for _, tc := range []struct {
		name        string
		passthrough bool
		lite        bool
		compaction  bool
	}{
		{"transform", false, false, true},
		{"passthrough", true, false, true},
		{"transform lite compaction", false, true, true},
		{"passthrough lite compaction", true, true, true},
		{"transform lite", false, true, false},
		{"passthrough lite", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			s := newAstraOAuthSetup(t, tc.passthrough)
			if tc.lite {
				s.c.Request.Header.Set(responsesLiteHeader, "true")
			}
			inner := `{"id":"resp_test","model":"gpt-5.5","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
			s.upstream.resp = &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(codexCompletedSSE(inner))),
			}
			body := []byte(openAIWebSearchHistoryCompactionBody)
			if tc.compaction {
				var err error
				body, err = sjson.SetRawBytes(body, "input.-1", []byte(`{"type":"compaction_trigger"}`))
				require.NoError(t, err)
			}

			result, err := s.svc.Forward(context.Background(), s.c, s.account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, s.upstream.lastReq)
			require.Equal(t, astraProCodexResponsesURL, s.upstream.lastReq.URL.String())

			forwarded := s.upstream.lastBody
			require.Equal(t, "web_search_call", gjson.GetBytes(forwarded, "input.1.type").String())
			require.Equal(t, "none", gjson.GetBytes(forwarded, "tool_choice").String())
			items := gjson.GetBytes(forwarded, "input").Array()
			if tc.compaction {
				require.Equal(t, "compaction_trigger", items[len(items)-1].Get("type").String())
				require.Empty(t, s.upstream.lastReq.Header.Get(responsesLiteHeader))
			}
			if !tc.lite || tc.compaction {
				tools := gjson.GetBytes(forwarded, "tools").Array()
				require.Len(t, tools, 1)
				require.Equal(t, "web_search", tools[0].Get("type").String())
				return
			}
			require.False(t, gjsonToolsContainWebSearch(gjson.GetBytes(forwarded, "tools")))
			additional := items[len(items)-1]
			require.Equal(t, "additional_tools", additional.Get("type").String())
			require.Equal(t, "web_search", additional.Get("tools.0.type").String())
		})
	}
}
