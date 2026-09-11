package repository

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/responsediag"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamCapturesDecompressedResponseWithoutChangingBytes(t *testing.T) {
	const payload = "data: {\"type\":\"response.in_progress\"}{\"type\":\"response.completed\"}\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Encoding", "gzip")
		compressed := gzip.NewWriter(w)
		_, _ = compressed.Write([]byte(payload))
		_ = compressed.Close()
	}))
	defer server.Close()
	ctx, _ := responsediag.Start(t.Context())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	request.Header.Set("Accept-Encoding", "gzip")
	upstream := NewHTTPUpstream(nil)
	resp, err := upstream.Do(request, "", 1, 1)
	require.NoError(t, err)
	actual, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, payload, string(actual))
	var record responsediag.Record
	require.NoError(t, json.Unmarshal(responsediag.Snapshot(ctx), &record))
	require.Equal(t, payload, record.Upstream.Body)
	require.Equal(t, "extra", record.Upstream.Status)
	require.True(t, record.Upstream.Complete)
	require.Equal(t, `{"type":"response.completed"}`, record.Upstream.Issues[0].Extra)
}
