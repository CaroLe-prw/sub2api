package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func apiKeyDeviceTestAccount(id int64, seed string) *Account {
	return &Account{
		ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key"},
		Extra: map[string]any{
			openAIAPIKeyDeviceConvergenceEnabledExtraKey: true,
			codexFingerprintSeedExtraKey:                 seed,
		},
	}
}

func apiKeyDeviceTestBody(installation string) []byte {
	return []byte(`{"model":"gpt-5.1","input":[{"role":"user","content":"keep /Users/device-a/workspace exactly"}],"previous_response_id":"resp-existing","prompt_cache_key":"cache-existing","client_metadata":{"installation_id":"` + installation + `","x-codex-installation-id":"` + installation + `","session_id":"session-existing","thread_id":"thread-existing","turn_id":"turn-existing","x-codex-window-id":"window-existing","large_number":9007199254740993,"x-codex-turn-metadata":"{\"installation_id\":\"` + installation + `\",\"x-codex-installation-id\":\"` + installation + `\",\"session_id\":\"session-existing\",\"thread_id\":\"thread-existing\",\"turn_id\":\"turn-existing\",\"window_id\":\"window-existing\",\"turn_started_at_unix_ms\":9007199254740993,\"sandbox\":\"seatbelt\",\"workspaces\":{\"/Users/device-a/project\":{}}}"}}`)
}

func apiKeyDeviceReadRequestBody(t *testing.T, request *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	return body
}

func TestAPIKeyDeviceBodyPreservesConversationAndUnrelatedMetadata(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	original := apiKeyDeviceTestBody("client-device-a")
	originalCopy := bytes.Clone(original)
	updated, changed, err := rewriteOpenAIAPIKeyDeviceBody(original, account)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, originalCopy, original, "the caller's retry/failover body must remain untouched")
	deviceID := gjson.GetBytes(updated, "client_metadata.installation_id").String()
	require.NotEmpty(t, deviceID)
	require.NotEqual(t, "client-device-a", deviceID)
	require.Equal(t, deviceID, gjson.GetBytes(updated, "client_metadata.x-codex-installation-id").String())
	for _, path := range []string{
		"input", "previous_response_id", "prompt_cache_key", "client_metadata.session_id",
		"client_metadata.thread_id", "client_metadata.turn_id", "client_metadata.x-codex-window-id",
		"client_metadata.large_number",
	} {
		require.Equal(t, gjson.GetBytes(original, path).Raw, gjson.GetBytes(updated, path).Raw, path)
	}
	beforeEmbedded := gjson.GetBytes(original, "client_metadata.x-codex-turn-metadata").String()
	afterEmbedded := gjson.GetBytes(updated, "client_metadata.x-codex-turn-metadata").String()
	require.Equal(t, deviceID, gjson.Get(afterEmbedded, "installation_id").String())
	require.Equal(t, deviceID, gjson.Get(afterEmbedded, "x-codex-installation-id").String())
	for _, path := range []string{"session_id", "thread_id", "turn_id", "window_id", "turn_started_at_unix_ms", "sandbox", "workspaces"} {
		require.Equal(t, gjson.Get(beforeEmbedded, path).Raw, gjson.Get(afterEmbedded, path).Raw, path)
	}
}

func TestAPIKeyDeviceBodyDoesNotInventMetadataFields(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	for _, body := range []string{
		`{"model":"gpt-5.1","input":"hello"}`,
		`{"model":"gpt-5.1","client_metadata":{"session_id":"keep","large_number":9007199254740993}}`,
		`{"model":"gpt-5.1","client_metadata":{"x-codex-turn-metadata":"{\"session_id\":\"keep\"}"}}`,
	} {
		t.Run(body, func(t *testing.T) {
			updated, changed, err := rewriteOpenAIAPIKeyDeviceBody([]byte(body), account)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, body, string(updated))
		})
	}
}

