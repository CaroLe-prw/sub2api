package responsediag

import (
	"context"
	"encoding/json"
)

// Storage uses an explicit allowlist: request bodies, response bodies and issue
// excerpts must never enter usage_logs, including through a caller-supplied snapshot.
type storedRecord struct {
	BodiesOmitted bool        `json:"bodies_omitted"`
	Summary       *Summary    `json:"summary,omitempty"`
	Upstream      *storedSide `json:"upstream,omitempty"`
	Downstream    *storedSide `json:"downstream,omitempty"`
}

type storedSide struct {
	LimitBytes    int           `json:"limit_bytes,omitempty"`
	Bytes         int64         `json:"bytes"`
	Truncated     bool          `json:"truncated"`
	Complete      bool          `json:"complete"`
	Status        string        `json:"status"`
	JSONDocuments int           `json:"json_documents"`
	Issues        []storedIssue `json:"issues,omitempty"`
}

type storedIssue struct {
	Kind  string `json:"kind"`
	Frame int    `json:"frame"`
}

// ForStorage removes all captured content. Invalid or oversized diagnostics are
// discarded rather than allowing optional diagnostics to break usage billing.
func ForStorage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var record storedRecord
	if json.Unmarshal(raw, &record) != nil || (record.Summary == nil && record.Upstream == nil && record.Downstream == nil) {
		return nil
	}
	record.BodiesOmitted = true
	for _, side := range []*storedSide{record.Upstream, record.Downstream} {
		if side != nil && len(side.Issues) > 8 {
			side.Issues = side.Issues[:8]
		}
	}
	value, err := json.Marshal(record)
	if err != nil || len(value) > 8*1024 {
		return nil
	}
	return value
}

// StorageSnapshot keeps the usage worker queue free of raw captured content.
// Response analysis and stream timing still use bounded in-memory captures.
func StorageSnapshot(ctx context.Context) json.RawMessage {
	return ForStorage(Snapshot(ctx))
}
