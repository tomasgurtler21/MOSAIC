package opencode_test

// Tests for configuration-scope enumeration and inspection.
//
// This harness merges two configuration scopes outside the sandbox —
// user-config (a document) and user-plugins (a directory listing) — plus the
// sandbox's own plugins directory. Inspection is driven entirely through a
// fixture ScopeProbe rather than the real filesystem, so the "never writes
// outside the sandbox" promise stays structural: the seam has no write
// method at all.
//
// The folding discipline mirrors the Claude Code adapter exactly: an
// absent, unreadable or malformed scope must never read downstream as an
// ordinary "inspected, clean" result.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/opencode"
)

func readScopeFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "opencode", "scopes", name))
	if err != nil {
		t.Fatalf("scopes: reading fixture %q: %v", name, err)
	}
	return data
}

// fixtureScopeProbe is a test-only ScopeProbe backed by an in-memory map,
// keyed by scope name, so scope inspection is testable from fixture
// documents rather than the real filesystem.
type fixtureScopeProbe struct {
	docs map[string][]byte
}

func (p fixtureScopeProbe) Read(scope domain.ConfigScope) ([]byte, error) {
	doc, ok := p.docs[scope.Name]
	if !ok {
		return nil, opencode.ErrScopeAbsent
	}
	return doc, nil
}

// TestScopes_EnumeratesUserConfigAndUserPluginsOutsideTheSandbox asserts the
// enumeration declares both non-sandbox scopes this harness merges, neither
// marked InSandbox.
func TestScopes_EnumeratesUserConfigAndUserPluginsOutsideTheSandbox(t *testing.T) {
	sb := newSandbox(t, t.TempDir())

	scopes := opencode.Scopes(sb)
	if len(scopes) == 0 {
		t.Fatalf("Scopes: got no scopes, want at least sandbox, user-config and user-plugins")
	}

	var sawUserConfig, sawUserPlugins bool
	for _, s := range scopes {
		if s.InSandbox {
			continue
		}
		switch s.Name {
		case "user-config":
			sawUserConfig = true
		case "user-plugins":
			sawUserPlugins = true
		}
	}
	if !sawUserConfig {
		t.Errorf("Scopes: no non-sandbox scope named %q found among %+v", "user-config", scopes)
	}
	if !sawUserPlugins {
		t.Errorf("Scopes: no non-sandbox scope named %q found among %+v", "user-plugins", scopes)
	}
}

// TestScopes_SandboxScopeIsIncludedAndComesLast asserts the enumeration
// includes the sandbox's own plugins directory as a complete description
// rather than a list of exceptions, and that it is ordered last: a project
// scope overrides a global one, so the sandbox scope is highest precedence.
func TestScopes_SandboxScopeIsIncludedAndComesLast(t *testing.T) {
	sb := newSandbox(t, t.TempDir())

	scopes := opencode.Scopes(sb)
	if len(scopes) == 0 {
		t.Fatalf("Scopes: got no scopes")
	}

	last := scopes[len(scopes)-1]
	if !last.InSandbox {
		t.Errorf("Scopes: last scope = %+v, want the sandbox scope (highest precedence) last", last)
	}
	if last.Name != "sandbox" {
		t.Errorf("Scopes: last scope name = %q, want %q", last.Name, "sandbox")
	}
}

// TestScopes_NonSandboxScopesAreNotClaimedIsolatable asserts that, absent a
// confirmed environment variable relocating OpenCode's user-scope
// configuration, neither non-sandbox scope is marked Isolatable — the
// adapter must not claim a neutralization capability it cannot back up.
func TestScopes_NonSandboxScopesAreNotClaimedIsolatable(t *testing.T) {
	sb := newSandbox(t, t.TempDir())

	for _, s := range opencode.Scopes(sb) {
		if s.InSandbox {
			continue
		}
		if s.Isolatable {
			t.Errorf("Scopes: scope %q Isolatable = true, want false (no confirmed relocation variable)", s.Name)
		}
	}
}

// TestInspectScope_UserConfigWithPluginArrayRewritesInput asserts that a
// user-config document declaring a non-empty plugin array is reported as
// RewritesInput: any plugin present may register a pre-invocation hook, and
// the document alone cannot tell, so presence is treated conservatively.
func TestInspectScope_UserConfigWithPluginArrayRewritesInput(t *testing.T) {
	scope := domain.ConfigScope{Name: "user-config", Path: "user-config-with-plugin.json"}
	doc := readScopeFixture(t, "user-config-with-plugin.json")

	finding, err := opencode.InspectScope(scope, doc)
	if err != nil {
		t.Fatalf("InspectScope: %v", err)
	}
	if !finding.RewritesInput {
		t.Errorf("InspectScope: RewritesInput = false, want true for a user-config document declaring a plugin")
	}
	if finding.Detail == "" {
		t.Errorf("InspectScope: Detail is empty; a rewriting finding must explain itself to the operator")
	}
}