func TestAPIKeyDevicePayloadDoesNotMutateNestedOriginal(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	metadata := map[string]any{
		"installation_id": "client-device-a", "session_id": "session-original",
		"large_number":          json.Number("9007199254740993"),
		"x-codex-turn-metadata": `{"installation_id":"client-device-a","turn_id":"turn-original"}`,
	}
	original := map[string]any{
		"type": "response.create", "client_metadata": metadata,
		"previous_response_id": "resp-original", "prompt_cache_key": "cache-original",
	}
	updated, err := rewriteOpenAIAPIKeyDevicePayload(original, account)
	require.NoError(t, err)
	require.Equal(t, "client-device-a", metadata["installation_id"])
	require.Equal(t, `{"installation_id":"client-device-a","turn_id":"turn-original"}`, metadata["x-codex-turn-metadata"])
	updatedMetadata, ok := updated["client_metadata"].(map[string]any)
	require.True(t, ok)
	require.NotEqual(t, metadata["installation_id"], updatedMetadata["installation_id"])
	require.Equal(t, metadata["session_id"], updatedMetadata["session_id"])
	require.Equal(t, metadata["large_number"], updatedMetadata["large_number"])
	require.Equal(t, original["previous_response_id"], updated["previous_response_id"])
	require.Equal(t, original["prompt_cache_key"], updated["prompt_cache_key"])
}

func TestAPIKeyDeviceRequestIsStablePerAccountAndIsolatesDifferentAccounts(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	otherAccount := apiKeyDeviceTestAccount(502, "22222222-2222-4222-8222-222222222222")
	deviceIDs := make([]string, 0, 3)
	for i, item := range []struct {
		account *Account
		client  string
	}{{account, "device-a"}, {account, "device-b"}, {otherAccount, "device-a"}} {
		request, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", bytes.NewReader(apiKeyDeviceTestBody(item.client)))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("User-Agent", "configured-client/1.0")
		prepared, err := prepareOpenAIAPIKeyDeviceRequest(request, item.account)
		require.NoError(t, err, i)
		id := prepared.Header.Get("x-codex-installation-id")
		require.NotEmpty(t, id, i)
		require.Equal(t, id, gjson.GetBytes(apiKeyDeviceReadRequestBody(t, prepared), "client_metadata.installation_id").String(), i)
		require.Equal(t, "configured-client/1.0", prepared.Header.Get("User-Agent"))
		deviceIDs = append(deviceIDs, id)
	}
	require.Equal(t, deviceIDs[0], deviceIDs[1], "two clients on one upstream account share the installation ID")
	require.NotEqual(t, deviceIDs[0], deviceIDs[2], "different persisted account seeds remain independent")
}

func TestAPIKeyDeviceHeaderFinalizationKeepsUAAndConversationHeaders(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	account.Credentials["header_override_enabled"] = true
	account.Credentials["header_overrides"] = map[string]any{
		"user-agent":              "configured-client/2.0",
		"x-codex-installation-id": "configured-device-conflict",
	}
	headers := http.Header{
		"X-CODEX-INSTALLATION-ID": {"duplicate-client-a"},
		"x-codex-installation-id": {"duplicate-client-b"},
		"installation_id":         {"alias-client"},
		"User-Agent":              {"inbound-client"},
		"Session_id":              {"session-original"},
		"Thread-Id":               {"thread-original"},
		"X-Codex-Window-Id":       {"window-original"},
		"X-Codex-Turn-State":      {"opaque-turn-state"},
		"X-Codex-Turn-Metadata":   {`{"installation_id":"client-device","turn_id":"turn-original","n":9007199254740993}`},
	}
	account.ApplyHeaderOverrides(headers)
	// Include a noncanonical duplicate after the configurable override to exercise
	// the finalizer's casing-independent replacement rather than Header.Set alone.
	headers["X-CODEX-INSTALLATION-ID"] = []string{"duplicate-after-override"}
	require.NoError(t, applyOpenAIAPIKeyDeviceHeaders(headers, account))
	id := headers.Get("x-codex-installation-id")
	require.NotEmpty(t, id)
	require.NotEqual(t, "configured-device-conflict", id)
	count := 0
	for name, values := range headers {
		if strings.EqualFold(name, "x-codex-installation-id") {
			count++
			require.Equal(t, []string{id}, values)
		}
		if strings.EqualFold(name, "installation_id") {
			require.Equal(t, []string{id}, values)
		}
	}
	require.Equal(t, 1, count, "the wire request must carry one installation header")
	require.Equal(t, "configured-client/2.0", headers.Get("User-Agent"))
	for name, value := range map[string]string{
		"session_id": "session-original", "thread-id": "thread-original",
		"x-codex-window-id": "window-original", "x-codex-turn-state": "opaque-turn-state",
	} {
		require.Equal(t, value, headers.Get(name), name)
	}
	embedded := headers.Get("x-codex-turn-metadata")
	require.Equal(t, id, gjson.Get(embedded, "installation_id").String())
	require.Equal(t, "turn-original", gjson.Get(embedded, "turn_id").String())
	require.Equal(t, "9007199254740993", gjson.Get(embedded, "n").Raw)
}

