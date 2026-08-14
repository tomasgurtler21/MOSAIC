package app

// retarget_helpers_test.go provides shared stubs, descriptors, and fixture helpers for
// Stage 5 tests (detect_harness_match_test.go, build_retargeted_agent_test.go).
// All helpers are in package app (not package app_test) so they can be shared across
// both test files and can access package-internal helpers where needed.
//
// Two synthetic harness modules are defined:
//
//   newAlphaHarnessModule — "alpha-harness"
//     ModelKey "model", ToolsKey "tools", distinctive plan key "alpha_stamp".
//     Universe: AlphaBash, AlphaRead, AlphaWrite.
//     Mappings: Bash→AlphaBash, Read→AlphaRead, Write→AlphaWrite.
//
//   newBetaHarnessModule — "beta-harness"
//     ModelKey "model_id", ToolsKey "allowed_tools", distinctive plan key "beta_stamp".
//     Universe: BetaBash, BetaRead.
//     Mappings: Bash→BetaBash, Read→BetaRead.
//
//   newGammaHarnessModule — "gamma-harness"
//     ModelKey "model_name", ToolsKey "main_tools", has a DestField diverted destination.
//     Maps Bash→main_tools "GammaBash", Read→DestField "server_tools" "gamma-mcp".

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ---------------------------------------------------------------------------
// retargetTestModule — a domain.HarnessModule backed by a real descriptor
// ---------------------------------------------------------------------------

// retargetTestModule satisfies domain.HarnessModule using real descriptor-level operations
// so that tool mapping and field classification work correctly in Stage 5 tests.
// All operations delegate to the descriptor package; no harness-specific logic is embedded.
type retargetTestModule struct {
	d domain.HarnessDescriptor
}

func (m *retargetTestModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{ID: m.d.ID, DisplayName: m.d.DisplayName, Tier: domain.TierDescriptor, Usable: true}
}

func (m *retargetTestModule) Descriptor() *domain.HarnessDescriptor { return &m.d }

func (m *retargetTestModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return descriptor.MapTools(&m.d, req)
}

func (m *retargetTestModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return descriptor.ApplyFrontmatterSpec(&m.d, req)
}

func (m *retargetTestModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return descriptor.ResolveTargetPath(&m.d, req)
}

func (m *retargetTestModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }

func (m *retargetTestModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "retargetTestModule stub"}, nil
}

func (m *retargetTestModule) Close() error { return nil }

// ---------------------------------------------------------------------------
// Alpha harness — "alpha-harness"
// ---------------------------------------------------------------------------

// newAlphaHarnessModule returns a retargetTestModule representing "alpha-harness".
//
// Distinctive characteristics that tests rely on:
//   - ToolsKey: "tools" (MOSAIC generic key — not alone distinctive, but universe is)
//   - FrontmatterSpec.Add: "alpha_stamp" = "alpha-v1" (distinctive non-MOSAIC key)
//   - KeyOrder: ["model", "tools", "alpha_stamp"]
//   - Universe: AlphaBash, AlphaRead, AlphaWrite
//   - Mappings: Bash→AlphaBash, Read→AlphaRead, Write→AlphaWrite
func newAlphaHarnessModule() *retargetTestModule {
	return &retargetTestModule{
		d: domain.HarnessDescriptor{
			ID:               "alpha-harness",
			DisplayName:      "Alpha Harness",
			TransformVersion: "alpha-1.0.0",
			InjectionsVersion: "alpha-1.0.0",
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{Supported: true, Project: "agents"},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".md",
			},
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "model",
				ToolsKey: "tools",
				Add: []domain.FrontmatterField{
					{Key: "alpha_stamp", Value: domain.ScalarValue("alpha-v1", domain.QuotePlain)},
				},
				KeyOrder: []string{"model", "tools", "alpha_stamp"},
			},
			Tools: domain.ToolSpec{
				Universe: []domain.HarnessTool{
					{Name: "AlphaBash"},
					{Name: "AlphaRead"},
					{Name: "AlphaWrite"},
				},
				Mappings: []domain.ToolMapping{
					{Generic: "Bash", Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"AlphaBash"}},
					}},
					{Generic: "Read", Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"AlphaRead"}},
					}},
					{Generic: "Write", Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"AlphaWrite"}},
					}},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Beta harness — "beta-harness"
