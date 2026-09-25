package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Normal SSE records consult Size once before output classification. Blocking
// that existing writer boundary lets the independent scanner queue a complete
// burst without relying on goroutine scheduling or a production test hook.
type gatedFirstOutputResponseWriter struct {
	gin.ResponseWriter
	sizeCalls        int
	firstEventSeen   chan struct{}
	releaseFirst     <-chan struct{}
	firstContentSeen chan struct{}
	releaseContent   <-chan struct{}
}

func (w *gatedFirstOutputResponseWriter) Size() int {
	w.sizeCalls++
	switch w.sizeCalls {
	case 1:
		close(w.firstEventSeen)
		<-w.releaseFirst
	case 3:
		close(w.firstContentSeen)
		<-w.releaseContent
	}
	return w.ResponseWriter.Size()
}

func TestOpenAIResponseFirstOutputCommit_QueuedContentFlushesIndependentlyOfTTFTMode(t *testing.T) {
	for _, mode := range []string{OpenAITTFTModeSemantic, OpenAITTFTModeVisible} {
		for _, content := range []struct {
			name    string
			first   string
			visible string
			tail    string
		}{
			{
				name:  "text",
				first: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"first\"}\n\n",
				tail:  "data: {\"type\":\"response.output_text.delta\",\"delta\":\"second\"}\n\n",
			},
			{
				name:  "tool",
				first: "data: {\"type\":\"response.custom_tool_call_input.delta\",\"delta\":\"first\"}\n\n",
				tail:  "data: {\"type\":\"response.custom_tool_call_input.delta\",\"delta\":\"second\"}\n\n",
			},
			{
				name:    "reasoning_then_text",
				first:   "data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"reasoning_commit\",\"type\":\"reasoning\",\"summary\":[],\"encrypted_content\":\"opaque\"}}\n\n",
				visible: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"first\"}\n\n",
				tail:    "data: {\"type\":\"response.output_text.delta\",\"delta\":\"second\"}\n\n",
			},
		} {
			t.Run(mode+"/"+content.name, func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				previousSettings := gatewayForwardingCache.Load()
				if previousSettings == nil {
					previousSettings = &cachedGatewayForwardingSettings{}
				}
				gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
					openAITTFTMode: mode,
					expiresAt:      time.Now().Add(time.Minute).UnixNano(),
				})
				t.Cleanup(func() { gatewayForwardingCache.Store(previousSettings) })

				preamble := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_commit\"}}\n\n" +
					"data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"reasoning_commit\",\"type\":\"reasoning\",\"summary\":[]}}\n\n"
				terminal := "data: [DONE]\n\n"
				body := preamble + content.first + content.visible + content.tail + terminal
				eofReached := make(chan struct{})
				reader := &stagedOpenAISSEReadCloser{segments: [][]byte{[]byte(body)}, eofReached: eofReached}
				recorder := newOpenAIResponseFlushRecorder()
				releaseFirst, releaseContent, releaseFlush := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var releaseFirstOnce, releaseContentOnce, releaseFlushOnce sync.Once
				unblockFirst := func() { releaseFirstOnce.Do(func() { close(releaseFirst) }) }
				unblockContent := func() { releaseContentOnce.Do(func() { close(releaseContent) }) }
				unblockFlush := func() { releaseFlushOnce.Do(func() { close(releaseFlush) }) }
				t.Cleanup(func() { unblockFirst(); unblockContent(); unblockFlush() })
				recorder.blockFlush = 1
				recorder.flushBlocked = make(chan struct{})
				recorder.releaseFlush = releaseFlush
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				writer := &gatedFirstOutputResponseWriter{
					ResponseWriter:   c.Writer,
					firstEventSeen:   make(chan struct{}),
					releaseFirst:     releaseFirst,
					firstContentSeen: make(chan struct{}),
					releaseContent:   releaseContent,
				}
				c.Writer = writer
				svc := &OpenAIGatewayService{
					cfg: &config.Config{Gateway: config.GatewayConfig{
						StreamDataIntervalTimeout: 30,
					}},
					toolCorrector: NewCodexToolCorrector(),
				}
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				started := time.Now()
				resultCh, errCh := make(chan *openaiStreamingResult, 1), make(chan error, 1)
				go func() {
					result, err := svc.handleStreamingResponse(context.Background(), resp, c, account, started, "gpt-5", "gpt-5")
					resultCh <- result
					errCh <- err
				}()

				waitOpenAIResponseFlushSignal(t, writer.firstEventSeen)
				// The burst has fewer records than the queue capacity. EOF while
				// its first record is blocked proves the content AND trailing
				// records are queued before the first content is processed.
				waitOpenAIResponseFlushSignal(t, eofReached)
				unblockFirst()
				waitOpenAIResponseFlushSignal(t, writer.firstContentSeen)
				beforeContent, beforeFlushes := recorder.snapshot()
				// A distinct interval verifies that historical TTFT still starts
				// at the empty reasoning event, while visible TTFT waits for data.
				time.Sleep(60 * time.Millisecond)
				contentReleasedMs := int(time.Since(started).Milliseconds())
				unblockContent()
				waitOpenAIResponseFlushSignal(t, recorder.flushBlocked)
				_, firstFlushes := recorder.snapshot()
				if content.visible != "" {
					// A completed encrypted reasoning item is publishable but is not
					// visible output. Its commit must not consume the first-visible
					// flush or move visible TTFT earlier than the following delta.
					time.Sleep(60 * time.Millisecond)
					contentReleasedMs = int(time.Since(started).Milliseconds())
				}
				unblockFlush()
				select {
				case err := <-errCh:
					require.NoError(t, err)
				case <-time.After(3 * time.Second):
					t.Fatal("stream handler did not finish")
				}
				result := <-resultCh
				gotBody, allFlushes := recorder.snapshot()

				require.Empty(t, beforeContent, "empty preamble must remain replayable until client output")
				require.Empty(t, beforeFlushes)
				require.NotNil(t, result)
				require.NotNil(t, result.firstTokenMs)
				if mode == OpenAITTFTModeSemantic {
					require.Less(t, *result.firstTokenMs, contentReleasedMs, "historical TTFT must retain the early semantic timestamp")
				} else {
					require.GreaterOrEqual(t, *result.firstTokenMs, contentReleasedMs, "visible TTFT must wait for effective content")
				}
				require.Equal(t, []string{preamble + content.first}, firstFlushes,
					"first complete output must flush before queued tail records and preserve every staged byte")
				if content.visible != "" {
					require.GreaterOrEqual(t, len(allFlushes), 2)
					require.Equal(t, preamble+content.first+content.visible, allFlushes[1],
						"first visible output must flush even after a non-visible output was committed and tail records remain queued")
				}
				require.Equal(t, body, gotBody, "marking output committed must not discard staged preamble or the first delta")
			})
		}
	}
}
