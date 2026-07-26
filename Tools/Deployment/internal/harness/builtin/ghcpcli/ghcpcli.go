// Package ghcpcli implements the GitHub Copilot CLI built-in harness module. It embeds the
// ghcp-cli.yaml descriptor and provides tool formatting as a flow-style YAML sequence with
// single-quoted items (the format GHCP CLI expects), which cannot be expressed through the
// descriptor data alone.
//
// Exceptions requiring module code beyond the descriptor (documented per I11.6):
//
//  1. Flow-style single-quoted tool list: GHCP CLI expects tools as a flow-style YAML
//     sequence with single-quoted items: ['read', 'edit', 'search']. The descriptor's
//     FieldValue schema supports KindList with style options, but does not allow specifying
//     both ListFlow style AND QuoteSingle on items simultaneously at the descriptor level
//     (the descriptor Add field is unmarshalled from YAML without per-item quote control).
//     Module code post-processes the descriptor-driven list to apply both the flow style
//     and the single-quote style on every item.
//
//  2. Skill path key subdirectory: Skills must be deployed under
//     "<skills-dir>/<key>/SKILL.md" to avoid filename collisions (all entry files share
//     the name SKILL.md). The descriptor path template is a flat directory without
//     support for an intermediate key segment, so module code handles that composition.
//
//  3. Frontmatter version stamps (model, transform_version, injections_version): these are
//     runtime values applied exclusively by the transform pipeline (Steps 3 and 4 of
//     applyFrontmatter). The module does not include them in FrontmatterPlan.Set; doing so
//     would produce duplicate FieldChange entries in transform.Report.Fields because the
//     transform applies them independently of the module plan.
//
// GHCP CLI does not support hooks. TargetPath for ArtifactHook returns
// ErrArtifactUnsupported; HookPlan always returns Supported: false.
//
// Registration: init() calls registry.Register so the module is available whenever this
// package is imported.
package ghcpcli

import (
	_ "embed"
	"fmt"
	"path"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/registry"
)

//go:embed ghcp-cli.yaml
var embeddedDescriptor []byte

func init() {
	registry.Register("ghcp-cli", func() (domain.HarnessModule, error) {
		return New()
	})
}

// module is the GitHub Copilot CLI built-in HarnessModule implementation.
type module struct {
	ref  domain.HarnessRef
	desc *domain.HarnessDescriptor
}

// New parses the embedded ghcp-cli.yaml descriptor and returns the GHCP CLI HarnessModule.
// The module formats tools as a flow-style single-quoted YAML sequence.
func New() (domain.HarnessModule, error) {
	desc, err := descriptor.Parse(embeddedDescriptor, "builtin:ghcp-cli")
	if err != nil {
		return nil, fmt.Errorf("parse embedded ghcp-cli descriptor: %w", err)
	}
	ref := domain.HarnessRef{
		ID:          desc.ID,
		DisplayName: desc.DisplayName,
		Tier:        domain.TierBuiltin,
		Usable:      true,
	}
	return &module{ref: ref, desc: desc}, nil
}

// Ref returns identity and provenance. Two calls return equal values.
func (m *module) Ref() domain.HarnessRef {
	return m.ref
}

// Descriptor returns the parsed descriptor. The same pointer is returned on every call;
// callers must treat it as read-only.
func (m *module) Descriptor() *domain.HarnessDescriptor {
	return m.desc
}

// Close releases resources. GHCP CLI holds no external resources; always returns nil.
func (m *module) Close() error {
	return nil
}

// Tools maps generic tool names to GHCP CLI tools and formats the result as a flow-style
// YAML sequence with single-quoted items: ['read', 'edit', 'search'].
//
// Many-to-one aliasing is handled by descriptor.MapTools: multiple generic tools that
// share a harness tool name produce one entry in the output list.
//
// See package documentation for why module code is required for this format.
func (m *module) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	if req.Placeholder != "" {
		return m.expandPlaceholder(), nil
	}
	result, err := descriptor.MapTools(m.desc, req)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return m.convertFieldsToFlowList(result), nil
}