// TestInspectScope_UserConfigCleanReportsNoRewrite asserts that a
// user-config document with no plugins declared is reported clean.
func TestInspectScope_UserConfigCleanReportsNoRewrite(t *testing.T) {
	scope := domain.ConfigScope{Name: "user-config", Path: "user-config-clean.json"}
	doc := readScopeFixture(t, "user-config-clean.json")

	finding, err := opencode.InspectScope(scope, doc)
	if err != nil {
		t.Fatalf("InspectScope: %v", err)
	}
	if finding.RewritesInput {
		t.Errorf("InspectScope: RewritesInput = true for a user-config document with no plugins declared")
	}
}

// TestInspectScope_UserConfigMalformedDocumentIsError asserts that a
// malformed user-config document fails cleanly with an error rather than
// panicking or silently reading as clean — a misjudged "clean" here is
// exactly the silently non-deterministic run this inspection exists to
// prevent.
func TestInspectScope_UserConfigMalformedDocumentIsError(t *testing.T) {
	scope := domain.ConfigScope{Name: "user-config", Path: "user-config-malformed.json"}
	doc := readScopeFixture(t, "user-config-malformed.json")

	finding, err := opencode.InspectScope(scope, doc)
	if err == nil {
		t.Fatalf("InspectScope: want error for a malformed document, got nil (finding=%+v)", finding)
	}
	if finding.RewritesInput {
		t.Errorf("InspectScope: a malformed document must not also carry a positive RewritesInput finding on an error path")
	}
}

// TestInspectScope_UserPluginsNonEmptyListingRewritesInput asserts that a
// non-empty plugin-directory listing is reported as RewritesInput: any
// plugin present may register a hook and the listing alone cannot tell.
func TestInspectScope_UserPluginsNonEmptyListingRewritesInput(t *testing.T) {
	scope := domain.ConfigScope{Name: "user-plugins", Path: "user-plugins-dir"}
	doc := []byte("some-third-party-plugin.js\n")

	finding, err := opencode.InspectScope(scope, doc)
	if err != nil {
		t.Fatalf("InspectScope: %v", err)
	}
	if !finding.RewritesInput {
		t.Errorf("InspectScope: RewritesInput = false, want true for a non-empty user-plugins directory listing")
	}
	if finding.Detail == "" {
		t.Errorf("InspectScope: Detail is empty; a rewriting finding must explain itself to the operator")
	}
}

// TestInspectScope_UserPluginsEmptyListingReportsNoRewrite asserts that an
// empty plugin-directory listing is reported clean.
func TestInspectScope_UserPluginsEmptyListingReportsNoRewrite(t *testing.T) {
	scope := domain.ConfigScope{Name: "user-plugins", Path: "user-plugins-dir"}

	for _, doc := range [][]byte{nil, []byte(""), []byte("   \n")} {
		finding, err := opencode.InspectScope(scope, doc)
		if err != nil {
			t.Fatalf("InspectScope(doc=%q): %v", doc, err)
		}
		if finding.RewritesInput {
			t.Errorf("InspectScope(doc=%q): RewritesInput = true, want false for an empty user-plugins directory listing", doc)
		}
	}
}

// TestInspectScopes_SkipsInSandboxScope asserts that InspectScopes never
// probes the sandbox scope: it is the sandbox this adapter itself writes
// into, and the port's contract is to examine only scopes outside it.
func TestInspectScopes_SkipsInSandboxScope(t *testing.T) {
	probe := fixtureScopeProbe{
		docs: map[string][]byte{
			"user-config":  readScopeFixture(t, "user-config-clean.json"),
			"user-plugins": []byte(""),
		},
	}
	adapter := opencode.New(opencode.Options{ScopeProbe: probe})

	findings, err := adapter.InspectScopes(testContext())
	if err != nil {
		t.Fatalf("InspectScopes: %v", err)
	}
	for _, f := range findings {
		if f.Scope.InSandbox {
			t.Errorf("InspectScopes: findings include an in-sandbox scope %+v; InspectScopes must only examine scopes outside the sandbox", f)
		}
	}
}

// TestInspectScopes_AbsentScopeIsNotReportedAsClean asserts that a scope the
// probe reports as absent is never folded into the findings as an ordinary
// "inspected, no rewrite" result.
func TestInspectScopes_AbsentScopeIsNotReportedAsClean(t *testing.T) {
	probe := fixtureScopeProbe{docs: map[string][]byte{}} // every scope absent
	adapter := opencode.New(opencode.Options{ScopeProbe: probe})

	findings, err := adapter.InspectScopes(testContext())
	if err != nil {
		t.Fatalf("InspectScopes: %v", err)
	}

	for _, f := range findings {
		if !f.RewritesInput && f.Detail == "" {
			t.Errorf("InspectScopes: absent scope %q was reported as clean with no explanation; an absent scope must never look like a clean inspection", f.Scope.Name)
		}
	}
}

