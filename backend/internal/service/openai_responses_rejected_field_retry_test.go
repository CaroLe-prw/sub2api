package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIResponsesRejectedFieldRetryStateRejectsDuplicateBodyAndCap(t *testing.T) {
	initialBody := []byte(`{"model":"gpt-5.5"}`)
	state := newOpenAIResponsesRejectedFieldRetryState(initialBody)

	require.False(t, state.Allow(initialBody))
	for attempt := 0; attempt < maxOpenAIResponsesRejectedFieldRetries; attempt++ {
		nextBody := []byte(fmt.Sprintf(`{"model":"gpt-5.5","variant":%d}`, attempt))
		require.True(t, state.Allow(nextBody))
		require.False(t, state.Allow(nextBody))
	}
	require.False(t, state.Allow([]byte(`{"model":"gpt-5.5","variant":"overflow"}`)))
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRejectsAmbiguousErrors(t *testing.T) {
	tests := []struct {
		name         string
		body         []byte
		responseBody []byte
	}{
		{
			name:         "namespace belongs to message",
			body:         []byte(`{"input":[{"type":"message","namespace":"keep"}]}`),
			responseBody: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: 'input[0].namespace'.","param":"input[0].namespace"}}`),
		},
		{
			name:         "max output tokens only mentioned",
			body:         []byte(`{"max_output_tokens":4096}`),
			responseBody: []byte(`{"error":{"code":"invalid_request_error","message":"max_output_tokens must be positive","param":"max_output_tokens"}}`),
		},
		{
			name:         "structured param overrides namespace mention",
			body:         []byte(`{"input":[{"type":"function_call","namespace":"keep","arguments":"{}"}]}`),
			responseBody: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: 'input[0].namespace'.","param":"tools"}}`),
		},
		{
			name:         "nested max output tokens param is not top level",
			body:         []byte(`{"max_output_tokens":4096,"input":[{"type":"message","content":{"max_output_tokens":"keep"}}]}`),
			responseBody: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: input[0].content.max_output_tokens","param":"input[0].content.max_output_tokens"}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, tt.body, tt.responseBody)
			require.NoError(t, err)
			require.False(t, changed)
			require.Nil(t, retryBody)
		})
	}
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyFindsNamespacePathInMessage(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call","namespace":"keep","arguments":"{}"},{"type":"function_call","namespace":"remove","arguments":"{}"}]}`)
	responseBody := []byte(`{"error":{"code":"unknown_parameter","message":"input[0] was accepted; Unknown parameter: 'input[1].namespace'."}}`)

	retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "keep", gjson.GetBytes(retryBody, "input.0.namespace").String())
	require.False(t, gjson.GetBytes(retryBody, "input.1.namespace").Exists())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyDropsMissingEncryptedReasoningItemFromStreamEvent(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"message","role":"user","content":"keep"},{"type":"reasoning","id":"rs_missing","summary":[]},{"type":"function_call_output","call_id":"call_1","output":"keep too"}]}`)
	responseBody := []byte(`{"type":"response.failed","response":{"error":{"code":"missing_required_parameter","type":"invalid_request_error","message":"Missing required parameter: 'input[1].encrypted_content'."}}}`)

	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "missing encrypted_content history item", reason)
	require.Equal(t, 2, int(gjson.GetBytes(retryBody, "input.#").Int()))
	require.Equal(t, "message", gjson.GetBytes(retryBody, "input.0.type").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(retryBody, "input.1.type").String())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyDoesNotDropNonReasoningMissingEncryptedItem(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"user","content":"keep"},{"type":"message","role":"assistant","content":"also keep"}]}`)
	responseBody := []byte(`{"error":{"code":"missing_required_parameter","message":"Missing required parameter: 'input[1].encrypted_content'.","param":"input[1].encrypted_content"}}`)

	retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.False(t, changed)
	require.Nil(t, retryBody)
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyBindsNamespacePathToRejectionPhrase(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call","namespace":"keep","arguments":"{}"},{"type":"function_call","namespace":"remove","arguments":"{}"}]}`)
	responseBody := []byte(`{"error":{"code":"unknown_parameter","message":"input[0].namespace is supported; Unknown parameter: input[1].namespace."}}`)

	retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "keep", gjson.GetBytes(retryBody, "input.0.namespace").String())
	require.False(t, gjson.GetBytes(retryBody, "input.1.namespace").Exists())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyDoesNotTreatMaxOutputTokensSuggestionAsRejection(t *testing.T) {
	body := []byte(`{"max_tokens":4096,"max_output_tokens":2048}`)
	responseBody := []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: max_tokens. Use max_output_tokens instead."}}`)

	retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.False(t, changed)
	require.Nil(t, retryBody)
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyBindsMaxOutputTokensToRejectionPhrase(t *testing.T) {
	body := []byte(`{"max_output_tokens":2048}`)
	responseBody := []byte(`{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: max_output_tokens."}}`)

	retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(retryBody, "max_output_tokens").Exists())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRemovesIndexedSummary(t *testing.T) {
	body := []byte(`{"input":[{"type":"reasoning","summary":[],"encrypted_content":"gAAA"},{"type":"message","summary":"keep"}]}`)
	responseBody := []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: 'input[0].summary'.","param":"input[0].summary"}}`)

	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "indexed summary parameter rejection", reason)
	require.False(t, gjson.GetBytes(retryBody, "input.0.summary").Exists())
	require.Equal(t, "keep", gjson.GetBytes(retryBody, "input.1.summary").String())

	retryBody, _, changed, err = normalizeOpenAIResponsesRejectedFieldRetryBody(
		http.StatusBadRequest,
		body,
		[]byte(`{"error":{"message":"Unknown parameter: 'input[0].summary'."}}`),
	)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(retryBody, "input.0.summary").Exists())
}