// expandPlaceholder handles the {tool-permissions} placeholder for orchestrator agents.
// It expands the descriptor's PlaceholderExpansion (or full Universe if empty) into a
// flow-style single-quoted list.
func (m *module) expandPlaceholder() domain.ToolResult {
	expansion := m.desc.Tools.PlaceholderExpansion
	if len(expansion) == 0 {
		expansion = make([]string, len(m.desc.Tools.Universe))
		for i, t := range m.desc.Tools.Universe {
			expansion[i] = t.Name
		}
	}
	items := make([]domain.FieldValue, len(expansion))
	for i, name := range expansion {
		items[i] = domain.FieldValue{Kind: domain.KindScalar, Scalar: name, Quote: domain.QuoteSingle}
	}
	return domain.ToolResult{
		Fields: []domain.FrontmatterField{{
			Key: m.desc.Frontmatter.ToolsKey,
			Value: domain.FieldValue{
				Kind:  domain.KindList,
				List:  domain.ListFlow,
				Items: items,
			},
		}},
		Resolutions: []domain.ToolResolution{},
	}
}

// convertFieldsToFlowList converts the KindList tools field returned by descriptor.MapTools
// into a flow-style list with single-quoted scalar items.
func (m *module) convertFieldsToFlowList(result domain.ToolResult) domain.ToolResult {
	if len(result.Fields) == 0 {
		return result
	}
	toolsKey := m.desc.Frontmatter.ToolsKey
	newFields := make([]domain.FrontmatterField, len(result.Fields))
	for i, f := range result.Fields {
		if f.Key == toolsKey && f.Value.Kind == domain.KindList {
			items := make([]domain.FieldValue, len(f.Value.Items))
			for j, item := range f.Value.Items {
				items[j] = domain.FieldValue{
					Kind:  domain.KindScalar,
					Scalar: item.Scalar,
					Quote: domain.QuoteSingle,
				}
			}
			newFields[i] = domain.FrontmatterField{
				Key: f.Key,
				Value: domain.FieldValue{
					Kind:  domain.KindList,
					List:  domain.ListFlow,
					Items: items,
				},
			}
		} else {
			newFields[i] = f
		}
	}
	return domain.ToolResult{
		Fields:      newFields,
		Resolutions: result.Resolutions,
	}
}

// Frontmatter builds the FrontmatterPlan for GHCP CLI agents. It returns the
// descriptor's static Add fields (containing user-invocable: false), remove list,
// and key order. Model and version stamps are applied exclusively by the transform
// pipeline and must not appear here; including them would produce duplicate FieldChange
// entries in transform.Report.Fields.
func (m *module) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{
		Set:      m.desc.Frontmatter.Add,
		Remove:   m.desc.Frontmatter.Drop,
		KeyOrder: m.desc.Frontmatter.KeyOrder,
	}, nil
}

// TargetPath returns the deployment path for one artifact.
//
// Skills receive a key-named subdirectory to prevent filename collisions (see package
// documentation). Hook artifacts return ErrArtifactUnsupported because GHCP CLI does
// not support hooks. All other kinds are handled by the descriptor algorithm.
func (m *module) TargetPath(req domain.TargetPathRequest) (string, error) {
	if req.Kind == domain.ArtifactSkill {
		return m.skillTargetPath(req)
	}
	return descriptor.ResolveTargetPath(m.desc, req)
}

// skillTargetPath resolves a skill deployment path with an intermediate key subdirectory:
// <skills-dir>/<key>/<filename>. This prevents filename collisions when multiple skills
// (all named SKILL.md by convention) are deployed to the same harness.
func (m *module) skillTargetPath(req domain.TargetPathRequest) (string, error) {
	sp := m.desc.Paths.Skills
	if !sp.Supported {
		return "", fmt.Errorf("%w: skill", domain.ErrArtifactUnsupported)
	}
	var baseDir string
	switch req.Scope {
	case domain.ScopeProject:
		baseDir = sp.Project
	case domain.ScopeUser:
		tmpl, ok := sp.User[req.GOOS]
		if !ok {
			tmpl, ok = sp.User[""]
			if !ok {
				return "", fmt.Errorf("%w: no user skill path declared for %s", domain.ErrArtifactUnsupported, req.GOOS)
			}
		}
		baseDir = tmpl
	default:
		return "", fmt.Errorf("unknown scope %q", req.Scope)
	}
	filename := req.FileName
	if filename == "" {
		filename = req.Key
	}
	return path.Join(baseDir, req.Key, filename), nil
}

// Injection returns the harness-level content for a canonical injection name as declared
// in the embedded descriptor's injections list.
func (m *module) Injection(name string) (string, bool) {
	for _, inj := range m.desc.Injections {
		if inj.Name == name {
			return inj.Content, true
		}
	}
	return "", false
}

// HookPlan always returns Supported: false because GHCP CLI does not support hooks.
// Any request for hooks (TargetPath for ArtifactHook or a HookPlan call) is reported
// rather than silently ignored.
func (m *module) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{
		Supported: false,
		Reason:    "GitHub Copilot CLI does not support hooks",
	}, nil
}
