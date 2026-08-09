package opencode_test

// Tests for the generated plugin's content (GeneratePlugin).
//
// The plugin's runtime behaviour cannot be verified without a real
// `opencode` binary, which is out of scope by explicit constraint. What is
// tested here is the generated content itself: it must register the
// pre-invocation hook that reaches the interceptor synchronously — before
// the real tool call runs — and the post-invocation hook must be
// observational only, per ContractsDesign.md's "Generated plugin contract".

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/opencode"
)

func buildTestBridges(t *testing.T, sb domain.Sandbox) (pre, post opencode.Bridge) {
	t.Helper()
	req := baseProvisionRequest(sb)

	pre, err := opencode.BuildBridge(req, domain.PhasePre)
	if err != nil {
		t.Fatalf("BuildBridge(PhasePre): %v", err)
	}
	post, err = opencode.BuildBridge(req, domain.PhasePost)
	if err != nil {
		t.Fatalf("BuildBridge(PhasePost): %v", err)
	}
	return pre, post
}

// TestGeneratePlugin_RegistersBothHooks asserts the rendered module
// registers exactly the two hooks this adapter's interception points use.
func TestGeneratePlugin_RegistersBothHooks(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	content, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "tool.execute.before") {
		t.Errorf("GeneratePlugin: content does not register %q; content=\n%s", "tool.execute.before", s)
	}
	if !strings.Contains(s, "tool.execute.after") {
		t.Errorf("GeneratePlugin: content does not register %q; content=\n%s", "tool.execute.after", s)
	}
}

// TestGeneratePlugin_GuardsOnInterceptedToolName asserts the generated
// module only forwards calls for the dispatch tool this adapter targets, so
// every other tool call returns immediately without reaching the
// interceptor.
func TestGeneratePlugin_GuardsOnInterceptedToolName(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	content, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, opencode.InterceptedToolName) {
		t.Errorf("GeneratePlugin: content does not reference the intercepted tool name %q; content=\n%s", opencode.InterceptedToolName, s)
	}
}

// preSection isolates the tool.execute.before handler's source so
// assertions about its behaviour do not accidentally match the
// tool.execute.after handler instead.
func preSection(t *testing.T, content string) string {
	t.Helper()
	beforeIdx := strings.Index(content, "tool.execute.before")
	afterIdx := strings.Index(content, "tool.execute.after")
	if beforeIdx == -1 || afterIdx == -1 {
		t.Fatalf("GeneratePlugin: could not locate both hook sections in content:\n%s", content)
	}
	if beforeIdx < afterIdx {
		return content[beforeIdx:afterIdx]
	}
	return content[beforeIdx:]
}

func postSection(t *testing.T, content string) string {
	t.Helper()
	beforeIdx := strings.Index(content, "tool.execute.before")
	afterIdx := strings.Index(content, "tool.execute.after")
	if beforeIdx == -1 || afterIdx == -1 {
		t.Fatalf("GeneratePlugin: could not locate both hook sections in content:\n%s", content)
	}
	if afterIdx < beforeIdx {
		return content[afterIdx:beforeIdx]
	}
	return content[afterIdx:]
}

// TestGeneratePlugin_PreHookAwaitsTheInterceptor asserts the pre-invocation
// handler is synchronous with respect to the call: it must await the
// interceptor's reply before returning, because rewriting the call's input
// only means anything if it happens before the call runs.
func TestGeneratePlugin_PreHookAwaitsTheInterceptor(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	content, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin: %v", err)
	}

	section := preSection(t, string(content))
	if !strings.Contains(section, "await") {
		t.Errorf("GeneratePlugin: tool.execute.before section does not contain %q; a broken interceptor could otherwise race the real tool call. section=\n%s", "await", section)
	}
}

// TestGeneratePlugin_PreHookCanRewriteArgs asserts the pre-invocation
// handler's source is capable of assigning onto output.args, the mechanism
// by which an "allow" decision rewrites the call before it runs.
func TestGeneratePlugin_PreHookCanRewriteArgs(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	content, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin: %v", err)
	}

	section := preSection(t, string(content))
	if !strings.Contains(section, "output.args") {
		t.Errorf("GeneratePlugin: tool.execute.before section does not reference %q; want the argument-rewrite mechanism present. section=\n%s", "output.args", section)
	}
}

// TestGeneratePlugin_PreHookCanDenyByThrowing asserts the pre-invocation
// handler's source is capable of throwing, this harness's only block
// mechanism for a "deny" decision.
func TestGeneratePlugin_PreHookCanDenyByThrowing(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	content, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin: %v", err)
	}

	section := preSection(t, string(content))
	if !strings.Contains(section, "throw") {
		t.Errorf("GeneratePlugin: tool.execute.before section does not contain %q; want the deny/block mechanism present. section=\n%s", "throw", section)
	}
}

// TestGeneratePlugin_PostHookNeverAssignsOutputArgs asserts the
// post-invocation handler is observational only: it must never assign onto
// output.args, since the real tool has already run by the time it fires and
// any such mutation is confirmed not to be honoured downstream.
func TestGeneratePlugin_PostHookNeverAssignsOutputArgs(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	content, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin: %v", err)
	}

	section := postSection(t, string(content))
	if strings.Contains(section, "output.args") {
		t.Errorf("GeneratePlugin: tool.execute.after section references %q; the post-invocation hook must be observational only. section=\n%s", "output.args", section)
	}
}

