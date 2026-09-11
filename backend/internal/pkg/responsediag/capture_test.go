package responsediag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeJSONAndSSE(t *testing.T) {
	for _, tc := range []struct {
		name, body, ct, status string
		documents              int
	}{
		{"json", `{"ok":true}`, "application/json", "ok", 1},
		{"whitespace", "{\"ok\":true}\n \t", "application/json", "ok", 1},
		{"concatenated", `{"ok":true}{"extra":1}`, "application/json", "extra_content", 1},
		{"tail", `{"ok":true}unexpected-tail`, "application/json", "extra_content", 1},
		{"escaped braces", `{"text":"} { \\\""}`, "application/json", "ok", 1},
		{"normal SSE", "data: {\"delta\":\"a\"}\n\ndata: {\"delta\":\"b\"}\n\ndata: [DONE]\n\n", "text/event-stream", "ok", 2},
		{"SSE comments", ": ping\r\n\r\nevent: message\r\ndata: {\"a\":1}\r\n\r\n", "text/event-stream", "ok", 1},
		{"multiline", "data: {\n" + "data: \"a\": 1}\n\n", "text/event-stream", "ok", 1},
		{"SSE joined", "data: {\"a\":1}{\"b\":2}\n\n", "text/event-stream", "extra_content", 1},
		{"SSE tail", "data: {\"a\":1}unexpected\n\n", "text/event-stream", "extra_content", 1},
		{"after done", "data: {}\n\ndata: [DONE]\n\ndata: {}\n\n", "text/event-stream", "extra_content", 1},
		{"invalid", `{"a":`, "application/json", "invalid", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := analyze(bodyCapture{Body: tc.body, ContentType: tc.ct, Complete: true})
			require.Equal(t, tc.status, got.Status)
			require.Equal(t, tc.documents, got.JSONDocuments)
			if got.Status == "extra_content" {
				require.NotEmpty(t, got.Issues)
			}
		})
	}
}
func TestCaptureTransparentBoundedAndImmutable(t *testing.T) {
	ctx, c := Start(context.Background())
	body := `{"a":"` + strings.Repeat("x", MaxBodyBytes) + `"}tail`
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}, ContentLength: int64(len(body))}
	WrapUpstream(ctx, resp)
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(got))
	require.NoError(t, resp.Body.Close())
	c.WriteDownstream([]byte(`{"ok":true}`), "application/json", false)
	snapshot := Snapshot(ctx)
	var record Record
	require.NoError(t, json.Unmarshal(snapshot, &record))
	require.Equal(t, "truncated", record.Upstream.Status)
	require.True(t, record.Upstream.Truncated)
	require.Len(t, record.Upstream.Body, MaxBodyBytes)
	require.Equal(t, int64(len(body)), record.Upstream.Bytes)
	require.Equal(t, "ok", record.Downstream.Status)
	c.WriteDownstream([]byte("tail"), "application/json", false)
	require.Equal(t, snapshot, Snapshot(WithSnapshot(context.Background(), snapshot)))
	require.NotEqual(t, snapshot, Snapshot(ctx))
}
func TestCaptureRetryAndIncompleteRead(t *testing.T) {
	ctx, c := Start(context.Background())
	first := &http.Response{Body: io.NopCloser(strings.NewReader(`{}bad`)), Header: http.Header{}, ContentLength: -1}
	WrapUpstream(ctx, first)
	_, _ = io.ReadAll(first.Body)
	second := &http.Response{Body: io.NopCloser(strings.NewReader(`{}unseen`)), Header: http.Header{}, ContentLength: -1}
	WrapUpstream(ctx, second)
	p := make([]byte, 2)
	_, _ = io.ReadFull(second.Body, p)
	c.WriteDownstream([]byte(`{}`), "application/json", false)
	var r Record
	require.NoError(t, json.Unmarshal(Snapshot(ctx), &r))
	require.Equal(t, `{}`, r.Upstream.Body)
	require.Equal(t, "incomplete", r.Upstream.Status)
	require.Equal(t, "ok", r.Downstream.Status)
}
func TestCaptureBrokenWriteNotReportedHealthy(t *testing.T) {
	ctx, c := Start(context.Background())
	c.WriteDownstream([]byte(`{}`), "application/json", true)
	var r Record
	require.NoError(t, json.Unmarshal(Snapshot(ctx), &r))
	require.Equal(t, "incomplete", r.Downstream.Status)
}

