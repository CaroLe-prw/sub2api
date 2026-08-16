package service

import (
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	openai "github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// selectChannelMonitorProbeModels reduces automatic probes to one cheap text
// representative per protocol family. Exact account-level allowlist entries
// remain explicit opt-ins and are therefore kept individually. Global and
// wildcard allowlists only constrain the candidate set; they do not multiply
// probes by themselves.
func selectChannelMonitorProbeModels(account *Account, candidates, accountWhitelist []string) []string {
	if account == nil || len(candidates) == 0 {
		return []string{}
	}

	explicit := make(map[string]struct{}, len(accountWhitelist))
	for _, pattern := range accountWhitelist {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern != "" && !strings.Contains(pattern, "*") {
			explicit[pattern] = struct{}{}
		}
	}

	selected := make([]string, 0, len(candidates))
	coveredFamilies := make(map[string]struct{})
	remainingByFamily := make(map[string][]string)
	for _, model := range normalizeModels(candidates) {
		family := channelMonitorProbeFamily(account, model)
		if _, ok := explicit[strings.ToLower(model)]; ok {
			selected = append(selected, model)
			coveredFamilies[family] = struct{}{}
			continue
		}
		if isHighCostChannelMonitorProbeModel(model) {
			continue
		}
		remainingByFamily[family] = append(remainingByFamily[family], model)
	}

	families := make([]string, 0, len(remainingByFamily))
	for family := range remainingByFamily {
		families = append(families, family)
	}
	sort.Strings(families)
	for _, family := range families {
		if _, covered := coveredFamilies[family]; covered {
			continue
		}
		if model := chooseChannelMonitorRepresentative(account, family, remainingByFamily[family]); model != "" {
			selected = append(selected, model)
		}
	}

	selected = normalizeModels(selected)
	sort.Slice(selected, func(i, j int) bool {
		return strings.ToLower(selected[i]) < strings.ToLower(selected[j])
	})
	return selected
}

func channelMonitorProbeFamily(account *Account, model string) string {
	if account == nil || account.Platform != PlatformAntigravity {
		if account == nil {
			return "unknown"
		}
		return account.Platform
	}
	combined := strings.ToLower(strings.TrimSpace(model) + " " + strings.TrimSpace(account.GetMappedModel(model)))
	switch {
	case strings.Contains(combined, "claude"):
		return PlatformAnthropic
	case strings.Contains(combined, "gemini"):
		return PlatformGemini
	case strings.Contains(combined, "grok"):
		return PlatformGrok
	default:
		return PlatformOpenAI
	}
}

func chooseChannelMonitorRepresentative(account *Account, family string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	preferred := channelMonitorDefaultProbeModel(family)
	for _, model := range candidates {
		mapped := ""
		if account != nil {
			mapped = account.GetMappedModel(model)
		}
		if strings.EqualFold(model, preferred) || strings.EqualFold(mapped, preferred) {
			return model
		}
	}

	ordered := append([]string(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := channelMonitorRepresentativeRank(ordered[i]), channelMonitorRepresentativeRank(ordered[j])
		if left != right {
			return left < right
		}
		return strings.ToLower(ordered[i]) < strings.ToLower(ordered[j])
	})
	return ordered[0]
}

func channelMonitorDefaultProbeModel(family string) string {
	switch family {
	case PlatformOpenAI:
		return openai.DefaultTestModel
	case PlatformGemini:
		return geminicli.DefaultTestModel
	case PlatformGrok:
		return xai.ResolveDefaultTextModel("")
	default:
		return claude.DefaultTestModel
	}
}

func channelMonitorRepresentativeRank(model string) int {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(model, "flash"), strings.Contains(model, "mini"), strings.Contains(model, "haiku"), strings.Contains(model, "luna"):
		return 0
	case strings.Contains(model, "sonnet"), strings.Contains(model, "fast"), strings.Contains(model, "terra"):
		return 1
	case strings.Contains(model, "opus"), strings.Contains(model, "pro"), strings.Contains(model, "max"), strings.Contains(model, "sol"):
		return 3
	default:
		return 2
	}
}

func isHighCostChannelMonitorProbeModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, marker := range []string{
		"image", "imagine", "video", "realtime", "audio", "voice", "tts", "embedding", "moderation", "dall-e", "sora",
	} {
		if strings.Contains(model, marker) {
			return true
		}
	}
	return false
}
