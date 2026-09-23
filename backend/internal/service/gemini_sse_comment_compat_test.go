package service

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiClientRejectsSSEComments(t *testing.T) {
	cases := []struct {
		name string
		hint string
		want bool
	}{
		{"go-genai (Antigravity CLI)", "google-genai-sdk/1.71.0 gl-go/go1.28-20260721-RC03 cl/951519500 +3ebc191975 X:fieldtrack,boringcrypto", true},
		{"python-genai", "google-genai-sdk/1.20.0 gl-python/3.12.4", true},
		{"js-genai tolerates comments", "google-genai-sdk/1.9.0 gl-node/22.3.0", false},
		{"gemini-cli", "GeminiCLI/0.60.0 (darwin; arm64)", false},
		{"curl", "curl/8.7.1", false},
		{"empty", "", false},
		{"case-insensitive", "Google-GenAI-SDK/1.0.0 GL-Go/go1.27", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, geminiClientRejectsSSEComments(tc.hint))
		})
	}
}

func TestDownstreamRejectsSSECommentsReadsBothHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := newAntigravityCompatContext(http.MethodPost, "/v1beta/models/gemini-3.8-flash:streamGenerateContent", nil)
	require.False(t, downstreamRejectsSSEComments(c))

	c.Request.Header.Set("X-Goog-Api-Client", "google-genai-sdk/1.71.0 gl-go/go1.28")
	require.True(t, downstreamRejectsSSEComments(c))

	c.Request.Header.Del("X-Goog-Api-Client")
	c.Request.Header.Set("User-Agent", "google-genai-sdk/1.71.0 gl-go/go1.28")
	require.True(t, downstreamRejectsSSEComments(c))

	require.False(t, downstreamRejectsSSEComments(nil))
}

// runAntigravityGeminiStreamWithIdle 使用虚拟时间运行真实流处理器：先发一个 data 事件，
// 然后空闲 idle 时长再关闭。数据故意晚于 ticker 起点，覆盖 CI 中的首轮检查跳过场景。
func runAntigravityGeminiStreamWithIdle(t *testing.T, userAgent string, idle time.Duration) string {
	t.Helper()
	var out string
	synctest.Test(t, func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		svc := newAntigravityCompatService(
			config.GatewayConfig{MaxLineSize: defaultMaxLineSize, StreamKeepaliveInterval: 1},
			nil,
		)
		c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1beta/models/gemini-3.8-flash:streamGenerateContent", nil)
		if userAgent != "" {
			c.Request.Header.Set("User-Agent", userAgent)
		}
		reader, writer := io.Pipe()
		t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
		resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
		done := make(chan error, 1)
		go func() {
			_, err := svc.handleGeminiStreamingResponse(c, resp, time.Now())
			done <- err
		}()
		// Deliberately offset the first event from the ticker start.
		synctest.Wait()
		time.Sleep(200 * time.Millisecond)
		_, err := io.WriteString(
			writer,
			`data: {"response":{"responseId":"resp_1","candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":1}}}`+"\n\n",
		)
		require.NoError(t, err)
		synctest.Wait()
		time.Sleep(idle)
		require.NoError(t, writer.Close())
		require.NoError(t, <-done)
		require.NoError(t, reader.Close())
		out = recorder.Body.String()
	})
	return out
}

func TestAntigravityGeminiStreamKeepsCommentKeepaliveForOrdinaryClients(t *testing.T) {
	// 第一次 1s tick 距 data 仅 800ms，会跳过；必须观察到第二次 tick。
	// synctest 的虚拟时间避免 CI 调度负载影响，也不增加实际等待时间。
	// The keepalive interval is configured in whole seconds. Leave enough idle
	// time for the first data event to be processed before the ticker fires;
	// otherwise a busy CI runner can consume the first tick too early and close
	// the stream before any heartbeat is written.
	out := runAntigravityGeminiStreamWithIdle(t, "curl/8.7.1", 2200*time.Millisecond)
	require.Contains(t, out, ":\n\n", "ordinary clients should still get the idle keepalive")
	require.Contains(t, out, `"text":"partial"`)
}

func TestAntigravityGeminiStreamSkipsCommentKeepaliveForGoGenai(t *testing.T) {
	out := runAntigravityGeminiStreamWithIdle(t, "google-genai-sdk/1.71.0 gl-go/go1.28-20260721-RC03", 2200*time.Millisecond)
	require.Contains(t, out, `"text":"partial"`)
	for _, event := range strings.Split(out, "\n\n") {
		require.False(t, strings.HasPrefix(event, ":"), "go-genai must never receive an SSE comment event, got %q", event)
	}
}

func TestAntigravityGeminiStreamSkipsFirstTickAfterRecentData(t *testing.T) {
	out := runAntigravityGeminiStreamWithIdle(t, "curl/8.7.1", 1200*time.Millisecond)
	require.Contains(t, out, `"text":"partial"`)
	require.NotContains(t, out, ":\n\n", "first tick is too close to the initial data event")
}
