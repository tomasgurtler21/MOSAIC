package descriptor

import (
	"fmt"
	"path"
	"sort"

	"mosaic-deploy/internal/domain"
)

// MapTools is the shared, descriptor-driven tool mapping algorithm (CD-2). It maps every
// generic tool in req.Generic to harness-specific tool names using d.Tools.Mappings and returns
// one ToolResolution per entry in req.Generic, preserving the same order.
//
// When req.Placeholder is non-empty (and req.Generic is empty), MapTools performs placeholder
// expansion instead: it expands d.Tools.PlaceholderExpansion (or the full Universe when that
// slice is empty) into a KindList field with ListFlow style and returns empty Resolutions.
// This allows descriptor-only and runtime-provisioned harnesses to handle the {tool-permissions}
// placeholder without module-specific code.
//
// Outcomes per generic tool (list case only):
//   - ToolSkipped: req.SkippedTools marks this tool as skipped by the user (checked first).
//   - ToolCustom: no mapping entry exists but req.CustomNames supplies a name.
//   - ToolMapped: an entry exists in d.Tools.Mappings for this generic name (including an entry
//     with an empty HarnessTools slice, which means "explicitly unsupported by this harness").
//   - ToolUnmapped: no mapping entry exists and no custom name was supplied.
//
// When multiple generic tools share the same harness tool (many-to-one), each produces its own
// ToolResolution; deduplication of the rendered Fields output is the caller's responsibility.
// When one generic tool maps to several harness tools (one-to-many), all are listed in the
// single ToolResolution for that generic tool, ordered by d.Tools.Universe order.
func MapTools(d *domain.HarnessDescriptor, req domain.ToolRequest) (domain.ToolResult, error) {
	// Placeholder case: expand the descriptor's PlaceholderExpansion into a list field.
	if req.Placeholder != "" {
		return expandPlaceholder(d), nil
	}

	// Build a fast lookup from generic name to its mapping entry.
	mappingByGeneric := make(map[string]*domain.ToolMapping, len(d.Tools.Mappings))
	for i := range d.Tools.Mappings {
		mappingByGeneric[d.Tools.Mappings[i].Generic] = &d.Tools.Mappings[i]
	}

	resolutions := make([]domain.ToolResolution, 0, len(req.Generic))

	for _, genericTool := range req.Generic {
		res := domain.ToolResolution{Generic: genericTool}

		switch {
		case req.SkippedTools[genericTool]:
			// User explicitly declined to configure this tool.
			res.Outcome = domain.ToolSkipped

		case req.CustomNames != nil && req.CustomNames[genericTool] != "":
			// User supplied a custom MCP server name for an otherwise unmapped tool.
			name := req.CustomNames[genericTool]
			if d.Tools.CustomToolTemplate != "" {
				name = fmt.Sprintf(d.Tools.CustomToolTemplate, name)
			}
			res.Outcome = domain.ToolCustom
			res.HarnessTools = []string{name}

		default:
			if mapping, ok := mappingByGeneric[genericTool]; ok {
				// Found in mappings — whether HarnessTools is empty or not, the outcome is ToolMapped.
				res.Outcome = domain.ToolMapped
				// Use a copy of the slice to avoid aliasing.
				if len(mapping.HarnessTools) > 0 {
					ht := make([]string, len(mapping.HarnessTools))
					copy(ht, mapping.HarnessTools)
					res.HarnessTools = ht
				} else {
					res.HarnessTools = []string{}
				}
				res.Field = mapping.Field
			} else {
				// Not in mappings at all.
				res.Outcome = domain.ToolUnmapped
			}
		}

		resolutions = append(resolutions, res)
	}

	fields := buildToolFields(d, resolutions)

	return domain.ToolResult{
		Fields:      fields,
		Resolutions: resolutions,
	}, nil
}

// expandPlaceholder handles the {tool-permissions} placeholder case for descriptor-only
// and runtime-provisioned harnesses. It expands d.Tools.PlaceholderExpansion (or the full
// Universe when that slice is empty) into a KindList field with ListFlow style.
// Built-in modules that need a different output format (e.g., Claude Code's comma-separated
// scalar) override this behaviour in their own Tools() implementation.
func expandPlaceholder(d *domain.HarnessDescriptor) domain.ToolResult {
	if d.Frontmatter.ToolsKey == "" {
		return domain.ToolResult{Resolutions: make([]domain.ToolResolution, 0)}
	}
	expansion := d.Tools.PlaceholderExpansion
	if len(expansion) == 0 {
		// Empty PlaceholderExpansion means "the whole Universe".
		expansion = make([]string, len(d.Tools.Universe))
		for i, t := range d.Tools.Universe {
			expansion[i] = t.Name
		}
	}
	items := make([]domain.FieldValue, len(expansion))
	for i, name := range expansion {
		items[i] = domain.ScalarValue(name, domain.QuotePlain)
	}
	return domain.ToolResult{
		Fields: []domain.FrontmatterField{{
			Key:   d.Frontmatter.ToolsKey,
			Value: domain.ListValue(items, domain.ListFlow),
		}},
		Resolutions: make([]domain.ToolResolution, 0),
	}
}

