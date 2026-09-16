//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIForwardWrapsSingletonMessageContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, passthrough := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/passthrough=%t", accountType, passthrough), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"id":"resp_test","model":"gpt-5.4","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
				}}
				if accountType == AccountTypeOAuth {
					upstream.resp.Header.Set("Content-Type", "text/event-stream")
					upstream.resp.Body = io.NopCloser(strings.NewReader("data: " + `{"type":"response.completed","response":{"id":"resp_test","model":"gpt-5.4","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n"))
				}
				svc := newOpenAIImageGenerationControlTestService(upstream)
				c, _ := newOpenAIImageGenerationControlTestContext(true, "codex_cli_rs/0.144.1")
				account := newOpenAIImageGenerationControlTestAccount()
				account.Type = accountType
				account.Credentials["access_token"] = "oauth-test-token"
				account.Extra = map[string]any{"openai_passthrough": passthrough}
				body := []byte(`{"model":"gpt-5.4","stream":false,"input":[{"role":"user","content":{"type":"input_text","text":"hello"}}]}`)
				_, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.NotNil(t, upstream.lastReq)
				require.True(t, gjson.GetBytes(upstream.lastBody, "input.0.content").IsArray(), "upstream rejects object content with invalid_type")
				require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String())
			})
		}
	}
}

func TestOpenAIWebSocketWrapsSingletonMessageContent(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		t.Run(accountType, func(t *testing.T) {
			body := []byte(`{"type":"response.create","model":"gpt-5.4","input":[{"type":"message","role":"user","content":{"type":"input_text","text":"hello"}}]}`)
			got, changed, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(body, &Account{Platform: PlatformOpenAI, Type: accountType}, false)
			require.NoError(t, err)
			require.True(t, changed)
			require.True(t, gjson.GetBytes(got, "input.0.content").IsArray())
			require.Equal(t, "hello", gjson.GetBytes(got, "input.0.content.0.text").String())
		})
	}
}