func TestCaptureNULCannotBreakJSONBStorage(t *testing.T) {
	ctx, c := Start(context.Background())
	c.WriteDownstream([]byte("{}\x00"), "application/json", false)
	var record Record
	require.NoError(t, json.Unmarshal(Snapshot(ctx), &record))
	require.Equal(t, "tail_symbols", record.Downstream.Status)
	require.True(t, record.Downstream.ControlsEscaped)
	require.Equal(t, `{}\x00`, record.Downstream.Body)
	require.NotContains(t, string(Snapshot(ctx)), `\u0000`)
}
func TestCaptureIncompleteFrameStillDetectsObservedTail(t *testing.T) {
	result := analyze(bodyCapture{Body: "data: {}tail", ContentType: "text/event-stream", Complete: false})
	require.Equal(t, "extra_content", result.Status)
}

func TestCaptureJSONUnicodeWhitespaceIsNotIgnored(t *testing.T) {
	require.Equal(t, "tail_symbols", analyze(bodyCapture{Body: "{}\u00a0", Complete: true}).Status)
	require.Equal(t, "invalid", analyze(bodyCapture{Body: "\u00a0{}", Complete: true}).Status)
}

func TestCaptureBoundarySeparatesTruncationFromFormatErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, status, kind string
		truncated, complete      bool
	}{
		{"cutoff after a single newline", "data: {}\n\ndata: {\"text\":\"cut\n", "truncated", "capture_truncated", true, true},
		{"last event cut by cap", "data: {}\n\ndata: {\"text\":\"cut", "truncated", "capture_truncated", true, true},
		{"unfinished read", "data: {}\n\ndata: {\"text\":\"cut", "incomplete", "incomplete_capture", false, false},
		{"actual incomplete response", "data: {}\n\ndata: {\"text\":\"cut", "invalid", "invalid_json", false, true},
		{"malformed complete event before cutoff", "data: {\"bad\":}\n\ndata: {\"text\":\"cut", "invalid", "invalid_json", true, true},
		{"incomplete JSON in complete event before cutoff", "data: {\"bad\":\n\ndata: {\"text\":\"cut", "invalid", "invalid_json", true, true},
		{"definite syntax error at cutoff", "data: {\"bad\":!", "invalid", "invalid_json", true, true},
		{"heartbeat appended to JSON before cutoff", "data: {}:\n\ndata: {\"text\":\"cut", "truncated", "trailing_symbols", true, true},
		{"unary response cut by cap", "{\"text\":\"cut", "truncated", "capture_truncated", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ct := "text/event-stream"
			if tc.name == "unary response cut by cap" {
				ct = "application/json"
			}
			got := analyze(bodyCapture{Body: tc.body, ContentType: ct, Truncated: tc.truncated, Complete: tc.complete})
			require.Equal(t, tc.status, got.Status)
			require.NotEmpty(t, got.Issues)
			require.Equal(t, tc.kind, got.Issues[0].Kind)
			if tc.name == "heartbeat appended to JSON before cutoff" {
				require.Equal(t, ":", got.Issues[0].Extra)
				require.Equal(t, "capture_truncated", got.Issues[1].Kind)
			}
		})
	}
}