func TestOpenAIResponsesRejectedParamFromMessageFindsIndexedInput(t *testing.T) {
	require.Equal(t, "input[7].input", openAIResponsesRejectedParamFromMessage("unknown parameter: 'input[7].input'."))
	require.Equal(t, "input[70].arguments", openAIResponsesRejectedParamFromMessage("Missing required parameter: 'input[70].arguments'."))
	require.Equal(t, "input[36].id", openAIResponsesRejectedParamFromMessage("Invalid 'input[36].id': string too long."))
	require.Equal(t, "input[2].content", openAIResponsesRejectedParamFromMessage("Unknown parameter: 'input[2].content'."))
	require.Equal(t, "input[51].role", openAIResponsesRejectedParamFromMessage("Unknown parameter: 'input[51].role'."))
	require.Empty(t, openAIResponsesRejectedParamFromMessage("Unknown parameter: input[2].content.format."))
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRemovesUnknownFieldByItemType(t *testing.T) {
	body := []byte(`{"input":[
		{"type":"function_call","call_id":"call_1","name":"first","arguments":"{}","content":"remove"},
		{"type":"message","role":"user","content":"keep"},
		{"type":"function_call","call_id":"call_2","name":"second","arguments":"{}","content":"remove-too"}
	]}`)
	responseBody := []byte(`{"error":{"message":"Unknown parameter: 'input[0].content'."}}`)

	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "indexed content parameter rejection", reason)
	require.False(t, gjson.GetBytes(retryBody, "input.0.content").Exists())
	require.Equal(t, "keep", gjson.GetBytes(retryBody, "input.1.content").String())
	require.False(t, gjson.GetBytes(retryBody, "input.2.content").Exists())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyBackfillsAllMissingArguments(t *testing.T) {
	input := make([]string, 71)
	for index := range input {
		input[index] = `{"type":"message","role":"user","content":"history"}`
	}
	input[9] = `{"type":"function_call","call_id":"call_9","name":"first"}`
	input[70] = `{"type":"function_call","call_id":"call_70","name":"second"}`
	body := []byte(`{"model":"gpt-5.6-sol","input":[` + strings.Join(input, ",") + `]}`)
	responseBody := []byte(`{"error":{"code":"missing_required_parameter","message":"Missing required parameter: 'input[70].arguments'."}}`)

	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "missing function call arguments", reason)
	require.Equal(t, "{}", gjson.GetBytes(retryBody, "input.9.arguments").String())
	require.Equal(t, "{}", gjson.GetBytes(retryBody, "input.70.arguments").String())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRemovesArbitraryOverlongID(t *testing.T) {
	input := make([]string, 37)
	for index := range input {
		input[index] = `{"type":"message","role":"user","content":"history"}`
	}
	overlongID := "fc_" + strings.Repeat("x", 63)
	input[36] = `{"type":"function_call","id":"` + overlongID + `","call_id":"call_36","name":"lookup","arguments":"{}"}`
	body := []byte(`{"model":"gpt-5.6-sol","input":[` + strings.Join(input, ",") + `]}`)
	responseBody := []byte(`{"error":{"message":"Invalid 'input[36].id': string too long. Expected a string with maximum length 64, but got a string with length 66 instead."}}`)

	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "overlong input item id", reason)
	require.False(t, gjson.GetBytes(retryBody, "input.36.id").Exists())
	require.Equal(t, "call_36", gjson.GetBytes(retryBody, "input.36.call_id").String())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRemovesWrongCustomToolCallIDPrefix(t *testing.T) {
	input := make([]string, 433)
	for index := range input {
		input[index] = `{"type":"message","role":"user","content":"history"}`
	}
	input[432] = `{"type":"custom_tool_call","id":"fc_088da576572f0bfd","call_id":"call_432","name":"apply_patch","input":"patch"}`
	body := []byte(`{"model":"gpt-5.6-sol","input":[` + strings.Join(input, ",") + `]}`)
	responseBody := []byte(`{"error":{"code":"invalid_value","message":"Invalid 'input[432].id': 'fc_088da576572f0bfd'. Expected an ID that begins with 'ctc'.","param":"input[432].id","type":"invalid_request_error"}}`)

	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "invalid input item id", reason)
	require.False(t, gjson.GetBytes(retryBody, "input.432.id").Exists())
	require.Equal(t, "call_432", gjson.GetBytes(retryBody, "input.432.call_id").String())
	require.Equal(t, "patch", gjson.GetBytes(retryBody, "input.432.input").String())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRemovesZeroLengthToolContent(t *testing.T) {
	input := make([]string, 58)
	for index := range input {
		input[index] = `{"type":"message","role":"user","content":[{"type":"input_text","text":"history"}]}`
	}
	input[9] = `{"type":"function_call","call_id":"call_9","name":"first","arguments":"{}","content":[{"type":"output_text","text":"remove too"}]}`
	input[57] = `{"type":"function_call","call_id":"call_57","name":"second","arguments":"{}","content":[{"type":"output_text","text":"remove"}]}`
	body := []byte(`{"model":"gpt-5.6-sol","input":[` + strings.Join(input, ",") + `]}`)
	responseBody := []byte(`{"error":{"code":"array_above_max_length","message":"Invalid 'input[57].content': array too long. Expected an array with maximum length 0, but got an array with length 1 instead.","param":"input[57].content","type":"invalid_request_error"}}`)

	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "indexed content parameter rejection", reason)
	require.False(t, gjson.GetBytes(retryBody, "input.9.content").Exists())
	require.False(t, gjson.GetBytes(retryBody, "input.57.content").Exists())
	require.Equal(t, "call_57", gjson.GetBytes(retryBody, "input.57.call_id").String())
	require.True(t, gjson.GetBytes(retryBody, "input.0.content").IsArray())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyDoesNotDeleteMessageContentForArrayLengthError(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"keep"}]}]}`)
	responseBody := []byte(`{"error":{"code":"array_above_max_length","message":"Invalid 'input[0].content': array too long. Expected an array with maximum length 0, but got an array with length 1 instead.","param":"input[0].content"}}`)

	_, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.False(t, changed)
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyDoesNotHandlePositiveContentLimit(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}","content":[{},{}]}]}`)
	responseBody := []byte(`{"error":{"code":"array_above_max_length","message":"Invalid 'input[0].content': array too long. Expected an array with maximum length 1, but got an array with length 2 instead.","param":"input[0].content"}}`)

	_, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.False(t, changed)
}