// TestInspectScopes_UnreadableScopeIsTreatedAsRewriting asserts that a
// probe read failure other than ErrScopeAbsent is folded into a rewriting,
// unresolved finding — never silently treated as clean.
func TestInspectScopes_UnreadableScopeIsTreatedAsRewriting(t *testing.T) {
	adapter := opencode.New(opencode.Options{ScopeProbe: erroringScopeProbe{}})

	findings, err := adapter.InspectScopes(testContext())
	if err != nil {
		t.Fatalf("InspectScopes: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("InspectScopes: got no findings for scopes that all fail to read")
	}
	for _, f := range findings {
		if !f.RewritesInput {
			t.Errorf("InspectScopes: unreadable scope %q reported RewritesInput = false, want true (treated as unresolved for safety)", f.Scope.Name)
		}
	}
}

// TestInspectScopes_MalformedScopeIsTreatedAsRewriting asserts that a scope
// whose document InspectScope itself rejects as malformed (as opposed to
// absent or unreadable) folds, through InspectScopes, into an unresolved
// rewriting finding rather than propagating a bare error or reading as
// clean. This is the third row of the inspectOneScope folding table,
// exercised here at the adapter level rather than only at the pure
// InspectScope level.
func TestInspectScopes_MalformedScopeIsTreatedAsRewriting(t *testing.T) {
	probe := fixtureScopeProbe{
		docs: map[string][]byte{
			"user-config":  readScopeFixture(t, "user-config-malformed.json"),
			"user-plugins": []byte(""),
		},
	}
	adapter := opencode.New(opencode.Options{ScopeProbe: probe})

	findings, err := adapter.InspectScopes(testContext())
	if err != nil {
		t.Fatalf("InspectScopes: %v", err)
	}

	var sawMalformedRewrite bool
	for _, f := range findings {
		if f.Scope.Name == "user-config" && f.RewritesInput {
			sawMalformedRewrite = true
			if f.Detail == "" {
				t.Errorf("InspectScopes: malformed-document finding for %q has empty Detail", f.Scope.Name)
			}
		}
	}
	if !sawMalformedRewrite {
		t.Errorf("InspectScopes: no RewritesInput finding reported for a user-config scope whose document is malformed; findings=%+v", findings)
	}
}

type erroringScopeProbe struct{}

func (erroringScopeProbe) Read(scope domain.ConfigScope) ([]byte, error) {
	return nil, os.ErrPermission
}

// TestInspectScopes_RefusesOnCompetingRewriteHook asserts that a competing
// outside-scope plugin declaration produces an unresolved rewriting
// finding, rather than letting setup proceed silently.
func TestInspectScopes_RefusesOnCompetingRewriteHook(t *testing.T) {
	probe := fixtureScopeProbe{
		docs: map[string][]byte{
			"user-config":  readScopeFixture(t, "user-config-with-plugin.json"),
			"user-plugins": []byte(""),
		},
	}
	adapter := opencode.New(opencode.Options{ScopeProbe: probe})

	findings, err := adapter.InspectScopes(testContext())
	if err != nil {
		t.Fatalf("InspectScopes: %v", err)
	}

	var sawUnresolvedRewrite bool
	for _, f := range findings {
		if f.Scope.Name == "user-config" && f.RewritesInput && !f.Neutralized {
			sawUnresolvedRewrite = true
			if f.Detail == "" {
				t.Errorf("InspectScopes: un-neutralized rewriting finding for %q has empty Detail", f.Scope.Name)
			}
		}
	}
	if !sawUnresolvedRewrite {
		t.Errorf("InspectScopes: no un-neutralized rewriting finding reported for a user-config scope declaring a plugin; findings=%+v", findings)
	}
}

// TestScopeProbe_NeverWritesOutsideSandbox asserts the structural property
// that ScopeProbe is read-only: the interface offers no write method, so a
// well-typed probe cannot write outside the sandbox by construction.
func TestScopeProbe_NeverWritesOutsideSandbox(t *testing.T) {
	var _ opencode.ScopeProbe = fixtureScopeProbe{}
	// fixtureScopeProbe implements only Read; if ScopeProbe ever grows a
	// write method, this file fails to compile until every probe
	// implementation, including this fixture, supplies one under review.
}

// TestConfigScopes_IncludesEveryScopeInspectScopesConsiders asserts that
// ConfigScopes() is a superset InspectScopes filters, not an independent
// list that could silently diverge from it.
func TestConfigScopes_IncludesEveryScopeInspectScopesConsiders(t *testing.T) {
	adapter := opencode.New(opencode.Options{})
	scopes := adapter.ConfigScopes()

	var sawUserConfig, sawUserPlugins bool
	for _, s := range scopes {
		switch s.Name {
		case "user-config":
			sawUserConfig = true
		case "user-plugins":
			sawUserPlugins = true
		}
	}
	if !sawUserConfig || !sawUserPlugins {
		t.Errorf("ConfigScopes: got %+v, want it to include both user-config and user-plugins", scopes)
	}
}
