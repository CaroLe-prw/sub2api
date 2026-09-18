package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
)

const (
	opsDiagnosticCaptureLimit = 64 * 1024
	opsDiagnosticBodyLimit    = 16 * 1024
	opsDiagnosticAttemptLimit = 4
	opsDiagnosticJSONLimit    = 192 * 1024
)

type OpsPayloadSnapshot struct {
	Method        string `json:"method,omitempty"`
	Path          string `json:"path,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	Body          string `json:"body,omitempty"`
	Bytes         int64  `json:"bytes"`
	Truncated     bool   `json:"truncated"`
	OmittedReason string `json:"omitted_reason,omitempty"`
}

type OpsDiagnosticAttempt struct {
	AccountID  int64              `json:"account_id"`
	StatusCode int                `json:"status_code"`
	RequestID  string             `json:"request_id,omitempty"`
	Request    OpsPayloadSnapshot `json:"request"`
	Response   OpsPayloadSnapshot `json:"response"`
}

type OpsRequestDiagnostics struct {
	ClientRequest    OpsPayloadSnapshot     `json:"client_request"`
	UpstreamAttempts []OpsDiagnosticAttempt `json:"upstream_attempts"`
	DroppedAttempts  int                    `json:"dropped_attempts,omitempty"`
}

type opsDiagnosticsKey struct{}
type opsBodyCapture struct {
	io.ReadCloser
	mu    sync.Mutex
	data  []byte
	bytes int64
}

func (c *opsBodyCapture) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.mu.Lock()
	c.bytes += int64(n)
	remaining := opsDiagnosticCaptureLimit - len(c.data)
	if remaining > n {
		remaining = n
	}
	if remaining > 0 {
		c.data = append(c.data, p[:remaining]...)
	}
	c.mu.Unlock()
	return n, err
}
func (c *opsBodyCapture) payload() ([]byte, int64) {
	if c == nil {
		return nil, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return bytes.Clone(c.data), c.bytes
}

type opsDiagnosticExchange struct {
	accountID       int64
	status          int
	requestID       string
	request         *http.Request
	requestBody     []byte
	requestBytes    int64
	requestOmission string
	response        *opsBodyCapture
	responseType    string
	networkError    bool
}
type opsDiagnosticsRecorder struct {
	mu         sync.Mutex
	client     *http.Request
	clientBody *opsBodyCapture
	attempts   []opsDiagnosticExchange
	last       *opsDiagnosticExchange
	dropped    int
}

// EnableOpsRequestDiagnostics installs bounded, request-local capture. Only
// BuildOpsRequestDiagnosticsJSON on an error path produces a persistent snapshot.
func EnableOpsRequestDiagnostics(req *http.Request) *http.Request {
	if req == nil {
		return req
	}
	recorder := &opsDiagnosticsRecorder{client: opsDiagnosticRequestMetadata(req)}
	if req.Body != nil {
		// Preserve the zero-copy preread fast path used by ingress middleware.
		if preread, ok := req.Body.(*httputil.PrereadBody); ok {
			raw := preread.Bytes()
			keep := len(raw)
			if keep > opsDiagnosticCaptureLimit {
				keep = opsDiagnosticCaptureLimit
			}
			recorder.clientBody = &opsBodyCapture{data: bytes.Clone(raw[:keep]), bytes: int64(len(raw))}
		} else {
			recorder.clientBody = &opsBodyCapture{ReadCloser: req.Body}
			req.Body = recorder.clientBody
		}
	}
	return req.WithContext(context.WithValue(req.Context(), opsDiagnosticsKey{}, recorder))
}

// RecordOpsUpstreamExchange observes the exact HTTP request after conversion.
// Error responses are teed as the caller reads them; successful streams are not
// buffered. Nothing is consumed early, and credential headers are never copied
// into the persisted representation.
func RecordOpsUpstreamExchange(req *http.Request, resp *http.Response, requestErr error, accountID int64) {
	if req == nil {
		return
	}
	recorder, _ := req.Context().Value(opsDiagnosticsKey{}).(*opsDiagnosticsRecorder)
	if recorder == nil {
		return
	}
	exchange := opsDiagnosticExchange{accountID: accountID, request: opsDiagnosticRequestMetadata(req), networkError: requestErr != nil}
	exchange.requestBytes = req.ContentLength
	switch {
	case req.ContentLength > opsDiagnosticCaptureLimit:
		exchange.requestOmission = "too_large"
	case req.GetBody == nil:
		exchange.requestOmission = "unavailable"
	default:
		reader, err := req.GetBody()
		if err != nil {
			exchange.requestOmission = "read_error"
		} else {
			raw, readErr := io.ReadAll(io.LimitReader(reader, opsDiagnosticCaptureLimit+1))
			_ = reader.Close()
			exchange.requestBody = raw
			if int64(len(raw)) > exchange.requestBytes {
				exchange.requestBytes = int64(len(raw))
			}
			if readErr != nil {
				exchange.requestBody = nil
				exchange.requestOmission = "read_error"
			}
		}
	}
	if resp != nil {
		exchange.status = resp.StatusCode
		exchange.requestID = firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("request-id"))
		exchange.responseType = resp.Header.Get("Content-Type")
		if resp.StatusCode >= 400 && resp.Body != nil {
			exchange.response = &opsBodyCapture{ReadCloser: resp.Body}
			resp.Body = exchange.response
		}
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.last = &exchange
	if requestErr != nil || (resp != nil && resp.StatusCode >= 400) {
		if len(recorder.attempts) == opsDiagnosticAttemptLimit {
			copy(recorder.attempts, recorder.attempts[1:])
			recorder.attempts = recorder.attempts[:opsDiagnosticAttemptLimit-1]
			recorder.dropped++
		}
		recorder.attempts = append(recorder.attempts, exchange)
	}
}

func BuildOpsRequestDiagnosticsJSON(ctx context.Context) *string {
	if ctx == nil {
		return nil
	}
	recorder, _ := ctx.Value(opsDiagnosticsKey{}).(*opsDiagnosticsRecorder)
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	attempts := append([]opsDiagnosticExchange(nil), recorder.attempts...)
	dropped := recorder.dropped
	// A 2xx stream can fail later; retain its actual request, without claiming to
	// have captured the stream. Earlier failed attempts stay individually paired.
	if recorder.last != nil && recorder.last.status < 400 && !recorder.last.networkError {
		if len(attempts) == opsDiagnosticAttemptLimit {
			attempts = attempts[1:]
			dropped++
		}
		attempts = append(attempts, *recorder.last)
	}
	recorder.mu.Unlock()
	raw, count := recorder.clientBody.payload()
	result := OpsRequestDiagnostics{ClientRequest: opsRequestSnapshot(recorder.client, raw, count), UpstreamAttempts: make([]OpsDiagnosticAttempt, 0, len(attempts)), DroppedAttempts: dropped}
	for _, attempt := range attempts {
		item := OpsDiagnosticAttempt{AccountID: attempt.accountID, StatusCode: attempt.status, RequestID: truncateString(sanitizeUpstreamErrorMessage(attempt.requestID), 256)}
		item.Request = opsRequestSnapshot(attempt.request, attempt.requestBody, attempt.requestBytes)
		if attempt.requestOmission != "" {
			item.Request.Body = ""
			item.Request.OmittedReason = attempt.requestOmission
		}

		raw, count := attempt.response.payload()
		item.Response = opsPayloadSnapshot(raw, count, attempt.responseType)
		if attempt.response == nil {
			item.Response.OmittedReason = "not_captured"
		}
		result.UpstreamAttempts = append(result.UpstreamAttempts, item)
	}
	for {
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil
		}
		if len(encoded) <= opsDiagnosticJSONLimit {
			text := string(encoded)
			return &text
		}
		if len(result.UpstreamAttempts) == 0 {
			return nil
		}
		result.UpstreamAttempts = result.UpstreamAttempts[1:]
		result.DroppedAttempts++
	}
}

func opsRequestSnapshot(req *http.Request, raw []byte, count int64) OpsPayloadSnapshot {
	out := opsPayloadSnapshot(raw, count, req.Header.Get("Content-Type"))
	out.Method = truncateString(req.Method, 16)
	if req.URL != nil {
		out.Path = truncateString(redactContentModerationSecrets(req.URL.EscapedPath()), 1024)
	}
	if encoding := strings.TrimSpace(req.Header.Get("Content-Encoding")); encoding != "" && encoding != "identity" {
		out.Body = ""
		out.OmittedReason = "encoded_body"
	}
	return out
}
func opsPayloadSnapshot(raw []byte, count int64, contentType string) OpsPayloadSnapshot {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	out := OpsPayloadSnapshot{Bytes: count, ContentType: truncateString(mediaType, 128)}
	if count > opsDiagnosticCaptureLimit || len(raw) > opsDiagnosticCaptureLimit {
		out.Truncated = true
		out.OmittedReason = "too_large"
		return out
	}
	if len(raw) == 0 {
		out.OmittedReason = "empty_or_unread"
		return out
	}
	if !json.Valid(raw) {
		out.OmittedReason = "non_json"
		return out
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		out.OmittedReason = "non_json"
		return out
	}
	value = redactOpsDiagnosticValue(value, 0, &out.Truncated)
	encoded, err := json.Marshal(value)
	if err != nil {
		out.OmittedReason = "unavailable"
		return out
	}
	out.Body = string(encoded)
	if len(out.Body) > opsDiagnosticBodyLimit {
		out.Body = truncateString(out.Body, opsDiagnosticBodyLimit)
		out.Truncated = true
	}
	return out
}
func redactOpsDiagnosticValue(value any, depth int, truncated *bool) any {
	if depth > 24 {
		*truncated = true
		return "[TRUNCATED]"
	}
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			switch strings.ToLower(key) {
			case "cookie", "set-cookie", "data", "file_data", "b64_json", "encrypted_content":
				v[key] = "[REDACTED]"
				continue
			}
			if isSensitiveKey(key) {
				v[key] = "[REDACTED]"
			} else {
				v[key] = redactOpsDiagnosticValue(item, depth+1, truncated)
			}
		}
	case []any:
		if len(v) > 64 {
			v = v[:64]
			*truncated = true
		}
		for i := range v {
			v[i] = redactOpsDiagnosticValue(v[i], depth+1, truncated)
		}
		return v
	case string:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "data:") {
			return "[BINARY DATA OMITTED]"
		}
		v = redactContentModerationSecrets(v)
		if len(v) > 2048 {
			v = truncateString(v, 2048)
			*truncated = true
		}
		return v
	}
	return value
}

// Keep only metadata and bounded body copies; never retain a GetBody closure
// that can pin an entire multi-megabyte request across retries.
func opsDiagnosticRequestMetadata(req *http.Request) *http.Request {
	out := &http.Request{Method: req.Method, Header: make(http.Header)}
	out.Header.Set("Content-Type", req.Header.Get("Content-Type"))
	out.Header.Set("Content-Encoding", req.Header.Get("Content-Encoding"))
	if req.URL != nil {
		out.URL = &url.URL{Path: req.URL.Path, RawPath: req.URL.RawPath}
	}
	return out
}
