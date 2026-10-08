package service

import (
	"math"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	openai "github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// selectChannelMonitorProbeModels reduces automatic probes to the lowest-priced
// text representative per protocol family. Exact account-level allowlist entries
// remain explicit opt-ins and are therefore kept individually. Global and
// wildcard allowlists only constrain the candidate set; they do not multiply
// probes by themselves.
func selectChannelMonitorProbeModels(account *Account, candidates, accountWhitelist []string, billing *BillingService) []string {
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
		if isHighCostChannelMonitorProbeModel(model) || isHighCostChannelMonitorProbeModel(account.GetMappedModel(model)) {
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
		if model := chooseChannelMonitorRepresentative(account, family, remainingByFamily[family], billing); model != "" {
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

func chooseChannelMonitorRepresentative(account *Account, family string, candidates []string, billing *BillingService) string {
	if len(candidates) == 0 {
		return ""
	}
	// Compare standard input + output + cache-read + cache-write token rates
	// with equal weight. This is a unit-price comparison, not a prediction of
	// the actual probe bill (token counts and cache hits vary). Use upstream prices,
	// just as recordChannelProbeUsage does; an account-wide multiplier cannot
	// change the relative ordering within that account.
	cheapest := ""
	lowestPrice := math.Inf(1)
	for _, model := range candidates {
		price, known := channelMonitorProbeModelPrice(account, model, billing)
		if known && (price < lowestPrice || (price == lowestPrice && strings.ToLower(model) < strings.ToLower(cheapest))) {
			cheapest, lowestPrice = model, price
		}
	}
	if cheapest != "" {
		return cheapest
	}

	// Preserve connectivity probes when none of the candidates has an
	// identified price. Unknown prices must never be treated as free.
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

func channelMonitorProbeModelPrice(account *Account, model string, billing *BillingService) (float64, bool) {
	if billing == nil {
		return 0, false
	}
	if account != nil {
		model = account.GetMappedModel(model)
	}
	// GetModelPricing alone can guess a family price for an unknown model.
	// Only compare identified catalog entries, including explicit free models.
	if !billing.HasIdentifiedTokenPricing(model) {
		return 0, false
	}
	pricing, err := billing.GetModelPricing(model)
	if err != nil || pricing == nil {
		return 0, false
	}
	price := 0.0
	for _, rate := range []float64{
		pricing.InputPricePerToken,
		pricing.OutputPricePerToken,
		pricing.CacheReadPricePerToken,
		pricing.CacheCreationPricePerToken,
	} {
		if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return 0, false
		}
		price += rate
	}
	return price, !math.IsInf(price, 0)
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
