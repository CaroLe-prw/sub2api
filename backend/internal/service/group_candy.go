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

// A selected account-list group wins. With no group filter, only infer a model
// when all attached OpenAI groups agree; never pick an arbitrary group's model.
func resolveCandyTestModel(account *Account, groupID int64) (string, error) {
	model := ""
	for _, group := range account.Groups {
		if group == nil || group.Platform != PlatformOpenAI || (groupID > 0 && group.ID != groupID) {
			continue
		}
		candidate, err := normalizeCandyTestModel(group.Platform, group.CandyTestModel)
		if err != nil {
			return "", err
		}
		if candidate == "" {
			candidate = defaultCandyTestModel
		}
		if groupID > 0 {
			return candidate, nil
		}
		if model != "" && model != candidate {
			return "", infraerrors.BadRequest("CANDY_TEST_GROUP_REQUIRED", "This account has different candy-test models across groups. Select a group in the account list before testing.")
		}
		model = candidate
	}
	if groupID > 0 {
		return "", infraerrors.BadRequest("CANDY_TEST_GROUP_INVALID", "Account does not belong to the selected OpenAI group")
	}
	if model == "" {
		model = defaultCandyTestModel
	}
	return model, nil
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
