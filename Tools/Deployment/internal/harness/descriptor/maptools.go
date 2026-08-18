package descriptor

import (
	"fmt"
	"path"
	"sort"
	"strings"

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
// For ToolCustom, the resolution's Destinations are determined as follows:
//   - When d.Tools.CustomToolDestinations is non-empty and no entry in d.Tools.Mappings exists
//     for the generic tool (i.e. no config-declared tool_destinations entry was merged in for it),
//     Destinations is a deep copy of the declared list with the formatted custom-tool name
//     injected into each destination's Names. Kind, Field, Format, and Separator are preserved
//     verbatim from the declaration.
//   - Otherwise, Destinations contains a single DestMain destination carrying the formatted
//     custom-tool name — the historical behaviour.
//
// HarnessTools for a ToolCustom resolution is always the single formatted name, regardless of
// how many destinations the tool fans out to.
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
			// When the descriptor declares a non-empty default custom-tool destination list and
			// no explicit mapping entry exists for this generic tool, use that list. Config-declared
			// tool_destinations entries are merged into d.Tools.Mappings before MapTools runs, so a
			// mapping entry means an explicit config entry takes precedence over the harness default.
			_, hasMappingEntry := mappingByGeneric[genericTool]
			if len(d.Tools.CustomToolDestinations) > 0 && !hasMappingEntry {
				dests := make([]domain.ToolDestination, len(d.Tools.CustomToolDestinations))
				for i, decl := range d.Tools.CustomToolDestinations {
					dests[i] = domain.ToolDestination{
						Kind:      decl.Kind,
						Field:     decl.Field,
						Format:    decl.Format,
						Separator: decl.Separator,
						Names:     []string{name},
					}
				}
				res.Destinations = dests
			} else {
				res.Destinations = []domain.ToolDestination{
					{Kind: domain.DestMain, Names: []string{name}},
				}
			}

		default:
			if mapping, ok := mappingByGeneric[genericTool]; ok {
				// Found in mappings — whether Destinations is empty or not, the outcome is ToolMapped.
				res.Outcome = domain.ToolMapped
				// Deep copy Destinations to avoid aliasing.
				dests := make([]domain.ToolDestination, len(mapping.Destinations))
				for i, dest := range mapping.Destinations {
					names := make([]string, len(dest.Names))
					copy(names, dest.Names)
					dests[i] = domain.ToolDestination{
						Kind:      dest.Kind,
						Field:     dest.Field,
						Format:    dest.Format,
						Separator: dest.Separator,
						Names:     names,
					}
				}
				res.Destinations = dests
				// HarnessTools is the flattened, deduplicated, first-seen union of all Names.
				// Initialized as empty non-nil so that mappings with no names (explicitly
				// unsupported) produce []string{} rather than nil, satisfying reflect.DeepEqual
				// comparisons in contracttests.
				seenNames := make(map[string]bool)
				ht := make([]string, 0)
				for _, dest := range dests {
					for _, name := range dest.Names {
						if !seenNames[name] {
							seenNames[name] = true
							ht = append(ht, name)
						}
					}
				}
				res.HarnessTools = ht
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

// destFieldBucket accumulates names for a single DestField destination key.
// Format and Separator are established by the first contributing destination and cannot
// be overridden by later contributions to the same key.
type destFieldBucket struct {
	format    domain.ToolValueFormat
	separator string
	names     map[string]bool
}

// emitFieldBuckets converts the collected DestField buckets into FrontmatterFields,
// emitting them in first-seen order with names sorted lexicographically within each bucket.
func emitFieldBuckets(buckets map[string]*destFieldBucket, order []string) []domain.FrontmatterField {
	fields := make([]domain.FrontmatterField, 0, len(order))
	for _, fieldKey := range order {
		bucket := buckets[fieldKey]
		sortedNames := make([]string, 0, len(bucket.names))
		for name := range bucket.names {
			sortedNames = append(sortedNames, name)
		}
		sort.Strings(sortedNames)

		format := bucket.format
		if format == "" {
			format = domain.DefaultToolValueFormat // defaults to list-block
		}

		switch format {
		case domain.FormatListBlock:
			items := make([]domain.FieldValue, len(sortedNames))
			for i, name := range sortedNames {
				items[i] = domain.FieldValue{Kind: domain.KindScalar, Scalar: name}
			}
			fields = append(fields, domain.FrontmatterField{
				Key:   fieldKey,
				Value: domain.ListValue(items, domain.ListBlock),
			})
		case domain.FormatListFlow:
			items := make([]domain.FieldValue, len(sortedNames))
			for i, name := range sortedNames {
				items[i] = domain.FieldValue{Kind: domain.KindScalar, Scalar: name}
			}
			fields = append(fields, domain.FrontmatterField{
				Key:   fieldKey,
				Value: domain.ListValue(items, domain.ListFlow),
			})
		case domain.FormatScalar:
			sep := bucket.separator
			if sep == "" {
				sep = domain.DefaultScalarSeparator
			}
			fields = append(fields, domain.FrontmatterField{
				Key:   fieldKey,
				Value: domain.FieldValue{Kind: domain.KindScalar, Scalar: strings.Join(sortedNames, sep)},
			})
		}
	}
	return fields
}

// buildListToolFields assembles a flat KindList frontmatter field for the ShapeList (and
// default) shape. By-convention tools are always included. ToolMapped and ToolCustom
// resolutions contribute names to destinations: DestMain names go to the main tools field
// (sorted by universe position), and DestField names each produce a separate FrontmatterField
// (first-seen order, names sorted lexicographically within each field).
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

	seenMain := make(map[string]bool)
	var mainEntries []toolEntry

	addMain := func(name string) {
		if seenMain[name] {
			return
		}
		seenMain[name] = true
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

	// Collect DestField buckets in first-seen order.
	fieldBuckets := make(map[string]*destFieldBucket)
	var fieldOrder []string

	addToField := func(fieldKey, name string, format domain.ToolValueFormat, separator string) {
		if _, exists := fieldBuckets[fieldKey]; !exists {
			fieldBuckets[fieldKey] = &destFieldBucket{
				format:    format,
				separator: separator,
				names:     make(map[string]bool),
			}
			fieldOrder = append(fieldOrder, fieldKey)
		}
		fieldBuckets[fieldKey].names[name] = true
	}

	// Add harness tools from ToolMapped and ToolCustom resolutions via Destinations.
	for _, res := range resolutions {
		if res.Outcome != domain.ToolMapped && res.Outcome != domain.ToolCustom {
			continue
		}
		for _, dest := range res.Destinations {
			switch dest.Kind {
			case domain.DestMain:
				for _, name := range dest.Names {
					addMain(name)
				}
			case domain.DestField:
				for _, name := range dest.Names {
					addToField(dest.Field, name, dest.Format, dest.Separator)
				}
			}
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

	// Emit one FrontmatterField per DestField destination, in first-seen order.
	fields = append(fields, emitFieldBuckets(fieldBuckets, fieldOrder)...)

	return fields
}

// buildPermissionToolFields assembles a KindMapping frontmatter field for the ShapePermission
// shape. Every tool in the Universe appears exactly once, ordered by Universe declaration order.
// A tool is assigned the disposition "allow" when it appears in the resolved DestMain set (i.e.
// a ToolMapped or ToolCustom resolution's DestMain destination names it), or when it is marked
// ByConvention. Otherwise the tool's Unused disposition (from the descriptor) is used.
// DestField destination names are emitted as separate FrontmatterFields and do not enter
// the permission map.
func buildPermissionToolFields(d *domain.HarnessDescriptor, resolutions []domain.ToolResolution) []domain.FrontmatterField {
	// Build the resolved (allow) set from DestMain destinations plus by-convention tools.
	resolved := make(map[string]bool, len(d.Tools.Universe))
	for _, t := range d.Tools.Universe {
		if t.ByConvention {
			resolved[t.Name] = true
		}
	}

	// Collect DestField buckets in first-seen order (same rules as the list shape).
	fieldBuckets := make(map[string]*destFieldBucket)
	var fieldOrder []string

	addToField := func(fieldKey, name string, format domain.ToolValueFormat, separator string) {
		if _, exists := fieldBuckets[fieldKey]; !exists {
			fieldBuckets[fieldKey] = &destFieldBucket{
				format:    format,
				separator: separator,
				names:     make(map[string]bool),
			}
			fieldOrder = append(fieldOrder, fieldKey)
		}
		fieldBuckets[fieldKey].names[name] = true
	}

	for _, res := range resolutions {
		if res.Outcome != domain.ToolMapped && res.Outcome != domain.ToolCustom {
			continue
		}
		for _, dest := range res.Destinations {
			switch dest.Kind {
			case domain.DestMain:
				for _, name := range dest.Names {
					resolved[name] = true
				}
			case domain.DestField:
				for _, name := range dest.Names {
					addToField(dest.Field, name, dest.Format, dest.Separator)
				}
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

	fields := []domain.FrontmatterField{
		{
			Key:   d.Frontmatter.ToolsKey,
			Value: domain.FieldValue{Kind: domain.KindMapping, Pairs: pairs},
		},
	}

	// Emit separate fields for DestField destinations.
	fields = append(fields, emitFieldBuckets(fieldBuckets, fieldOrder)...)

	return fields
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

// ExtractToolEntries returns the harness-side tool names a deployed agent declares, read
// from the value stored under the harness's own Frontmatter.ToolsKey, interpreted according
// to d.Tools.Shape. It is the exact inverse of buildToolFields.
//
//   - ShapePermission: value is a mapping of tool name to disposition. Only entries whose
//     disposition equals string(domain.Allow) are returned; every other entry is the
//     harness's universe listing and must not become a generic tool.
//   - ShapeList (and any other shape): value is a list; every scalar item is returned.
//
// Regardless of shape, an entry naming a d.Tools.Universe tool whose ByConvention is true is
// excluded (AD-14). Names are returned in document order, deduplicated first-seen. A zero
// FieldValue, a value of an unexpected kind, or a descriptor with an empty ToolsKey returns
// an empty slice — never an error, because an absent tools key is a legitimate deployed shape.
func ExtractToolEntries(d *domain.HarnessDescriptor, toolsValue domain.FieldValue) []string {
	// An empty ToolsKey means the harness declares no tools field; nothing to extract.
	if d.Frontmatter.ToolsKey == "" {
		return nil
	}

	// Build a lookup set of by-convention tool names. Their presence in a deployed file
	// carries no information about the agent's generic tools (AD-14).
	byConvention := make(map[string]bool, len(d.Tools.Universe))
	for _, t := range d.Tools.Universe {
		if t.ByConvention {
			byConvention[t.Name] = true
		}
	}

	var result []string
	seen := make(map[string]bool)

	// addEntry applies the by-convention exclusion and first-seen deduplication.
	addEntry := func(name string) {
		if name == "" || byConvention[name] || seen[name] {
			return
		}
		seen[name] = true
		result = append(result, name)
	}

	if d.Tools.Shape == domain.ShapePermission {
		// Permission shape: value is a KindMapping. Only allow-dispositioned entries count.
		if toolsValue.Kind != domain.KindMapping {
			return nil
		}
		allowStr := string(domain.Allow)
		for _, pair := range toolsValue.Pairs {
			if pair.Value.Kind == domain.KindScalar && pair.Value.Scalar == allowStr {
				addEntry(pair.Key)
			}
		}
	} else {
		// List shape (and any other shape): value is a KindList or a comma-separated KindScalar;
		// every scalar item counts.
		switch toolsValue.Kind {
		case domain.KindList:
			for _, item := range toolsValue.Items {
				if item.Kind == domain.KindScalar {
					addEntry(item.Scalar)
				}
			}
		case domain.KindScalar:
			// Accept a comma-separated scalar (e.g. "Read, Write, Edit") as produced by the
			// claude-code built-in harness. Split on commas, trim whitespace, discard empty parts.
			for _, part := range strings.Split(toolsValue.Scalar, ",") {
				addEntry(strings.TrimSpace(part))
			}
		default:
			return nil
		}
	}

	return result
}

// ReverseMapTool resolves one harness-side tool name to the generic tool name that maps to
// it. A mapping matches when any of its Destinations lists the name in Destination.Names,
// regardless of destination Kind — a generic tool routed to a DestField destination is
// still that generic tool.
//
// Many-to-one: when several mappings name the same harness tool, the first match in
// d.Tools.Mappings declaration order wins (AD-5). Declaration order is the ordering authority.
//
// ok is false when no mapping names the tool, including the case where d.Tools.Mappings is
// nil or empty — a harness with no mapping data resolves nothing and guesses nothing.
func ReverseMapTool(d *domain.HarnessDescriptor, harnessName string) (generic string, ok bool) {
	// Iterate in declaration order so the first match wins for many-to-one cases (AD-5).
	for i := range d.Tools.Mappings {
		mapping := &d.Tools.Mappings[i]
		for _, dest := range mapping.Destinations {
			for _, name := range dest.Names {
				if name == harnessName {
					return mapping.Generic, true
				}
			}
		}
	}
	return "", false
}

// ReverseMapTools resolves a sequence of harness-side tool names, returning one
// ReverseToolResolution per input entry in the same order. Input order is preserved even
// for unmapped entries so the caller can prompt in a stable order; deduplication of the
// resulting generic names is the caller's responsibility.
func ReverseMapTools(d *domain.HarnessDescriptor, harnessNames []string) []domain.ReverseToolResolution {
	if len(harnessNames) == 0 {
		return []domain.ReverseToolResolution{}
	}
	result := make([]domain.ReverseToolResolution, len(harnessNames))
	for i, name := range harnessNames {
		genericName, ok := ReverseMapTool(d, name)
		if ok {
			result[i] = domain.ReverseToolResolution{
				Harness: name,
				Generic: genericName,
				Outcome: domain.ToolMapped,
			}
		} else {
			result[i] = domain.ReverseToolResolution{
				Harness: name,
				Generic: "",
				Outcome: domain.ToolUnmapped,
			}
		}
	}
	return result
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
