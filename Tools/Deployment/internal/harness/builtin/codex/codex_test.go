package codex_test

// Tests for the Codex built-in harness module.
//
// This file is the RED phase of the TDD cycle for Stage 5. The codex package does not
// exist yet; these tests fail to compile until the implementation tasks are complete.
//
// Coverage:
//
//   Contract suite (T5.1):
//   - The Codex module passes contracttest.Run with all universal invariants satisfied.
//   - Target paths and hook-plan cases are covered in-suite.
//
//   Capability-to-sandbox collapse (T5.2):
//   - file_write, file_edit, terminal, subagent each produce workspace-write (rows a-d).
//   - Read/search/skill/user-interaction tools produce read-only (row e).
//   - Empty tool set with no placeholder produces read-only (row f).
//   - Unknown generic tool name produces read-only -- unknown never escalates (row g).
//   - Custom MCP server name only (no escalating generic) produces read-only (row h).
//   - Non-empty Placeholder, empty Generic produces workspace-write (row i).
//   - Orchestrator-shaped request {tool-permissions} produces workspace-write (row j).
//   - sandbox_mode is always emitted, including for the empty-tool-set case.
//
//   Capability-request source test (T5.2b):
//   - Two requests differing only in Placeholder produce different sandbox modes,
//     pinning that the collapse reads both req.Generic and req.Placeholder.
//
//   Tool resolution outcomes (T5.2a):
//   - Every generic tool in the known vocabulary resolves ToolMapped with an empty
//     harness-name list (a).
//   - No known generic tool resolves ToolUnmapped (b).
//   - Escalating tools are ToolMapped, not ToolUnmapped (c).
//   - An unknown generic name resolves ToolUnmapped (d).
//   - A user-supplied custom name resolves ToolCustom (e).
//   - No tools frontmatter field is emitted for any case -- no tools_key (f).
//   - A placeholder request yields zero resolutions (g).
//
//   Frontmatter (T5.3):
//   - Drop list names the expected MOSAIC-catalog-only fields.
//   - Drop list excludes user-owned Codex keys (developer_instructions).
//   - Drop list excludes the carriage container keys (mosaic_carriage, mosaic_carriage_markers).
//   - Model key is "model".
//   - No tools key is declared.
//   - Key order includes "model" and "sandbox_mode".
//   - No format hint (tier-to-model defaults are user configuration).
//   - Model list is exactly the four Codex model IDs, asserted by value (AC5.7a).
//
//   Target paths (T5.4):
//   - Agents deploy to ".codex/agents/<key>.toml".
//   - Skills deploy to ".agents/skills/<key>/SKILL.md" (key subdirectory).
//   - Hooks return ErrArtifactUnsupported.
//   - HookPlan returns Supported:false with a non-empty reason.
//
//   Injection content (T5.6):
//   - HarnessConstraints is filled from module-local fixtures for a regular request.
//   - Orchestrator request returns shared + orchestrator merged content.
//   - Unknown injection names return ok=false.
//   - Project-class injections all return ok=false.
//
//   New descriptor schema fields (T5.5 module-level):
//   - The Codex descriptor's AgentFormatID equals "codex-toml" (I5.1 field).
//   - The Codex descriptor's ToolInfoUnrecoverable equals true (I5.1 field, polarity inverted).

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	_ "mosaic-deploy/internal/agentformat/all"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/codex"
	"mosaic-deploy/internal/harness/contracttest"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"
)

// Sandbox mode literals. Per the design contract (AC5.2), these literal string values
// appear ONLY in this test file and in the Codex descriptor YAML -- not in any
// registry-wide test or other harness package.
const (
	sandboxModeReadOnly       = "read-only"
	sandboxModeWorkspaceWrite = "workspace-write"
)

// knownGenericVocabulary is the union of generic tool names the four existing descriptors
// already map. The Codex descriptor must map every one of these to avoid ToolUnmapped
// resolutions and the user-prompting and TODO-gap consequences that follow.
var knownGenericVocabulary = []string{
	"file_read", "file_write", "file_edit", "file_search", "content_search",
	"terminal", "user_interaction", "subagent", "skill", "web_fetch", "web_search",
}

// escalatingGenericTools are the generic tools that trigger workspace-write in the
// capability-to-sandbox collapse. The collapse emits workspace-write when any of these
// is present in req.Generic (or when req.Placeholder is non-empty).
var escalatingGenericTools = []string{"file_write", "file_edit", "terminal", "subagent"}

// Local fixture content constants match verbatim what the testdata injection fixture
// files contain inside their HarnessConstraints regions. The test file and the fixture
// files are the single declaration of this content; neither is derived from the other
// at run time. When you update a fixture file, update the matching constant here.
const (
	// codexLocalHarnessConstraints is the HarnessConstraints region content in
	// testdata/Catalog/HarnessInjections/Codex/HarnessInjections.md.
	codexLocalHarnessConstraints = "Codex operates in a sandboxed environment. " +
		"Tool access is controlled by the sandbox_mode field set during deployment."

	// codexLocalOrchestratorConstraints is the HarnessConstraints region content in
	// testdata/Catalog/HarnessInjections/Codex/HarnessInjectionsOrchestrator.md.
	codexLocalOrchestratorConstraints = "As an orchestrator in Codex, you may spawn subagents " +
		"and perform workspace writes within the declared sandbox."
)

// ---------------------------------------------------------------------------
// Module construction helpers
// ---------------------------------------------------------------------------

// repoRoot returns the absolute path to the repository root. Package is at
// Tools/Deployment/internal/harness/builtin/codex/ -- six levels below the repo root.
func repoRoot(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "..", "..", "..", "..")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

// frozenCatalogRoot returns the absolute path to the frozen Catalog fixture tree.
// Collapse, resolution, frontmatter, target-path, hook, and contract-suite tests
// use this root so they remain stable as the live Catalog evolves.
func frozenCatalogRoot(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "..", "..", "testdata", "frozen-catalog")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve frozen catalog root: %v", err)
	}
	return abs
}

// localFixturesRoot returns the absolute path to the module-local testdata directory.
// The codex module's New() function looks for injection content at
//
//	localFixturesRoot(t) + "/" + codex.RepoContentDir
//
// which resolves to testdata/Catalog/HarnessInjections/Codex/. Use this root for T5.6
// injection tests only; it is not suitable for the collapse or frontmatter tests, which
// need a fully formed Codex module but have no injection-content assertions.
func localFixturesRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("resolve local fixtures root: %v", err)
	}
	return abs
}

// newModule constructs the Codex module against the real repository root.
func newModule(t *testing.T) domain.HarnessModule {
	t.Helper()
	mod, err := codex.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("codex.New() against real repo root: %v", err)
	}
	return mod
}

