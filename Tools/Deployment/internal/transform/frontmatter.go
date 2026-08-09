package transform

import (
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// resolveTools determines the tool frontmatter output for the transformation. It handles
// two distinct cases:
//
//   - Placeholder case: the source declares tools as a scalar placeholder (e.g.
//     `tools: {tool-permissions}`). The placeholder is expanded using the descriptor's
//     PlaceholderExpansion list. Report.Tools is an empty, non-nil slice because the
//     expansion is not decomposed into per-tool resolutions at this stage.
//
//   - List case: the source declares a concrete list of generic tool names. These are
//     passed to Module.Tools, which maps them to harness-specific names and builds the
//     frontmatter field(s). Report.Tools mirrors the resolution list from the module.
//
// If the source has no "tools" field, or if the descriptor declares no tools_key, a safe
// empty result is returned. All harness-specific tool knowledge flows through Module.Tools
// or through the descriptor's PlaceholderExpansion; no harness name appears here (AC8.4).
func resolveTools(req Request, fm *docformat.Frontmatter, desc *domain.HarnessDescriptor) (domain.ToolResult, error) {
	v, ok := fm.Get("tools")
	if !ok {
		// Source has no tools field — return empty, non-nil Resolutions for a safe report.
		return domain.ToolResult{Resolutions: make([]domain.ToolResolution, 0)}, nil
	}

	if v.Kind == domain.KindScalar {
		// Placeholder case: route through module.Tools() with Placeholder set so the module
		// controls the output format. Built-in modules may render the placeholder expansion
		// differently (e.g., Claude Code uses a comma-separated scalar while GHCP CLI uses a
		// flow-style single-quoted list). The Placeholder field carries the verbatim source
		// placeholder text (e.g., "{tool-permissions}").
		toolReq := domain.ToolRequest{
			AgentKey:     req.Key,
			Placeholder:  v.Scalar,
			CustomNames:  req.CustomTools,
			SkippedTools: req.SkippedTools,
		}
		result, err := req.Module.Tools(toolReq)
		if err != nil {
			return domain.ToolResult{}, err
		}
		if result.Resolutions == nil {
			result.Resolutions = make([]domain.ToolResolution, 0)
		}
		return result, nil
	}

	// List case: extract generic tool names and call Module.Tools.
	var genericTools []string
	if v.Kind == domain.KindList {
		genericTools = make([]string, len(v.Items))
		for i, item := range v.Items {
			genericTools[i] = item.Scalar
		}
	}

	toolReq := domain.ToolRequest{
		AgentKey:     req.Key,
		Generic:      genericTools,
		CustomNames:  req.CustomTools,
		SkippedTools: req.SkippedTools,
	}
	result, err := req.Module.Tools(toolReq)
	if err != nil {
		return domain.ToolResult{}, err
	}
	if result.Resolutions == nil {
		result.Resolutions = make([]domain.ToolResolution, 0)
	}
	return result, nil
}

// applyFrontmatter applies the descriptor-driven frontmatter plan (removes, adds, and
// key order) together with the model selection, version stamps, and resolved tool fields.
// It mutates fm in place and returns:
//   - fieldChanges: one FieldChange per frontmatter key that was added, overwritten, or
//     removed (satisfying AC8.5).
//   - gaps: domain.Gap entries produced during the transformation (e.g. GapNoModel).
//
// Step order is significant: removes happen before adds so that a key that is dropped and
// re-added with a different value under the same name works correctly. Version stamps and
// tool fields are applied after the descriptor plan so that the plan's key-order list can
// include them in the desired position.
func applyFrontmatter(
	fm *docformat.Frontmatter,
	plan domain.FrontmatterPlan,
	toolResult domain.ToolResult,
	req Request,
	desc *domain.HarnessDescriptor,
) ([]FieldChange, []domain.Gap) {
	var changes []FieldChange
	var gaps []domain.Gap

	// Step 1: Remove fields declared in the descriptor drop list.
	for _, key := range plan.Remove {
		if v, ok := fm.Get(key); ok {
			changes = append(changes, FieldChange{
				Key:    key,
				Before: renderValue(v),
				After:  "",
				Reason: "descriptor drop",
			})
			fm.Remove(key)
		}
	}

	// Step 2: Add or overwrite fields from the descriptor add list.
	for _, field := range plan.Set {
		before := ""
		if v, ok := fm.Get(field.Key); ok {
			before = renderValue(v)
		}
		changes = append(changes, FieldChange{
			Key:    field.Key,
			Before: before,
			After:  renderValue(field.Value),
			Reason: "descriptor add",
		})
		fm.Set(field.Key, field.Value)
	}

	// Step 3: Set the model field from the resolved model selection.
	modelKey := desc.Frontmatter.ModelKey
	if modelKey != "" {
		if req.Model.Resolved() {
			before := ""
			if v, ok := fm.Get(modelKey); ok {
				before = renderValue(v)
			}
			changes = append(changes, FieldChange{
				Key:    modelKey,
				Before: before,
				After:  req.Model.ModelID,
				Reason: "model selection",
			})
			fm.Set(modelKey, domain.ScalarValue(req.Model.ModelID, domain.QuotePlain))
		} else {
			// No model resolved: remove the placeholder field and record a gap so the
			// TODO checklist can surface the missing model for the user.
			if v, ok := fm.Get(modelKey); ok {
				changes = append(changes, FieldChange{
					Key:    modelKey,
					Before: renderValue(v),
					After:  "",
					Reason: "model unresolved",
				})
				fm.Remove(modelKey)
			}
			gaps = append(gaps, domain.Gap{
				Kind:    domain.GapNoModel,
				Subject: req.Key,
				Detail:  "no model selected for this agent",
			})
		}
	}

	// Step 4: Stamp transform_version, injections_version, and tool_mappings_version.
	if c := applyVersionStamp(fm, "transform_version", desc.TransformVersion); c != nil {
		changes = append(changes, *c)
	}
	if c := applyVersionStamp(fm, "injections_version", desc.InjectionsVersion); c != nil {
		changes = append(changes, *c)
	}
	// tool_mappings_version is a content hash of the effective tool_destinations
	// mappings for this harness (combined project + user config). It lets `update`
	// detect stale tool mappings without re-diffing tool lists. See "The
	// tool_mappings_version stamp" in Tools/Deployment/docs/configuration.md.
	if c := applyVersionStamp(fm, "tool_mappings_version", req.ToolMappingsVersion); c != nil {
		changes = append(changes, *c)
	}

	// Step 4b: Stamp bundle_version for roles that receive bundle blocks. The stamp enables
	// staleness detection on subsequent runs. Written only when AppliesToRole is true, so the
	// orchestrator (which today receives no bundle blocks) is never stamped.
	if req.Bundle.AppliesToRole(req.Role) && req.Bundle.Version != "" {
		before := ""
		if v, ok := fm.Get("bundle_version"); ok {
			before = renderValue(v)
		}
		fm.Set("bundle_version", domain.ScalarValue(req.Bundle.Version, domain.QuotePlain))
		changes = append(changes, FieldChange{
			Key:    "bundle_version",
			Before: before,
			After:  req.Bundle.Version,
			Reason: "bundle version stamp",
		})
	}

	// Step 5: Set the resolved tool fields produced by Module.Tools (or PlaceholderExpansion).
	for _, field := range toolResult.Fields {
		before := ""
		if v, ok := fm.Get(field.Key); ok {
			before = renderValue(v)
		}
		changes = append(changes, FieldChange{
			Key:    field.Key,
			Before: before,
			After:  renderValue(field.Value),
			Reason: "tool mapping",
		})
		fm.Set(field.Key, field.Value)
	}

	// Step 6: Accumulate gaps for unresolved tool mappings. ToolUnmapped and ToolSkipped
	// resolutions are never emitted into the output; each produces a GapUnmappedTool so the
	// TODO checklist can surface the missing tool mapping. ToolCustom resolutions are resolved
	// and must not produce a gap.
	for _, res := range toolResult.Resolutions {
		if res.Outcome == domain.ToolUnmapped || res.Outcome == domain.ToolSkipped {
			gaps = append(gaps, domain.Gap{
				Kind:    domain.GapUnmappedTool,
				Subject: req.Key,
				Detail:  "unmapped tool: " + res.Generic,
			})
		}
	}

	// Step 7: Reorder the frontmatter keys according to the descriptor's key_order.
	// Unlisted keys keep their current relative order and are appended after listed keys.
	if len(plan.KeyOrder) > 0 {
		fm.Reorder(plan.KeyOrder)
	}

	return changes, gaps
}

// applyVersionStamp sets a single version stamp field in the frontmatter and returns the
// corresponding FieldChange. Returns nil when value is empty (nothing to stamp).
func applyVersionStamp(fm *docformat.Frontmatter, key, value string) *FieldChange {
	if value == "" {
		return nil
	}
	before := ""
	if v, ok := fm.Get(key); ok {
		before = renderValue(v)
	}
	fm.Set(key, domain.ScalarValue(value, domain.QuotePlain))
	return &FieldChange{
		Key:    key,
		Before: before,
		After:  value,
		Reason: "version stamp",
	}
}

// renderValue converts a domain.FieldValue to a concise string for use in FieldChange
// Before/After fields. The rendering is for human-readable audit purposes only; it is
// not a YAML round-trip serialisation. Callers must not parse the returned string.
func renderValue(v domain.FieldValue) string {
	switch v.Kind {
	case domain.KindScalar:
		return v.Scalar
	case domain.KindList:
		items := make([]string, len(v.Items))
		for i, item := range v.Items {
			items[i] = renderValue(item)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case domain.KindMapping:
		pairs := make([]string, len(v.Pairs))
		for i, p := range v.Pairs {
			pairs[i] = p.Key + ": " + renderValue(p.Value)
		}
		return "{" + strings.Join(pairs, ", ") + "}"
	default:
		return ""
	}
}