func TestAPIKeyDeviceRequestRetainsOriginalAndReplayBody(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	body := apiKeyDeviceTestBody("client-device-a")
	request, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Codex-Installation-Id", "client-device-a")
	originalHeaders := request.Header.Clone()
	prepared, err := prepareOpenAIAPIKeyDeviceRequest(request, account)
	require.NoError(t, err)
	require.NotSame(t, request, prepared)
	require.Equal(t, originalHeaders, request.Header)
	updatedBody := apiKeyDeviceReadRequestBody(t, prepared)
	require.Equal(t, int64(len(updatedBody)), prepared.ContentLength)
	require.NotNil(t, prepared.GetBody)
	for i := 0; i < 2; i++ {
		replay, replayErr := prepared.GetBody()
		require.NoError(t, replayErr)
		replayedBody, readErr := io.ReadAll(replay)
		require.NoError(t, readErr)
		require.NoError(t, replay.Close())
		require.Equal(t, updatedBody, replayedBody)
	}
	off := *account
	off.Extra = nil
	unchanged, err := prepareOpenAIAPIKeyDeviceRequest(request, &off)
	require.NoError(t, err)
	require.Same(t, request, unchanged)
	require.Equal(t, body, apiKeyDeviceReadRequestBody(t, unchanged), "retrying the original through an off account must not inherit the prior account ID")
}

func TestAPIKeyDeviceRequestLeavesMultipartBytesUntouched(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	body := []byte("--image-boundary\r\nContent-Disposition: form-data; name=\"image\"; filename=\"input.png\"\r\nContent-Type: image/png\r\n\r\n\x89PNG\x00{\"client_metadata\":{\"installation_id\":\"binary-data\"}}\r\n--image-boundary--\r\n")
	request, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/images/edits", bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=image-boundary")
	prepared, err := prepareOpenAIAPIKeyDeviceRequest(request, account)
	require.NoError(t, err)
	require.NotEmpty(t, prepared.Header.Get("x-codex-installation-id"))
	require.Equal(t, request.Header.Get("Content-Type"), prepared.Header.Get("Content-Type"))
	require.Equal(t, int64(len(body)), prepared.ContentLength)
	require.Equal(t, body, apiKeyDeviceReadRequestBody(t, prepared))
}