// ---------------------------------------------------------------------------

// newBetaHarnessModule returns a retargetTestModule representing "beta-harness".
//
// Distinctive characteristics that tests rely on:
//   - ModelKey: "model_id" (non-MOSAIC key — distinctive)
//   - ToolsKey: "allowed_tools" (non-MOSAIC key — distinctive)
//   - FrontmatterSpec.Add: "beta_stamp" = "beta-v2" (distinctive non-MOSAIC key)
//   - KeyOrder: ["model_id", "allowed_tools", "beta_stamp"]
//   - Universe: BetaBash, BetaRead
//   - Mappings: Bash→BetaBash, Read→BetaRead
func newBetaHarnessModule() *retargetTestModule {
	return &retargetTestModule{
		d: domain.HarnessDescriptor{
			ID:               "beta-harness",
			DisplayName:      "Beta Harness",
			TransformVersion: "beta-2.0.0",
			InjectionsVersion: "beta-2.0.0",
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{Supported: true, Project: "deployed"},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".agent.md",
			},
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "model_id",
				ToolsKey: "allowed_tools",
				Add: []domain.FrontmatterField{
					{Key: "beta_stamp", Value: domain.ScalarValue("beta-v2", domain.QuotePlain)},
				},
				KeyOrder: []string{"model_id", "allowed_tools", "beta_stamp"},
			},
			Tools: domain.ToolSpec{
				Universe: []domain.HarnessTool{
					{Name: "BetaBash"},
					{Name: "BetaRead"},
				},
				Mappings: []domain.ToolMapping{
					{Generic: "Bash", Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"BetaBash"}},
					}},
					{Generic: "Read", Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"BetaRead"}},
					}},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Gamma harness — "gamma-harness" (with a DestField diverted tool)
// ---------------------------------------------------------------------------

// newGammaHarnessModule returns a retargetTestModule representing "gamma-harness".
// Gamma has a DestField destination ("server_tools") so diverted tool field tests
// can exercise that path through the transform core.
//
// Distinctive characteristics:
//   - ModelKey: "model_name" (non-MOSAIC key — distinctive)
//   - ToolsKey: "main_tools" (non-MOSAIC key — distinctive)
//   - FrontmatterSpec.Add: "gamma_stamp" = "gamma-v3"
//   - KeyOrder: ["model_name", "main_tools", "server_tools", "gamma_stamp"]
//   - Universe: GammaBash, gamma-mcp-server
//   - Mappings: Bash→DestMain "GammaBash", Read→DestField "server_tools" "gamma-mcp-server"
func newGammaHarnessModule() *retargetTestModule {
	return &retargetTestModule{
		d: domain.HarnessDescriptor{
			ID:               "gamma-harness",
			DisplayName:      "Gamma Harness",
			TransformVersion: "gamma-3.0.0",
			InjectionsVersion: "gamma-3.0.0",
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{Supported: true, Project: "gamma-agents"},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".md",
			},
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "model_name",
				ToolsKey: "main_tools",
				Add: []domain.FrontmatterField{
					{Key: "gamma_stamp", Value: domain.ScalarValue("gamma-v3", domain.QuotePlain)},
				},
				KeyOrder: []string{"model_name", "main_tools", "server_tools", "gamma_stamp"},
			},
			Tools: domain.ToolSpec{
				Universe: []domain.HarnessTool{
					{Name: "GammaBash"},
					{Name: "gamma-mcp-server"},
				},
				Mappings: []domain.ToolMapping{
					{Generic: "Bash", Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"GammaBash"}},
					}},
					{Generic: "Read", Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "server_tools", Format: domain.FormatListBlock, Names: []string{"gamma-mcp-server"}},
					}},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// orchestratorConditionalModule — a target module with per-agent-key conditional stamping
// ---------------------------------------------------------------------------

// orchestratorConditionalModule wraps a retargetTestModule and overrides Frontmatter to
// implement a per-agent-key conditional for mosaic_orchestrator_injections_version, mirroring
// the shape of the real Claude Code module. Only AgentKey "orchestrator" receives the stamp;
// all other agents must not carry it.
//
// The inner descriptor has OrchestratorInjectionsVersion set so that any code which reads
// the descriptor and stamps the field unconditionally (bypassing the module's Frontmatter
// decision) would produce a visible, testable regression.
type orchestratorConditionalModule struct {
	inner       *retargetTestModule
	orchVersion string
}

