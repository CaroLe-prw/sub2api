package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// openAIModelsWithoutResponsesLite must use the full Responses contract even
// when a stale Codex model manifest or an account header override requests the
// private Responses Lite transport. The unsuffixed GPT-5.6 alias resolves to
// GPT-5.6 Sol and therefore has the same restriction.
var openAIModelsWithoutResponsesLite = map[string]struct{}{
	"gpt-5.6":       {},
	"gpt-5.6-sol":   {},
	"gpt-5.6-terra": {},
	"gpt-5.6-luna":  {},
}

func openAIModelRequiresFullResponses(model string) bool {
	_, restricted := openAIModelsWithoutResponsesLite[strings.ToLower(strings.TrimSpace(model))]
	return restricted || isOpenAIGPT6AstraModel(model)
}

func openAIResponsesLitePayloadModel(body []byte, fallbackModel string) string {
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if model == "" {
		model = strings.TrimSpace(fallbackModel)
	}
	return model
}

func openAIResponsesLiteRequiresFullResponses(account *Account, body []byte, fallbackModel string) bool {
	model := openAIResponsesLitePayloadModel(body, fallbackModel)
	if openAIModelRequiresFullResponses(model) {
		return true
	}
	if account == nil || model == "" {
		return false
	}
	return openAIModelRequiresFullResponses(account.GetMappedModel(model))
}

func isOpenAIResponsesLiteRequestedForPayload(c *gin.Context, account *Account, body []byte, fallbackModel string) bool {
	requested := isOpenAIResponsesLiteWebSocketPayload(body) ||
		isOpenAIResponsesLiteHeaderEnabledForAccount(openAIResponsesLiteHeaderFromContext(c), account)
	return requested && !openAIResponsesLiteRequiresFullResponses(account, body, fallbackModel)
}

func stripOpenAIResponsesLiteHeaderForUnsupportedModel(headers http.Header, account *Account, body []byte, fallbackModel string) {
	if headers == nil || !openAIResponsesLiteRequiresFullResponses(account, body, fallbackModel) {
		return
	}
	deleteHeaderAllForms(headers, responsesLiteHeaderKey)
}

func stripOpenAIResponsesLiteWSMetadataForUnsupportedModel(body []byte, account *Account, fallbackModel string) ([]byte, bool, error) {
	if len(body) == 0 || !openAIResponsesLiteRequiresFullResponses(account, body, fallbackModel) ||
		!gjson.GetBytes(body, "client_metadata."+responsesLiteWSMetadataKey).Exists() {
		return body, false, nil
	}
	updated, err := sjson.DeleteBytes(body, "client_metadata."+responsesLiteWSMetadataKey)
	if err != nil {
		return body, false, fmt.Errorf("remove Responses Lite metadata for unsupported model: %w", err)
	}
	return updated, true, nil
}
