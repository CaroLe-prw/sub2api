// Package responsediag observes response bytes without changing forwarding.
package responsediag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

const MaxBodyBytes = 256 * 1024

type contextKey struct{}
type snapshotKey struct{}

type Capture struct {
	mu         sync.Mutex
	upstream   *bodyCapture
	downstream bodyCapture
}
type bodyCapture struct {
	Body            string `json:"body"`
	ContentType     string `json:"content_type"`
	Bytes           int64  `json:"bytes"`
	Truncated       bool   `json:"truncated"`
	Complete        bool   `json:"complete"`
	ControlsEscaped bool   `json:"controls_escaped,omitempty"`
	buffer          []byte
	failed          bool
}
type Side struct {
	bodyCapture
	Status        string  `json:"status"`
	JSONDocuments int     `json:"json_documents"`
	Issues        []Issue `json:"issues,omitempty"`
}
type Issue struct {
	Kind  string `json:"kind"`
	Frame int    `json:"frame"`
	JSON  string `json:"json,omitempty"`
	Extra string `json:"extra,omitempty"`
}
type Summary struct {
	Upstream   string `json:"upstream"`
	Downstream string `json:"downstream"`
}
type Record struct {
	Summary    Summary `json:"summary"`
	Upstream   *Side   `json:"upstream,omitempty"`
	Downstream Side    `json:"downstream"`
}

func Start(ctx context.Context) (context.Context, *Capture) {
	c := &Capture{}
	return context.WithValue(ctx, contextKey{}, c), c
}
func (b *bodyCapture) append(p []byte) {
	b.Bytes += int64(len(p))
	remaining := MaxBodyBytes - len(b.buffer)
	if len(p) > remaining {
		b.Truncated = true
		p = p[:remaining]
	}
	b.buffer = append(b.buffer, p...)
}
func (c *Capture) WriteDownstream(p []byte, contentType string, failed bool) {
	if contentType != "" && !strings.Contains(contentType, "json") && !strings.Contains(contentType, "event-stream") && !strings.HasPrefix(contentType, "text/") {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.downstream.ContentType = contentType
	c.downstream.append(p)
	if failed {
		c.downstream.failed = true
	}
}

// WrapUpstream records only bytes the gateway actually reads. Early close is
// deliberately incomplete: observing a prefix cannot rule out an unseen tail.
// A retry starts a fresh capture, keeping the final attempt separate.
func WrapUpstream(ctx context.Context, resp *http.Response) {
	c, _ := ctx.Value(contextKey{}).(*Capture)
	if c == nil || resp == nil || resp.Body == nil {
		return
	}
	c.mu.Lock()
	c.upstream = nil
	c.mu.Unlock()
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(ct, "json") && !strings.Contains(ct, "event-stream") && !strings.HasPrefix(ct, "text/") {
		return
	}
	b := &bodyCapture{ContentType: ct}
	c.mu.Lock()
	c.upstream = b
	c.mu.Unlock()
	resp.Body = &reader{ReadCloser: resp.Body, capture: c, body: b, length: resp.ContentLength}
}

type reader struct {
	io.ReadCloser
	capture *Capture
	body    *bodyCapture
	length  int64
}

func (r *reader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.capture.mu.Lock()
	r.body.append(p[:n])
	if err == io.EOF || (r.length > 0 && r.body.Bytes == r.length) {
		r.body.Complete = true
	}
	r.capture.mu.Unlock()
	return n, err
}

// Snapshot copies under the lock, then parses outside it. The immutable JSON
// travels to the usage worker; no gin.Context or live writer crosses goroutines.
func Snapshot(ctx context.Context) json.RawMessage {
	if ctx == nil {
		return nil
	}
	if value, ok := ctx.Value(snapshotKey{}).(json.RawMessage); ok {
		return value
	}
	c, _ := ctx.Value(contextKey{}).(*Capture)
	if c == nil {
		return nil
	}
	c.mu.Lock()
	down := c.downstream
	// Called when forwarding has finished and usage is submitted.
	down.Complete = !down.failed
	down.Body = string(down.buffer)
	var up *bodyCapture
	if c.upstream != nil {
		copy := *c.upstream
		copy.Body = string(copy.buffer)
		up = &copy
	}
	c.mu.Unlock()
	if down.Bytes == 0 && up == nil {
		return nil
	}
	record := Record{Downstream: analyze(down), Summary: Summary{Upstream: "unavailable"}}
	if up != nil {
		side := analyze(*up)
		record.Upstream = &side
		record.Summary.Upstream = side.Status
	}
	record.Summary.Downstream = record.Downstream.Status
	record.Downstream.escapeControls()
	if record.Upstream != nil {
		record.Upstream.escapeControls()
	}
	raw, _ := json.Marshal(record)
	return raw
}
func WithSnapshot(ctx context.Context, value json.RawMessage) context.Context {
	return context.WithValue(ctx, snapshotKey{}, value)
}

// PostgreSQL JSONB cannot represent U+0000. Analyze the untouched bytes first,
// then escape NUL for display so malformed upstream output cannot break billing.
func (s *Side) escapeControls() {
	if !strings.ContainsRune(s.Body, 0) {
		return
	}
	s.ControlsEscaped = true
	s.Body = strings.ReplaceAll(s.Body, "\x00", "\\x00")
	for i := range s.Issues {
		s.Issues[i].JSON = strings.ReplaceAll(s.Issues[i].JSON, "\x00", "\\x00")
		s.Issues[i].Extra = strings.ReplaceAll(s.Issues[i].Extra, "\x00", "\\x00")
	}
}