// TestGeneratePlugin_EmbedsPreAndPostBridgeExecutables asserts the rendered
// module embeds both bridges' resolved executables, so the plugin actually
// invokes the interceptor rather than a placeholder.
func TestGeneratePlugin_EmbedsPreAndPostBridgeExecutables(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	content, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, pre.Executable) {
		t.Errorf("GeneratePlugin: content does not embed the pre-invocation bridge executable %q; content=\n%s", pre.Executable, s)
	}
	if !strings.Contains(s, post.Executable) {
		t.Errorf("GeneratePlugin: content does not embed the post-invocation bridge executable %q; content=\n%s", post.Executable, s)
	}
}

// TestGeneratePlugin_EmbedsPhaseArgumentsForBothBridges asserts the
// generated content carries each bridge's own --phase argument value, so
// the interceptor can distinguish which invocation point called it.
func TestGeneratePlugin_EmbedsPhaseArgumentsForBothBridges(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	content, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, string(domain.PhasePre)) {
		t.Errorf("GeneratePlugin: content does not embed the pre phase value %q; content=\n%s", domain.PhasePre, s)
	}
	if !strings.Contains(s, string(domain.PhasePost)) {
		t.Errorf("GeneratePlugin: content does not embed the post phase value %q; content=\n%s", domain.PhasePost, s)
	}
}

// TestGeneratePlugin_DocumentsKnownBlindSpots asserts the required comments
// naming this adapter's known limitations are present in the generated
// content: hooks do not fire for MCP-routed tool calls, and may not
// intercept calls made by harness-spawned subagents.
func TestGeneratePlugin_DocumentsKnownBlindSpots(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	content, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin: %v", err)
	}
	s := strings.ToLower(string(content))

	if !strings.Contains(s, "mcp") {
		t.Errorf("GeneratePlugin: content does not document the MCP-routed blind spot; content=\n%s", content)
	}
	if !strings.Contains(s, "subagent") {
		t.Errorf("GeneratePlugin: content does not document the harness-spawned-subagent blind spot; content=\n%s", content)
	}
}

// TestGeneratePlugin_DocumentsAfterHookMutationUnused asserts the required
// comment stating that after-hook result mutation is deliberately unused is
// present, since a reader of the generated file must be told why the
// post-invocation handler never touches output.
func TestGeneratePlugin_DocumentsAfterHookMutationUnused(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	content, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin: %v", err)
	}
	s := strings.ToLower(string(content))

	if !strings.Contains(s, "unused") {
		t.Errorf("GeneratePlugin: content does not document that after-hook mutation is deliberately unused; content=\n%s", content)
	}
}

// TestGeneratePlugin_IsDeterministic asserts GeneratePlugin is pure: the
// same bridges must produce byte-identical content on every call.
func TestGeneratePlugin_IsDeterministic(t *testing.T) {
	sb := newSandbox(t, t.TempDir())
	pre, post := buildTestBridges(t, sb)

	first, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin (first call): %v", err)
	}
	second, err := opencode.GeneratePlugin(pre, post)
	if err != nil {
		t.Fatalf("GeneratePlugin (second call): %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("GeneratePlugin: not deterministic; two calls with identical bridges produced different content")
	}
}

// TestGeneratePlugin_ReflectsBridgeArguments asserts the generated content
// actually depends on its inputs — a bridge with a different control
// directory must produce different content, so the function is not silently
// ignoring what it was given.
func TestGeneratePlugin_ReflectsBridgeArguments(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	sb1 := newSandbox(t, dir1)
	sb2 := newSandbox(t, dir2)

	pre1, post1 := buildTestBridges(t, sb1)
	pre2, post2 := buildTestBridges(t, sb2)

	content1, err := opencode.GeneratePlugin(pre1, post1)
	if err != nil {
		t.Fatalf("GeneratePlugin (sandbox 1): %v", err)
	}
	content2, err := opencode.GeneratePlugin(pre2, post2)
	if err != nil {
		t.Fatalf("GeneratePlugin (sandbox 2): %v", err)
	}
	if string(content1) == string(content2) {
		t.Errorf("GeneratePlugin: content identical for two bridges with different control directories (%q vs %q); the bridge's arguments must be reflected in the output", sb1.ControlDir, sb2.ControlDir)
	}
}

// TestPluginFileName_IsATypeScriptFileUnderThePluginsDirectory asserts the
// generated file's declared name is recognisable as a TypeScript plugin
// module, which is what OpenCode's directory-drop discovery expects.
func TestPluginFileName_IsATypeScriptFileUnderThePluginsDirectory(t *testing.T) {
	if opencode.PluginFileName == "" {
		t.Fatalf("PluginFileName is empty")
	}
	if !strings.HasSuffix(opencode.PluginFileName, ".ts") {
		t.Errorf("PluginFileName = %q, want a .ts file name", opencode.PluginFileName)
	}
}
