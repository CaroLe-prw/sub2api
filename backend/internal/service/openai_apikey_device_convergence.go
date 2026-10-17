package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// A local projection failure must not become a network failure/failover: a
// fallback account could have convergence disabled and expose the raw IDs.
type openAIAPIKeyDeviceConvergenceError struct{ cause error }

func (e *openAIAPIKeyDeviceConvergenceError) Error() string { return e.cause.Error() }
func (e *openAIAPIKeyDeviceConvergenceError) Unwrap() error { return e.cause }

// API-key convergence is deliberately narrower than OAuth convergence: only
// installation IDs change. Sessions, threads, cache keys and continuation
// state stay under their existing routing/transport rules.
func openAIAPIKeyConvergedDeviceID(account *Account) (string, error) {
	if !account.IsOpenAIAPIKeyDeviceConvergenceEnabled() {
		return "", nil
	}
	seed, ok := codexFingerprintSeed(account.Extra)
	if !ok {
		return "", fmt.Errorf("API key device convergence requires a persisted account seed")
	}
	return resolveConvergedInstallationID(account, seed), nil
}

func setAPIKeyDeviceHeader(headers http.Header, name, value string) {
	for existing := range headers {
		if strings.EqualFold(existing, name) {
			delete(headers, existing)
		}
	}
	headers.Set(name, value)
}

func applyOpenAIAPIKeyDeviceHeaders(headers http.Header, account *Account) error {
	id, err := openAIAPIKeyConvergedDeviceID(account)
	if err != nil || id == "" || headers == nil {
		return err
	}
	// Validate embedded metadata before mutating anything. Other fields are
	// spliced unchanged, including integer timestamps larger than 2^53.
	var metadata string
	var hasMetadata, hasAlias bool
	for name, values := range headers {
		if strings.EqualFold(name, "installation_id") {
			hasAlias = true
		}
		if strings.EqualFold(name, openAIWSTurnMetadataHeader) {
			if hasMetadata || len(values) != 1 {
				return fmt.Errorf("ambiguous device turn metadata header")
			}
			hasMetadata = true
			metadata = values[0]
		}
	}
	if hasMetadata && strings.TrimSpace(metadata) != "" {
		updated, _, rewriteErr := rewriteAPIKeyInstallationFields([]byte(metadata), id)
		if rewriteErr != nil {
			return rewriteErr
		}
		metadata = string(updated)
	}
	setAPIKeyDeviceHeader(headers, "x-codex-installation-id", id)
	if hasAlias {
		setAPIKeyDeviceHeader(headers, "installation_id", id)
	}
	if hasMetadata {
		setAPIKeyDeviceHeader(headers, openAIWSTurnMetadataHeader, metadata)
	}
	return nil
}

// Duplicate identity keys are ambiguous across JSON parsers (first vs last
// wins). Reject only keys on the supported metadata paths, not user content.
func rejectRepeatedDeviceMetadataKeys(raw []byte, keys ...string) error {
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		seen[key] = false
	}
	var repeated string
	gjson.ParseBytes(raw).ForEach(func(key, _ gjson.Result) bool {
		name := key.String()
		if found, watched := seen[name]; watched {
			if found {
				repeated = name
				return false
			}
			seen[name] = true
		}
		return true
	})
	if repeated != "" {
		return fmt.Errorf("ambiguous device metadata key %q", repeated)
	}
	return nil
}

func rewriteAPIKeyInstallationFields(raw []byte, id string) ([]byte, bool, error) {
	if !gjson.ValidBytes(raw) || !gjson.ParseBytes(raw).IsObject() {
		return raw, false, fmt.Errorf("device metadata must be a JSON object")
	}
	if err := rejectRepeatedDeviceMetadataKeys(raw, "installation_id", "x-codex-installation-id", openAIWSTurnMetadataHeader); err != nil {
		return raw, false, err
	}
	next := raw
	changed := false
	for _, field := range []string{"installation_id", "x-codex-installation-id"} {
		value := gjson.GetBytes(next, field)
		if !value.Exists() || (value.Type == gjson.String && value.String() == id) {
			continue
		}
		updated, err := sjson.SetBytes(next, field, id)
		if err != nil {
			return raw, false, fmt.Errorf("rewrite device installation field: %w", err)
		}
		next, changed = updated, true
	}
	return next, changed, nil
}

func rewriteAPIKeyClientMetadata(raw []byte, id string) ([]byte, bool, error) {
	next, changed, err := rewriteAPIKeyInstallationFields(raw, id)
	if err != nil {
		return raw, false, err
	}
	embedded := gjson.GetBytes(next, openAIWSTurnMetadataHeader)
	if !embedded.Exists() || embedded.Type == gjson.Null || (embedded.Type == gjson.String && strings.TrimSpace(embedded.String()) == "") {
		return next, changed, nil
	}
	var embeddedRaw []byte
	switch {
	case embedded.Type == gjson.String:
		embeddedRaw = []byte(embedded.String())
	case embedded.IsObject():
		embeddedRaw = []byte(embedded.Raw)
	default:
		return raw, false, fmt.Errorf("unsupported device turn metadata value")
	}
	rewritten, modified, err := rewriteAPIKeyInstallationFields(embeddedRaw, id)
	if err != nil || !modified {
		return next, changed, err
	}
	if embedded.Type == gjson.String {
		next, err = sjson.SetBytes(next, openAIWSTurnMetadataHeader, string(rewritten))
	} else {
		next, err = sjson.SetRawBytes(next, openAIWSTurnMetadataHeader, rewritten)
	}
	return next, true, err
}