func TestOpenAIGatewayService_APIKeyStripsAllIndexedNamespacesBeforeFirstForward(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"function_call","name":"first","namespace":"remove-first","arguments":"{}"},{"type":"custom_tool_call","name":"second","namespace":"remove-second","input":"{}"}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		newOpenAIRejectedFieldTestAccount(),
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.False(t, gjson.GetBytes(upstream.bodies[0], "input.0.namespace").Exists())
	require.False(t, gjson.GetBytes(upstream.bodies[0], "input.1.namespace").Exists())
}

func TestOpenAIGatewayService_OpenAIHTTPStripsInputNamespacesBeforeFirstForward(t *testing.T) {
	accounts := []struct {
		name    string
		account *Account
	}{
		{name: "oauth", account: newOpenAIOAuthNamespaceTestAccount()},
		{name: "apikey", account: newOpenAIRejectedFieldTestAccount()},
	}
	for _, tt := range accounts {
		for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
			t.Run(tt.name+path, func(t *testing.T) {
				body := []byte(`{"model":"gpt-5.5","stream":false,"instructions":"test","input":[{"type":"message","role":"user","namespace":"remove","content":[{"type":"input_text","text":"hello","namespace":"nested-keep"}]}]}`)
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"resp_namespace_ok","output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
				}}
				c := newOpenAIRejectedFieldTestContext(body)
				c.Request.URL.Path = path

				result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
					context.Background(),
					c,
					tt.account,
					body,
				)

				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.bodies, 1, "namespace must be removed before the first upstream request")
				require.False(t, gjson.GetBytes(upstream.bodies[0], "input.0.namespace").Exists())
				require.Equal(t, "nested-keep", gjson.GetBytes(upstream.bodies[0], "input.0.content.0.namespace").String())
			})
		}
	}
}

