package responsediag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestCapturePreservesWireBytesAndRedactsCredentials(t *testing.T) {
	ctx, _ := Start(context.Background())
	const original = `{"model":"client-model","max_tokens":1200,"metadata":{"api_key":"nested-secret"},"input":[{"content":"Use client-token-secret"}],"headers":{"Authorization":"hidden"},"n":9007199254740993,"client-token-secret":"also redact keys"}`
	incoming := httptest.NewRequest(http.MethodPost, "/v1/responses?api_key=query-token-secret", strings.NewReader(original)).WithContext(ctx)
	incoming.Header.Set("Authorization", "Bearer client-token-secret")
	incoming = WrapRequest(incoming, false)
	actual, err := io.ReadAll(incoming.Body)
	require.NoError(t, err)
	require.Equal(t, original, string(actual))
	const forwarded = `{"model":"upstream-model","input":"hello","client_secret":"upstream-secret"}`
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://upstream/v1/responses", strings.NewReader(forwarded))
	require.NoError(t, err)
	upstream.Header.Set("X-Api-Key", "upstream-header-secret")
	upstream = WrapRequest(upstream, true)
	replay, err := upstream.GetBody()
	require.NoError(t, err)
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.NoError(t, replay.Close())
	require.Equal(t, forwarded, string(replayed))
	actual, err = io.ReadAll(upstream.Body)
	require.NoError(t, err)
	require.Equal(t, forwarded, string(actual))
	require.Equal(t, int64(len(forwarded)), upstream.ContentLength)
	require.Equal(t, "upstream-header-secret", upstream.Header.Get("X-Api-Key"))
	snapshot := Snapshot(ctx)
	for _, secret := range []string{"nested-secret", "client-token-secret", "query-token-secret", "upstream-secret", "upstream-header-secret"} {
		require.NotContains(t, string(snapshot), secret)
	}
	var record Record
	require.NoError(t, json.Unmarshal(snapshot, &record))
	require.Contains(t, record.IncomingRequest.Body, `"max_tokens":1200`)
	require.Contains(t, record.IncomingRequest.Body, `9007199254740993`)
	require.Contains(t, record.IncomingRequest.Body, "[REDACTED]")
	require.Contains(t, record.UpstreamRequest.Body, "upstream-model")
	require.Equal(t, int64(len(original)), record.IncomingRequest.Bytes)
	refreshed := RefreshAnalysis(snapshot)
	var fresh Record
	require.NoError(t, json.Unmarshal(refreshed, &fresh))
	require.Equal(t, record.IncomingRequest, fresh.IncomingRequest)
	require.Equal(t, record.UpstreamRequest, fresh.UpstreamRequest)
}

func TestRequestCaptureNeverStoresUnsafePrefixes(t *testing.T) {
	for _, tc := range []struct {
		name, body, ct, reason string
		readAll                bool
	}{
		{"invalid", `{"api_key":"sensitive`, "application/json", "invalid_json", true},
		{"concatenated", `{"input":"hello"}{"api_key":"sensitive"}`, "application/json", "invalid_json", true},
		{"oversized", `{"input":"` + strings.Repeat("x", MaxRequestInspectBytes) + `","api_key":"sensitive"}`, "application/json", "inspection_limit", true},
		{"partial", `{"input":"hello","api_key":"sensitive"}`, "application/json", "incomplete_body", false},
		{"multipart", "sensitive multipart bytes", "multipart/form-data; boundary=abc", "unsupported_body", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := Start(context.Background())
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(tc.body)).WithContext(ctx)
			req.Header.Set("Content-Type", tc.ct)
			req = WrapRequest(req, false)
			if tc.readAll {
				_, err := io.ReadAll(req.Body)
				require.NoError(t, err)
			} else {
				_, err := io.ReadFull(req.Body, make([]byte, 10))
				require.NoError(t, err)
			}
			var record Record
			raw := Snapshot(ctx)
			require.NoError(t, json.Unmarshal(raw, &record))
			require.Equal(t, tc.reason, record.IncomingRequest.OmittedReason)
			require.Empty(t, record.IncomingRequest.Body)
			require.NotContains(t, string(raw), "sensitive")
		})
	}
}

func TestRequestCaptureRedactsBeforeDisplayTruncationAndTracksRetry(t *testing.T) {
	ctx, _ := Start(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"api_key":"sensitive","input":"`+strings.Repeat("x", MaxBodyBytes)+`"}`)).WithContext(ctx)
	req = WrapRequest(req, false)
	_, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	var record Record
	raw := Snapshot(ctx)
	require.NoError(t, json.Unmarshal(raw, &record))
	require.True(t, record.IncomingRequest.Truncated)
	require.True(t, record.IncomingRequest.Complete)
	require.Empty(t, record.IncomingRequest.OmittedReason)
	require.LessOrEqual(t, len(record.IncomingRequest.Body), MaxBodyBytes)
	require.NotContains(t, string(raw), "sensitive")
	for _, model := range []string{"first", "last"} {
		req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"`+model+`"}`)).WithContext(ctx)
		req = WrapRequest(req, true)
		_, err = io.ReadAll(req.Body)
		require.NoError(t, err)
	}
	require.NoError(t, json.Unmarshal(Snapshot(ctx), &record))
	require.Contains(t, record.UpstreamRequest.Body, "last")
	require.NotContains(t, record.UpstreamRequest.Body, "first")
}

func TestRequestCaptureSensitiveKeysAndEmptyBody(t *testing.T) {
	for _, key := range []string{"api_key", "apiKey", "API-KEY", "Authorization", "access_token", "refresh_token", "password", "clientSecret", "credentials", "http_headers"} {
		require.True(t, sensitiveRequestKey(key), key)
	}
	for _, key := range []string{"model", "messages", "input", "max_tokens", "max_output_tokens", "temperature"} {
		require.False(t, sensitiveRequestKey(key), key)
	}
	ctx, _ := Start(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	req = WrapRequest(req, false)
	require.Equal(t, http.NoBody, req.Body)
	var record Record
	require.NoError(t, json.Unmarshal(Snapshot(ctx), &record))
	require.True(t, record.IncomingRequest.Complete)
	require.Empty(t, record.IncomingRequest.Body)
	require.Empty(t, record.IncomingRequest.OmittedReason)
}

func TestConfiguredRequestCaptureAndInspectionLimit(t *testing.T) {
	const limit = 1024
	for _, tc := range []struct {
		length  int
		omitted string
	}{{1500, ""}, {2500, "inspection_limit"}} {
		ctx, _ := Start(context.Background(), limit)
		original := `{"input":"` + strings.Repeat("x", tc.length) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(original)).WithContext(ctx)
		req = WrapRequest(req, false)
		actual, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, original, string(actual))
		var record Record
		require.NoError(t, json.Unmarshal(Snapshot(ctx), &record))
		require.Equal(t, limit, record.IncomingRequest.LimitBytes)
		require.Equal(t, 2*limit, record.IncomingRequest.InspectionLimitBytes)
		require.Equal(t, tc.omitted, record.IncomingRequest.OmittedReason)
		require.True(t, record.IncomingRequest.Truncated)
		if tc.omitted == "" {
			require.Len(t, record.IncomingRequest.Body, limit)
		} else {
			require.Empty(t, record.IncomingRequest.Body)
		}
	}
}
