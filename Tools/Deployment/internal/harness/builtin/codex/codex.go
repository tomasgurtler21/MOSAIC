// Package codex implements the Codex built-in harness module. It embeds the
// codex.yaml descriptor and applies the Codex-specific transformation logic.
//
// Codex is a TOML-format harness that collapses the agent's generic tool set
// into a single sandbox_mode field with two possible values: "read-only" and
// "workspace-write". The collapse rule is:
//
//   - If req.Placeholder is non-empty (MOSAIC orchestrator uses {tool-permissions}),
//     the mode is "workspace-write" regardless of req.Generic.
//   - Otherwise, if any escalating generic tool (file_write, file_edit, terminal,
//     subagent) is present in req.Generic, the mode is "workspace-write".
//   - Otherwise, the mode is "read-only".
//
// The descriptor declares no tools_key so the shared field builder contributes
// nothing to ToolResult.Fields. Only sandbox_mode is emitted as a field.
//
// Every generic tool in the known vocabulary is mapped to an empty destinations
// list, which resolves each as ToolMapped with no harness-side tool names. A
// placeholder request yields zero resolutions (the descriptor has no tools_key).
//
// Registration: init() calls registry.Register so the module is available
// whenever this package is imported.
package codex

import (
	_ "embed"
	"fmt"
	"path/filepath"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/injectionfile"
	"mosaic-deploy/internal/harness/registry"
)

//go:embed codex.yaml
var embeddedDescriptor []byte

func init() {
	registry.Register("codex", func(opts registry.BuiltinOptions) (domain.HarnessModule, error) {
		return New(opts)
	})
}

// module is the Codex built-in HarnessModule implementation.
type module struct {
	ref            domain.HarnessRef
	desc           *domain.HarnessDescriptor
	injections     map[string]string // parsed from HarnessInjections.md at construction time
	orchInjections map[string]string // parsed from HarnessInjectionsOrchestrator.md at construction time
}

// New parses the embedded codex.yaml descriptor and reads HarnessInjections.md and
// HarnessInjectionsOrchestrator.md from the declared repository directory at construction
// time. Editing those files and rerunning the tool changes the injected content with no
// rebuild required. The descriptor YAML remains embedded.
func New(opts registry.BuiltinOptions) (domain.HarnessModule, error) {
	desc, err := descriptor.Parse(embeddedDescriptor, "builtin:codex")
	if err != nil {
		return nil, fmt.Errorf("parse embedded codex descriptor: %w", err)
	}
	contentDir := filepath.Join(opts.MosaicRoot, RepoContentDir)
	content, err := injectionfile.LoadDir(contentDir)
	if err != nil {
		return nil, fmt.Errorf("codex: load harness content: %w", err)
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

// Close releases resources. Codex holds no external resources; always returns nil.
func (m *module) Close() error {
	return nil
}

// escalatingTools is the set of generic tool names that require workspace-level access.
// Their presence in a ToolRequest.Generic slice causes the collapse to emit workspace-write.
var escalatingTools = map[string]bool{
	"file_write": true,
	"file_edit":  true,
	"terminal":   true,
	"subagent":   true,
}

// collapseSandboxMode derives the sandbox_mode value from a ToolRequest.
//
// The placeholder branch is checked first: any non-empty Placeholder indicates the MOSAIC
// orchestrator's {tool-permissions} request, which contains every escalating capability.
// This must come before the Generic scan so a placeholder request with an empty Generic
// still produces workspace-write (FR-11b).
func collapseSandboxMode(req domain.ToolRequest) string {
	if req.Placeholder != "" {
		return "workspace-write"
	}
	for _, name := range req.Generic {
		if escalatingTools[name] {
			return "workspace-write"
		}
	}
	return "read-only"
}

// Tools maps generic tool names to Codex sandbox mode and returns the sandbox_mode field.
//
// descriptor.MapTools resolves each generic tool against the descriptor's mappings list.
// Since the Codex descriptor declares no tools_key, the shared field builder contributes
// nothing to result.Fields. This method appends exactly one field (sandbox_mode) whose
// value is derived from collapseSandboxMode.
//
// For a placeholder request, descriptor.MapTools returns zero Resolutions and no Fields
// (the descriptor has no tools_key, so expandPlaceholder returns an empty result). The
// sandbox_mode field is still appended, ensuring it is present in every output.
func (m *module) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	result, err := descriptor.MapTools(m.desc, req)
	if err != nil {
		return domain.ToolResult{}, err
	}
	mode := collapseSandboxMode(req)
	result.Fields = append(result.Fields, domain.FrontmatterField{
		Key:   "sandbox_mode",
		Value: domain.ScalarValue(mode, domain.QuotePlain),
	})
	return result, nil
}

// Frontmatter builds the FrontmatterPlan for Codex agents.
//
// The descriptor's Add, Drop, and KeyOrder fields are applied by ApplyFrontmatterSpec.
// The sandbox_mode field is emitted by Tools, not here.
func (m *module) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return descriptor.ApplyFrontmatterSpec(m.desc, req)
}

// TargetPath returns the deployment path for one artifact.
//
// Agents deploy to .codex/agents/<key>.toml. Skills deploy to
// .agents/skills/<key>/SKILL.md (key subdirectory, handled natively by
// descriptor.ResolveTargetPath). Hooks return ErrArtifactUnsupported.
func (m *module) TargetPath(req domain.TargetPathRequest) (string, error) {
	return descriptor.ResolveTargetPath(m.desc, req)
}

// Injection returns the harness-level content for a canonical injection name.
//
// For orchestrator role requests, shared content (from HarnessInjections.md) is merged
// with orchestrator-only content (from HarnessInjectionsOrchestrator.md). For all other
// roles, only shared content is returned.
func (m *module) Injection(req domain.InjectionRequest) (string, bool) {
	sharedContent, sharedOk := m.injections[req.Name]
	if req.Role != domain.RoleOrchestrator {
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

// HookPlan returns an unsupported result because Codex does not support hooks.
func (m *module) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{
		Supported: false,
		Reason:    "Codex does not support hooks",
	}, nil
}