func TestAPIKeyDeviceMultipartMetadataChangesWithoutTouchingImage(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	require.NoError(t, writer.SetBoundary("device-test-boundary"))
	metadata := `{"installation_id":"client-device","session_id":"session-original","n":9007199254740993}`
	require.NoError(t, writer.WriteField("client_metadata", metadata))
	require.NoError(t, writer.WriteField("prompt", "keep /Users/device-a/workspace and client-device"))
	file, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	imageBytes := []byte("\x89PNG\r\n\x00{\"installation_id\":\"binary-client-device\"}\xff\x00")
	_, err = file.Write(imageBytes)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	original := bytes.Clone(input.Bytes())
	request, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/images/edits", bytes.NewReader(original))
	require.NoError(t, err)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	prepared, err := prepareOpenAIAPIKeyDeviceRequest(request, account)
	require.NoError(t, err)
	updated := apiKeyDeviceReadRequestBody(t, prepared)
	require.Equal(t, int64(len(updated)), prepared.ContentLength)
	reader := multipart.NewReader(bytes.NewReader(updated), writer.Boundary())
	parts := 0
	for {
		part, readErr := reader.NextRawPart()
		if readErr == io.EOF {
			break
		}
		require.NoError(t, readErr)
		content, readErr := io.ReadAll(part)
		require.NoError(t, readErr)
		switch part.FormName() {
		case "client_metadata":
			require.Equal(t, prepared.Header.Get("x-codex-installation-id"), gjson.GetBytes(content, "installation_id").String())
			require.Equal(t, "session-original", gjson.GetBytes(content, "session_id").String())
			require.Equal(t, "9007199254740993", gjson.GetBytes(content, "n").Raw)
		case "prompt":
			require.Equal(t, "keep /Users/device-a/workspace and client-device", string(content))
		case "image":
			require.Equal(t, "input.png", part.FileName())
			require.Equal(t, imageBytes, content)
		default:
			t.Fatalf("unexpected multipart field %q", part.FormName())
		}
		parts++
	}
	require.Equal(t, 3, parts)
	require.Equal(t, original, apiKeyDeviceReadRequestBody(t, request))
}

func TestAPIKeyDeviceRequestRejectsNonReplayableJSON(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	request := httptest.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", bytes.NewReader(apiKeyDeviceTestBody("client-device")))
	request.Header.Set("Content-Type", "application/json")
	require.Nil(t, request.GetBody)
	_, err := prepareOpenAIAPIKeyDeviceRequest(request, account)
	require.Error(t, err)
}

func TestAPIKeyDeviceRequestRejectsEnabledAccountWithoutSeed(t *testing.T) {
	for _, seed := range []string{"", "not-a-uuid"} {
		t.Run(seed, func(t *testing.T) {
			account := apiKeyDeviceTestAccount(501, seed)
			request := httptest.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", bytes.NewReader(apiKeyDeviceTestBody("client-device")))
			request.Header.Set("Content-Type", "application/json")
			_, err := prepareOpenAIAPIKeyDeviceRequest(request, account)
			require.Error(t, err, "enabled convergence must not silently forward raw device identifiers")
		})
	}
}

type apiKeyDeviceCaptureUpstream struct {
	request *http.Request
	body    []byte
	calls   int
}

func (u *apiKeyDeviceCaptureUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	u.request = request.Clone(request.Context())
	var err error
	u.body, err = io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

func (u *apiKeyDeviceCaptureUpstream) DoWithTLS(request *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxyURL, accountID, concurrency)
}

func TestAPIKeyDeviceSharedHTTPTransportFinalizesEveryModelEndpoint(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	for _, endpoint := range []string{"responses", "responses/compact", "chat/completions", "embeddings", "images/generations"} {
		t.Run(endpoint, func(t *testing.T) {
			upstream := &apiKeyDeviceCaptureUpstream{}
			service := &OpenAIGatewayService{httpUpstream: upstream}
			request, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/"+endpoint, bytes.NewReader(apiKeyDeviceTestBody("client-device")))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("User-Agent", "configured-client/1.0")
			response, err := service.doOpenAIUpstream(request, "", account)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, 1, upstream.calls)
			id := upstream.request.Header.Get("x-codex-installation-id")
			require.NotEmpty(t, id)
			require.NotEqual(t, "client-device", id)
			require.Equal(t, id, gjson.GetBytes(upstream.body, "client_metadata.installation_id").String())
			require.Equal(t, "configured-client/1.0", upstream.request.Header.Get("User-Agent"))
		})
	}
	upstream := &apiKeyDeviceCaptureUpstream{}
	service := &OpenAIGatewayService{httpUpstream: upstream}
	badAccount := apiKeyDeviceTestAccount(502, "")
	request := httptest.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", strings.NewReader(`{"model":"gpt-5.1"}`))
	_, err := service.doOpenAIUpstream(request, "", badAccount)
	require.Error(t, err)
	require.Zero(t, upstream.calls)
}