func TestOpenAIGatewayService_RetriesExplicitMaxOutputTokensRejection(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"max_output_tokens":4096,"input":[{"type":"message","role":"user","content":{"max_output_tokens":"keep"}}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: max_output_tokens","param":"max_output_tokens","type":"invalid_request_error"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		newOpenAIRejectedFieldTestAccount(),
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, int64(4096), gjson.GetBytes(upstream.bodies[0], "max_output_tokens").Int())
	require.False(t, gjson.GetBytes(upstream.bodies[1], "max_output_tokens").Exists())
	require.Equal(t, "keep", gjson.GetBytes(upstream.bodies[1], "input.0.content.max_output_tokens").String())
}

func TestOpenAIGatewayService_ComposesProactiveNamespaceStripWithRejectedFieldRetry(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"max_output_tokens":2048,"input":[{"type":"function_call","name":"first","namespace":"remove-first","arguments":"{}"},{"type":"custom_tool_call","name":"second","namespace":"remove-second","input":"{}"}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: max_output_tokens","param":"max_output_tokens"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		newOpenAIRejectedFieldTestAccount(),
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	for _, forwardedBody := range upstream.bodies {
		require.False(t, gjson.GetBytes(forwardedBody, "input.0.namespace").Exists())
		require.False(t, gjson.GetBytes(forwardedBody, "input.1.namespace").Exists())
	}
	require.Equal(t, int64(2048), gjson.GetBytes(upstream.bodies[0], "max_output_tokens").Int())
	require.False(t, gjson.GetBytes(upstream.bodies[1], "max_output_tokens").Exists())
}

func TestOpenAIGatewayService_APIKeyPassthroughRetriesStreamMissingEncryptedContentOnSameAccount(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-terra","stream":true,"input":[{"type":"message","role":"user","content":"keep"},{"type":"reasoning","id":"rs_missing","summary":[]},{"type":"function_call_output","call_id":"call_1","output":"keep too"}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"event: response.created\n" +
					`data: {"type":"response.created","response":{"id":"resp_rejected"}}` + "\n\n" +
					"event: response.failed\n" +
					`data: {"type":"response.failed","response":{"error":{"code":"missing_required_parameter","type":"invalid_request_error","message":"Missing required parameter: 'input[1].encrypted_content'."}}}` + "\n\n",
			)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"event: response.completed\n" +
					`data: {"type":"response.completed","response":{"id":"resp_recovered","usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}` + "\n\n",
			)),
		},
	}}
	account := newOpenAIRejectedFieldTestAccount()
	account.Extra["openai_passthrough"] = true

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		account,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, 3, int(gjson.GetBytes(upstream.bodies[0], "input.#").Int()))
	require.Equal(t, 2, int(gjson.GetBytes(upstream.bodies[1], "input.#").Int()))
	require.Equal(t, "message", gjson.GetBytes(upstream.bodies[1], "input.0.type").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(upstream.bodies[1], "input.1.type").String())
}