// buildToolFields assembles the frontmatter Fields output from the tool resolutions.
// It dispatches to the shape-specific builder based on d.Tools.Shape.
func buildToolFields(d *domain.HarnessDescriptor, resolutions []domain.ToolResolution) []domain.FrontmatterField {
	if d.Frontmatter.ToolsKey == "" {
		return nil
	}
	if d.Tools.Shape == domain.ShapePermission {
		return buildPermissionToolFields(d, resolutions)
	}
	return buildListToolFields(d, resolutions)
}

// buildListToolFields assembles a flat KindList frontmatter field for the ShapeList (and
// default) shape. By-convention tools are always included. ToolMapped and ToolCustom
// resolutions contribute to the main list unless they carry a non-empty Field, in which
// case each unique diversion key produces its own separate FrontmatterField.
func buildListToolFields(d *domain.HarnessDescriptor, resolutions []domain.ToolResolution) []domain.FrontmatterField {
	// Build a position index from the Universe so we can sort by canonical order.
	universePos := make(map[string]int, len(d.Tools.Universe))
	for i, t := range d.Tools.Universe {
		universePos[t.Name] = i
	}

	type toolEntry struct {
		name string
		pos  int
	}

	seen := make(map[string]bool)
	var mainEntries []toolEntry

	addMain := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		pos, ok := universePos[name]
		if !ok {
			pos = len(universePos) // append after all Universe tools
		}
		mainEntries = append(mainEntries, toolEntry{name: name, pos: pos})
	}

	// By-convention tools are always emitted regardless of what the agent declares.
	for _, t := range d.Tools.Universe {
		if t.ByConvention {
			addMain(t.Name)
		}
	}

	// Collect field-diverted tools: diversion key → set of harness tool names.
	diversionNames := make(map[string]map[string]bool)
	var diversionOrder []string // tracks first-seen order for deterministic output

	// Add harness tools from ToolMapped and ToolCustom resolutions.
	for _, res := range resolutions {
		if res.Outcome != domain.ToolMapped && res.Outcome != domain.ToolCustom {
			continue
		}
		if res.Field != "" {
			// Routed to a separate frontmatter key rather than the main tools list.
			if _, exists := diversionNames[res.Field]; !exists {
				diversionNames[res.Field] = make(map[string]bool)
				diversionOrder = append(diversionOrder, res.Field)
			}
			for _, ht := range res.HarnessTools {
				diversionNames[res.Field][ht] = true
			}
			continue
		}
		for _, ht := range res.HarnessTools {
			addMain(ht)
		}
	}

	// Sort main entries by Universe position so output order is deterministic.
	sort.Slice(mainEntries, func(i, j int) bool {
		return mainEntries[i].pos < mainEntries[j].pos
	})

	items := make([]domain.FieldValue, len(mainEntries))
	for i, e := range mainEntries {
		items[i] = domain.FieldValue{Kind: domain.KindScalar, Scalar: e.name}
	}

	fields := []domain.FrontmatterField{
		{
			Key:   d.Frontmatter.ToolsKey,
			Value: domain.ListValue(items, domain.ListBlock),
		},
	}

	// Emit one FrontmatterField per diversion destination, in first-seen order.
	for _, fieldName := range diversionOrder {
		toolSet := diversionNames[fieldName]
		// Sort tool names within the diversion field for deterministic output.
		sortedNames := make([]string, 0, len(toolSet))
		for name := range toolSet {
			sortedNames = append(sortedNames, name)
		}
		sort.Strings(sortedNames)
		divItems := make([]domain.FieldValue, len(sortedNames))
		for i, name := range sortedNames {
			divItems[i] = domain.FieldValue{Kind: domain.KindScalar, Scalar: name}
		}
		fields = append(fields, domain.FrontmatterField{
			Key:   fieldName,
			Value: domain.FieldValue{Kind: domain.KindList, Items: divItems},
		})
	}

	return fields
}