func TestAPIKeyDeviceRejectsAmbiguousMetadataBeforeHTTPDispatch(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"root client metadata", `{"client_metadata":{"installation_id":"a"},"client_metadata":{"installation_id":"b"}}`},
		{"root response", `{"response":{"client_metadata":{"installation_id":"a"}},"response":{"client_metadata":{"installation_id":"b"}}}`},
		{"root session", `{"session":{"client_metadata":{"installation_id":"a"}},"session":{"client_metadata":{"installation_id":"b"}}}`},
		{"response client metadata", `{"response":{"client_metadata":{"installation_id":"a"},"client_metadata":{"installation_id":"b"}}}`},
		{"session client metadata", `{"session":{"client_metadata":{"installation_id":"a"},"client_metadata":{"installation_id":"b"}}}`},
		{"installation field", `{"client_metadata":{"installation_id":"a","installation_id":"b","n":9007199254740993}}`},
		{"installation header field", `{"client_metadata":{"x-codex-installation-id":"a","x-codex-installation-id":"b"}}`},
		{"embedded metadata field", `{"client_metadata":{"x-codex-turn-metadata":"{\"installation_id\":\"a\"}","x-codex-turn-metadata":"{\"installation_id\":\"b\"}"}}`},
		{"embedded string installation", `{"client_metadata":{"x-codex-turn-metadata":"{\"installation_id\":\"a\",\"installation_id\":\"b\"}"}}`},
		{"embedded string header installation", `{"client_metadata":{"x-codex-turn-metadata":"{\"x-codex-installation-id\":\"a\",\"x-codex-installation-id\":\"b\"}"}}`},
		{"embedded object installation", `{"client_metadata":{"x-codex-turn-metadata":{"installation_id":"a","installation_id":"b"}}}`},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
			upstream := &apiKeyDeviceCaptureUpstream{}
			service := &OpenAIGatewayService{httpUpstream: upstream}
			request, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", strings.NewReader(item.body))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("x-codex-installation-id", "client-device")
			_, err = service.doOpenAIUpstream(request, "", account)
			require.Error(t, err, "rewriting just one duplicate would leave another client identifier exposed")
			require.Zero(t, upstream.calls)
			require.Equal(t, "client-device", request.Header.Get("x-codex-installation-id"))
			require.Equal(t, item.body, string(apiKeyDeviceReadRequestBody(t, request)))

			// The feature is opt-in; existing accounts retain their previous
			// request handling even for ambiguous metadata.
			off := *account
			off.Extra = nil
			offRequest, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", strings.NewReader(item.body))
			require.NoError(t, err)
			offRequest.Header.Set("Content-Type", "application/json")
			response, err := service.doOpenAIUpstream(offRequest, "", &off)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, 1, upstream.calls)
			require.Equal(t, item.body, string(upstream.body))
		})
	}
}

func TestAPIKeyDeviceRejectsAmbiguousTurnMetadataHeadersBeforeHTTPDispatch(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	for _, headers := range []http.Header{
		{"X-Codex-Turn-Metadata": {`{"installation_id":"a"}`, `{"installation_id":"b"}`}},
		{"X-Codex-Turn-Metadata": {`{"installation_id":"a"}`}, "x-codex-turn-metadata": {`{"installation_id":"b"}`}},
		{"X-Codex-Turn-Metadata": {`{"installation_id":"a","installation_id":"b"}`}},
	} {
		upstream := &apiKeyDeviceCaptureUpstream{}
		service := &OpenAIGatewayService{httpUpstream: upstream}
		request, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", strings.NewReader(`{"model":"gpt-5.1"}`))
		require.NoError(t, err)
		request.Header = headers.Clone()
		request.Header.Set("Content-Type", "application/json")
		original := request.Header.Clone()
		_, err = service.doOpenAIUpstream(request, "", account)
		require.Error(t, err)
		require.Zero(t, upstream.calls)
		require.Equal(t, original, request.Header)
	}
}

