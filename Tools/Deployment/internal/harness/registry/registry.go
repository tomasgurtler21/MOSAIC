package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ErrHarnessNotFound is returned by Resolve when no harness with the given id has been
// registered or discovered in any tier.
var ErrHarnessNotFound = errors.New("harness not found")

// ErrExternalNotPermitted is returned by Resolve when a harness is provided by an external
// module and AllowExternal is false in the Options.
var ErrExternalNotPermitted = errors.New("external harness modules require explicit opt-in")

// Package-level built-in registry. Register is called from init() functions in built-in
// harness packages; the factories are snapshotted by Discover at call time.
var (
	builtinMu        sync.RWMutex
	builtinFactories = make(map[string]BuiltinFactory)
)

// Options configures the Discover call.
type Options struct {
	// MosaicRoot is the root of the MOSAIC repository. Runtime harness folders are discovered
	// under <MosaicRoot>/MosaicDeploy/harnesses/.
	MosaicRoot string

	// AllowExternal enables the external-module tier. When false, external modules appear in
	// List() with Usable == false and RequiresOptIn == true, and are never returned by Resolve.
	AllowExternal bool

	// GOOS is the target operating system string used for path resolution. If empty the host
	// platform is used. Supplied explicitly so resolution stays testable on any host platform.
	GOOS string

	// ToolMappings, when non-nil, is invoked once per successfully constructed module during
	// Discover. It receives the harness id and the descriptor's own declared tool mappings, and
	// returns the effective mapping set to use for that module. Returning nil means "no config
	// override for this harness; use the descriptor's own declared mappings unchanged."
	//
	// A non-nil return wraps the module in a thin shim whose Descriptor() reflects the
	// effective mappings and whose Tools() temporarily installs the effective mappings on the
	// inner descriptor for the duration of each call — preserving any module-specific
	// post-processing (such as Claude Code's scalar conversion) — then restores.
	//
	// The hook is a function rather than a data map so that registry stays independent of the
	// config package; the composition point supplies a closure over the loaded project-level
	// and user-local configuration.
	ToolMappings func(harnessID string, declared []domain.ToolMapping) []domain.ToolMapping
}

// Registry is the resolved set of harnesses available in a run. It is produced by Discover
// and consumed by the app layer.
type Registry interface {
	// List returns every discovered harness, including gated external ones with Usable == false
	// and a non-empty UnusableReason. This is exactly the data the harness selection screen
	// renders (AC6.4, AC6.6).
	List() []domain.HarnessRef

	// Resolve constructs and returns the HarnessModule for id. The external opt-in gate is
	// enforced here so no consumer can obtain an external module without passing the gate
	// (AC6.3). Returns ErrHarnessNotFound if id is unknown in any tier. Returns
	// ErrExternalNotPermitted if the harness is in the external tier and AllowExternal is false.
	Resolve(id string) (domain.HarnessModule, error)
}

// Register adds a built-in harness factory to the package-level registry. It must be called
// from the harness module's own init() function. Adding a built-in harness costs exactly one
// call to Register.
func Register(id string, factory BuiltinFactory) {
	builtinMu.Lock()
	defer builtinMu.Unlock()
	builtinFactories[id] = factory
}

// toolMappingsModule wraps a HarnessModule with effective tool mappings computed by the
// registry ToolMappings hook. It is created by applyToolMappingsHook when the hook returns
// a non-nil effective mapping set.
//
// Descriptor() returns a shallow copy of the inner descriptor with effective mappings so
// callers that inspect Descriptor().Tools.Mappings see the hook's result without
// permanently mutating the inner's descriptor. Tools() borrows the inner descriptor —
// temporarily installing the effective mappings, delegating to the inner's own Tools()
// implementation (preserving any module-specific post-processing such as Claude Code's
// scalar conversion), then restoring the original declared mappings. This borrow approach
// is correct because the deploy flow is sequential within a single run; no two goroutines
// share a registry module instance.
type toolMappingsModule struct {
	inner     domain.HarnessModule
	desc      *domain.HarnessDescriptor // shallow copy with effective mappings, for Descriptor()
	effective []domain.ToolMapping      // mapping set the hook returned
	declared  []domain.ToolMapping      // inner's original declared mappings, saved for restore
}

