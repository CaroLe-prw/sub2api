package responsediag

import (
	"context"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// Timing is retained only for the already bounded response prefix. No extra
// body copies or JSON parsing are performed on the read/write hot path.
const maxTimingMarks = 1024

type chunkTiming struct {
	end                   int
	startedMS, finishedMS int64
}

type flushTiming struct {
	end                   int64
	startedMS, finishedMS int64
}

// FlushMark captures the byte boundary before Flush, so a later write cannot
// accidentally be attributed to an earlier flush.
type FlushMark struct {
	end     int64
	started time.Time
}

func (c *Capture) BeginFlush() FlushMark {
	c.mu.Lock()
	defer c.mu.Unlock()
	return FlushMark{end: c.downstream.Bytes, started: time.Now()}
}

func (c *Capture) EndFlush(mark FlushMark) {
	finished := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.flushes) >= maxTimingMarks {
		c.flushTimingTruncated = true
		return
	}
	c.flushes = append(c.flushes, flushTiming{end: mark.end, startedMS: mark.started.Sub(c.started).Milliseconds(), finishedMS: finished.Sub(c.started).Milliseconds()})
}

func (b *bodyCapture) appendTimed(p []byte, started, finished time.Duration) {
	before := len(b.buffer)
	b.append(p)
	if len(b.buffer) == before {
		return
	}
	if len(b.timing) >= maxTimingMarks {
		b.timingTruncated = true
		return
	}
	b.timing = append(b.timing, chunkTiming{end: len(b.buffer), startedMS: started.Milliseconds(), finishedMS: finished.Milliseconds()})
}

type StreamEventTiming struct {
	Type             string `json:"type"`
	SequenceNumber   *int64 `json:"sequence_number,omitempty"`
	Kind             string `json:"kind,omitempty"`
	ObservedMS       int64  `json:"observed_ms"`
	WriteStartedMS   *int64 `json:"write_started_ms,omitempty"`
	FlushStartedMS   *int64 `json:"flush_started_ms,omitempty"`
	FlushCompletedMS *int64 `json:"flush_completed_ms,omitempty"`
}

type StreamSideTiming struct {
	FirstEvent          *StreamEventTiming `json:"first_event,omitempty"`
	FirstContent        *StreamEventTiming `json:"first_content,omitempty"`
	CompactionStarted   *StreamEventTiming `json:"compaction_started,omitempty"`
	CompactionCompleted *StreamEventTiming `json:"compaction_completed,omitempty"`
	Truncated           bool               `json:"truncated,omitempty"`
	UnparsedEvents      int                `json:"unparsed_events,omitempty"`
}

type StreamTimingRecord struct {
	ElapsedMS int64 `json:"elapsed_ms"`
	// Only HTTP responses that reached capture are counted. Connection failures
	// before response headers are not counted; this is not a total retry count.
	UpstreamResponses int               `json:"upstream_responses"`
	Upstream          *StreamSideTiming `json:"upstream,omitempty"`
	Downstream        *StreamSideTiming `json:"downstream,omitempty"`
}

// StreamTiming reports local observations, never client receipt times. The
// upstream is the final captured HTTP response; all offsets share the inbound
// diagnostics start time, including time spent on earlier attempts.
func StreamTiming(ctx context.Context) *StreamTimingRecord {
	if ctx == nil {
		return nil
	}
	c, _ := ctx.Value(contextKey{}).(*Capture)
	if c == nil {
		return nil
	}
	c.mu.Lock()
	copyBody := func(b bodyCapture) bodyCapture {
		b.Body = string(b.buffer)
		b.buffer = nil
		b.timing = append([]chunkTiming(nil), b.timing...)
		return b
	}
	down := copyBody(c.downstream)
	var up *bodyCapture
	if c.upstream != nil {
		v := copyBody(*c.upstream)
		up = &v
	}
	flushes := append([]flushTiming(nil), c.flushes...)
	flushTruncated := c.flushTimingTruncated
	r := &StreamTimingRecord{ElapsedMS: time.Since(c.started).Milliseconds(), UpstreamResponses: c.upstreamResponses}
	c.mu.Unlock()
	if up != nil {
		r.Upstream = analyzeStreamTiming(*up, nil, false)
	}
	r.Downstream = analyzeStreamTiming(down, flushes, true)
	if r.Downstream != nil {
		r.Downstream.Truncated = r.Downstream.Truncated || flushTruncated
	}
	if r.Upstream == nil && r.Downstream == nil {
		return nil
	}
	return r
}