// newOrchestratorConditionalModule returns an orchestratorConditionalModule backed by a
// beta-like descriptor whose OrchestratorInjectionsVersion is set to orchVersion. This lets
// a test verify that BuildRetargetedAgent does not auto-stamp the field from the descriptor
// when the module's own Frontmatter() decision excludes it (non-orchestrator agent keys).
func newOrchestratorConditionalModule(orchVersion string) *orchestratorConditionalModule {
	base := newBetaHarnessModule()
	base.d.OrchestratorInjectionsVersion = orchVersion
	return &orchestratorConditionalModule{inner: base, orchVersion: orchVersion}
}

func (m *orchestratorConditionalModule) Ref() domain.HarnessRef {
	return m.inner.Ref()
}

func (m *orchestratorConditionalModule) Descriptor() *domain.HarnessDescriptor {
	return m.inner.Descriptor()
}

func (m *orchestratorConditionalModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return m.inner.Tools(req)
}

// Frontmatter overrides the inner module to implement the per-agent-key conditional:
// mosaic_orchestrator_injections_version is added to plan.Set only for AgentKey "orchestrator".
func (m *orchestratorConditionalModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	plan, err := m.inner.Frontmatter(req)
	if err != nil {
		return plan, err
	}
	if req.AgentKey == "orchestrator" && m.orchVersion != "" {
		orchField, _ := agentfields.ByDeployedName("orchestrator_injections_version")
		plan.Set = append(plan.Set, domain.FrontmatterField{
			Key:   orchField.Deployed,
			Value: domain.ScalarValue(m.orchVersion, domain.QuotePlain),
		})
	}
	// For all other agent keys, the field is absent from plan.Set and must not appear in output.
	return plan, nil
}

func (m *orchestratorConditionalModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return m.inner.TargetPath(req)
}

func (m *orchestratorConditionalModule) Injection(req domain.InjectionRequest) (string, bool) {
	return m.inner.Injection(req)
}

func (m *orchestratorConditionalModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	return m.inner.HookPlan(req)
}

func (m *orchestratorConditionalModule) Close() error {
	return m.inner.Close()
}

// ---------------------------------------------------------------------------
// Source agent byte fixtures
// ---------------------------------------------------------------------------

// alphaDeployedAgentBytes returns the bytes of a minimal deployed agent for "alpha-harness".
// It carries:
//   - transform_version (eligibility signal one)
//   - Two canonical SECTION tags (eligibility signal two)
//   - model key under "model" (alpha's model key)
//   - tools list with AlphaBash, AlphaRead, and a custom tool "CustomMCP" (no known mapping)
//   - alpha_stamp (the harness plan key distinctive to alpha)
//   - MOSAIC stamp fields
//   - Injection regions with content (for injection preservation tests)
//   - Deployed regions with content (for deployed region preservation tests)
func alphaDeployedAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"alpha-1.0.0\"\n" +
		"injections_version: \"alpha-1.0.0\"\n" +
		"tool_mappings_version: \"abc123\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"42\"\n" +
		"name: retarget-test-agent\n" +
		"description: An agent used for harness-to-harness transform tests\n" +
		"role: subagent\n" +
		"model: claude-3-5-sonnet\n" +
		"tools:\n" +
		"- AlphaBash\n" +
		"- AlphaRead\n" +
		"- CustomMCP\n" +
		"alpha_stamp: alpha-v1\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the RetargetTest agent. This prose must survive transformation unchanged.\n" +
		"<IdentityExtension type=\"project\">\n" +
		"Project-specific identity guidance goes here.\n" +
		"This content must survive byte-for-byte in the target output.\n" +
		"</IdentityExtension>\n" +
		"<ClosingProcedure type=\"managed\">\n" +
		"These are the closing procedure steps from the source harness.\n" +
		"</ClosingProcedure>\n" +
		"</Identity>\n" +
		"---\n" +
		"<Capabilities type=\"core\">\n" +
		"Core capabilities prose.\n" +
		"<CodebaseContext type=\"project\">\n" +
		"The codebase uses Go 1.22. This context must survive transform unchanged.\n" +
		"</CodebaseContext>\n" +
		"</Capabilities>\n")
}

