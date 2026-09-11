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
		{"concatenated", `{"ok":true}{"extra":1}`, "application/json", "extra", 1},
		{"tail", `{"ok":true}unexpected-tail`, "application/json", "extra", 1},
		{"escaped braces", `{"text":"} { \\\""}`, "application/json", "ok", 1},
		{"normal SSE", "data: {\"delta\":\"a\"}\n\ndata: {\"delta\":\"b\"}\n\ndata: [DONE]\n\n", "text/event-stream", "ok", 2},
		{"SSE comments", ": ping\r\n\r\nevent: message\r\ndata: {\"a\":1}\r\n\r\n", "text/event-stream", "ok", 1},
		{"multiline", "data: {\n" + "data: \"a\": 1}\n\n", "text/event-stream", "ok", 1},
		{"SSE joined", "data: {\"a\":1}{\"b\":2}\n\n", "text/event-stream", "extra", 1},
		{"SSE tail", "data: {\"a\":1}unexpected\n\n", "text/event-stream", "extra", 1},
		{"after done", "data: {}\n\ndata: [DONE]\n\ndata: {}\n\n", "text/event-stream", "extra", 1},
		{"invalid", `{"a":`, "application/json", "invalid", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := analyze(bodyCapture{Body: tc.body, ContentType: tc.ct, Complete: true})
			require.Equal(t, tc.status, got.Status)
			require.Equal(t, tc.documents, got.JSONDocuments)
			if got.Status == "extra" {
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
	require.Equal(t, "incomplete", record.Upstream.Status)
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
	require.Equal(t, "extra", record.Downstream.Status)
	require.True(t, record.Downstream.ControlsEscaped)
	require.Equal(t, `{}\x00`, record.Downstream.Body)
	require.NotContains(t, string(Snapshot(ctx)), `\u0000`)
}
func TestCaptureIncompleteFrameStillDetectsObservedTail(t *testing.T) {
	result := analyze(bodyCapture{Body: "data: {}tail", ContentType: "text/event-stream", Complete: false})
	require.Equal(t, "extra", result.Status)
}

func TestCaptureJSONUnicodeWhitespaceIsNotIgnored(t *testing.T) {
	require.Equal(t, "extra", analyze(bodyCapture{Body: "{}\u00a0", Complete: true}).Status)
	require.Equal(t, "invalid", analyze(bodyCapture{Body: "\u00a0{}", Complete: true}).Status)
}