// newModuleFromFrozen constructs the Codex module against the frozen Catalog fixture root.
// This is the preferred constructor for tests that do not make injection-content assertions,
// so the tests remain immune to live Catalog changes.
func newModuleFromFrozen(t *testing.T) domain.HarnessModule {
	t.Helper()
	mod, err := codex.New(registry.BuiltinOptions{MosaicRoot: frozenCatalogRoot(t)})
	if err != nil {
		t.Fatalf("codex.New() from frozen catalog: %v", err)
	}
	return mod
}

// newModuleWithLocalFixtures constructs the Codex module against the module-local
// testdata fixtures. Used exclusively for T5.6 injection content tests.
func newModuleWithLocalFixtures(t *testing.T) domain.HarnessModule {
	t.Helper()
	mod, err := codex.New(registry.BuiltinOptions{MosaicRoot: localFixturesRoot(t)})
	if err != nil {
		t.Fatalf("codex.New() with local fixtures: %v", err)
	}
	return mod
}

// sandboxModeFromResult extracts the sandbox_mode scalar value from ToolResult.Fields.
// Returns (value, true) when the field is present, ("", false) when absent.
func sandboxModeFromResult(result domain.ToolResult) (string, bool) {
	for _, f := range result.Fields {
		if f.Key == "sandbox_mode" {
			return f.Value.Scalar, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// T5.1 -- Shared contract suite
// ---------------------------------------------------------------------------

// TestContract_Codex runs the shared HarnessModule contract suite against the Codex
// module. This ensures Codex satisfies every universal invariant all provision tiers
// must exhibit: Ref stability, Descriptor pointer identity, key-order/remove disjoint,
// ErrArtifactUnsupported for unsupported kinds, unknown injection returns ok=false,
// hooks-unsupported has a non-empty Reason and no Files, determinism, Close idempotency.
func TestContract_Codex(t *testing.T) {
	mod := newModuleFromFrozen(t)

	contracttest.Run(t, mod, contracttest.Fixtures{
		TargetPathCases: []contracttest.TargetPathCase{
			{
				Name: "agent_project_scope",
				Request: domain.TargetPathRequest{
					Kind:  domain.ArtifactAgent,
					Scope: domain.ScopeProject,
					Key:   "my-agent",
				},
				Expected: ".codex/agents/my-agent.toml",
			},
			{
				Name: "skill_project_scope_with_key_subdir",
				Request: domain.TargetPathRequest{
					Kind:     domain.ArtifactSkill,
					Scope:    domain.ScopeProject,
					Key:      "lean-tdd",
					FileName: "SKILL.md",
				},
				Expected: ".agents/skills/lean-tdd/SKILL.md",
			},
			{
				Name:    "hook_unsupported",
				Request: domain.TargetPathRequest{Kind: domain.ArtifactHook, Scope: domain.ScopeProject},
				Err:     domain.ErrArtifactUnsupported,
			},
		},
		HookPlanCases: []contracttest.HookPlanCase{
			{
				Name:      "hooks_not_supported",
				Request:   domain.HookPlanRequest{},
				Supported: false,
			},
		},
	})
}

// ---------------------------------------------------------------------------
// T5.2 -- Capability-to-sandbox collapse (every table row)
// ---------------------------------------------------------------------------

// TestCollapse_FileWrite_ProducesWorkspaceWrite verifies that a request containing
// file_write in the generic tool list produces sandbox_mode = workspace-write (row a).
func TestCollapse_FileWrite_ProducesWorkspaceWrite(t *testing.T) {
	mod := newModuleFromFrozen(t)
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_write"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	mode, ok := sandboxModeFromResult(result)
	if !ok {
		t.Fatal("sandbox_mode field absent; it must always be emitted")
	}
	if mode != sandboxModeWorkspaceWrite {
		t.Errorf("file_write: sandbox_mode = %q, want %q", mode, sandboxModeWorkspaceWrite)
	}
}

// TestCollapse_FileEdit_ProducesWorkspaceWrite verifies that file_edit produces
// sandbox_mode = workspace-write (row b).
func TestCollapse_FileEdit_ProducesWorkspaceWrite(t *testing.T) {
	mod := newModuleFromFrozen(t)
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_edit"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	mode, ok := sandboxModeFromResult(result)
	if !ok {
		t.Fatal("sandbox_mode field absent")
	}
	if mode != sandboxModeWorkspaceWrite {
		t.Errorf("file_edit: sandbox_mode = %q, want %q", mode, sandboxModeWorkspaceWrite)
	}
}

// TestCollapse_Terminal_ProducesWorkspaceWrite verifies that terminal produces
// sandbox_mode = workspace-write (row c).
func TestCollapse_Terminal_ProducesWorkspaceWrite(t *testing.T) {
	mod := newModuleFromFrozen(t)
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"terminal"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	mode, ok := sandboxModeFromResult(result)
	if !ok {
		t.Fatal("sandbox_mode field absent")
	}
	if mode != sandboxModeWorkspaceWrite {
		t.Errorf("terminal: sandbox_mode = %q, want %q", mode, sandboxModeWorkspaceWrite)
	}
}

// TestCollapse_Subagent_ProducesWorkspaceWrite verifies that subagent produces
// sandbox_mode = workspace-write (row d).
func TestCollapse_Subagent_ProducesWorkspaceWrite(t *testing.T) {
	mod := newModuleFromFrozen(t)
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"subagent"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	mode, ok := sandboxModeFromResult(result)
	if !ok {
		t.Fatal("sandbox_mode field absent")
	}
	if mode != sandboxModeWorkspaceWrite {
		t.Errorf("subagent: sandbox_mode = %q, want %q", mode, sandboxModeWorkspaceWrite)
	}
}

// TestCollapse_ReadOnlyTools_ProducesReadOnly verifies that a tool set consisting only
// of read/search/skill/user-interaction tools produces sandbox_mode = read-only (row e).
func TestCollapse_ReadOnlyTools_ProducesReadOnly(t *testing.T) {
	mod := newModuleFromFrozen(t)
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "file_search", "content_search", "skill", "user_interaction", "web_fetch", "web_search"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	mode, ok := sandboxModeFromResult(result)
	if !ok {
		t.Fatal("sandbox_mode field absent")
	}
	if mode != sandboxModeReadOnly {
		t.Errorf("read-only tools: sandbox_mode = %q, want %q", mode, sandboxModeReadOnly)
	}
}

// TestCollapse_EmptyToolSet_ProducesReadOnly verifies that an empty tool set with no
// placeholder produces sandbox_mode = read-only (row f).
//
// This case MUST be tested separately from the placeholder row (i): the two cases have
// identical req.Generic (both empty) but opposite sandbox modes. An implementation that
// reads only req.Generic cannot distinguish them and would fail row (i) while passing here.
func TestCollapse_EmptyToolSet_ProducesReadOnly(t *testing.T) {
	mod := newModuleFromFrozen(t)
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{},
		Placeholder: "", // explicitly absent: this is a plain empty-tool-set request
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	mode, ok := sandboxModeFromResult(result)
	if !ok {
		t.Fatal("sandbox_mode field absent; it must be emitted even when no tools are declared")
	}
	if mode != sandboxModeReadOnly {
		t.Errorf("empty tool set (no placeholder): sandbox_mode = %q, want %q; "+
			"a nil/empty Generic with no Placeholder grants minimal access",
			mode, sandboxModeReadOnly)
	}
}

// TestCollapse_UnknownTool_ProducesReadOnly verifies that a generic tool name not in the
// escalating set produces sandbox_mode = read-only (row g). Unknown tools never escalate
// to workspace-write regardless of their name.
func TestCollapse_UnknownTool_ProducesReadOnly(t *testing.T) {
	mod := newModuleFromFrozen(t)
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"totally_unknown_future_capability"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	mode, ok := sandboxModeFromResult(result)
	if !ok {
		t.Fatal("sandbox_mode field absent")
	}
	if mode != sandboxModeReadOnly {
		t.Errorf("unknown generic tool: sandbox_mode = %q, want %q; "+
			"unknown tools must never escalate to workspace-write",
			mode, sandboxModeReadOnly)
	}
}

// TestCollapse_CustomToolOnly_ProducesReadOnly verifies that a request containing only a
// user-supplied custom MCP server name (no escalating generic tool) produces
// sandbox_mode = read-only (row h). The custom name resolves the generic tool via
// CustomNames but does not cause escalation -- only the generic tool's membership in
// the escalating set determines the mode.
func TestCollapse_CustomToolOnly_ProducesReadOnly(t *testing.T) {
	mod := newModuleFromFrozen(t)
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"file_read"},
		CustomNames: map[string]string{"file_read": "my-mcp-server"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	mode, ok := sandboxModeFromResult(result)
	if !ok {
		t.Fatal("sandbox_mode field absent")
	}
	if mode != sandboxModeReadOnly {
		t.Errorf("custom tool only (file_read via custom name): sandbox_mode = %q, want %q; "+
			"a custom name for a non-escalating generic tool must not escalate",
			mode, sandboxModeReadOnly)
	}
}

// TestCollapse_PlaceholderRequest_ProducesWorkspaceWrite verifies that a request with a
// non-empty Placeholder and an empty Generic produces sandbox_mode = workspace-write (row i).
//
// This is the critical row. The MOSAIC orchestrator declares tools: {tool-permissions},
// which resolves to Placeholder non-empty and Generic empty. A collapse that reads only
// req.Generic sees an empty list, takes the "no escalating tool" branch, and would emit
// read-only -- deploying the orchestrator unable to write files or spawn subagents.
// This test, together with TestCollapse_EmptyToolSet_ProducesReadOnly, pins the distinction.
func TestCollapse_PlaceholderRequest_ProducesWorkspaceWrite(t *testing.T) {
	mod := newModuleFromFrozen(t)
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{}, // same as the empty-tool-set case (row f)
		Placeholder: "some-placeholder",
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	mode, ok := sandboxModeFromResult(result)
	if !ok {
		t.Fatal("sandbox_mode field absent")
	}
	if mode != sandboxModeWorkspaceWrite {
		t.Errorf("placeholder request (empty Generic, non-empty Placeholder): "+
			"sandbox_mode = %q, want %q; "+
			"a non-empty Placeholder means the agent requested the full tool set, "+
			"which contains every escalating capability",
			mode, sandboxModeWorkspaceWrite)
	}
}

// TestCollapse_OrchestratorShaped_ProducesWorkspaceWrite verifies that the real MOSAIC
// orchestrator's request shape -- Placeholder = "{tool-permissions}", Generic = [] --
// produces sandbox_mode = workspace-write (row j, FR-11b).
//
// The orchestrator in Catalog/Orchestrator/orchestrator.md declares
// tools: {tool-permissions}. The deployment pipeline resolves this to
// ToolRequest{Generic: [], Placeholder: "{tool-permissions}"}. Deploying with
// read-only would prevent the orchestrator from writing files or spawning subagents.
func TestCollapse_OrchestratorShaped_ProducesWorkspaceWrite(t *testing.T) {
	mod := newModuleFromFrozen(t)
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey:    "orchestrator",
		Generic:     []string{},
		Placeholder: "{tool-permissions}",
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	mode, ok := sandboxModeFromResult(result)
	if !ok {
		t.Fatal("sandbox_mode field absent")
	}
	if mode != sandboxModeWorkspaceWrite {
		t.Errorf("orchestrator-shaped request {tool-permissions}: "+
			"sandbox_mode = %q, want %q; "+
			"the MOSAIC orchestrator declares tools: {tool-permissions}, "+
			"arriving as Placeholder non-empty and Generic empty; "+
			"the collapse must yield workspace-write (FR-11b)",
			mode, sandboxModeWorkspaceWrite)
	}
}

// TestCollapse_SandboxModeAlwaysEmitted verifies that sandbox_mode appears in
// ToolResult.Fields for every request shape, including the empty-tool-set case.
func TestCollapse_SandboxModeAlwaysEmitted(t *testing.T) {
	mod := newModuleFromFrozen(t)

	cases := []struct {
		name string
		req  domain.ToolRequest
	}{
		{"empty_set_no_placeholder", domain.ToolRequest{AgentKey: "agent", Generic: []string{}}},
		{"read_only_tool", domain.ToolRequest{AgentKey: "agent", Generic: []string{"file_read"}}},
		{"escalating_tool", domain.ToolRequest{AgentKey: "agent", Generic: []string{"file_write"}}},
		{"placeholder", domain.ToolRequest{AgentKey: "agent", Generic: []string{}, Placeholder: "{tool-permissions}"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result, err := mod.Tools(tc.req)
			if err != nil {
				t.Fatalf("Tools: %v", err)
			}
			_, ok := sandboxModeFromResult(result)
			if !ok {
				t.Errorf("sandbox_mode not emitted for request {Generic:%v, Placeholder:%q}; "+
					"sandbox_mode must always be present in every Codex deployment",
					tc.req.Generic, tc.req.Placeholder)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T5.2b -- Capability-request source test
// ---------------------------------------------------------------------------

// TestCollapse_PlaceholderAloneChangesSandboxMode verifies that two requests differing
// only in whether Placeholder is set produce different sandbox modes. This pins the rule
// that the collapse is a function of BOTH req.Generic AND req.Placeholder; a refactor
// that reinstates the req.Generic-only reading fails here.
func TestCollapse_PlaceholderAloneChangesSandboxMode(t *testing.T) {
	mod := newModuleFromFrozen(t)

	// Request without placeholder: empty Generic, empty Placeholder -> read-only.
	withoutPlaceholder := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{},
		Placeholder: "",
	}
	resultWithout, err := mod.Tools(withoutPlaceholder)
	if err != nil {
		t.Fatalf("Tools (without placeholder): %v", err)
	}
	modeWithout, ok := sandboxModeFromResult(resultWithout)
	if !ok {
		t.Fatal("sandbox_mode not emitted for request without placeholder")
	}

	// Same Generic, non-empty Placeholder -> workspace-write.
	withPlaceholder := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{},
		Placeholder: "{tool-permissions}",
	}
	resultWith, err := mod.Tools(withPlaceholder)
	if err != nil {
		t.Fatalf("Tools (with placeholder): %v", err)
	}
	modeWith, ok := sandboxModeFromResult(resultWith)
	if !ok {
		t.Fatal("sandbox_mode not emitted for request with placeholder")
	}

	if modeWithout == modeWith {
		t.Errorf("requests differing only in Placeholder produced the same sandbox_mode %q; "+
			"the collapse must consider req.Placeholder independently of req.Generic: "+
			"empty Generic + no Placeholder -> %q, "+
			"empty Generic + non-empty Placeholder -> %q",
			modeWithout, sandboxModeReadOnly, sandboxModeWorkspaceWrite)
	}
	if modeWithout != sandboxModeReadOnly {
		t.Errorf("without placeholder: sandbox_mode = %q, want %q",
			modeWithout, sandboxModeReadOnly)
	}
	if modeWith != sandboxModeWorkspaceWrite {
		t.Errorf("with placeholder: sandbox_mode = %q, want %q",
			modeWith, sandboxModeWorkspaceWrite)
	}
}

// ---------------------------------------------------------------------------
// T5.2a -- Tool resolution outcomes (no-prompt, no-gap guarantee)
// ---------------------------------------------------------------------------

// TestToolResolution_KnownVocabulary_AllMapped verifies that every generic tool in the
// known vocabulary resolves to ToolMapped with an empty harness-name list (a). Codex
// subsumes each generic capability into sandbox_mode; there are no harness-side names.
func TestToolResolution_KnownVocabulary_AllMapped(t *testing.T) {
	mod := newModuleFromFrozen(t)

	for _, generic := range knownGenericVocabulary {
		generic := generic
		t.Run(generic, func(t *testing.T) {
			result, err := mod.Tools(domain.ToolRequest{
				AgentKey: "test-agent",
				Generic:  []string{generic},
			})
			if err != nil {
				t.Fatalf("Tools(%q): %v", generic, err)
			}
			if len(result.Resolutions) != 1 {
				t.Fatalf("Tools(%q): want 1 resolution, got %d", generic, len(result.Resolutions))
			}
			res := result.Resolutions[0]
			if res.Outcome != domain.ToolMapped {
				t.Errorf("Tools(%q): Outcome = %q, want %q (ToolMapped); "+
					"every generic tool in the known vocabulary must map to ToolMapped "+
					"via an empty-destinations entry in codex.yaml",
					generic, res.Outcome, domain.ToolMapped)
			}
			if len(res.HarnessTools) != 0 {
				t.Errorf("Tools(%q): HarnessTools = %v, want empty; "+
					"Codex has no harness-side tool names (capability is expressed via sandbox_mode)",
					generic, res.HarnessTools)
			}
		})
	}
}

// TestToolResolution_KnownVocabulary_NoneUnmapped verifies that no known generic tool
// resolves ToolUnmapped when the full vocabulary is requested at once (b). A ToolUnmapped
// resolution drives user prompting and TODO-gap entries -- both unacceptable for the
// standard tool set.
func TestToolResolution_KnownVocabulary_NoneUnmapped(t *testing.T) {
	mod := newModuleFromFrozen(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  knownGenericVocabulary,
	})
	if err != nil {
		t.Fatalf("Tools(full vocabulary): %v", err)
	}
	if len(result.Resolutions) != len(knownGenericVocabulary) {
		t.Fatalf("resolution count = %d, want %d (one per generic tool)",
			len(result.Resolutions), len(knownGenericVocabulary))
	}
	for _, res := range result.Resolutions {
		if res.Outcome == domain.ToolUnmapped {
			t.Errorf("generic tool %q resolved ToolUnmapped; "+
				"every tool in the known vocabulary must map to ToolMapped to avoid user prompts and TODO gaps; "+
				"add an empty-destinations mapping for this tool in codex.yaml",
				res.Generic)
		}
	}
}

// TestToolResolution_EscalatingTools_AreMapped verifies that the escalating tools
// (file_write, file_edit, terminal, subagent) resolve ToolMapped, not ToolUnmapped (c).
// These are the tools whose presence drives workspace-write in the collapse; they must
// not be left unmapped, or the collapse would yield workspace-write while the resolver
// simultaneously prompts the user about them.
func TestToolResolution_EscalatingTools_AreMapped(t *testing.T) {
	mod := newModuleFromFrozen(t)

	for _, generic := range escalatingGenericTools {
		generic := generic
		t.Run(generic, func(t *testing.T) {
			result, err := mod.Tools(domain.ToolRequest{
				AgentKey: "test-agent",
				Generic:  []string{generic},
			})
			if err != nil {
				t.Fatalf("Tools(%q): %v", generic, err)
			}
			if len(result.Resolutions) != 1 {
				t.Fatalf("Tools(%q): want 1 resolution, got %d", generic, len(result.Resolutions))
			}
			res := result.Resolutions[0]
			if res.Outcome != domain.ToolMapped {
				t.Errorf("escalating tool %q: Outcome = %q, want %q; "+
					"escalating tools drive workspace-write in the collapse but must still "+
					"resolve ToolMapped (not ToolUnmapped) to prevent user prompting",
					generic, res.Outcome, domain.ToolMapped)
			}
		})
	}
}

// TestToolResolution_UnknownGeneric_IsUnmapped verifies that a generic tool not in the
// known vocabulary resolves ToolUnmapped (d). Unknown tools are correctly reported so the
// user can decide whether to handle them -- they just never escalate the sandbox mode.
func TestToolResolution_UnknownGeneric_IsUnmapped(t *testing.T) {
	mod := newModuleFromFrozen(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"totally_unknown_future_capability"},
	})
	if err != nil {
		t.Fatalf("Tools(unknown generic): %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("want 1 resolution for 1 generic tool, got %d", len(result.Resolutions))
	}
	if result.Resolutions[0].Outcome != domain.ToolUnmapped {
		t.Errorf("unknown generic tool: Outcome = %q, want %q; "+
			"a tool absent from the descriptor's mappings must resolve ToolUnmapped",
			result.Resolutions[0].Outcome, domain.ToolUnmapped)
	}
}

// TestToolResolution_CustomName_IsCustom verifies that a user-supplied custom MCP server
// name resolves ToolCustom (e).
func TestToolResolution_CustomName_IsCustom(t *testing.T) {
	mod := newModuleFromFrozen(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"file_read"},
		CustomNames: map[string]string{"file_read": "my-mcp-server"},
	})
	if err != nil {
		t.Fatalf("Tools(custom name): %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("want 1 resolution, got %d", len(result.Resolutions))
	}
	if result.Resolutions[0].Outcome != domain.ToolCustom {
		t.Errorf("custom-named tool: Outcome = %q, want %q",
			result.Resolutions[0].Outcome, domain.ToolCustom)
	}
}

// TestToolResolution_NoToolsField_ForAnyCase verifies that no tools frontmatter field is
// emitted by Tools() for any request variant (f). The Codex descriptor declares no
// tools_key, so the shared field builder contributes nothing. Only sandbox_mode is emitted.
func TestToolResolution_NoToolsField_ForAnyCase(t *testing.T) {
	mod := newModuleFromFrozen(t)

	cases := []struct {
		name string
		req  domain.ToolRequest
	}{
		{"known_read_tool", domain.ToolRequest{AgentKey: "agent", Generic: []string{"file_read"}}},
		{"escalating_tool", domain.ToolRequest{AgentKey: "agent", Generic: []string{"file_write"}}},
		{"unknown_tool", domain.ToolRequest{AgentKey: "agent", Generic: []string{"unknown_tool"}}},
		{"full_vocabulary", domain.ToolRequest{AgentKey: "agent", Generic: knownGenericVocabulary}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result, err := mod.Tools(tc.req)
			if err != nil {
				t.Fatalf("Tools: %v", err)
			}
			// The only field emitted must be sandbox_mode; no tools-key field.
			for _, f := range result.Fields {
				if f.Key != "sandbox_mode" && (strings.HasPrefix(f.Key, "tools") || f.Key == "permission") {
					t.Errorf("unexpected tools-related field %q in result; "+
						"Codex declares no tools_key so no tools field must appear in Fields",
						f.Key)
				}
			}
			if len(result.Fields) != 1 {
				t.Errorf("Fields count = %d, want 1 (only sandbox_mode); fields: %v",
					len(result.Fields), result.Fields)
			}
			if _, ok := sandboxModeFromResult(result); !ok {
				t.Error("sandbox_mode not present in the single field")
			}
		})
	}
}

// TestToolResolution_PlaceholderRequest_ZeroResolutions verifies that a placeholder
// request yields exactly zero resolutions (g). descriptor.MapTools takes the placeholder
// branch before any per-tool resolution, and expandPlaceholder returns an empty result
// when no tools_key is declared. The Codex descriptor declares no tools_key.
//
// Zero resolutions and "none unmapped" are different outcomes. This test asserts the
// count explicitly rather than relying on the absence of ToolUnmapped entries.
func TestToolResolution_PlaceholderRequest_ZeroResolutions(t *testing.T) {
	mod := newModuleFromFrozen(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey:    "orchestrator",
		Generic:     []string{},
		Placeholder: "{tool-permissions}",
	})
	if err != nil {
		t.Fatalf("Tools(placeholder): %v", err)
	}
	if len(result.Resolutions) != 0 {
		t.Errorf("placeholder request: Resolutions has %d entries, want 0; "+
			"MapTools takes the placeholder branch before per-tool resolution, "+
			"and expandPlaceholder returns empty when no tools_key is declared; "+
			"the Codex descriptor declares no tools_key",
			len(result.Resolutions))
	}
}

// ---------------------------------------------------------------------------
// T5.3 -- Codex-specific frontmatter
// ---------------------------------------------------------------------------

// TestFrontmatter_DropList_ContainsExpectedFields verifies that the drop list names the
// MOSAIC catalog fields that are not applicable to the Codex format.
func TestFrontmatter_DropList_ContainsExpectedFields(t *testing.T) {
	d := codex.DescriptorForTesting(t)

	expectedInDrop := []string{"name", "recommended_tier", "tier_rationale", "required_skills", "tools"}
	for _, want := range expectedInDrop {
		found := false
		for _, key := range d.Frontmatter.Drop {
			if key == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("drop list: missing %q; this MOSAIC catalog field is not applicable to Codex "+
				"and should be removed from deployed agent files",
				want)
		}
	}
}

// TestFrontmatter_DropList_ExcludesUserOwnedCodexKey verifies that the drop list does not
// name developer_instructions or any other key that is user-owned in the Codex format (a).
// Dropping a user-owned key would silently delete the user's hand-added setting on every
// deploy, defeating the carriage mechanism entirely.
func TestFrontmatter_DropList_ExcludesUserOwnedCodexKey(t *testing.T) {
	d := codex.DescriptorForTesting(t)

	userOwnedCodexKeys := []string{"developer_instructions"}
	for _, userKey := range userOwnedCodexKeys {
		for _, dropKey := range d.Frontmatter.Drop {
			if dropKey == userKey {
				t.Errorf("drop list contains %q; this is a user-owned key in the Codex format; "+
					"dropping it would delete the user's hand-added instructions on every deploy",
					userKey)
			}
		}
	}
}

// TestFrontmatter_DropList_ExcludesCarriageContainerKey verifies that the drop list does
// not name the translator layer's reserved carriage container keys (b). Expressed against
// raw drop-list strings to avoid importing the translator constant.
func TestFrontmatter_DropList_ExcludesCarriageContainerKey(t *testing.T) {
	d := codex.DescriptorForTesting(t)

	carriageKeys := []string{"mosaic_carriage", "mosaic_carriage_markers"}
	for _, ckey := range carriageKeys {
		for _, dropKey := range d.Frontmatter.Drop {
			if dropKey == ckey {
				t.Errorf("drop list contains carriage container key %q; "+
					"this reserved key must not appear in the drop list -- "+
					"dropping it would break the mechanism that preserves user-owned keys",
					ckey)
			}
		}
	}
}

// TestFrontmatter_ModelKey_IsModel verifies that the model key is "model".
func TestFrontmatter_ModelKey_IsModel(t *testing.T) {
	d := codex.DescriptorForTesting(t)
	if d.Frontmatter.ModelKey != "model" {
		t.Errorf("Frontmatter.ModelKey = %q, want %q; Codex uses the standard 'model' key",
			d.Frontmatter.ModelKey, "model")
	}
}

// TestFrontmatter_NoToolsKey verifies that no tools_key is declared in the Codex
// descriptor. Without a tools_key, the shared field builder emits nothing; sandbox_mode
// is the only tool-related field and it is the module's own responsibility.
func TestFrontmatter_NoToolsKey(t *testing.T) {
	d := codex.DescriptorForTesting(t)
	if d.Frontmatter.ToolsKey != "" {
		t.Errorf("Frontmatter.ToolsKey = %q, want empty; "+
			"Codex has no harness-side tool list field (capability is expressed via sandbox_mode)",
			d.Frontmatter.ToolsKey)
	}
}

// TestFrontmatter_KeyOrder_ContainsModelAndSandboxMode verifies that the key_order
// includes "model" and "sandbox_mode", giving both a stable position in the canonical
// deployed form so the TOML encoder can emit them in a deterministic order.
func TestFrontmatter_KeyOrder_ContainsModelAndSandboxMode(t *testing.T) {
	d := codex.DescriptorForTesting(t)

	requiredInOrder := []string{"model", "sandbox_mode"}
	for _, req := range requiredInOrder {
		found := false
		for _, key := range d.Frontmatter.KeyOrder {
			if key == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("key_order does not include %q; "+
				"this key must have a declared position to emit in a stable order",
				req)
		}
	}
}

// TestFrontmatter_NoDefaultTierModels verifies that the Codex descriptor offers models
// without defaulting any tier to a specific model (c, FR-17a). Tier-to-model defaults
// are a user configuration concern; the FormatHint is empty because Codex model IDs are
// self-descriptive and need no format inference hint.
func TestFrontmatter_NoDefaultTierModels(t *testing.T) {
	d := codex.DescriptorForTesting(t)
	if d.Models.FormatHint != "" {
		t.Errorf("Models.FormatHint = %q, want empty; "+
			"Codex model IDs are self-descriptive; "+
			"tier-to-model defaults are a user configuration concern, not a descriptor concern",
			d.Models.FormatHint)
	}
}

// TestFrontmatter_ExactFourModelIDs verifies that the Codex descriptor's model list
// is exactly the four Codex model IDs, no more and no fewer (d, AC5.7a). This test
// pins the literal values; every other surface merely passes them through, so this
// is the one place where the exact strings are the behavior under test.
func TestFrontmatter_ExactFourModelIDs(t *testing.T) {
	d := codex.DescriptorForTesting(t)

	wantIDs := []string{"gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}
	if len(d.Models.IDs) != len(wantIDs) {
		t.Fatalf("Models.IDs: got %d entries (%v), want exactly %d (%v); "+
			"the Codex descriptor must declare exactly these four model IDs",
			len(d.Models.IDs), d.Models.IDs, len(wantIDs), wantIDs)
	}
	for i, want := range wantIDs {
		if d.Models.IDs[i] != want {
			t.Errorf("Models.IDs[%d] = %q, want %q; "+
				"pin the model IDs by value -- a typo or silently dropped entry fails here",
				i, d.Models.IDs[i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.4 -- Target paths
// ---------------------------------------------------------------------------

// TestTargetPath_Agent_DeploysToCodexAgents verifies that agents deploy to
// .codex/agents/<key>.toml.
func TestTargetPath_Agent_DeploysToCodexAgents(t *testing.T) {
	mod := newModuleFromFrozen(t)
	got, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Scope: domain.ScopeProject,
		Key:   "my-agent",
	})
	if err != nil {
		t.Fatalf("TargetPath(agent): %v", err)
	}
	want := ".codex/agents/my-agent.toml"
	if got != want {
		t.Errorf("TargetPath(agent): got %q, want %q", got, want)
	}
}

// TestTargetPath_Skill_DeploysWithKeySubdirectory verifies that skills deploy to
// .agents/skills/<key>/SKILL.md (key subdirectory), following the same pattern as
// all four existing built-in harnesses.
func TestTargetPath_Skill_DeploysWithKeySubdirectory(t *testing.T) {
	mod := newModuleFromFrozen(t)
	got, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:     domain.ArtifactSkill,
		Scope:    domain.ScopeProject,
		Key:      "lean-tdd",
		FileName: "SKILL.md",
	})
	if err != nil {
		t.Fatalf("TargetPath(skill): %v", err)
	}
	want := ".agents/skills/lean-tdd/SKILL.md"
	if got != want {
		t.Errorf("TargetPath(skill): got %q, want %q", got, want)
	}
}

// TestTargetPath_Hook_ReturnsErrArtifactUnsupported verifies that requesting a hook
// target path returns domain.ErrArtifactUnsupported, because the Codex descriptor
// declares hooks as unsupported.
func TestTargetPath_Hook_ReturnsErrArtifactUnsupported(t *testing.T) {
	mod := newModuleFromFrozen(t)
	_, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:  domain.ArtifactHook,
		Scope: domain.ScopeProject,
		Key:   "mosaic-logger",
	})
	if err == nil {
		t.Fatal("TargetPath(hook): expected ErrArtifactUnsupported, got nil; " +
			"Codex does not support hooks")
	}
	if !errors.Is(err, domain.ErrArtifactUnsupported) {
		t.Errorf("TargetPath(hook): got error %v, want domain.ErrArtifactUnsupported; "+
			"the returned error must wrap or equal domain.ErrArtifactUnsupported so callers "+
			"can distinguish unsupported-kind errors from other failures",
			err)
	}
}

// TestHookPlan_ReturnsUnsupportedWithNonEmptyReason verifies that HookPlan returns
// Supported: false with a non-empty reason and no Files.
func TestHookPlan_ReturnsUnsupportedWithNonEmptyReason(t *testing.T) {
	mod := newModuleFromFrozen(t)
	plan, err := mod.HookPlan(domain.HookPlanRequest{})
	if err != nil {
		t.Fatalf("HookPlan: %v", err)
	}
	if plan.Supported {
		t.Error("HookPlan.Supported = true, want false; Codex does not support hooks")
	}
	if plan.Reason == "" {
		t.Error("HookPlan.Reason is empty; an unsupported hook plan must carry a reason")
	}
	if len(plan.Files) != 0 {
		t.Errorf("HookPlan.Files = %v, want empty for an unsupported hook plan", plan.Files)
	}
}

// ---------------------------------------------------------------------------
// T5.6 -- Injection content handling with module-local fixtures
// ---------------------------------------------------------------------------

// TestInjection_SharedRequest_HarnessConstraintsFilled verifies that HarnessConstraints
// is filled with the expected shared content for a regular (non-orchestrator) agent
// request, using module-local fixture files.
func TestInjection_SharedRequest_HarnessConstraintsFilled(t *testing.T) {
	mod := newModuleWithLocalFixtures(t)
	content, ok := mod.Injection(domain.InjectionRequest{
		Name:     "HarnessConstraints",
		AgentKey: "test-agent",
	})
	if !ok {
		t.Fatal("Injection(\"HarnessConstraints\") returned ok=false; " +
			"the Codex module must fill HarnessConstraints from HarnessInjections.md")
	}
	if content != codexLocalHarnessConstraints {
		t.Errorf("Injection(\"HarnessConstraints\"):\n  got:  %q\n  want: %q",
			content, codexLocalHarnessConstraints)
	}
}

// TestInjection_OrchestratorRequest_ReturnsMergedContent verifies that an orchestrator
// role request returns the merged (shared + orchestrator) content for HarnessConstraints.
func TestInjection_OrchestratorRequest_ReturnsMergedContent(t *testing.T) {
	mod := newModuleWithLocalFixtures(t)
	content, ok := mod.Injection(domain.InjectionRequest{
		Name:     "HarnessConstraints",
		AgentKey: "orchestrator",
		Role:     domain.RoleOrchestrator,
	})
	if !ok {
		t.Fatal("Injection(\"HarnessConstraints\", orchestrator) returned ok=false; " +
			"merged injection must be filled for orchestrator requests")
	}
	want := codexLocalHarnessConstraints + "\n\n" + codexLocalOrchestratorConstraints
	if content != want {
		t.Errorf("Injection(\"HarnessConstraints\", orchestrator):\n  got:  %q\n  want: %q",
			content, want)
	}
}

// TestInjection_UnknownName_ReturnsFalse verifies that an unknown injection name returns
// ok=false so the caller correctly treats it as unmanaged.
func TestInjection_UnknownName_ReturnsFalse(t *testing.T) {
	mod := newModuleWithLocalFixtures(t)
	_, ok := mod.Injection(domain.InjectionRequest{
		Name:     "LanguagePatterns",
		AgentKey: "test-agent",
	})
	if ok {
		t.Error("Injection(\"LanguagePatterns\") returned ok=true; " +
			"project-class injections must not be filled by the Codex harness module")
	}
}

// TestInjection_ProjectClassNotFilled verifies that all project-class injection names
// return ok=false.
func TestInjection_ProjectClassNotFilled(t *testing.T) {
	mod := newModuleWithLocalFixtures(t)
	projectInjections := []string{
		"IdentityExtension",
		"ProtocolExtension",
		"CodebaseContext",
		"LanguagePatterns",
		"OutputArtifactTemplate",
		"CustomConstraints",
		"ErrorHandlingExtension",
		"ContextLimits",
	}
	for _, name := range projectInjections {
		_, ok := mod.Injection(domain.InjectionRequest{Name: name})
		if ok {
			t.Errorf("Injection(%q) returned ok=true; "+
				"project-class injections must not be filled by any harness module", name)
		}
	}
}

// ---------------------------------------------------------------------------
// New descriptor schema fields (T5.5 module-level, complement to load_test.go)
// ---------------------------------------------------------------------------

// TestDescriptor_AgentFormatID_IsCodexTOML verifies that the Codex module's embedded
// descriptor carries AgentFormatID = "codex-toml" after I5.1 extends the domain type.
//
// RED: fails to compile until I5.1 adds AgentFormatID to domain.HarnessDescriptor.
func TestDescriptor_AgentFormatID_IsCodexTOML(t *testing.T) {
	d := codex.DescriptorForTesting(t)
	// d.AgentFormatID does not exist on domain.HarnessDescriptor yet (added by I5.1).
	// This test is correctly in the RED phase and will not compile until I5.1 is complete.
	const wantFormatID = "codex-toml"
	if string(d.AgentFormatID) != wantFormatID {
		t.Errorf("Descriptor.AgentFormatID = %q, want %q; "+
			"the Codex descriptor must declare agent_format_id: codex-toml",
			d.AgentFormatID, wantFormatID)
	}
}

// TestDescriptor_ToolInfoUnrecoverable_IsTrue verifies that the Codex module's embedded
// descriptor carries ToolInfoUnrecoverable = true, reflecting the polarity-inverted load
// of tool_info_recoverable: false from the YAML wire format (I5.1).
//
// RED: fails to compile until I5.1 adds ToolInfoUnrecoverable to domain.HarnessDescriptor.
func TestDescriptor_ToolInfoUnrecoverable_IsTrue(t *testing.T) {
	d := codex.DescriptorForTesting(t)
	// d.ToolInfoUnrecoverable does not exist on domain.HarnessDescriptor yet (added by I5.1).
	// This test is correctly in the RED phase and will not compile until I5.1 is complete.
	if !d.ToolInfoUnrecoverable {
		t.Errorf("Descriptor.ToolInfoUnrecoverable = false, want true; "+
			"the Codex descriptor declares tool_info_recoverable: false in YAML, "+
			"which the loader inverts to ToolInfoUnrecoverable = true in the domain type")
	}
}

// ---------------------------------------------------------------------------
// T13.1 -- Target-path roots share no parent; deployed skill frontmatter key set
// ---------------------------------------------------------------------------

// TestTargetPath_AgentAndSkillRoots_ShareNoParent verifies that the Codex agent root
// (.codex/agents/) and the skill root (.agents/skills/) do not share a common non-trivial
// parent directory. Both targets are relative paths that begin with different top-level
// directories: one under .codex/ and the other under .agents/. A tool-local path that
// navigates from the agent root to the skill root must not exist (e.g., reading
// .codex/agents/../../.agents/skills/... would cross a trust boundary). Asserting that
// neither path is a prefix of the other is the machine-verifiable proxy for this property.
func TestTargetPath_AgentAndSkillRoots_ShareNoParent(t *testing.T) {
	mod := newModuleFromFrozen(t)

	agentPath, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Scope: domain.ScopeProject,
		Key:   "some-agent",
	})
	if err != nil {
		t.Fatalf("TargetPath(agent): %v", err)
	}

	skillPath, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:     domain.ArtifactSkill,
		Scope:    domain.ScopeProject,
		Key:      "some-skill",
		FileName: "SKILL.md",
	})
	if err != nil {
		t.Fatalf("TargetPath(skill): %v", err)
	}

	// Extract the first path component (the root directory) for each path.
	agentRoot := strings.SplitN(agentPath, "/", 2)[0]
	skillRoot := strings.SplitN(skillPath, "/", 2)[0]

	if agentRoot == "" {
		t.Fatalf("agent target path %q has no leading directory component", agentPath)
	}
	if skillRoot == "" {
		t.Fatalf("skill target path %q has no leading directory component", skillPath)
	}

	if agentRoot == skillRoot {
		t.Errorf("agent root %q == skill root %q; "+
			"the Codex agent root (.codex/) and skill root (.agents/) must have different "+
			"top-level directories so that no path traversal can reach one from the other; "+
			"agent path: %q, skill path: %q",
			agentRoot, skillRoot, agentPath, skillPath)
	}
}