// buildPermissionToolFields assembles a KindMapping frontmatter field for the ShapePermission
// shape. Every tool in the Universe appears exactly once, ordered by Universe declaration order.
// A tool is assigned the disposition "allow" when it appears in the resolved tool set (i.e. a
// ToolMapped or ToolCustom resolution includes it), or when it is marked ByConvention. Otherwise
// the tool's Unused disposition (from the descriptor) is used.
func buildPermissionToolFields(d *domain.HarnessDescriptor, resolutions []domain.ToolResolution) []domain.FrontmatterField {
	// Build the resolved (allow) set from non-diverted mapped/custom resolutions plus
	// by-convention tools.
	resolved := make(map[string]bool, len(d.Tools.Universe))

	for _, t := range d.Tools.Universe {
		if t.ByConvention {
			resolved[t.Name] = true
		}
	}

	for _, res := range resolutions {
		if res.Field != "" {
			continue // diverted to a separate frontmatter key
		}
		if res.Outcome == domain.ToolMapped || res.Outcome == domain.ToolCustom {
			for _, ht := range res.HarnessTools {
				resolved[ht] = true
			}
		}
	}

	// Build pairs in Universe declaration order so output is deterministic.
	pairs := make([]domain.FieldPair, len(d.Tools.Universe))
	for i, t := range d.Tools.Universe {
		disposition := string(t.Unused)
		if resolved[t.Name] {
			disposition = string(domain.Allow)
		}
		pairs[i] = domain.FieldPair{
			Key:   t.Name,
			Value: domain.FieldValue{Kind: domain.KindScalar, Scalar: disposition},
		}
	}

	return []domain.FrontmatterField{
		{
			Key:   d.Frontmatter.ToolsKey,
			Value: domain.FieldValue{Kind: domain.KindMapping, Pairs: pairs},
		},
	}
}

// ApplyFrontmatterSpec derives the FrontmatterPlan that expresses the harness's field shaping
// rules (add, drop, reorder) for a single artifact. It is the shared algorithm used by the
// descriptor-only adapter and every built-in module.
func ApplyFrontmatterSpec(d *domain.HarnessDescriptor, req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	_ = req // reserved for future per-kind or per-agent shaping rules

	return domain.FrontmatterPlan{
		Set:      d.Frontmatter.Add,
		Remove:   d.Frontmatter.Drop,
		KeyOrder: d.Frontmatter.KeyOrder,
	}, nil
}

// ResolveTargetPath expands the deployment path template for a given artifact kind.
// Returns a path relative to the deployment root using forward slashes.
// Returns domain.ErrArtifactUnsupported when the descriptor declares no path for the kind.
// Returns domain.ErrUnsupportedScope when req.Scope is not domain.ScopeProject.
func ResolveTargetPath(d *domain.HarnessDescriptor, req domain.TargetPathRequest) (string, error) {
	if req.Scope != domain.ScopeProject {
		return "", fmt.Errorf("%w: scope %q is not supported; only %q is accepted", domain.ErrUnsupportedScope, req.Scope, domain.ScopeProject)
	}

	sp, err := scopedPathsForKind(d, req.Kind)
	if err != nil {
		return "", err
	}

	// Determine the target filename: use req.FileName when given (skill or hook bundles
	// may have source file names that differ from the artifact key), otherwise derive
	// from the artifact key plus the harness's extension rule.
	filename := req.FileName
	if filename == "" {
		ext := d.Extensions[req.Kind]
		filename = req.Key + ext
	}

	if sp.Project == "" {
		return "", fmt.Errorf("%w: no project path declared for %s", domain.ErrArtifactUnsupported, req.Kind)
	}
	if req.Kind == domain.ArtifactSkill {
		// Skills share the filename "SKILL.md" across all skill bundles. Deploy each
		// under its own key subdirectory to prevent filename collisions.
		return path.Join(sp.Project, req.Key, filename), nil
	}
	return path.Join(sp.Project, filename), nil
}

// scopedPathsForKind returns the ScopedPaths for the requested artifact kind, or
// domain.ErrArtifactUnsupported when the kind is not supported by this descriptor.
func scopedPathsForKind(d *domain.HarnessDescriptor, kind domain.ArtifactKind) (domain.ScopedPaths, error) {
	var sp domain.ScopedPaths
	switch kind {
	case domain.ArtifactAgent:
		sp = d.Paths.Agents
	case domain.ArtifactSkill:
		sp = d.Paths.Skills
	case domain.ArtifactHook:
		sp = d.Paths.Hooks
	default:
		return domain.ScopedPaths{}, fmt.Errorf("%w: unknown artifact kind %q", domain.ErrArtifactUnsupported, kind)
	}
	if !sp.Supported {
		return domain.ScopedPaths{}, fmt.Errorf("%w: %s", domain.ErrArtifactUnsupported, kind)
	}
	return sp, nil
}
