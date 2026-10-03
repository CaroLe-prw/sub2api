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

func TestFailedUpstreamRetryDoesNotPersistPreviousResponse(t *testing.T) {
	ctx, _ := Start(context.Background())
	response := &http.Response{Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{}`))}
	WrapUpstream(ctx, response)
	_, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotNil(t, StorageSnapshot(ctx))
	StartUpstreamAttempt(ctx)
	require.Nil(t, StorageSnapshot(ctx))
}

func TestStorageSnapshotOmitsContentAndPreservesAnalysis(t *testing.T) {
	ctx, capture := Start(context.Background())
	response := `{"text":"` + strings.Repeat("private-response", 20000) + `"}private-tail`
	capture.WriteDownstream([]byte(response), "application/json", false)
	raw := StorageSnapshot(ctx)
	require.Less(t, len(raw), 1024)
	require.NotContains(t, string(raw), "private-")
	require.NotContains(t, string(raw), `"body"`)
	require.NotContains(t, string(raw), `"json"`)
	require.NotContains(t, string(raw), `"extra"`)
	var record Record
	require.NoError(t, json.Unmarshal(raw, &record))
	require.True(t, record.BodiesOmitted)
	require.Equal(t, "extra_content", record.Summary.Downstream)
	require.Equal(t, int64(len(response)), record.Downstream.Bytes)
	require.Equal(t, 1, record.Downstream.JSONDocuments)
	require.Equal(t, "trailing_content", record.Downstream.Issues[0].Kind)
	require.Equal(t, 1, record.Downstream.Issues[0].Frame)
	// A metadata-only record cannot be reanalyzed as an empty/non-JSON body.
	require.JSONEq(t, string(raw), string(RefreshAnalysis(raw)))
	require.JSONEq(t, string(raw), string(StorageSnapshot(WithSnapshot(context.Background(), raw))))
}

func TestForStorageFiltersSuppliedSnapshotAndBoundsMetadata(t *testing.T) {
	payload := map[string]any{
		"incoming_request": map[string]string{"body": strings.Repeat("private-input", 10000)},
		"upstream_request": map[string]string{"body": "private-forwarded"},
		"summary":          Summary{Upstream: "ok", Downstream: "ok"},
		"upstream":         map[string]any{"body": "private-upstream", "status": "ok", "bytes": 123},
		"downstream":       map[string]any{"body": "private-downstream", "status": "ok"},
		"future_raw_field": "private-unknown",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	stored := StorageSnapshot(WithSnapshot(context.Background(), raw))
	require.Less(t, len(stored), 1024)
	require.NotContains(t, string(stored), "private-")
	require.NotContains(t, string(stored), "request")
	require.JSONEq(t, string(stored), string(ForStorage(stored)))
	for _, invalid := range []json.RawMessage{nil, []byte(`null`), []byte(`{}`), []byte(`not json`)} {
		require.Nil(t, ForStorage(invalid))
	}
	raw, err = json.Marshal(map[string]any{"summary": Summary{Downstream: strings.Repeat("x", 9000)}})
	require.NoError(t, err)
	require.Nil(t, ForStorage(raw))

	issues := make([]storedIssue, 100)
	for i := range issues {
		issues[i] = storedIssue{Kind: "invalid_json", Frame: i + 1}
	}
	raw, err = json.Marshal(storedRecord{Downstream: &storedSide{Status: "invalid", Issues: issues}})
	require.NoError(t, err)
	var bounded storedRecord
	require.NoError(t, json.Unmarshal(ForStorage(raw), &bounded))
	require.Len(t, bounded.Downstream.Issues, 8)
}