func TestOpenAIGatewayService_APIKeyPassthroughFailsOverAfterStreamBodyRepairFails(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-terra","stream":true,"input":[{"type":"message","role":"user","content":"keep"},{"type":"reasoning","id":"rs_missing","summary":[]},{"type":"function_call_output","call_id":"call_1","output":"keep too"}]}`)
	failedStream := func(responseID string) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"event: response.failed\n" +
					`data: {"type":"response.failed","response":{"id":"` + responseID + `","error":{"code":"missing_required_parameter","type":"invalid_request_error","message":"Missing required parameter: 'input[1].encrypted_content'."}}}` + "\n\n",
			)),
		}
	}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		failedStream("resp_rejected_first"),
		failedStream("resp_rejected_after_repair"),
	}}
	account := newOpenAIRejectedFieldTestAccount()
	account.Extra["openai_passthrough"] = true

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		account,
		body,
	)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.False(t, failoverErr.RetryableOnSameAccount)
	require.True(t, failoverErr.RequestScopedTransient)
	require.True(t, failoverErr.ShouldRetryNextAccount())
	require.False(t, failoverErr.ShouldReportAccountScheduleFailure())
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, 2, int(gjson.GetBytes(upstream.bodies[1], "input.#").Int()))
}

func newOpenAIRejectedFieldTestService(upstream *httpUpstreamRecorder) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
		httpUpstream: upstream,
	}
}

func newOpenAIRejectedFieldTestContext(body []byte) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "curl/8.0")
	return c
}

