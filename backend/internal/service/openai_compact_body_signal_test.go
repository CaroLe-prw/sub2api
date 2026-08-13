//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestHasCompactionTriggerInInput_DetectsCompactSignal(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"stream":true,
		"input":[
			{"type":"message","role":"user","content":"hello"},
			{"type":"compaction_trigger"}
		]
	}`)
	require.True(t, HasCompactionTriggerInInput(body))
}

func TestHasCompactionTriggerInInput_NoTrigger(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"input":[
			{"type":"message","role":"user","content":"hello"}
		]
	}`)
	require.False(t, HasCompactionTriggerInInput(body))
}

func TestHasCompactionTriggerInInput_EmptyInput(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[]}`)
	require.False(t, HasCompactionTriggerInInput(body))
}

func TestHasCompactionTriggerInInput_NoInputField(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5"}`)
	require.False(t, HasCompactionTriggerInInput(body))
}

func TestHasCompactionTriggerInInput_EmptyBody(t *testing.T) {
	require.False(t, HasCompactionTriggerInInput(nil))
	require.False(t, HasCompactionTriggerInInput([]byte{}))
}

func TestHasCompactionTriggerInInput_StringInput(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":"compaction_trigger"}`)
	require.False(t, HasCompactionTriggerInInput(body))
}

func TestHasCompactionTriggerInInput_CompactTriggerOnly(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`)
	require.True(t, HasCompactionTriggerInInput(body))
}

func TestOpenAIServerSideCompactionRequestRecognizesContextManagement(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","context_management":[{"type":"compaction"}],"input":"hello"}`)
	require.True(t, hasOpenAIServerSideCompactionInBody(body))
	c := newOpenAIAutoContextCompactionTestContext("/v1/responses/compact", body)
	require.True(t, isOpenAIServerSideCompactionRequest(c, []byte(`{"model":"gpt-5.6-sol","input":"hello"}`)))
}

func TestStripOpenAIResponsesLiteInputForCompaction(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","context_management":[{"type":"compaction"}],"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"collaboration"}]},{"type":"message","role":"user","content":"hello"}]}`)
	updated, changed, err := stripOpenAIResponsesLiteInputForCompaction(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(updated, `input.#(type=="additional_tools")`).Exists())
	require.True(t, gjson.GetBytes(updated, `input.#(type=="message")`).Exists())
}

func TestStripOpenAIResponsesLiteInputAfterPathIdentifiesCompaction(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"collaboration"}]},{"type":"message","role":"user","content":"hello"}]}`)
	updated, changed, err := stripOpenAIResponsesLiteInput(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(updated, `input.#(type=="additional_tools")`).Exists())
	require.True(t, gjson.GetBytes(updated, `input.#(type=="message")`).Exists())
}

func TestStripOpenAIResponsesLiteWSMetadataForCompaction(t *testing.T) {
	body := []byte(`{"type":"response.create","stream":true,"input":[{"type":"compaction_trigger"}],"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}}`)
	updated, changed, err := stripOpenAIResponsesLiteWSMetadataForCompaction(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, isOpenAIResponsesLiteWebSocketPayload(updated))
}
