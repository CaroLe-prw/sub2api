package service

import (
	"strings"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func normalizeCandyTestModel(platform, model string) (string, error) {
	if platform != PlatformOpenAI {
		return "", nil
	}
	model = strings.TrimSpace(model)
	if len(model) > 100 || strings.IndexFunc(model, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 || isOpenAIImageModel(model) {
		return "", infraerrors.BadRequest("INVALID_CANDY_TEST_MODEL", "Candy test model must be a text model ID of at most 100 characters without whitespace")
	}
	return model, nil
}

func normalizeCandyTestReasoningEffort(platform, effort string) (string, error) {
	if platform != PlatformOpenAI {
		return "", nil
	}
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" {
		return defaultCandyTestReasoningEffort, nil
	}
	switch effort {
	case "low", "medium", "high", "xhigh", "max":
		return effort, nil
	default:
		return "", infraerrors.BadRequest("INVALID_CANDY_TEST_REASONING_EFFORT", "Candy test reasoning effort must be low, medium, high, xhigh, or max")
	}
}

type candyTestConfig struct {
	Model           string
	ReasoningEffort string
}

// A selected group wins. Without a group filter, all attached OpenAI groups
// must agree on both settings; never arbitrarily select a group's effort.
func resolveCandyTestConfig(account *Account, groupID int64) (candyTestConfig, error) {
	config := candyTestConfig{Model: defaultCandyTestModel, ReasoningEffort: defaultCandyTestReasoningEffort}
	found := false
	for _, group := range account.Groups {
		if group == nil || group.Platform != PlatformOpenAI || (groupID > 0 && group.ID != groupID) {
			continue
		}
		model, err := normalizeCandyTestModel(group.Platform, group.CandyTestModel)
		if err != nil {
			return candyTestConfig{}, err
		}
		if model == "" {
			model = defaultCandyTestModel
		}
		effort, err := normalizeCandyTestReasoningEffort(group.Platform, group.CandyTestReasoningEffort)
		if err != nil {
			return candyTestConfig{}, err
		}
		candidate := candyTestConfig{Model: model, ReasoningEffort: effort}
		if groupID > 0 {
			return candidate, nil
		}
		if found && config != candidate {
			return candyTestConfig{}, infraerrors.BadRequest("CANDY_TEST_GROUP_REQUIRED", "This account has different candy-test models or reasoning efforts across groups. Select a group in the account list before testing.")
		}
		config, found = candidate, true
	}
	if groupID > 0 {
		return candyTestConfig{}, infraerrors.BadRequest("CANDY_TEST_GROUP_INVALID", "Account does not belong to the selected OpenAI group")
	}
	return config, nil
}

// Zero keeps automatic checks disabled; existing groups never consume quota by default.
func normalizeCandyTestInterval(platform string, seconds int) (int, error) {
	if platform != PlatformOpenAI {
		return 0, nil
	}
	if seconds < 0 || seconds > 86400 {
		return 0, infraerrors.BadRequest("INVALID_CANDY_TEST_INTERVAL", "Candy test interval must be a whole number of seconds from 0 to 86400; 0 disables automatic checks")
	}
	return seconds, nil
}
