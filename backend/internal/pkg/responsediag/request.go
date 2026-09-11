package responsediag

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Inspect the complete JSON before redaction, then retain a bounded display
// excerpt. Never persist an unredacted prefix of oversized or malformed input.
const MaxRequestInspectBytes = 2 * MaxBodyBytes

type RequestRecord struct {
	LimitBytes           int    `json:"limit_bytes,omitempty"`
	InspectionLimitBytes int    `json:"inspection_limit_bytes,omitempty"`
	Method               string `json:"method"`
	ContentType          string `json:"content_type"`
	Body                 string `json:"body"`
	Bytes                int64  `json:"bytes"`
	Complete             bool   `json:"complete"`
	Truncated            bool   `json:"truncated"`
	Redacted             bool   `json:"redacted"`
	OmittedReason        string `json:"omitted_reason,omitempty"`
}
type requestCapture struct {
	method, contentType, omittedReason string
	limitBytes, inspectionLimitBytes   int
	raw                                []byte
	bytes                              int64
	complete, oversized                bool
}

// WrapRequest preserves the body, ContentLength, GetBody, and headers. It observes
// reads performed by the normal handler/transport and never reads ahead.
func WrapRequest(req *http.Request, upstream bool) *http.Request {
	if req == nil {
		return req
	}
	c, _ := req.Context().Value(contextKey{}).(*Capture)
	if c == nil {
		return req
	}
	ct, _, _ := mime.ParseMediaType(req.Header.Get("Content-Type"))
	capture := &requestCapture{method: req.Method, contentType: ct, limitBytes: c.maxBodyBytes, inspectionLimitBytes: 2 * c.maxBodyBytes}
	if (ct != "" && ct != "application/json" && !strings.HasSuffix(ct, "+json")) || req.Header.Get("Content-Encoding") != "" {
		capture.omittedReason = "unsupported_body"
	}
	if req.Body == nil || req.Body == http.NoBody {
		capture.complete = true
	}
	c.mu.Lock()
	if upstream {
		c.upstreamRequest = capture
		c.upstream = nil
	} else {
		c.incomingRequest = capture
	}
	for _, name := range []string{"Authorization", "Proxy-Authorization", "X-Api-Key", "Api-Key", "X-Goog-Api-Key", "Cookie"} {
		for _, value := range req.Header.Values(name) {
			c.addSecret(value)
			if strings.HasSuffix(strings.ToLower(name), "authorization") {
				if _, token, ok := strings.Cut(value, " "); ok {
					c.addSecret(strings.TrimSpace(token))
				}
			}
		}
	}
	// Query credentials are never recorded, but scrub them if echoed in the body.
	if req.URL != nil {
		for key, values := range req.URL.Query() {
			if sensitiveRequestKey(key) {
				for _, value := range values {
					c.addSecret(value)
				}
			}
		}
	}
	c.mu.Unlock()
	if capture.complete {
		return req
	}
	clone := req.Clone(req.Context())
	clone.Body = &requestReader{ReadCloser: req.Body, capture: c, body: capture, length: req.ContentLength}
	return clone
}
func (c *Capture) addSecret(value string) {
	if value == "" {
		return
	}
	for _, existing := range c.secrets {
		if existing == value {
			return
		}
	}
	c.secrets = append(c.secrets, value)
}

type requestReader struct {
	io.ReadCloser
	capture *Capture
	body    *requestCapture
	length  int64
}

func (r *requestReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.capture.mu.Lock()
	r.body.bytes += int64(n)
	if r.body.omittedReason == "" && !r.body.oversized {
		if len(r.body.raw)+n > r.body.inspectionLimitBytes {
			r.body.oversized = true
			r.body.raw = nil
		} else {
			r.body.raw = append(r.body.raw, p[:n]...)
		}
	}
	if err == io.EOF || (r.length > 0 && r.body.bytes == r.length) {
		r.body.complete = true
	}
	r.capture.mu.Unlock()
	return n, err
}

// Called under the capture lock. The detached copy cannot race with a transport
// still consuming a request body when an early upstream response arrives.
func copyRequestCapture(value *requestCapture) *requestCapture {
	if value == nil {
		return nil
	}
	result := *value
	result.raw = bytes.Clone(value.raw)
	return &result
}
func sanitizeRequest(value *requestCapture, secrets []string) *RequestRecord {
	if value == nil {
		return nil
	}
	record := &RequestRecord{LimitBytes: value.limitBytes, InspectionLimitBytes: value.inspectionLimitBytes, Method: value.method, ContentType: value.contentType, Bytes: value.bytes, Complete: value.complete, Redacted: true}
	switch {
	case value.omittedReason != "":
		record.OmittedReason = value.omittedReason
	case value.oversized:
		record.OmittedReason = "inspection_limit"
		record.Truncated = true
	case !value.complete:
		record.OmittedReason = "incomplete_body"
	case value.bytes == 0: // An empty body needs no sanitization.
	default:
		if !json.Valid(value.raw) {
			record.OmittedReason = "invalid_json"
			break
		}
		decoder := json.NewDecoder(bytes.NewReader(value.raw))
		decoder.UseNumber()
		var payload any
		if decoder.Decode(&payload) != nil {
			record.OmittedReason = "invalid_json"
			break
		}
		payload = redactRequestValue(payload, secrets)
		sanitized, err := json.Marshal(payload)
		if err != nil {
			record.OmittedReason = "invalid_json"
			break
		}
		if len(sanitized) > value.limitBytes {
			sanitized = sanitized[:value.limitBytes]
			for !utf8.Valid(sanitized) {
				sanitized = sanitized[:len(sanitized)-1]
			}
			record.Truncated = true
		}
		record.Body = string(sanitized)
	}
	return record
}
func sensitiveRequestKey(key string) bool {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
	switch normalized {
	case "authorization", "proxyauthorization", "apikey", "xapikey", "xgoogapikey", "accesskey", "accesskeyid", "secretaccesskey", "accesskeysecret", "token", "accesstoken", "refreshtoken", "idtoken", "password", "passwd", "secret", "clientsecret", "credential", "credentials", "cookie", "setcookie", "headers", "httpheaders", "privatekey":
		return true
	}
	return strings.HasSuffix(normalized, "apikey") || strings.HasSuffix(normalized, "password") || strings.HasSuffix(normalized, "secret")
}
func redactRequestValue(value any, secrets []string) any {
	switch value := value.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(value))
		for key, child := range value {
			cleanKey := key
			for _, secret := range secrets {
				cleanKey = strings.ReplaceAll(cleanKey, secret, "[REDACTED]")
			}
			if sensitiveRequestKey(key) {
				cleaned[cleanKey] = "[REDACTED]"
			} else {
				cleaned[cleanKey] = redactRequestValue(child, secrets)
			}
		}
		return cleaned
	case []any:
		for i := range value {
			value[i] = redactRequestValue(value[i], secrets)
		}
		return value
	case string:
		for _, secret := range secrets {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
		// JSONB cannot represent a decoded NUL; display it as escaped text.
		return strings.ReplaceAll(value, "\x00", "\\x00")
	default:
		return value
	}
}
