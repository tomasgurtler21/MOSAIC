// Package opencode implements the OpenCode built-in harness module. It embeds the
// opencode.yaml descriptor and applies the OpenCode-specific transformation logic.
//
// OpenCode is the most complex transformation case in the system:
//   - Replaces the generic tools list with a nested permission mapping (allow/deny for every
//     tool in the harness's universe, in stable descriptor order).
//   - Adds "mode: subagent" to every regular agent; the orchestrator receives "mode: primary".
//   - Drops the "name" field present in the generic source.
//   - Applies a distinct key order: id, version, transform_version, injections_version,
//     description, mode, model, permission.
//   - Deploys hooks as plugins into .opencode/plugins/ with no registration steps required.
//   - Skills use a key-named subdirectory to prevent filename collisions.
//
// The module's transformation logic has no in-process-only assumptions so it can be
// rebuilt as an out-of-process external module (AC12.7).
//
// Exceptions requiring module code beyond the descriptor:
//
//  1. Mode field value: "mode: subagent" for regular agents vs "mode: primary" for the
//     orchestrator. The descriptor can only declare static Add fields; the orchestrator
//     distinction requires module code that inspects FrontmatterRequest.AgentKey.
//
//  2. Skill path key subdirectory: skills must be deployed under
//     "<skills-dir>/<key>/SKILL.md" to avoid filename collisions (all entry files share
//     the name SKILL.md). The descriptor path template is a flat directory without
//     support for an intermediate key segment.
//
//  3. Placeholder expansion for permission shape: descriptor.MapTools expands placeholders
//     into a flat list; OpenCode needs a full permission block from the expansion set.
//
// Registration: init() calls registry.Register so the module is available whenever
// this package is imported.
package opencode

import (
	_ "embed"
	"fmt"
	"path"
	"path/filepath"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/injectionfile"
	"mosaic-deploy/internal/harness/registry"
)

//go:embed opencode.yaml
var embeddedDescriptor []byte

func init() {
	registry.Register("opencode", func(opts registry.BuiltinOptions) (domain.HarnessModule, error) {
		return New(opts)
	})
}

// module is the OpenCode built-in HarnessModule implementation.
type module struct {
	ref            domain.HarnessRef
	desc           *domain.HarnessDescriptor
	injections     map[string]string // parsed from HarnessInjections.md at construction time
	orchInjections map[string]string // parsed from HarnessInjectionsOrchestrator.md at construction time
}

// New parses the embedded opencode.yaml descriptor and reads HarnessInjections.md and
// HarnessInjectionsOrchestrator.md from the declared repository directory at construction
// time. Editing those files and rerunning the tool changes the injected content with no
// rebuild required. The descriptor YAML remains embedded.
func New(opts registry.BuiltinOptions) (domain.HarnessModule, error) {
	desc, err := descriptor.Parse(embeddedDescriptor, "builtin:opencode")
	if err != nil {
		return nil, fmt.Errorf("parse embedded opencode descriptor: %w", err)
	}
	contentDir := filepath.Join(opts.MosaicRoot, RepoContentDir)
	content, err := injectionfile.LoadDir(contentDir)
	if err != nil {
		return nil, fmt.Errorf("opencode: load harness content: %w", err)
	}
	desc.OrchestratorInjectionsVersion = content.OrchestratorVersion
	ref := domain.HarnessRef{
		ID:          desc.ID,
		DisplayName: desc.DisplayName,
		Tier:        domain.TierBuiltin,
		Usable:      true,
	}
	return &module{ref: ref, desc: desc, injections: content.Shared, orchInjections: content.Orchestrator}, nil
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

// Close releases resources. OpenCode holds no external resources; always returns nil.
func (m *module) Close() error {
	return nil
}

// Tools maps generic tool names to OpenCode permissions and returns a KindMapping field.
//
// OpenCode uses the permission shape: every tool in the universe is listed explicitly
// with either "allow" or "deny". The descriptor's buildPermissionToolFields handles this
// for the normal (non-placeholder) case.
//
// For the {tool-permissions} placeholder (orchestrators), the base descriptor.MapTools
// would produce a flat list, which is incorrect for the permission shape. This method
// overrides that path to produce the full permission block from the descriptor's
// placeholder_expansion set.
func (m *module) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	if req.Placeholder != "" {
		return m.expandPlaceholderPermission(), nil
	}
	return descriptor.MapTools(m.desc, req)
}

