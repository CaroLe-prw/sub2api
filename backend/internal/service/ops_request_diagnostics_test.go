package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpsRequestDiagnosticsPreservesTrafficAndRedactsSnapshots(t *testing.T) {
	body := `{"input":[{"role":"user","content":{"type":"input_text","text":"hello"}}],"api_key":"sensitive-value","cookie":"session=secret","opaque":9007199254740993,"file_data":"private-binary"}`
	inbound := EnableOpsRequestDiagnostics(httptest.NewRequest("POST", "/v1/responses?api_key=secret", strings.NewReader(body)))
	got, err := io.ReadAll(inbound.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(got))
	for i := 0; i < 6; i++ {
		upstream, err := http.NewRequestWithContext(inbound.Context(), "POST", "https://upstream.example/v1/responses?token=secret", strings.NewReader(body))
		require.NoError(t, err)
		response := &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_type"},"access_token":"response-secret"}`))}
		RecordOpsUpstreamExchange(upstream, response, nil, int64(i+1))
		got, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.Contains(t, string(got), "response-secret")
		require.NoError(t, response.Body.Close())
	}
	result := BuildOpsRequestDiagnosticsJSON(inbound.Context())
	require.NotNil(t, result)
	require.NotContains(t, *result, "sensitive-value")
	require.NotContains(t, *result, "response-secret")
	require.NotContains(t, *result, "private-binary")
	require.NotContains(t, *result, "session=secret")
	require.NotContains(t, *result, "?token")
	parsed := gjson.Parse(*result)
	require.Equal(t, int64(4), parsed.Get("upstream_attempts.#").Int())
	require.Equal(t, int64(2), parsed.Get("dropped_attempts").Int())
	saved := gjson.Parse(parsed.Get("client_request.body").String())
	require.True(t, saved.Get("input.0.content").IsObject())
	require.Equal(t, "9007199254740993", saved.Get("opaque").Raw)
	require.Equal(t, "invalid_type", gjson.Parse(parsed.Get("upstream_attempts.0.response.body").String()).Get("error.code").String())
}

func TestOpsRequestDiagnosticsOmissionAndLimits(t *testing.T) {
	for _, body := range []string{`{"password":"secret`, strings.Repeat("secret", opsDiagnosticCaptureLimit)} {
		req := EnableOpsRequestDiagnostics(httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
		got, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(got))
		snapshot := BuildOpsRequestDiagnosticsJSON(req.Context())
		require.NotNil(t, snapshot)
		require.NotContains(t, *snapshot, "secret")
		require.NotEmpty(t, gjson.Get(*snapshot, "client_request.omitted_reason").String())
		require.Less(t, len(*snapshot), 1024)
	}
	require.Nil(t, BuildOpsRequestDiagnosticsJSON(context.Background()))
}

func TestOpsRequestDiagnosticsDoesNotPinLargeRequests(t *testing.T) {
	inbound := EnableOpsRequestDiagnostics(httptest.NewRequest("POST", "/v1/responses", nil))
	request, err := http.NewRequestWithContext(inbound.Context(), "POST", "https://example.com/v1/responses", strings.NewReader(strings.Repeat("x", opsDiagnosticCaptureLimit+1)))
	require.NoError(t, err)
	request.GetBody = func() (io.ReadCloser, error) {
		t.Fatal("oversized diagnostic copies must not be read")
		return nil, nil
	}
	RecordOpsUpstreamExchange(request, &http.Response{StatusCode: 400}, nil, 1)
	recorder := inbound.Context().Value(opsDiagnosticsKey{}).(*opsDiagnosticsRecorder)
	require.Nil(t, recorder.attempts[0].request.Body)
	require.Nil(t, recorder.attempts[0].request.GetBody)
	require.Empty(t, recorder.attempts[0].requestBody)
	snapshot := BuildOpsRequestDiagnosticsJSON(inbound.Context())
	require.Equal(t, "too_large", gjson.Get(*snapshot, "upstream_attempts.0.request.omitted_reason").String())
	require.Equal(t, *snapshot, *BuildOpsRequestDiagnosticsJSON(inbound.Context()))
}

func TestOpsRequestDiagnosticsExcludedFromUserDetails(t *testing.T) {
	detail := &OpsErrorLogDetail{RequestDiagnostics: `{"client_request":{"body":"admin-only-prompt"}}`}
	encoded, err := json.Marshal(ToUserErrorRequestDetail(detail))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "admin-only-prompt")
	require.NotContains(t, string(encoded), "request_diagnostics")
}

func TestOpsRequestDiagnosticsRedactsTextTokensAndDataURLs(t *testing.T) {
	snapshot := opsPayloadSnapshot([]byte(`{"input":[{"content":{"type":"input_image","image_url":"data:image/png;base64,private-image"}}],"text":"Bearer secret-long-token-123456789","refresh_token":"short"}`), 200, "application/json; api_key=secret")
	require.NotContains(t, snapshot.Body, "private-image")
	require.NotContains(t, snapshot.Body, "secret-long-token")
	require.NotContains(t, snapshot.Body, "short")
	require.Equal(t, "application/json", snapshot.ContentType)
	require.True(t, gjson.Get(snapshot.Body, "input.0.content").IsObject())
}
