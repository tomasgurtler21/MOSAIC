package registry

import (
	"os"
	"path/filepath"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/injectionfile"
)

// runtimeModule implements domain.HarnessModule for harnesses provided by an on-disk
// harness.yaml folder. The module's entire behaviour is driven by the descriptor through
// shared algorithms in harness/descriptor (CD-2):
//   - descriptor.MapTools handles tool mapping
//   - descriptor.ApplyFrontmatterSpec handles frontmatter shaping
//   - descriptor.ResolveTargetPath handles deployment paths
//
// This is the zero-code runtime harness tier (AC6.2): placing a folder with a valid
// harness.yaml under MosaicDeploy/harnesses/ makes it immediately available with no rebuild.
//
// The same struct is used as a placeholder for the external tier (TierExternal) until
// Stage 23 adds the JSON-over-stdio protocol. At that point, the external tier gains its
// own implementation that forwards calls across the process boundary.
type runtimeModule struct {
	ref             domain.HarnessRef
	desc            *domain.HarnessDescriptor
	injections      map[string]string // parsed from HarnessInjections.md at resolve time; may be empty
	orchInjections  map[string]string // parsed from HarnessInjectionsOrchestrator.md; may be empty
}

// newRuntimeModule constructs a descriptor-driven module for the given ref and descriptor.
// Both the descriptor-only and (for now) external tiers use this implementation.
// injections may be nil when no HarnessInjections.md is available (external tier, or absent file).
// orchInjections may be nil when no HarnessInjectionsOrchestrator.md is available.
func newRuntimeModule(ref domain.HarnessRef, desc *domain.HarnessDescriptor, injections map[string]string, orchInjections map[string]string) *runtimeModule {
	if injections == nil {
		injections = make(map[string]string)
	}
	if orchInjections == nil {
		orchInjections = make(map[string]string)
	}
	return &runtimeModule{ref: ref, desc: desc, injections: injections, orchInjections: orchInjections}
}

// loadInjections reads HarnessInjections.md from the same directory as the harness YAML file
// and parses it using injectionfile.ParseDeployedRegions. If the file does not exist, an empty
// map is returned (absence is treated as no harness-level injections). Any other read or
// parse error is returned to the caller.
func loadInjections(yamlPath string) (map[string]string, error) {
	mdPath := filepath.Join(filepath.Dir(yamlPath), "HarnessInjections.md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	return injectionfile.ParseDeployedRegions(data)
}

// loadOrchInjections reads HarnessInjectionsOrchestrator.md from the same directory as the
// harness YAML file and parses it using injectionfile.ParseDeployedRegionsWithVersion. If the
// file does not exist, an empty map and an empty version are returned (absence is treated as
// no orchestrator-only injections, which is the common case). Any other read or parse error
// is returned to the caller.
func loadOrchInjections(yamlPath string) (regions map[string]string, version string, err error) {
	mdPath := filepath.Join(filepath.Dir(yamlPath), "HarnessInjectionsOrchestrator.md")
	data, readErr := os.ReadFile(mdPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return make(map[string]string), "", nil
		}
		return nil, "", readErr
	}
	return injectionfile.ParseDeployedRegionsWithVersion(data)
}

// Ref returns the identity and provenance of this harness. The value is stable; calling Ref
// twice returns equal values.
func (m *runtimeModule) Ref() domain.HarnessRef {
	return m.ref
}

// Descriptor returns the parsed descriptor. The same pointer is returned on every call;
// callers must treat it as read-only.
func (m *runtimeModule) Descriptor() *domain.HarnessDescriptor {
	return m.desc
}

// Close releases resources held by this module. The descriptor-only tier holds no resources
// (no child processes, no open files), so this always returns nil.
func (m *runtimeModule) Close() error {
	return nil
}

// Tools maps an agent's generic tool list to harness-specific fields using the shared
// descriptor-driven algorithm. Every entry in req.Generic appears exactly once in the
// returned Resolutions, in the same order.
func (m *runtimeModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return descriptor.MapTools(m.desc, req)
}

// Frontmatter returns the ordered field operations the harness applies to a generic agent's
// frontmatter. The plan is derived from the descriptor's frontmatter.add / frontmatter.drop /
// frontmatter.key_order declarations.
func (m *runtimeModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return descriptor.ApplyFrontmatterSpec(m.desc, req)
}

// TargetPath returns the deployment path for one artifact, delegating to the shared
// descriptor path-resolution algorithm. Returns domain.ErrArtifactUnsupported when the
// descriptor declares no path for the requested artifact kind.
func (m *runtimeModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return descriptor.ResolveTargetPath(m.desc, req)
}

// Injection returns the harness-level content for a canonical injection name.
//
// For the "orchestrator" agent key, shared content (from HarnessInjections.md) is merged
// with orchestrator-only content (from HarnessInjectionsOrchestrator.md, loaded at
// construction time). For all other agent keys, only shared content is returned.
func (m *runtimeModule) Injection(req domain.InjectionRequest) (string, bool) {
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

// HookPlan returns a plan for deploying one hook bundle with this harness. When the
// descriptor declares hooks.supported: false, the returned plan carries Supported: false
// and a non-empty Reason. Full hook variant file resolution for the descriptor-only tier
// is out of Stage 6's scope.
func (m *runtimeModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	if !m.desc.Hooks.Supported {
		return domain.HookPlan{
			Supported: false,
			Reason:    "harness descriptor declares hooks not supported",
		}, nil
	}
	// Descriptor-only harnesses do not ship hook variant files. Hook deployment for
	// runtime-provisioned harnesses is out of scope for this stage.
	return domain.HookPlan{
		Supported: false,
		Reason:    "hook variant files are not available for runtime-provisioned harnesses",
	}, nil
}