func (m *toolMappingsModule) Ref() domain.HarnessRef { return m.inner.Ref() }
func (m *toolMappingsModule) Descriptor() *domain.HarnessDescriptor { return m.desc }
func (m *toolMappingsModule) Close() error { return m.inner.Close() }

// Tools temporarily installs the effective mappings on the inner descriptor, delegates
// to the inner's Tools() (so its own post-processing — e.g. Claude Code's scalar
// conversion — runs with the effective mappings), then restores the declared mappings.
func (m *toolMappingsModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	innerDesc := m.inner.Descriptor()
	innerDesc.Tools.Mappings = m.effective
	defer func() { innerDesc.Tools.Mappings = m.declared }()
	return m.inner.Tools(req)
}

func (m *toolMappingsModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return m.inner.Frontmatter(req)
}
func (m *toolMappingsModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return m.inner.TargetPath(req)
}
func (m *toolMappingsModule) Injection(req domain.InjectionRequest) (string, bool) {
	return m.inner.Injection(req)
}
func (m *toolMappingsModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	return m.inner.HookPlan(req)
}

// applyToolMappingsHook calls the ToolMappings hook (if set) for the given harness module
// and returns a module whose Descriptor() and Tools() reflect the effective mappings.
//
// When the hook is nil or returns nil, the module is returned unchanged and its descriptor
// is not mutated — nil return means "no config override for this harness; use the
// descriptor's own declared mappings."
//
// When the hook returns a non-nil effective mapping set, applyToolMappingsHook wraps the
// module in a toolMappingsModule that presents the effective mappings without permanently
// mutating the inner's descriptor. This is important for correctness: in tests (and
// hypothetically in production) the same module or descriptor instance may be reused across
// Discover calls, and a permanent mutation would bleed from one Discover into the next.
//
// applyToolMappingsHook is only called for built-in and descriptor-only modules. External
// modules manage tool resolution through their subprocess protocol rather than through
// Descriptor().Tools.Mappings, so the hook is not applied to them.
func applyToolMappingsHook(id string, mod domain.HarnessModule, hook func(string, []domain.ToolMapping) []domain.ToolMapping) domain.HarnessModule {
	if hook == nil {
		return mod
	}
	declared := mod.Descriptor().Tools.Mappings
	effective := hook(id, declared)
	if effective == nil {
		// Nil return means "no config override for this harness" — return the module
		// unchanged so its declared mappings remain intact.
		return mod
	}
	// Build a shallow copy of the descriptor with effective mappings installed.
	// domain.HarnessDescriptor and domain.ToolSpec are value types, so the shallow copy
	// produces an independent Mappings slice reference without deep-copying the other fields.
	descCopy := *mod.Descriptor()
	descCopy.Tools.Mappings = effective
	return &toolMappingsModule{
		inner:     mod,
		desc:      &descCopy,
		effective: effective,
		declared:  declared,
	}
}

// harnessEntry holds a discovered harness's public ref and its pre-constructed module.
// mod is nil when the harness is not usable (invalid descriptor, load failure, or ungated external).
type harnessEntry struct {
	ref domain.HarnessRef
	mod domain.HarnessModule // nil when not resolvable
}

// registryImpl is the concrete Registry returned by Discover.
type registryImpl struct {
	allowExternal bool
	entries       map[string]harnessEntry
	refs          []domain.HarnessRef // deterministically ordered for List()
}

func (r *registryImpl) List() []domain.HarnessRef {
	return r.refs
}

func (r *registryImpl) Resolve(id string) (domain.HarnessModule, error) {
	e, ok := r.entries[id]
	if !ok {
		return nil, ErrHarnessNotFound
	}
	// Gate external modules at resolve time, regardless of how the entry was listed.
	if e.ref.Tier == domain.TierExternal && !r.allowExternal {
		return nil, ErrExternalNotPermitted
	}
	if e.mod == nil {
		// Harness exists but is not usable (e.g. invalid descriptor).
		return nil, fmt.Errorf("harness %q is not usable: %s", id, e.ref.UnusableReason)
	}
	return e.mod, nil
}

