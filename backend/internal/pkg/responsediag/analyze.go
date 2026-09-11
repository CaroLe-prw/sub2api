package responsediag

import (
	"encoding/json"
	"strings"
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
		flush := func() {
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
			s.document(payload, frame)
		}
		for _, line := range strings.Split(strings.ReplaceAll(b.Body, "\r\n", "\n"), "\n") {
			if line == "" {
				flush()
				continue
			}
			if line == "data" {
				lines = append(lines, "")
			} else if strings.HasPrefix(line, "data:") {
				lines = append(lines, strings.TrimPrefix(line[5:], " "))
			}
		}
		flush()
	} else if data != "" {
		s.document(data, 1)
	}
	if s.Status == "ok" {
		if b.Truncated || !b.Complete {
			s.Status = "incomplete"
		} else if s.JSONDocuments == 0 {
			s.Status = "non_json"
		}
	}
	return s
}
func (s *Side) issue(issue Issue) {
	s.Status = "extra"
	// Bound diagnostic duplication as well as the raw capture.
	if len(s.Issues) < 8 {
		if len(issue.JSON) > 4096 {
			issue.JSON = issue.JSON[:4096]
		}
		if len(issue.Extra) > 4096 {
			issue.Extra = issue.Extra[:4096]
		}
		s.Issues = append(s.Issues, issue)
	}
}
func (s *Side) document(payload string, frame int) {
	decoder := json.NewDecoder(strings.NewReader(payload))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		if s.Status != "extra" {
			s.Status = "invalid"
		}
		if len(s.Issues) < 8 {
			s.Issues = append(s.Issues, Issue{Kind: "invalid_json", Frame: frame})
		}
		if s.Truncated || !s.Complete {
			if s.Status != "extra" {
				s.Status = "incomplete"
			}
		}
		return
	}
	s.JSONDocuments++
	if extra := strings.Trim(payload[decoder.InputOffset():], " \t\r\n"); extra != "" {
		s.issue(Issue{Kind: "trailing_content", Frame: frame, JSON: string(raw), Extra: extra})
	}
}