func TestAPIKeyDeviceWSHandshakePreservesUserAgentAndContinuation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	account.Credentials["header_override_enabled"] = true
	account.Credentials["header_overrides"] = map[string]any{
		"user-agent": "configured-ws-client/1.0", "x-codex-installation-id": "override-device",
	}
	contextGin, _ := gin.CreateTestContext(httptest.NewRecorder())
	contextGin.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	contextGin.Request.Header.Set("User-Agent", "inbound-device-client")
	contextGin.Request.Header.Set("x-codex-installation-id", "inbound-device")
	contextGin.Request.Header.Set("session_id", "session-original")
	contextGin.Request.Header.Set("thread-id", "thread-original")
	contextGin.Request.Header.Set("x-codex-window-id", "window-original")
	service := &OpenAIGatewayService{}
	headers, _, err := service.buildOpenAIWSHeaders(context.Background(), contextGin, account, "test-key",
		OpenAIWSProtocolDecision{}, false, "opaque-turn-state",
		`{"installation_id":"inbound-device","turn_id":"turn-original"}`, "cache-original", "gpt-5.1", "")
	require.NoError(t, err)
	id := headers.Get("x-codex-installation-id")
	require.NotEmpty(t, id)
	require.NotEqual(t, "inbound-device", id)
	require.NotEqual(t, "override-device", id)
	require.Equal(t, "configured-ws-client/1.0", headers.Get("User-Agent"))
	require.Equal(t, "session-original", headers.Get("session_id"))
	require.Equal(t, "thread-original", headers.Get("thread-id"))
	require.Equal(t, "window-original", headers.Get("x-codex-window-id"))
	require.Equal(t, "opaque-turn-state", headers.Get(openAIWSTurnStateHeader))
	require.Equal(t, id, gjson.Get(headers.Get(openAIWSTurnMetadataHeader), "installation_id").String())
	require.Equal(t, "turn-original", gjson.Get(headers.Get(openAIWSTurnMetadataHeader), "turn_id").String())
	require.Equal(t, "inbound-device", contextGin.Request.Header.Get("x-codex-installation-id"))
}

type apiKeyDeviceCaptureFrameConn struct {
	payloads [][]byte
	types    []coderws.MessageType
}

func (c *apiKeyDeviceCaptureFrameConn) ReadFrame(context.Context) (coderws.MessageType, []byte, error) {
	return coderws.MessageText, nil, io.EOF
}

func (c *apiKeyDeviceCaptureFrameConn) WriteFrame(_ context.Context, kind coderws.MessageType, payload []byte) error {
	c.types = append(c.types, kind)
	c.payloads = append(c.payloads, bytes.Clone(payload))
	return nil
}

func (c *apiKeyDeviceCaptureFrameConn) Close() error { return nil }

func TestAPIKeyDeviceWSFrameWrapperCoversLaterTurnsAndSessionUpdates(t *testing.T) {
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	upstream := &apiKeyDeviceCaptureFrameConn{}
	wrapper := &openAIAPIKeyDeviceFrameConn{FrameConn: upstream, account: account}
	tests := []struct {
		kind coderws.MessageType
		body string
		path string
	}{
		{coderws.MessageText, `{"type":"response.create","client_metadata":{"installation_id":"first-client","session_id":"keep"},"prompt_cache_key":"cache-original"}`, "client_metadata"},
		{coderws.MessageBinary, `{"type":"response.create","response":{"client_metadata":{"installation_id":"second-client","session_id":"keep"}},"previous_response_id":"resp-original"}`, "response.client_metadata"},
		{coderws.MessageText, `{"type":"session.update","session":{"client_metadata":{"installation_id":"third-client","session_id":"keep"}},"tools":[]}`, "session.client_metadata"},
	}
	var firstID string
	for i, item := range tests {
		source := []byte(item.body)
		require.NoError(t, wrapper.WriteFrame(context.Background(), item.kind, source))
		require.Equal(t, item.body, string(source), "the client's frame remains available for retry")
		require.Len(t, upstream.payloads, i+1)
		actual := upstream.payloads[i]
		id := gjson.GetBytes(actual, item.path+".installation_id").String()
		require.NotEmpty(t, id)
		if i == 0 {
			firstID = id
		}
		require.Equal(t, firstID, id)
		require.Equal(t, "keep", gjson.GetBytes(actual, item.path+".session_id").String())
		for _, path := range []string{"type", "prompt_cache_key", "previous_response_id", "tools"} {
			require.Equal(t, gjson.Get(item.body, path).Raw, gjson.GetBytes(actual, path).Raw, path)
		}
		require.Equal(t, item.kind, upstream.types[i])
	}
	plain := []byte(`{"type":"response.cancel","response_id":"resp-original"}`)
	require.NoError(t, wrapper.WriteFrame(context.Background(), coderws.MessageText, plain))
	require.Equal(t, plain, upstream.payloads[len(upstream.payloads)-1])
}

