package service

import (
	"strings"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	maxCandyTestModels            = 20
	maxCandyTestModelLength       = 100
	maxCandyTestModelConfigLength = 2048
)

// Keep the existing string setting compatible with single-model groups. Multiple
// candidates are normalized to one model per line, preserving priority order.
func normalizeCandyTestModel(platform, model string) (string, error) {
	if platform != PlatformOpenAI {
		return "", nil
	}
	invalid := infraerrors.BadRequest("INVALID_CANDY_TEST_MODEL", "Candy tests accept up to 20 text model IDs, each at most 100 characters without whitespace, separated by commas or newlines (2048 characters total)")
	model = strings.TrimSpace(model)
	if len(model) > maxCandyTestModelConfigLength {
		return "", invalid
	}
	parts := strings.FieldsFunc(model, func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == '\r' })
	models := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, candidate := range parts {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if len(candidate) > maxCandyTestModelLength || strings.IndexFunc(candidate, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 || isOpenAIImageModel(candidate) {
			return "", invalid
		}
		if !seen[candidate] {
			models = append(models, candidate)
			seen[candidate] = true
		}
		if len(models) > maxCandyTestModels {
			return "", invalid
		}
	}
	return strings.Join(models, "\n"), nil
}

// Select locally from normalized candidates; never probe an unsupported model or
// fall through after a wrong answer/API error. Each candy run sends one model.
func selectCandyTestModel(account *Account, candidates string) (string, error) {
	for _, model := range strings.Split(candidates, "\n") {
		if account.isModelMappingSupported(model) {
			return model, nil
		}
	}
	return "", errCandyTestModelNotAllowed.WithMetadata(map[string]string{"models": candidates})
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
	// Model is the normalized ordered candidate setting, not the selected model.
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