// betaDeployedAgentBytes returns bytes of a minimal deployed agent for "beta-harness".
// It carries beta's distinctive keys (model_id, allowed_tools, beta_stamp) and beta's
// universe tool names (BetaBash, BetaRead). Used to verify HarnessMatchNo when checked
// against the alpha descriptor.
func betaDeployedAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"beta-2.0.0\"\n" +
		"injections_version: \"beta-2.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"77\"\n" +
		"name: beta-agent\n" +
		"description: An agent deployed for beta-harness\n" +
		"role: subagent\n" +
		"model_id: gpt-4-turbo\n" +
		"allowed_tools:\n" +
		"- BetaBash\n" +
		"- BetaRead\n" +
		"beta_stamp: beta-v2\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the BetaAgent.\n" +
		"</Identity>\n")
}

// mosaicOnlyAgentBytes returns bytes of a deployed MOSAIC agent with no harness-distinctive
// keys — only MOSAIC-known generic vocabulary keys and MOSAIC stamps. Used to verify
// HarnessMatchIndeterminate: the file is a valid agent but carries no distinguishing signal
// for any specific harness.
func mosaicOnlyAgentBytes() []byte {
	return []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"10\"\n" +
		"name: bare-agent\n" +
		"description: An agent with only MOSAIC-known fields\n" +
		"role: subagent\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the BareAgent.\n" +
		"</Identity>\n")
}

// notAnAgentBytes returns bytes of a document that is parseable but does not satisfy
// the two-signal eligibility rule (no transform_version). Used to verify
// HarnessMatchNotAgent.
func notAnAgentBytes() []byte {
	return []byte("---\n" +
		"name: plain-document\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Just a plain document, not a transformed MOSAIC agent.\n" +
		"</Identity>\n")
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

// parseFrontmatterScalar parses src and returns the scalar value of the named
// frontmatter field, failing the test if parsing fails or the field has a non-scalar kind.
// present is false when the field is absent.
func parseFrontmatterScalar(t *testing.T, src []byte, key string) (value string, present bool) {
	t.Helper()
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("parseFrontmatterScalar: docformat.Parse failed: %v", err)
	}
	v, ok := doc.Frontmatter().Get(key)
	if !ok {
		return "", false
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("parseFrontmatterScalar: field %q has kind %v, want KindScalar", key, v.Kind)
	}
	return v.Scalar, true
}

// frontmatterKeys parses src and returns the set of all frontmatter keys present.
func frontmatterKeys(t *testing.T, src []byte) map[string]bool {
	t.Helper()
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("frontmatterKeys: docformat.Parse failed: %v", err)
	}
	keys := make(map[string]bool)
	for _, k := range doc.Frontmatter().Keys() {
		keys[k] = true
	}
	return keys
}

// toolsListFromFrontmatter parses src and returns the tool names stored under the given
// frontmatter key as a []string. The value must be a KindList of scalar items.
// Returns nil when the key is absent or the value is not a list.
func toolsListFromFrontmatter(t *testing.T, src []byte, toolsKey string) []string {
	t.Helper()
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("toolsListFromFrontmatter: docformat.Parse failed: %v", err)
	}
	v, ok := doc.Frontmatter().Get(toolsKey)
	if !ok || v.Kind != domain.KindList {
		return nil
	}
	var result []string
	for _, item := range v.Items {
		if item.Kind == domain.KindScalar {
			result = append(result, item.Scalar)
		}
	}
	return result
}

// injectionRegionContents parses src and returns a map from injection name to content bytes.
func injectionRegionContents(t *testing.T, src []byte) map[string][]byte {
	t.Helper()
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("injectionRegionContents: docformat.Parse failed: %v", err)
	}
	result := make(map[string][]byte)
	for _, node := range doc.Body().Injections() {
		result[node.Name()] = node.Content()
	}
	return result
}

// sliceHas reports whether slice contains s. Named distinctly from containsString
// to avoid conflicting with the same-named helper in harness_only_refresh_test.go.
func sliceHas(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
