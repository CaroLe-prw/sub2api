package responsediag

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
)

// Each SSE frame is a separate JSON document. Multiple normal frames, comments,
// and [DONE] are protocol framing, not concatenated JSON corruption.
func analyze(b bodyCapture) Side {
	s := Side{bodyCapture: b, Status: "ok"}
	data := strings.Trim(b.Body, " \t\r\n")
	if strings.Contains(b.ContentType, "event-stream") || strings.HasPrefix(data, "data:") || strings.HasPrefix(data, "event:") || strings.HasPrefix(data, ":") {
		var lines []string
		frame := 0
		done := false
		flush := func(partial bool) {
			if len(lines) == 0 {
				return
			}
			frame++
			payload := strings.Join(lines, "\n")
			lines = nil
			if done {
				s.issue(Issue{Kind: "after_done", Frame: frame, Extra: payload})
				return
			}
			if strings.TrimSpace(payload) == "[DONE]" {
				done = true
				return
			}
			s.document(payload, frame, partial)
		}
		inputLines := strings.Split(strings.ReplaceAll(b.Body, "\r\n", "\n"), "\n")
		// A trailing split sentinel is not another blank SSE line. In
		// particular, a cutoff after a single newline does not finish a frame.
		if len(inputLines) > 0 && inputLines[len(inputLines)-1] == "" {
			inputLines = inputLines[:len(inputLines)-1]
		}
		for _, line := range inputLines {
			if line == "" {
				flush(false)
				continue
			}
			if line == "data" {
				lines = append(lines, "")
			} else if strings.HasPrefix(line, "data:") {
				lines = append(lines, strings.TrimPrefix(line[5:], " "))
			}
		}
		flush(b.Truncated || !b.Complete)
	} else if data != "" {
		s.document(data, 1, b.Truncated || !b.Complete)
	}
	if s.Status == "ok" || s.Status == "tail_symbols" {
		if b.Truncated {
			s.Status = "truncated"
		} else if !b.Complete {
			s.Status = "incomplete"
		} else if s.JSONDocuments == 0 {
			s.Status = "non_json"
		}
	}
	return s
}
func (s *Side) issue(issue Issue) {
	if isProtocolTail(issue.Extra) {
		issue.Kind = "trailing_symbols"
		if s.Status == "ok" {
			s.Status = "tail_symbols"
		}
	} else {
		s.Status = "extra_content"
	}
	// Bound diagnostic duplication as well as the raw capture.
	if len(issue.JSON) > 4096 {
		issue.JSON = issue.JSON[:4096]
	}
	if len(issue.Extra) > 4096 {
		issue.Extra = issue.Extra[:4096]
	}
	if len(s.Issues) < 8 {
		s.Issues = append(s.Issues, issue)
	} else if issue.Kind != "trailing_symbols" {
		// Informational tails must not hide later substantive content.
		for i := len(s.Issues) - 1; i >= 0; i-- {
			if s.Issues[i].Kind == "trailing_symbols" {
				s.Issues[i] = issue
				break
			}
		}
	}
}
func (s *Side) document(payload string, frame int, partial bool) {
	decoder := json.NewDecoder(strings.NewReader(payload))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		kind := "invalid_json"
		status := "invalid"
		// Only an unfinished final event at the capture boundary can be
		// explained by truncation. Earlier malformed events remain errors.
		if partial && (errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)) {
			kind, status = "incomplete_capture", "incomplete"
			if s.Truncated {
				kind, status = "capture_truncated", "truncated"
			}
		}
		if s.Status != "extra_content" && s.Status != "invalid" {
			s.Status = status
		}
		if len(s.Issues) < 8 {
			s.Issues = append(s.Issues, Issue{Kind: kind, Frame: frame})
		}
		return
	}

	s.JSONDocuments++
	if extra := strings.Trim(payload[decoder.InputOffset():], " \t\r\n"); extra != "" {
		s.issue(Issue{Kind: "trailing_content", Frame: frame, JSON: string(raw), Extra: extra})
	}
}

// RefreshAnalysis updates the display of previously stored captures without
// rewriting history or changing their recorded bytes.
func RefreshAnalysis(raw json.RawMessage) json.RawMessage {
	var record Record
	if json.Unmarshal(raw, &record) != nil || record.Downstream.Status == "" {
		return raw
	}
	if !record.Downstream.ControlsEscaped {
		record.Downstream = analyze(record.Downstream.bodyCapture)
	}
	record.Summary.Downstream = record.Downstream.Status
	if record.Upstream != nil {
		if !record.Upstream.ControlsEscaped {
			side := analyze(record.Upstream.bodyCapture)
			record.Upstream = &side
		}
		record.Summary.Upstream = record.Upstream.Status
	}
	updated, err := json.Marshal(record)
	if err != nil {
		return raw
	}
	return updated
}

// Recognized framing residue is informational, not evidence of added content.
// Do not ignore JSON objects, strings, numbers, or arbitrary text as symbols.
func isProtocolTail(tail string) bool {
	if tail == "" {
		return false
	}
	trimmed := strings.TrimSpace(tail)
	if trimmed == ": keepalive" || trimmed == ": ping" || trimmed == "[DONE]" {
		return true
	}
	// Even punctuation-only JSON, such as {} or [], is an additional document.
	if json.Valid([]byte(trimmed)) {
		return false
	}
	return strings.TrimFunc(tail, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.IsPunct(r) || strings.ContainsRune("+*=|~^$", r)
	}) == ""
}