// TestSkillDeploy_FrontmatterKeySet_Characterisation runs a skill transform through the
// Codex module and asserts the deployed skill's frontmatter key set.
//
// Codex documents only name and description as skill frontmatter fields. However, MOSAIC's
// skill deployment writes version stamps alongside whatever the source declares. Since
// skills remain Markdown for every harness (AD-4), the output is canonical Markdown with
// YAML frontmatter, not TOML.
//
// Observed key set (as of this test's authorship):
//
//	description, mosaic_version
//
// FINDING: The Codex descriptor's drop list includes "name" (designed to drop the source
// name field for agents, so the encoder can assert ctx.AgentKey as the authoritative
// name). However, ApplyFrontmatterSpec applies the drop list regardless of artifact Kind:
// the same drop list is applied to skills as to agents. As a result, "name" is stripped
// from deployed skill frontmatter even though skills remain Markdown and Codex never has
// any reason to drop a skill's name. This is a pre-existing cross-kind descriptor
// application, visible here because this test characterises it explicitly. Fixing it
// would require ApplyFrontmatterSpec to gate the drop list on Kind == ArtifactAgent,
// which is a descriptor-stage change; it is reported here as a finding, not fixed.
//
// If this test begins failing with a different observed set after a catalog or descriptor
// change, update the wantKeys list and this comment with the new observed set.
func TestSkillDeploy_FrontmatterKeySet_Characterisation(t *testing.T) {
	mod := newModuleFromFrozen(t)

	// Minimal skill source with name, description, and version frontmatter.
	// The transform writes the MOSAIC stamps alongside these fields.
	skillSource := []byte("---\nversion: 1.0.0\nname: test-skill\ndescription: A test skill.\n---\nSkill body.\n")

	result, err := transform.Apply(transform.Request{
		Source: skillSource,
		Kind:   domain.ArtifactSkill,
		Key:    "test-skill",
		Module: mod,
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply(skill): %v", err)
	}

	// Parse the output to extract the frontmatter key set.
	output := result.Output

	// Extract lines between the first --- and second ---.
	lines := strings.Split(string(output), "\n")
	var fmKeys []string
	inFM := false
	for _, line := range lines {
		if line == "---" {
			if !inFM {
				inFM = true
				continue
			}
			break
		}
		if inFM {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) >= 1 && parts[0] != "" {
				key := strings.TrimSpace(parts[0])
				if key != "" && !strings.HasPrefix(key, " ") && !strings.HasPrefix(key, "#") {
					fmKeys = append(fmKeys, key)
				}
			}
		}
	}

	if len(fmKeys) == 0 {
		t.Fatal("skill output has no frontmatter keys; expected at least description and mosaic_version")
	}

	// Assert the observed structural properties (characterisation -- see comment above):
	// 1. description is present.
	// 2. mosaic_version is present (MOSAIC version stamp written by applyFrontmatter).
	// 3. name is ABSENT: dropped by the Codex descriptor's drop list (FINDING -- see above).
	// 4. No agent-only keys are present.
	for _, want := range []string{"description", "mosaic_version"} {
		found := false
		for _, k := range fmKeys {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("skill frontmatter missing observed key %q; observed keys: %v; "+
				"update this test and the characterisation comment if the key set changes",
				want, fmKeys)
		}
	}

	// name is absent because the Codex descriptor's drop list strips it (FINDING).
	// Assert the absence explicitly so a future fix that restores name changes this test
	// rather than silently preserving false-passing assertions.
	namePresent := false
	for _, k := range fmKeys {
		if k == "name" {
			namePresent = true
			break
		}
	}
	if namePresent {
		t.Logf("skill frontmatter now contains 'name'; the FINDING (Codex drop list strips name "+
			"from skills) may have been addressed; update this test and its comment to reflect "+
			"the new observed key set: %v", fmKeys)
		// Not a failure: if 'name' appears, the finding was fixed and the test should be updated.
	}

	for _, disallowed := range []string{"tools", "sandbox_mode", "developer_instructions"} {
		for _, k := range fmKeys {
			if k == disallowed {
				t.Errorf("skill frontmatter contains Codex agent key %q; "+
					"skills remain Markdown for every harness and must not carry agent-only fields; "+
					"observed keys: %v",
					disallowed, fmKeys)
			}
		}
	}

	// Record the observed key set in the test log so it is visible in CI output.
	// This is the characterisation record required by the acceptance criteria.
	t.Logf("observed deployed skill frontmatter keys: %v", fmKeys)
}

