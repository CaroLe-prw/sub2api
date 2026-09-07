package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const responseModelMappingKey = "response_model_mapping"

// NormalizeResponseModelMappingCredentials validates an optional, exact-match
// downstream alias table. It does not affect request routing or billing.
func NormalizeResponseModelMappingCredentials(credentials map[string]any) error {
	raw, exists := credentials[responseModelMappingKey]
	if !exists || raw == nil {
		return nil
	}
	mapping, ok := raw.(map[string]any)
	if typed, typedOK := raw.(map[string]string); typedOK {
		mapping = make(map[string]any, len(typed))
		for from, to := range typed {
			mapping[from] = to
		}
		ok = true
	}
	invalid := func() error {
		return infraerrors.New(http.StatusBadRequest, "INVALID_RESPONSE_MODEL_MAPPING",
			"Response model mapping must contain at most 64 unique, non-empty model pairs (up to 200 characters each, no control characters)")
	}
	if !ok || len(mapping) > 64 {
		return invalid()
	}
	normalized := make(map[string]any, len(mapping))
	for from, rawTo := range mapping {
		to, ok := rawTo.(string)
		from, to = strings.TrimSpace(from), strings.TrimSpace(to)
		if !ok || from == "" || to == "" || len([]rune(from)) > 200 || len([]rune(to)) > 200 ||
			strings.ContainsFunc(from+to, unicode.IsControl) {
			return invalid()
		}
		if _, duplicate := normalized[from]; duplicate {
			return invalid()
		}
		normalized[from] = to
	}
	credentials[responseModelMappingKey] = normalized
	return nil
}

// Response aliases apply at the HTTP output boundary, after upstream observation
// and protocol conversion. This keeps original model evidence and usage intact.
// Each attempt owns its wrapper; retries restore the prior writer. Nested
// protocol forwarding must not apply aliases twice (a -> b -> c).
func installResponseModelMapping(c *gin.Context, account *Account) func() {
	noop := func() {}
	if c == nil || c.Writer == nil || account == nil || account.Type != AccountTypeAPIKey {
		return noop
	}
	if _, installed := c.Writer.(*responseModelMappingWriter); installed {
		return noop
	}
	mapping := stringMappingFromRaw(account.Credentials[responseModelMappingKey])
	if len(mapping) == 0 {
		return noop
	}
	previous := c.Writer
	w := &responseModelMappingWriter{ResponseWriter: previous, mapping: mapping}
	c.Writer = w
	return func() {
		// Preserve malformed/truncated upstream payloads without inventing data.
		if len(w.pending) > 0 {
			_, _ = previous.Write(w.pending)
		}
		c.Writer = previous
	}
}

type responseModelMappingWriter struct {
	gin.ResponseWriter
	mapping map[string]string
	pending []byte
	bypass  bool
}

func (w *responseModelMappingWriter) WriteHeaderNow() {
	// A rewritten body can have a different byte length.
	w.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeaderNow()
}

func (w *responseModelMappingWriter) Flush() {
	w.WriteHeaderNow()
	w.ResponseWriter.Flush()
}

func (w *responseModelMappingWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *responseModelMappingWriter) Write(p []byte) (int, error) {
	ct := w.Header().Get("Content-Type")
	stream := strings.HasPrefix(ct, "text/event-stream")
	jsonBody := strings.Contains(ct, "application/json")
	encoding := w.Header().Get("Content-Encoding")
	if w.bypass || w.Status() >= 400 || (!stream && !jsonBody) || (encoding != "" && encoding != "identity") {
		return w.ResponseWriter.Write(p)
	}
	w.WriteHeaderNow()
	w.pending = append(w.pending, p...)
	// Bound memory for malformed or unusually large individual payloads. Once
	// exceeded, preserve this response verbatim rather than truncate it.
	if len(w.pending) > 16<<20 {
		w.bypass = true
		_, err := w.ResponseWriter.Write(w.pending)
		w.pending = nil
		return len(p), err
	}
	if jsonBody {
		if !gjson.ValidBytes(w.pending) {
			return len(p), nil
		}
		_, err := w.ResponseWriter.Write(mapResponseModelPayload(w.pending, w.mapping))
		w.pending = nil
		return len(p), err
	}
	for {
		// SSE events may span writes and may use LF or CRLF line endings.
		end, delimiter := bytes.Index(w.pending, []byte("\n\n")), 2
		if crlf := bytes.Index(w.pending, []byte("\r\n\r\n")); crlf >= 0 && (end < 0 || crlf < end) {
			end, delimiter = crlf, 4
		}
		if end < 0 {
			return len(p), nil
		}
		frame := w.pending[:end+delimiter]
		_, err := w.ResponseWriter.Write(mapResponseModelSSEFrame(frame, w.mapping))
		w.pending = w.pending[end+delimiter:]
		if err != nil {
			w.pending = nil
			return len(p), err
		}
	}
}

func mapResponseModelPayload(payload []byte, mapping map[string]string) []byte {
	if !gjson.ValidBytes(payload) {
		return payload
	}
	for _, path := range []string{"model", "message.model", "response.model"} {
		model := gjson.GetBytes(payload, path)
		if model.Type != gjson.String {
			continue
		}
		if to := mapping[model.Str]; to != "" && to != model.Str {
			if updated, err := sjson.SetBytes(payload, path, to); err == nil {
				payload = updated
			}
		}
	}
	return payload
}

func mapResponseModelSSEFrame(frame []byte, mapping map[string]string) []byte {
	lines := bytes.Split(frame, []byte("\n"))
	var data [][]byte
	first := -1
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("data:")) {
			if first < 0 {
				first = i
			}
			value := bytes.TrimSuffix(line[5:], []byte("\r"))
			data = append(data, bytes.TrimPrefix(value, []byte(" ")))
		}
	}
	payload := bytes.Join(data, []byte("\n"))
	mapped := mapResponseModelPayload(payload, mapping)
	if first < 0 || bytes.Equal(payload, mapped) {
		return frame
	}
	// Compact multi-line JSON into one data field, retaining event/id/comments.
	var compact bytes.Buffer
	if err := json.Compact(&compact, mapped); err != nil {
		return frame
	}
	output := make([][]byte, 0, len(lines))
	for i, line := range lines {
		if i == first {
			ending := ""
			if bytes.HasSuffix(line, []byte("\r")) {
				ending = "\r"
			}
			output = append(output, []byte("data: "+compact.String()+ending))
		} else if !bytes.HasPrefix(line, []byte("data:")) {
			output = append(output, line)
		}
	}
	return bytes.Join(output, []byte("\n"))
}