func rewriteOpenAIAPIKeyDeviceBody(body []byte, account *Account) ([]byte, bool, error) {
	id, err := openAIAPIKeyConvergedDeviceID(account)
	if err != nil || id == "" || len(body) == 0 {
		return body, false, err
	}
	// Only protocol metadata is eligible. User messages, tools, inputs and
	// arbitrary nested JSON must never be searched/replaced for device strings.
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return body, false, nil
	}
	if err := rejectRepeatedDeviceMetadataKeys(body, "client_metadata", "response", "session"); err != nil {
		return body, false, err
	}
	for _, container := range []string{"response", "session"} {
		if value := gjson.GetBytes(body, container); value.IsObject() {
			if err := rejectRepeatedDeviceMetadataKeys([]byte(value.Raw), "client_metadata"); err != nil {
				return body, false, err
			}
		}
	}
	next := body
	changed := false
	for _, path := range []string{"client_metadata", "response.client_metadata", "session.client_metadata"} {
		metadata := gjson.GetBytes(next, path)
		if !metadata.Exists() || metadata.Type == gjson.Null {
			continue
		}
		rewritten, modified, rewriteErr := rewriteAPIKeyClientMetadata([]byte(metadata.Raw), id)
		if rewriteErr != nil {
			return body, false, rewriteErr
		}
		if !modified {
			continue
		}
		next, err = sjson.SetRawBytes(next, path, rewritten)
		if err != nil {
			return body, false, err
		}
		changed = true
	}
	return next, changed, nil
}

func rewriteOpenAIAPIKeyDevicePayload(payload map[string]any, account *Account) (map[string]any, error) {
	id, err := openAIAPIKeyConvergedDeviceID(account)
	if err != nil || id == "" || payload == nil {
		return payload, err
	}
	metadata, exists := payload["client_metadata"]
	if !exists || metadata == nil {
		return payload, nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	rewritten, changed, err := rewriteAPIKeyClientMetadata(raw, id)
	if err != nil || !changed {
		return payload, err
	}
	var replacement map[string]any
	if err := decodeOpenAIJSONUseNumber(rewritten, &replacement); err != nil {
		return nil, err
	}
	next := maps.Clone(payload)
	next["client_metadata"] = replacement
	return next, nil
}

func rewriteOpenAIAPIKeyDeviceMultipart(body []byte, boundary, id string) ([]byte, bool, error) {
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var output bytes.Buffer
	writer := multipart.NewWriter(&output)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, false, err
	}
	changed := false
	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, err
		}
		dest, err := writer.CreatePart(part.Header)
		if err != nil {
			return nil, false, err
		}
		if part.FormName() == "client_metadata" && part.FileName() == "" {
			raw, readErr := io.ReadAll(part)
			if readErr != nil {
				return nil, false, readErr
			}
			rewritten, modified, rewriteErr := rewriteAPIKeyClientMetadata(raw, id)
			if rewriteErr != nil {
				return nil, false, rewriteErr
			}
			if _, err = dest.Write(rewritten); err != nil {
				return nil, false, err
			}
			changed = changed || modified
		} else if _, err = io.Copy(dest, part); err != nil {
			return nil, false, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, false, err
	}
	if !changed {
		return body, false, nil
	}
	return output.Bytes(), true, nil
}

func prepareOpenAIAPIKeyDeviceRequest(request *http.Request, account *Account) (*http.Request, error) {
	id, err := openAIAPIKeyConvergedDeviceID(account)
	if err != nil || id == "" || request == nil {
		return request, err
	}
	next := request.Clone(request.Context())
	if next.Header == nil {
		next.Header = make(http.Header)
	}
	if err := applyOpenAIAPIKeyDeviceHeaders(next.Header, account); err != nil {
		return nil, err
	}
	if request.Body == nil || request.Body == http.NoBody {
		return next, nil
	}
	mediaType, params, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	jsonBody := mediaType == "" || mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
	if !jsonBody && mediaType != "multipart/form-data" {
		return next, nil
	}
	// Gateway requests are buffered/replayable. Read a fresh body so failover
	// inputs and a caller's original request retain their original identities.
	if request.GetBody == nil {
		return nil, fmt.Errorf("API key device convergence requires a replayable request body")
	}
	reader, err := request.GetBody()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var rewritten []byte
	var changed bool
	if jsonBody {
		rewritten, changed, err = rewriteOpenAIAPIKeyDeviceBody(body, account)
	} else {
		rewritten, changed, err = rewriteOpenAIAPIKeyDeviceMultipart(body, params["boundary"], id)
	}
	if err != nil {
		return nil, err
	}
	if changed {
		next.Body = io.NopCloser(bytes.NewReader(rewritten))
		next.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(rewritten)), nil }
		next.ContentLength = int64(len(rewritten))
		next.Header.Del("Content-Length")
	}
	return next, nil
}

// All writes (first frame, subsequent turns and session.update) use the same
// device projection. Reads and all connection lifecycle operations pass through.
type openAIAPIKeyDeviceFrameConn struct {
	openaiwsv2.FrameConn
	account *Account
}

func (c *openAIAPIKeyDeviceFrameConn) WriteFrame(ctx context.Context, messageType coderws.MessageType, payload []byte) error {
	if messageType == coderws.MessageText || messageType == coderws.MessageBinary {
		next, _, err := rewriteOpenAIAPIKeyDeviceBody(payload, c.account)
		if err != nil {
			return err
		}
		payload = next
	}
	return c.FrameConn.WriteFrame(ctx, messageType, payload)
}