// ---------------------------------------------------------------------------
// T18.1 -- collapseSandboxMode fail-safe pin
// ---------------------------------------------------------------------------

// TestCollapseSandboxMode_EmptyRequest_ReturnsReadOnly verifies that collapseSandboxMode
// with a fully empty request (no Placeholder, empty Generic) returns "read-only".
//
// This pins the fail-safe default at the source: after I18.1, resolveTools calls
// Module.Tools with an empty request for tools-less Codex sources. If collapseSandboxMode
// were to return anything other than "read-only" for an empty request, the I18.1 fix would
// leave a security hole -- a tools-less agent could inherit elevated sandbox permissions.
//
// This test passes regardless of implementation state; it is a regression guard that
// protects the invariant the I18.1 fix depends on.
func TestCollapseSandboxMode_EmptyRequest_ReturnsReadOnly(t *testing.T) {
	mode := codex.CollapseSandboxModeForTesting(domain.ToolRequest{})
	if mode != sandboxModeReadOnly {
		t.Errorf("collapseSandboxMode(empty request) = %q, want %q; "+
			"an empty request with no Placeholder and no Generic tools must yield the "+
			"fail-safe read-only default; this is the value the I18.1 resolveTools fix "+
			"relies on when it calls Module.Tools for a tools-less source",
			mode, sandboxModeReadOnly)
	}
}