// Discover aggregates registered built-ins, descriptor-only harnesses found on disk under
// <opts.MosaicRoot>/MosaicDeploy/harnesses/, and external modules found in the same tree.
//
// Precedence when one id is provided by more than one tier, highest first:
//
//	external (opted in) > descriptor-only > built-in
//
// Runtime provision overrides a shipped built-in, enabling harness patching without a rebuild.
// Precedence is documented and deterministic; it never depends on map iteration order (AC6.5).
func Discover(opts Options) (Registry, error) {
	// Snapshot the package-level built-in registry so concurrent Register calls do not
	// interfere with the remainder of this function.
	builtinMu.RLock()
	factories := make(map[string]BuiltinFactory, len(builtinFactories))
	for id, f := range builtinFactories {
		factories[id] = f
	}
	builtinMu.RUnlock()

	// entryWithTier pairs an entry with its tier for conflict-resolution comparisons.
	type entryWithTier struct {
		entry harnessEntry
		tier  domain.ProvisionTier
	}

	candidates := make(map[string]entryWithTier)

	// Step 1 — Built-ins (lowest precedence). Invoke each factory now to obtain the ref
	// needed for List(); built-in factories are cheap, in-memory constructions.
	builtinOpts := BuiltinOptions{MosaicRoot: opts.MosaicRoot}
	for id, factory := range factories {
		mod, err := factory(builtinOpts)
		if err != nil {
			candidates[id] = entryWithTier{
				entry: harnessEntry{
					ref: domain.HarnessRef{
						ID:             id,
						Tier:           domain.TierBuiltin,
						Usable:         false,
						UnusableReason: fmt.Sprintf("failed to initialise built-in: %v", err),
					},
				},
				tier: domain.TierBuiltin,
			}
			continue
		}
		// Apply the ToolMappings hook to each successfully constructed built-in module,
		// wrapping it with a descriptor copy so the original descriptor is not mutated.
		wrapped := applyToolMappingsHook(id, mod, opts.ToolMappings)
		candidates[id] = entryWithTier{
			entry: harnessEntry{ref: wrapped.Ref(), mod: wrapped},
			tier:  domain.TierBuiltin,
		}
	}

	// Step 2 — On-disk harnesses (descriptor-only and external tiers).
	harnessesDir := filepath.Join(opts.MosaicRoot, "MosaicDeploy", "harnesses")
	diskHarnesses, err := discoverDisk(harnessesDir)
	if err != nil {
		return nil, fmt.Errorf("discover on-disk harnesses: %w", err)
	}

	// Step 3 — Merge disk entries; higher-precedence tiers override lower ones.
	for _, h := range diskHarnesses {
		tier := domain.TierDescriptor
		if h.execPath != "" {
			tier = domain.TierExternal
		}

		// The YAML id field is the authoritative identifier; fall back to folder name only
		// when the descriptor could not be loaded.
		id := h.folderName
		if h.desc != nil {
			id = h.desc.ID
		}

		var newEntry harnessEntry
		switch {
		case h.loadErr != "":
			// Descriptor could not be loaded — list with Usable: false so the selection
			// screen can display an explanation rather than silently hiding the harness.
			newEntry = harnessEntry{
				ref: domain.HarnessRef{
					ID:             id,
					Tier:           tier,
					SourcePath:     h.yamlPath,
					Usable:         false,
					UnusableReason: h.loadErr,
				},
			}

		case tier == domain.TierExternal:
			// External module: list always; usability depends on AllowExternal.
			usable := opts.AllowExternal
			unusableReason := ""
			if !usable {
				unusableReason = "external harness module requires explicit opt-in (--allow-external)"
			}
			ref := domain.HarnessRef{
				ID:             id,
				DisplayName:    h.desc.DisplayName,
				Tier:           domain.TierExternal,
				SourcePath:     h.yamlPath,
				ExecutablePath: h.execPath,
				RequiresOptIn:  true,
				Usable:         usable,
				UnusableReason: unusableReason,
			}
			var mod domain.HarnessModule
			if usable {
				// External tier: inject from HarnessInjections.md if present alongside harness.yaml.
				extInjections, _ := loadInjections(h.yamlPath)
				extOrchInjections, extOrchVersion, _ := loadOrchInjections(h.yamlPath)
				h.desc.OrchestratorInjectionsVersion = extOrchVersion
				mod = newRuntimeModule(ref, h.desc, extInjections, extOrchInjections)
				// External modules resolve tools through their subprocess protocol, not through
				// Descriptor().Tools.Mappings. The ToolMappings hook is not applied to them so
				// that their RPC-based Tools() implementation is preserved unchanged.
			}
			newEntry = harnessEntry{ref: ref, mod: mod}

		default:
			// Descriptor-only: always usable when the descriptor is valid.
			// Load injection content from HarnessInjections.md and HarnessInjectionsOrchestrator.md
			// in the same directory. Absence of either file is treated as no injections (empty map).
			injections, injErr := loadInjections(h.yamlPath)
			if injErr != nil {
				injections = make(map[string]string)
			}
			orchInjections, orchVersion, _ := loadOrchInjections(h.yamlPath)
			h.desc.OrchestratorInjectionsVersion = orchVersion
			ref := domain.HarnessRef{
				ID:          id,
				DisplayName: h.desc.DisplayName,
				Tier:        domain.TierDescriptor,
				SourcePath:  h.yamlPath,
				Usable:      true,
			}
			mod := newRuntimeModule(ref, h.desc, injections, orchInjections)
			// Apply the ToolMappings hook and wrap the descriptor-only module with a copy
			// carrying the effective mappings.
			wrapped := applyToolMappingsHook(id, mod, opts.ToolMappings)
			newEntry = harnessEntry{ref: ref, mod: wrapped}
		}

		// Apply precedence: external > descriptor-only > built-in.
		existing, hasExisting := candidates[id]
		override := !hasExisting
		if hasExisting {
			switch tier {
			case domain.TierExternal:
				override = true // external always wins
			case domain.TierDescriptor:
				override = existing.tier == domain.TierBuiltin // descriptor-only beats built-in
			}
		}
		if override {
			candidates[id] = entryWithTier{entry: newEntry, tier: tier}
		}
	}

	// Step 4 — Produce a deterministically ordered ref list and a flat id→entry map.
	ids := make([]string, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	refs := make([]domain.HarnessRef, 0, len(ids))
	flat := make(map[string]harnessEntry, len(ids))
	for _, id := range ids {
		e := candidates[id].entry
		refs = append(refs, e.ref)
		flat[id] = e
	}

	return &registryImpl{
		allowExternal: opts.AllowExternal,
		entries:       flat,
		refs:          refs,
	}, nil
}

// onDiskHarness holds the raw discovery data for one harness folder.
type onDiskHarness struct {
	folderName string
	yamlPath   string // path to harness.yaml
	execPath   string // path to the harness executable; empty for descriptor-only
	desc       *domain.HarnessDescriptor
	loadErr    string // non-empty when descriptor.Load failed
}

// discoverDisk reads every immediate subdirectory of harnessesDir. An absent directory is not
// an error. Folders that contain no harness.yaml are silently skipped.
func discoverDisk(harnessesDir string) ([]onDiskHarness, error) {
	dirEntries, err := os.ReadDir(harnessesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read directory %s: %w", harnessesDir, err)
	}

	var result []onDiskHarness
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		folderName := de.Name()
		dir := filepath.Join(harnessesDir, folderName)
		yamlPath := filepath.Join(dir, "harness.yaml")

		if _, statErr := os.Stat(yamlPath); statErr != nil {
			// No harness.yaml (or unreadable) — silently skip.
			continue
		}

		h := onDiskHarness{
			folderName: folderName,
			yamlPath:   yamlPath,
			execPath:   findExecutable(dir),
		}

		desc, loadErr := descriptor.Load(yamlPath)
		if loadErr != nil {
			h.loadErr = loadErr.Error()
		} else {
			h.desc = desc
		}

		result = append(result, h)
	}
	return result, nil
}

// findExecutable returns the absolute path to the harness executable if one is present in dir,
// or an empty string. Detection is platform-specific:
//   - Windows: harness-exec.exe (compiled Go) or harness-exec.bat (scripted stub)
//   - Other:   harness-exec (Unix executable, mode 0o755)
func findExecutable(dir string) string {
	if runtime.GOOS == "windows" {
		for _, name := range []string{"harness-exec.exe", "harness-exec.bat"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return ""
	}
	p := filepath.Join(dir, "harness-exec")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}