func analyzeStreamTiming(b bodyCapture, flushes []flushTiming, downstream bool) *StreamSideTiming {
	if !strings.Contains(b.ContentType, "event-stream") && !strings.HasPrefix(strings.TrimSpace(b.Body), "event:") && !strings.HasPrefix(strings.TrimSpace(b.Body), "data:") && !strings.HasPrefix(strings.TrimSpace(b.Body), ":") {
		return nil
	}
	r := &StreamSideTiming{Truncated: b.Truncated || b.timingTruncated}
	var data []string
	eventType := ""
	frameStart, offset := 0, 0
	observe := func(end int) {
		if len(data) == 0 {
			return
		}
		payload := strings.Join(data, "\n")
		if strings.TrimSpace(payload) == "[DONE]" {
			return
		}
		if !gjson.Valid(payload) {
			r.UnparsedEvents++
			return
		}
		o := gjson.Parse(payload)
		typ := o.Get("type").String()
		if typ == "" {
			typ = eventType
		}
		if typ == "keepalive" || typ == "ping" {
			return
		}
		if !strings.HasPrefix(typ, "response.") {
			return
		}
		var mark *chunkTiming
		for i := range b.timing {
			if b.timing[i].end >= end {
				mark = &b.timing[i]
				break
			}
		}
		if mark == nil {
			r.Truncated = true
			return
		}
		e := &StreamEventTiming{Type: typ, ObservedMS: mark.finishedMS}
		if seq := o.Get("sequence_number"); seq.Exists() && seq.Type == gjson.Number {
			v := seq.Int()
			e.SequenceNumber = &v
		}
		if downstream {
			for _, m := range b.timing {
				if m.end > frameStart {
					v := m.startedMS
					e.WriteStartedMS = &v
					break
				}
			}
			for _, f := range flushes {
				if f.end >= int64(end) {
					a, z := f.startedMS, f.finishedMS
					e.FlushStartedMS, e.FlushCompletedMS = &a, &z
					break
				}
			}
		}
		if r.FirstEvent == nil {
			r.FirstEvent = e
		}
		item := o.Get("item")
		if r.CompactionStarted == nil && (typ == "response.compaction.compacting" || (typ == "response.output_item.added" && item.Get("type").String() == "compaction")) {
			r.CompactionStarted = e
		}
		if typ == "response.output_item.done" && item.Get("type").String() == "compaction" && nonBlank(item.Get("encrypted_content").String()) {
			e.Kind = "compaction"
			if r.CompactionCompleted == nil {
				r.CompactionCompleted = e
			}
		} else {
			e.Kind = streamContentKind(o, typ)
		}
		if e.Kind != "" && r.FirstContent == nil {
			r.FirstContent = e
		}
	}
	for _, line := range strings.SplitAfter(b.Body, "\n") {
		if !strings.HasSuffix(line, "\n") {
			break
		}
		offset += len(line)
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			observe(offset)
			data = nil
			eventType = ""
			frameStart = offset
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(line[6:])
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(line[5:], " "))
		}
		if line == "data" {
			data = append(data, "")
		}
	}
	return r
}

func nonBlank(value string) bool { return strings.TrimSpace(value) != "" }

// Whitespace, progress and encrypted reasoning metadata are not usable output.
// Complete compaction results are classified separately from normal text.
func streamContentKind(o gjson.Result, typ string) string {
	switch typ {
	case "response.output_text.delta", "response.audio_transcript.delta":
		if nonBlank(o.Get("delta").String()) {
			return "text"
		}
	case "response.output_text.done", "response.audio_transcript.done":
		if nonBlank(o.Get("text").String()) {
			return "text"
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if nonBlank(o.Get("delta").String()) {
			return "reasoning"
		}
	case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		if nonBlank(o.Get("delta").String()) {
			return "tool"
		}
	case "response.function_call_arguments.done":
		if nonBlank(o.Get("arguments").String()) {
			return "tool"
		}
	case "response.custom_tool_call_input.done":
		if nonBlank(o.Get("input").String()) {
			return "tool"
		}
	case "response.image_generation_call.partial_image":
		if nonBlank(o.Get("partial_image_b64").String()) {
			return "image"
		}
	case "response.content_part.added", "response.content_part.done":
		if nonBlank(o.Get("part.text").String()) {
			return "text"
		}
	case "response.output_item.added", "response.output_item.done":
		return streamItemContentKind(o.Get("item"), typ == "response.output_item.done")
	case "response.completed", "response.done":
		for _, item := range o.Get("response.output").Array() {
			if kind := streamItemContentKind(item, true); kind != "" {
				return kind
			}
		}
	}
	return ""
}

func streamItemContentKind(item gjson.Result, completed bool) string {
	switch item.Get("type").String() {
	case "compaction":
		if completed && nonBlank(item.Get("encrypted_content").String()) {
			return "compaction"
		}
	case "function_call", "custom_tool_call":
		if nonBlank(item.Get("arguments").String()) || nonBlank(item.Get("input").String()) {
			return "tool"
		}
	case "image_generation_call":
		if nonBlank(item.Get("result").String()) {
			return "image"
		}
	case "message", "reasoning":
		for _, path := range []string{"content", "summary"} {
			for _, part := range item.Get(path).Array() {
				if nonBlank(part.Get("text").String()) {
					if item.Get("type").String() == "reasoning" {
						return "reasoning"
					}
					return "text"
				}
			}
		}
	}
	return ""
}
