package responsediag

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

func timingFrame(payload string) string { return "data: " + payload + "\n\n" }

func appendTimingFrame(b *bodyCapture, frame string, startMS, endMS int64) {
	b.appendTimed([]byte(frame), time.Duration(startMS)*time.Millisecond, time.Duration(endMS)*time.Millisecond)
	b.Body = string(b.buffer)
}

func TestStreamTimingSeparatesWhitespaceAndDelayedToolContent(t *testing.T) {
	up := bodyCapture{ContentType: "text/event-stream"}
	appendTimingFrame(&up, timingFrame(`{"type":"response.created","sequence_number":0}`), 0, 5000)
	appendTimingFrame(&up, timingFrame(`{"type":"response.output_text.delta","sequence_number":4,"delta":" "}`), 5000, 5010)
	appendTimingFrame(&up, ":\n\n"+timingFrame(`{"type":"keepalive","sequence_number":5}`), 5010, 15000)
	tool := timingFrame(`{"type":"response.custom_tool_call_input.delta","sequence_number":9,"delta":"private tool input"}`)
	appendTimingFrame(&up, tool, 15000, 90000)
	u := analyzeStreamTiming(up, nil, false)
	require.EqualValues(t, 5000, u.FirstEvent.ObservedMS)
	require.EqualValues(t, 90000, u.FirstContent.ObservedMS)
	require.Equal(t, "tool", u.FirstContent.Kind)
	require.EqualValues(t, 9, *u.FirstContent.SequenceNumber)
	require.Nil(t, u.FirstContent.WriteStartedMS)

	// Replay a missing preamble: the downstream starts at the real tool event.
	down := bodyCapture{ContentType: "text/event-stream"}
	appendTimingFrame(&down, ":\n\n", 10000, 10000)
	heartbeatEnd := int64(len(down.buffer))
	appendTimingFrame(&down, tool[:20], 90001, 90010)
	appendTimingFrame(&down, tool[20:], 90011, 90020)
	d := analyzeStreamTiming(down, []flushTiming{
		{end: heartbeatEnd, startedMS: 10000, finishedMS: 10001},
		{end: int64(len(down.buffer)), startedMS: 90021, finishedMS: 94000},
	}, true)
	require.EqualValues(t, 9, *d.FirstEvent.SequenceNumber)
	require.EqualValues(t, 90001, *d.FirstContent.WriteStartedMS)
	require.EqualValues(t, 90020, d.FirstContent.ObservedMS)
	require.EqualValues(t, 90021, *d.FirstContent.FlushStartedMS)
	require.EqualValues(t, 94000, *d.FirstContent.FlushCompletedMS)
	raw, err := json.Marshal(d)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private tool input")
}

func TestStreamTimingCompactionProgressIsNotCompletedContent(t *testing.T) {
	b := bodyCapture{ContentType: "text/event-stream"}
	appendTimingFrame(&b, timingFrame(`{"type":"response.output_item.added","sequence_number":2,"item":{"type":"compaction","encrypted_content":"initial"}}`), 0, 6000)
	appendTimingFrame(&b, timingFrame(`{"type":"response.compaction.compacting","sequence_number":3}`), 6000, 30000)
	appendTimingFrame(&b, timingFrame(`{"type":"response.output_item.done","sequence_number":9,"item":{"type":"compaction","encrypted_content":"complete"}}`), 30000, 139000)
	r := analyzeStreamTiming(b, nil, false)
	require.EqualValues(t, 6000, r.CompactionStarted.ObservedMS)
	require.EqualValues(t, 139000, r.CompactionCompleted.ObservedMS)
	require.EqualValues(t, 139000, r.FirstContent.ObservedMS)
	require.Equal(t, "compaction", r.FirstContent.Kind)
}

func TestStreamTimingBoundsAndPartialFrames(t *testing.T) {
	t.Run("body limit", func(t *testing.T) {
		b := bodyCapture{ContentType: "text/event-stream", LimitBytes: 20}
		appendTimingFrame(&b, timingFrame(`{"type":"response.output_text.delta","delta":"content"}`), 0, 100)
		r := analyzeStreamTiming(b, nil, false)
		require.True(t, r.Truncated)
		require.Nil(t, r.FirstContent)
	})
	t.Run("timing limit", func(t *testing.T) {
		b := bodyCapture{ContentType: "text/event-stream"}
		for i := 0; i < maxTimingMarks; i++ {
			appendTimingFrame(&b, ":\n\n", int64(i), int64(i))
		}
		appendTimingFrame(&b, timingFrame(`{"type":"response.output_text.delta","delta":"late"}`), 2000, 2000)
		r := analyzeStreamTiming(b, nil, false)
		require.True(t, r.Truncated)
		require.Len(t, b.timing, maxTimingMarks)
		require.Nil(t, r.FirstContent)
	})
	t.Run("no completed event or flush", func(t *testing.T) {
		b := bodyCapture{ContentType: "text/event-stream"}
		frame := timingFrame(`{"type":"response.output_text.delta","delta":"word"}`)
		appendTimingFrame(&b, frame[:len(frame)-1], 0, 20)
		require.Nil(t, analyzeStreamTiming(b, nil, true).FirstContent)
		appendTimingFrame(&b, "\n", 30, 40)
		r := analyzeStreamTiming(b, nil, true)
		require.EqualValues(t, 40, r.FirstContent.ObservedMS)
		require.Nil(t, r.FirstContent.FlushCompletedMS)
	})
}

func TestStreamTimingMultilineAndRetryCapture(t *testing.T) {
	ctx, capture := Start(context.Background())
	first := &http.Response{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(timingFrame(`{"type":"response.failed","sequence_number":7}`)))}
	WrapUpstream(ctx, first)
	_, err := io.ReadAll(first.Body)
	require.NoError(t, err)
	frame := "event: response.output_text.delta\r\ndata: {\r\ndata: \"delta\":\"private answer\",\"sequence_number\":9}\r\n\r\n"
	second := &http.Response{Header: first.Header, Body: io.NopCloser(strings.NewReader(frame))}
	WrapUpstream(ctx, second)
	_, err = io.ReadAll(second.Body)
	require.NoError(t, err)
	capture.WriteDownstream([]byte(frame), "text/event-stream", false)
	capture.EndFlush(capture.BeginFlush())
	r := StreamTiming(ctx)
	require.Equal(t, 2, r.UpstreamResponses)
	require.EqualValues(t, 9, *r.Upstream.FirstEvent.SequenceNumber)
	require.Equal(t, "text", r.Upstream.FirstContent.Kind)
	require.NotNil(t, r.Downstream.FirstContent.FlushCompletedMS)
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private answer")
	require.NotContains(t, string(raw), "response.failed")
	require.Nil(t, StreamTiming(context.Background()))
}
