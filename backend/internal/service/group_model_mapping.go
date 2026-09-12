package service

import (
	"context"
	"net/http"
	"strings"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// NormalizeGroupModelMapping validates exact, single-hop request aliases for every group platform.
// An empty map disables mapping; nil on update leaves the stored map unchanged.
func NormalizeGroupModelMapping(_ string, mapping map[string]string) (map[string]string, error) {
	invalid := func() error {
		return infraerrors.New(http.StatusBadRequest, "INVALID_GROUP_MODEL_MAPPING", "Group model mapping requires at most 64 unique, non-empty exact model pairs (up to 200 characters, no wildcards or control characters)")
	}
	if len(mapping) > 64 {
		return nil, invalid()
	}
	out := make(map[string]string, len(mapping))
	for from, to := range mapping {
		from, to = strings.TrimSpace(from), strings.TrimSpace(to)
		if from == "" || to == "" || len([]rune(from)) > 200 || len([]rune(to)) > 200 || strings.ContainsAny(from+to, "*") || strings.ContainsFunc(from+to, unicode.IsControl) {
			return nil, invalid()
		}
		if _, exists := out[from]; exists {
			return nil, invalid()
		}
		out[from] = to
	}
	return out, nil
}

func cloneGroupModelMapping(mapping map[string]string) map[string]string {
	if mapping == nil {
		return nil
	}
	out := make(map[string]string, len(mapping))
	for from, to := range mapping {
		out[from] = to
	}
	return out
}

// applyGroupModelMapping runs before account selection. A matching group rule
// takes precedence over a channel alias and pins billing to the group's target.
// Account mappings can still translate that target to a provider-specific ID.
func applyGroupModelMapping(ctx context.Context, groupID *int64, model string, mapping ChannelMappingResult) ChannelMappingResult {
	if groupID == nil {
		return mapping
	}
	group, ok := ctx.Value(ctxkey.Group).(*Group)
	if !ok || group == nil || group.ID != *groupID {
		return mapping
	}
	// Composite middleware may already have rewritten the body to the provider's
	// model. Resolve the group rule using the retained public request, and keep
	// its billing target separate from the composite route's upstream model.
	forwardModel := ""
	if group.Platform == PlatformComposite {
		if publicModel, ok := RequestedPublicModelFromContext(ctx); ok {
			model = publicModel
			forwardModel, _ = ResolvedUpstreamModelFromContext(ctx)
		}
	}
	if target := group.ModelMapping[model]; target != "" {
		mapping.GroupMapped = true
		mapping.Mapped = true
		mapping.MappedModel = target
		mapping.GroupBillingModel = target
		if forwardModel != "" {
			mapping.MappedModel = forwardModel
		}
		mapping.BillingModelSource = BillingModelSourceChannelMapped
	}
	return mapping
}