func newOpenAIRejectedFieldTestAccount() *Account {
	return &Account{
		ID:          5107,
		Name:        "responses-compatible",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat.example",
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
			openai_compat.ExtraKeyResponsesSupported: true,
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func newOpenAIOAuthNamespaceTestAccount() *Account {
	return &Account{
		ID:          5108,
		Name:        "openai-oauth-namespace",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-account",
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func newOpenAIRejectedFieldTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestOpenAIResponsesRejectedFieldRetryStateForRequestAllowsSameTransformAcrossAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	initialBody := []byte(`{"model":"gpt-5.5","truncation":"auto"}`)
	retryBody := []byte(`{"model":"gpt-5.5"}`)

	accountA := openAIResponsesRejectedFieldRetryStateForRequest(c, initialBody)
	require.True(t, accountA.Allow(retryBody))
	require.False(t, accountA.Allow(retryBody), "one account must not repeat the same transform")

	accountB := openAIResponsesRejectedFieldRetryStateForRequest(c, initialBody)
	require.NotSame(t, accountA, accountB)
	require.Same(t, accountA.budget, accountB.budget)
	require.True(t, accountB.Allow(retryBody), "a failover account must be allowed to apply the same transform")
}

func TestOpenAIResponsesRejectedFieldRetryStateForRequestSharesBoundedBudgetAcrossAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	for attempt := 0; attempt < maxOpenAIResponsesRejectedFieldRetries; attempt++ {
		state := openAIResponsesRejectedFieldRetryStateForRequest(c, []byte(fmt.Sprintf(`{"account":%d}`, attempt)))
		require.True(t, state.Allow([]byte(`{"same":"retry"}`)))
	}
	overflow := openAIResponsesRejectedFieldRetryStateForRequest(c, []byte(`{"account":"overflow"}`))
	require.False(t, overflow.Allow([]byte(`{"new":"retry"}`)))
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRepairsAutomationMissingRootType(t *testing.T) {
	body := []byte(`{"tools":[{"type":"function","name":"automation_update","parameters":{"oneOf":[{"type":"object"},{"type":"object","properties":{}}]}}]}`)
	responseBody := []byte(`{"error":{"code":"invalid_function_parameters","message":"Invalid schema for function 'automation_update': got 'type: \"None\"'.","param":"tools[0].parameters"}}`)

	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "tool parameter root type rejection", reason)
	require.Equal(t, "object", gjson.GetBytes(retryBody, "tools.0.parameters.type").String())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyDoesNotGuessAutomationRootType(t *testing.T) {
	body := []byte(`{"tools":[{"type":"function","name":"automation_update","parameters":{"oneOf":[{"type":"object"}]}}]}`)
	tests := []string{
		`{"error":{"code":"invalid_function_parameters","message":"got type: \"None\"","param":"metadata.parameters"}}`,
		`{"error":{"code":"invalid_request_error","message":"got type: \"None\"","param":"tools[0].parameters"}}`,
		`{"error":{"code":"invalid_function_parameters","message":"expected an object","param":"tools[0].parameters"}}`,
	}
	for _, response := range tests {
		retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, []byte(response))
		require.NoError(t, err)
		require.False(t, changed)
		require.Nil(t, retryBody)
	}
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRemovesExactIndexedStatus(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","status":"keep","content":"one"},{"type":"reasoning","status":"remove","summary":[]}]}`)
	responses := []struct {
		name string
		body []byte
	}{
		{
			name: "structured param",
			body: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: 'input[1].status'.","param":"input[1].status"}}`),
		},
		{
			name: "message param",
			body: []byte(`{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: input[1].status."}}`),
		},
	}
	for _, tt := range responses {
		t.Run(tt.name, func(t *testing.T) {
			retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, tt.body)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, "keep", gjson.GetBytes(retryBody, "input.0.status").String())
			require.False(t, gjson.GetBytes(retryBody, "input.1.status").Exists())
		})
	}
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyNormalizesExactNullContent(t *testing.T) {
	tests := []struct {
		name       string
		body       []byte
		wantChange bool
		wantValue  string
		wantExists bool
	}{
		{
			name:       "message becomes empty string",
			body:       []byte(`{"input":[{"type":"message","role":"assistant","content":null}]}`),
			wantChange: true,
			wantValue:  "",
			wantExists: true,
		},
		{
			name:       "reasoning content is removed",
			body:       []byte(`{"input":[{"type":"reasoning","content":null,"summary":[]}]}`),
			wantChange: true,
			wantExists: false,
		},
		{
			name:       "unknown item is unchanged",
			body:       []byte(`{"input":[{"type":"future_item","content":null}]}`),
			wantChange: false,
		},
		{
			name:       "non null content is unchanged",
			body:       []byte(`{"input":[{"type":"message","content":"keep"}]}`),
			wantChange: false,
		},
	}
	responseBody := []byte(`{"error":{"code":"invalid_type","message":"Invalid type for 'input[0].content': expected one of a string or a list of input items, but got null instead.","param":"input[0].content"}}`)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, tt.body, responseBody)
			require.NoError(t, err)
			require.Equal(t, tt.wantChange, changed)
			if !tt.wantChange {
				require.Nil(t, retryBody)
				return
			}
			content := gjson.GetBytes(retryBody, "input.0.content")
			require.Equal(t, tt.wantExists, content.Exists())
			if tt.wantExists {
				require.Equal(t, tt.wantValue, content.String())
				require.Equal(t, gjson.String, content.Type)
			}
		})
	}
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRemovesExactReasoningContentAboveMaximumZero(t *testing.T) {
	body := []byte(`{"input":[{"type":"reasoning","content":[{"type":"reasoning_text","text":"remove"}],"summary":[]},{"type":"message","content":[{"type":"input_text","text":"keep"}]}]}`)
	responseBody := []byte(`{"error":{"code":"array_above_max_length","message":"Invalid 'input[0].content': array too long. Expected an array with maximum length 0, but got an array with length 1 instead.","param":"input[0].content","type":"invalid_request_error"}}`)

	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "indexed reasoning content maximum-length rejection", reason)
	require.False(t, gjson.GetBytes(retryBody, "input.0.content").Exists())
	require.Equal(t, "keep", gjson.GetBytes(retryBody, "input.1.content.0.text").String())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRejectsUnsafeReasoningMaximumZeroMutations(t *testing.T) {
	tests := []struct {
		name         string
		body         []byte
		responseBody []byte
	}{
		{
			name:         "message content is not reasoning",
			body:         []byte(`{"input":[{"type":"message","content":[{"type":"input_text","text":"keep"}]}]}`),
			responseBody: []byte(`{"error":{"code":"array_above_max_length","message":"Invalid 'input[0].content': array too long. Expected an array with maximum length 0, but got an array with length 1 instead.","param":"input[0].content"}}`),
		},
		{
			name:         "structured param and message disagree",
			body:         []byte(`{"input":[{"type":"reasoning","content":[{"text":"keep"}]},{"type":"reasoning","content":[{"text":"keep too"}]}]}`),
			responseBody: []byte(`{"error":{"code":"array_above_max_length","message":"Invalid 'input[1].content': array too long. Expected an array with maximum length 0, but got an array with length 1 instead.","param":"input[0].content"}}`),
		},
		{
			name:         "different error code",
			body:         []byte(`{"input":[{"type":"reasoning","content":[{"text":"keep"}]}]}`),
			responseBody: []byte(`{"error":{"code":"invalid_request_error","message":"Invalid 'input[0].content': array too long. Expected an array with maximum length 0, but got an array with length 1 instead.","param":"input[0].content"}}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, tt.body, tt.responseBody)
			require.NoError(t, err)
			require.False(t, changed)
			require.Nil(t, retryBody)
		})
	}
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRemovesExplicitlyRejectedTopLevelTruncation(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","truncation":"auto","input":"keep"}`)
	responses := [][]byte{
		[]byte(`{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: 'truncation'.","param":"truncation"}}`),
		[]byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: truncation."}}`),
	}
	for _, responseBody := range responses {
		retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "truncation parameter rejection", reason)
		require.False(t, gjson.GetBytes(retryBody, "truncation").Exists())
		require.Equal(t, "keep", gjson.GetBytes(retryBody, "input").String())
	}
}

func TestOpenAIGatewayService_APIKeyRetriesExplicitlyRejectedTopLevelTruncation(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"truncation":"auto","input":"keep"}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: 'truncation'.","param":"truncation"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), newOpenAIRejectedFieldTestContext(body), newOpenAIRejectedFieldTestAccount(), body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, "auto", gjson.GetBytes(upstream.bodies[0], "truncation").String())
	require.False(t, gjson.GetBytes(upstream.bodies[1], "truncation").Exists())
	require.Equal(t, "keep", gjson.GetBytes(upstream.bodies[1], "input").String())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRejectsUnsafeIndexedMutations(t *testing.T) {
	tests := []struct {
		name         string
		body         []byte
		responseBody []byte
	}{
		{
			name:         "nested status path",
			body:         []byte(`{"input":[{"type":"message","content":{"status":"keep"}}]}`),
			responseBody: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: input[0].content.status.","param":"input[0].content.status"}}`),
		},
		{
			name:         "status index out of bounds",
			body:         []byte(`{"input":[{"type":"message","status":"keep"}]}`),
			responseBody: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: input[4].status.","param":"input[4].status"}}`),
		},
		{
			name:         "status path only mentioned",
			body:         []byte(`{"input":[{"type":"message","status":"keep"}]}`),
			responseBody: []byte(`{"error":{"code":"invalid_request_error","message":"input[0].status must be completed","param":"input[0].status"}}`),
		},
		{
			name:         "content param and message disagree",
			body:         []byte(`{"input":[{"type":"message","content":null},{"type":"message","content":null}]}`),
			responseBody: []byte(`{"error":{"code":"invalid_type","message":"Invalid type for input[1].content: got null instead.","param":"input[0].content"}}`),
		},
		{
			name:         "content error only mentions null",
			body:         []byte(`{"input":[{"type":"message","content":null}]}`),
			responseBody: []byte(`{"error":{"code":"invalid_type","message":"content cannot be null","param":"input[0].content"}}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, tt.body, tt.responseBody)
			require.NoError(t, err)
			require.False(t, changed)
			require.Nil(t, retryBody)
		})
	}
}

func TestOpenAIGatewayService_OAuthRetriesExactRejectedStatus(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":true,"instructions":"test","input":[{"type":"message","role":"user","status":"completed","content":"hello"}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"unknown_parameter","message":"Unknown parameter: 'input[0].status'.","param":"input[0].status"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusOK, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\ndata: [DONE]\n\n"),
	}}
	upstream.responses[1].Header.Set("Content-Type", "text/event-stream")

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), newOpenAIRejectedFieldTestContext(body), newOpenAIOAuthNamespaceTestAccount(), body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, "completed", gjson.GetBytes(upstream.bodies[0], "input.0.status").String())
	require.False(t, gjson.GetBytes(upstream.bodies[1], "input.0.status").Exists())
}

func TestOpenAIGatewayService_APIKeyRetriesExactRejectedNullMessageContent(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"message","role":"assistant","content":null},{"type":"message","role":"user","content":"continue"}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"invalid_type","message":"Invalid type for 'input[0].content': expected one of a string or a list of input items, but got null instead.","param":"input[0].content"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), newOpenAIRejectedFieldTestContext(body), newOpenAIRejectedFieldTestAccount(), body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, gjson.Null, gjson.GetBytes(upstream.bodies[0], "input.0.content").Type)
	require.Equal(t, gjson.String, gjson.GetBytes(upstream.bodies[1], "input.0.content").Type)
	require.Equal(t, "continue", gjson.GetBytes(upstream.bodies[1], "input.1.content").String())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRemovesModelRejectedPromptCacheBreakpoint(t *testing.T) {
	tests := []struct {
		name         string
		body         []byte
		responseBody []byte
		removedPath  string
		preserved    string
		reason       string
	}{
		{
			name:         "top level",
			body:         []byte(`{"model":"gpt-5.6-sol","prompt_cache_breakpoint":{"type":"message_start"},"input":"hello"}`),
			responseBody: []byte(`{"error":{"code":"invalid_parameter","message":"prompt_cache_breakpoint is not supported on this model","param":"prompt_cache_breakpoint"}}`),
			removedPath:  "prompt_cache_breakpoint",
			preserved:    "input",
			reason:       "prompt_cache_breakpoint parameter rejection",
		},
		{
			name:         "indexed path from message",
			body:         []byte(`{"input":[{"type":"message","prompt_cache_breakpoint":{"type":"message_start"}},{"type":"message","prompt_cache_breakpoint":{"type":"message_end"}}]}`),
			responseBody: []byte(`{"error":{"code":"invalid_parameter","message":"input[1].prompt_cache_breakpoint is not supported on this model"}}`),
			removedPath:  "input.1.prompt_cache_breakpoint",
			preserved:    "input.0.prompt_cache_breakpoint",
			reason:       "indexed prompt_cache_breakpoint parameter rejection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, tt.body, tt.responseBody)

			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, tt.reason, reason)
			require.False(t, gjson.GetBytes(retryBody, tt.removedPath).Exists())
			require.True(t, gjson.GetBytes(retryBody, tt.preserved).Exists())
		})
	}
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRejectsAmbiguousPromptCacheBreakpointErrors(t *testing.T) {
	body := []byte(`{"prompt_cache_breakpoint":{"type":"message_start"},"input":[{"type":"message","prompt_cache_breakpoint":{"type":"message_end"}}]}`)
	tests := []struct {
		name         string
		responseBody []byte
	}{
		{
			name:         "structured param disagrees",
			responseBody: []byte(`{"error":{"code":"invalid_parameter","message":"input[0].prompt_cache_breakpoint is not supported on this model","param":"prompt_cache_breakpoint"}}`),
		},
		{
			name:         "index out of bounds",
			responseBody: []byte(`{"error":{"code":"invalid_parameter","message":"input[4].prompt_cache_breakpoint is not supported on this model","param":"input[4].prompt_cache_breakpoint"}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, tt.responseBody)

			require.NoError(t, err)
			require.False(t, changed)
			require.Nil(t, retryBody)
		})
	}
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyAcceptsEitherCacheModelRejectionSignal(t *testing.T) {
	tests := []struct {
		name         string
		responseBody []byte
	}{
		{
			name:         "invalid parameter code",
			responseBody: []byte(`{"error":{"code":"invalid_parameter","message":"This optional cache hint cannot be used here","param":"prompt_cache_breakpoint"}}`),
		},
		{
			name:         "model rejection message",
			responseBody: []byte(`{"error":{"code":"invalid_request_error","message":"prompt_cache_breakpoint is not supported on this model","param":"prompt_cache_breakpoint"}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, []byte(`{"prompt_cache_breakpoint":true,"input":"keep"}`), tt.responseBody)

			require.NoError(t, err)
			require.True(t, changed)
			require.False(t, gjson.GetBytes(retryBody, "prompt_cache_breakpoint").Exists())
			require.Equal(t, "keep", gjson.GetBytes(retryBody, "input").String())
		})
	}
}

func TestOpenAIResponsesRejectedFieldRetryStateAllowsPromptCacheBreakpointVariantOnce(t *testing.T) {
	body := []byte(`{"input":[{"prompt_cache_breakpoint":{"type":"message_start"}}]}`)
	responseBody := []byte(`{"error":{"code":"invalid_parameter","message":"input[0].prompt_cache_breakpoint is not supported on this model","param":"input[0].prompt_cache_breakpoint"}}`)
	state := newOpenAIResponsesRejectedFieldRetryState(body)

	retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, state.Allow(retryBody))
	require.False(t, state.Allow(retryBody))
}