// expandPlaceholderPermission handles the {tool-permissions} placeholder for orchestrator
// agents. It expands the descriptor's PlaceholderExpansion into a full permission block
// where the expansion tools are "allow", by_convention tools are "allow", and all others
// are "deny" per their Unused disposition.
func (m *module) expandPlaceholderPermission() domain.ToolResult {
	expansion := m.desc.Tools.PlaceholderExpansion
	if len(expansion) == 0 {
		expansion = make([]string, len(m.desc.Tools.Universe))
		for i, t := range m.desc.Tools.Universe {
			expansion[i] = t.Name
		}
	}

	// Build the allowed set from the expansion list.
	allowed := make(map[string]bool, len(expansion))
	for _, name := range expansion {
		allowed[name] = true
	}

	// Build the full permission block in Universe order.
	pairs := make([]domain.FieldPair, len(m.desc.Tools.Universe))
	for i, t := range m.desc.Tools.Universe {
		disposition := string(t.Unused)
		if allowed[t.Name] || t.ByConvention {
			disposition = string(domain.Allow)
		}
		pairs[i] = domain.FieldPair{
			Key:   t.Name,
			Value: domain.ScalarValue(disposition, domain.QuotePlain),
		}
	}

	return domain.ToolResult{
		Fields: []domain.FrontmatterField{{
			Key:   m.desc.Frontmatter.ToolsKey,
			Value: domain.MappingValue(pairs),
		}},
		Resolutions: []domain.ToolResolution{},
	}
}

// Frontmatter builds the FrontmatterPlan for OpenCode agents.
//
// The mode field is dynamic: regular agents receive "mode: subagent" and the orchestrator
// receives "mode: primary". All other operations (drop, key order) come from the descriptor.
// Model and version stamps are applied exclusively by the transform pipeline and must not
// appear here; including them would produce duplicate FieldChange entries in transform.Report.Fields.
//
// Exception: orchestrator_injections_version is stamped here for the orchestrator agent when
// non-empty, and filtered from the key_order for non-orchestrator agents and orchestrator agents
// with an empty version (where the field is not emitted).
func (m *module) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	mode := "subagent"
	if req.AgentKey == "orchestrator" {
		mode = "primary"
	}

	set := []domain.FrontmatterField{
		{Key: "mode", Value: domain.ScalarValue(mode, domain.QuotePlain)},
	}

	keyOrder := m.desc.Frontmatter.KeyOrder
	orchField, _ := agentfields.ByDeployedName("orchestrator_injections_version")
	if req.AgentKey == "orchestrator" && req.Versions.OrchestratorInjectionsVersion != "" {
		set = append(set, domain.FrontmatterField{
			Key:   orchField.Deployed,
			Value: domain.ScalarValue(req.Versions.OrchestratorInjectionsVersion, domain.QuotePlain),
		})
	} else {
		keyOrder = filterKeyOrder(keyOrder, orchField.Deployed)
	}

	return domain.FrontmatterPlan{
		Set:      set,
		Remove:   m.desc.Frontmatter.Drop,
		KeyOrder: keyOrder,
	}, nil
}

// filterKeyOrder returns a copy of keys with the given key removed.
func filterKeyOrder(keys []string, exclude string) []string {
	filtered := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != exclude {
			filtered = append(filtered, k)
		}
	}
	return filtered
}

// TargetPath returns the deployment path for one artifact.
//
// Skills receive a key-named subdirectory to prevent filename collisions (see package
// documentation). All other artifact kinds are handled by the descriptor algorithm.
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
	if req.Scope != domain.ScopeProject {
		return "", fmt.Errorf("%w: scope %q is not supported; only %q is accepted", domain.ErrUnsupportedScope, req.Scope, domain.ScopeProject)
	}
	sp := m.desc.Paths.Skills
	if !sp.Supported {
		return "", fmt.Errorf("%w: skill", domain.ErrArtifactUnsupported)
	}
	filename := req.FileName
	if filename == "" {
		filename = req.Key
	}
	return path.Join(sp.Project, req.Key, filename), nil
}

// Injection returns the harness-level content for a canonical injection name.
//
// For the "orchestrator" agent key, shared content (from HarnessInjections.md) is merged
// with orchestrator-only content (from HarnessInjectionsOrchestrator.md). For all other
// agent keys, only shared content is returned.
func (m *module) Injection(req domain.InjectionRequest) (string, bool) {
	sharedContent, sharedOk := m.injections[req.Name]
	if req.AgentKey != "orchestrator" {
		return sharedContent, sharedOk
	}
	orchContent, orchOk := m.orchInjections[req.Name]
	if !sharedOk && !orchOk {
		return "", false
	}
	if sharedContent != "" && orchContent != "" {
		return sharedContent + "\n\n" + orchContent, true
	}
	if sharedContent != "" {
		return sharedContent, true
	}
	if orchContent != "" {
		return orchContent, true
	}
	return "", true
}

// HookPlan resolves a hook bundle for OpenCode. OpenCode deploys hooks to the plugins
// directory (.opencode/plugins/) and requires no registration steps — presence alone
// activates a plugin.
func (m *module) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	if !m.desc.Hooks.Supported {
		return domain.HookPlan{
			Supported: false,
			Reason:    "harness descriptor declares hooks not supported",
		}, nil
	}
	variantKey := m.desc.Hooks.VariantKey
	if variantKey == "" {
		variantKey = m.desc.ID
	}
	targetDir := m.desc.Paths.Hooks.Project
	variant, ok := req.Bundle.Variants[variantKey]
	if !ok {
		// No variant in this bundle for OpenCode.
		return domain.HookPlan{
			Supported: true,
			TargetDir: targetDir,
		}, nil
	}
	return domain.HookPlan{
		Supported:    true,
		TargetDir:    targetDir,
		Files:        variant.Files,
		Registration: variant.Registration,
	}, nil
}