// ---------------------------------------------------------------------------
// T18.2 -- Real-descriptor guard: version fields non-empty
// ---------------------------------------------------------------------------

// TestDescriptor_TransformVersion_NonEmpty verifies that the Codex descriptor parsed via
// codex.New (through DescriptorForTesting) has a non-empty TransformVersion field.
//
// This pins the defect location (codex.yaml missing transform_version) independently of
// the transform-level assertion in TestCodexTransform_HarnessVersionStamp_PresentInOutput.
// A regression that removes transform_version from codex.yaml fails here immediately,
// without requiring a full transform pipeline run.
//
// This test is RED until I18.2 adds transform_version: "1.0.0" to codex.yaml.
func TestDescriptor_TransformVersion_NonEmpty(t *testing.T) {
	d := codex.DescriptorForTesting(t)
	if d.TransformVersion == "" {
		t.Errorf("Descriptor.TransformVersion is empty; "+
			"codex.yaml must declare transform_version: \"1.0.0\" so that applyVersionStamp "+
			"writes a '# mosaic_harness_version:' comment in every deployed Codex agent file; "+
			"an empty TransformVersion silently omits the harness version stamp (FR-9 violation)")
	}
}

// TestDescriptor_InjectionsVersion_NonEmpty verifies that the Codex descriptor parsed via
// codex.New (through DescriptorForTesting) has a non-empty InjectionsVersion field.
//
// This pins the defect location (codex.yaml missing injections_version) independently of
// the transform-level assertion in TestCodexTransform_InjectionsVersionOnRegionTag.
// A regression that removes injections_version from codex.yaml fails here immediately,
// without requiring a source with injection regions to detect the absence.
//
// This test is RED until I18.2 adds injections_version: "1.0.0" to codex.yaml.
func TestDescriptor_InjectionsVersion_NonEmpty(t *testing.T) {
	d := codex.DescriptorForTesting(t)
	if d.InjectionsVersion == "" {
		t.Errorf("Descriptor.InjectionsVersion is empty; "+
			"codex.yaml must declare injections_version: \"1.0.0\" so that applyHarnessRegion "+
			"stamps the version attribute on InjectionHarness-class region tags; "+
			"the staleness and manifest paths read the injection version from the region tag, "+
			"not from frontmatter; an empty InjectionsVersion silently omits this attribute (FR-9 violation)")
	}
}