func TestRefreshAnalysisReclassifiesHistoricalCaptureWithoutChangingBody(t *testing.T) {
	old := Record{Summary: Summary{Upstream: "unavailable", Downstream: "extra"}, Downstream: Side{
		bodyCapture: bodyCapture{Body: "data: {}:\n\ndata: {\"text\":\"cut", ContentType: "text/event-stream", Truncated: true, Complete: true},
		Status:      "extra", Issues: []Issue{{Kind: "trailing_content", Frame: 1, Extra: ":"}, {Kind: "invalid_json", Frame: 2}},
	}}
	raw, err := json.Marshal(old)
	require.NoError(t, err)
	before := string(raw)
	var got Record
	require.NoError(t, json.Unmarshal(RefreshAnalysis(raw), &got))
	require.Equal(t, before, string(raw))
	require.Equal(t, old.Downstream.Body, got.Downstream.Body)
	require.Equal(t, "truncated", got.Downstream.Status)
	require.Equal(t, "trailing_symbols", got.Downstream.Issues[0].Kind)
	require.Equal(t, "capture_truncated", got.Downstream.Issues[1].Kind)
	require.Nil(t, RefreshAnalysis(nil))
}

func TestTrailingSymbolsDoNotCountAsAdditionalContent(t *testing.T) {
	for _, tail := range []string{":", ":\n:\n", ";,.", "}", "]", "。", ": keepalive", ": ping", "[DONE]"} {
		t.Run(tail, func(t *testing.T) {
			got := analyze(bodyCapture{Body: `{"ok":true}` + tail, ContentType: "application/json", Complete: true})
			require.Equal(t, "tail_symbols", got.Status)
			require.Equal(t, "trailing_symbols", got.Issues[0].Kind)
			require.Equal(t, strings.Trim(tail, " \t\r\n"), got.Issues[0].Extra)
		})
	}
	for _, tail := range []string{"extra answer", "额外生成的内容", "123", "null", `{}`, `[]`, `""`, `{"text":"extra"}`, ": extra text"} {
		t.Run(tail, func(t *testing.T) {
			got := analyze(bodyCapture{Body: `{"ok":true}` + tail, ContentType: "application/json", Complete: true})
			require.Equal(t, "extra_content", got.Status)
			require.Equal(t, "trailing_content", got.Issues[0].Kind)
		})
	}
}
func TestSymbolsCannotHideLaterAdditionalContent(t *testing.T) {
	body := strings.Repeat("data: {}:\n\n", 12) + "data: {}extra text\n\n"
	got := analyze(bodyCapture{Body: body, ContentType: "text/event-stream", Complete: true})
	require.Equal(t, "extra_content", got.Status)
	require.Len(t, got.Issues, 8)
	require.Equal(t, "extra text", got.Issues[7].Extra)
	require.Equal(t, "trailing_content", got.Issues[7].Kind)
}

func TestReplacementContentExcerptRemainsBounded(t *testing.T) {
	body := strings.Repeat("data: {}:\n\n", 8) + "data: {}" + strings.Repeat("x", 5000) + "\n\n"
	got := analyze(bodyCapture{Body: body, ContentType: "text/event-stream", Complete: true})
	require.Equal(t, "extra_content", got.Status)
	require.Len(t, got.Issues[7].Extra, 4096)
}

func TestConfiguredResponseCaptureLimitAndHistoricalMetadata(t *testing.T) {
	for _, limit := range []int{1024, MaxBodyBytes, 2 * MaxBodyBytes} {
		ctx, c := Start(context.Background(), limit)
		body := `{"text":"` + strings.Repeat("x", limit) + `"}`
		resp := &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
		WrapUpstream(ctx, resp)
		actual, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(actual))
		require.NoError(t, resp.Body.Close())
		c.WriteDownstream([]byte(body), "application/json", false)
		raw := Snapshot(ctx)
		var record Record
		require.NoError(t, json.Unmarshal(raw, &record))
		require.Equal(t, limit, record.Upstream.LimitBytes)
		require.Equal(t, limit, record.Downstream.LimitBytes)
		require.Len(t, record.Upstream.Body, limit)
		require.Len(t, record.Downstream.Body, limit)
		require.True(t, record.Upstream.Truncated)
		require.NoError(t, json.Unmarshal(RefreshAnalysis(raw), &record))
		require.Equal(t, limit, record.Downstream.LimitBytes)
	}
	ctx, c := Start(context.Background())
	c.WriteDownstream([]byte(`{}`), "application/json", false)
	var record Record
	require.NoError(t, json.Unmarshal(Snapshot(ctx), &record))
	require.Equal(t, 1024*1024, record.Downstream.LimitBytes)
}