func TestAPIKeyDeviceStagedIDsRejectOtherAccountsAndSessionModes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	contextGin, _ := gin.CreateTestContext(httptest.NewRecorder())
	account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
	for _, mode := range []codexFingerprintMode{codexFingerprintSession, codexFingerprintFull} {
		stageCodexFingerprintIDs(contextGin, &codexFingerprintIDs{
			accountID: account.ID, mode: mode, installationID: "stale-device",
			sessionID: "stale-session", threadID: "stale-thread",
		})
		require.Nil(t, stagedCodexFingerprintIDs(contextGin, account), "API key device convergence must not inherit %s projection", mode)
	}
	device := &codexFingerprintIDs{accountID: account.ID, mode: codexFingerprintDevice, installationID: "stable-device"}
	stageCodexFingerprintIDs(contextGin, device)
	require.Same(t, device, stagedCodexFingerprintIDs(contextGin, account))
	otherAccount := apiKeyDeviceTestAccount(502, "22222222-2222-4222-8222-222222222222")
	require.Nil(t, stagedCodexFingerprintIDs(contextGin, otherAccount), "failover must not reuse another account's staged IDs")
}

func TestAPIKeyDeviceLocalMetadataFailureNeverBecomesUpstreamFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		name := "normal"
		if passthrough {
			name = "passthrough_wrapped_error"
		}
		t.Run(name, func(t *testing.T) {
			account := apiKeyDeviceTestAccount(501, testCodexFingerprintSeed)
			upstream := &apiKeyDeviceCaptureUpstream{}
			service := &OpenAIGatewayService{httpUpstream: upstream}
			body := `{"model":"gpt-5.1","client_metadata":{"installation_id":"first-device","installation_id":"second-device"}}`
			request, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", strings.NewReader(body))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			response, sendErr := service.doOpenAIUpstream(request, "", account)
			require.Error(t, sendErr)
			require.Nil(t, response)
			var marker *openAIAPIKeyDeviceConvergenceError
			require.ErrorAs(t, sendErr, &marker)
			require.Zero(t, upstream.calls)

			recorder := httptest.NewRecorder()
			contextGin, _ := gin.CreateTestContext(recorder)
			contextGin.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			if passthrough {
				sendErr = errors.Join(errors.New("forwarding request failed"), sendErr)
			}
			handledErr := service.handleOpenAIUpstreamTransportError(context.Background(), contextGin, account, sendErr, passthrough)
			require.Same(t, marker, handledErr)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(handledErr, &failover), "local normalization failure must not retry through an account with convergence disabled")
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Equal(t, "device_metadata_invalid", gjson.Get(recorder.Body.String(), "error.code").String())
			require.True(t, HasOpsClientBusinessLimited(contextGin))
			require.Equal(t, OpsClientBusinessLimitedReasonLocalPolicyDenied, OpsClientBusinessLimitedReason(contextGin))
			require.Zero(t, upstream.calls)
			require.NotPanics(t, func() {
				require.Same(t, marker, service.handleOpenAIUpstreamTransportError(context.Background(), nil, account, sendErr, passthrough))
			})
		})
	}
}
