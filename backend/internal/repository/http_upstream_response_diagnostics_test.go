package repository

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	require.Equal(t, "extra_content", record.Upstream.Status)
	require.True(t, record.Upstream.Complete)
	require.Equal(t, `{"type":"response.completed"}`, record.Upstream.Issues[0].Extra)
}

func TestHTTPUpstreamCapturesTheForwardedRequestWithoutSendingRedactions(t *testing.T) {
	const payload = `{"model":"mapped-model","input":"hello","api_key":"actual-secret"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != payload || r.Header.Get("Authorization") != "Bearer upstream-secret" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	ctx, _ := responsediag.Start(t.Context())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(payload))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer upstream-secret")
	response, err := NewHTTPUpstream(nil).Do(request, "", 1, 1)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	var record responsediag.Record
	snapshot := responsediag.Snapshot(ctx)
	require.NoError(t, json.Unmarshal(snapshot, &record))
	require.Contains(t, record.UpstreamRequest.Body, "mapped-model")
	require.NotContains(t, string(snapshot), "actual-secret")
	require.NotContains(t, string(snapshot), "upstream-secret")
	require.Equal(t, int64(len(payload)), record.UpstreamRequest.Bytes)
}
