package claudecode_test

// Tests for configuration scopes the workspace does not own (T10.5).
//
// The adapter enumerates the scopes this harness merges beyond the project
// (user scope, and any enterprise layer it honours). Setup inspects those
// scopes and fails with a precise message when any of them registers an
// input-rewriting hook for the intercepted tool. Where the harness offers
// scope isolation through CLAUDE_CONFIG_DIR, the adapter uses it and
// reports the scope as neutralized rather than merely inspected.
//
// Two properties are non-negotiable and tested here: the tool never writes
// outside the workspace, and a competing outside-scope hook produces a
// clear refusal rather than a silently non-deterministic run.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func readScopeFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "claudecode", "scopes", name))
	if err != nil {
		t.Fatalf("scopes: reading fixture %q: %v", name, err)
	}
	return data
}

// TestScopes_EnumeratesUserScopeAsIsolatable asserts that the user scope is
// declared, is not the sandbox scope, and is marked isolatable — this
// harness offers CLAUDE_CONFIG_DIR to relocate it.
func TestScopes_EnumeratesUserScopeAsIsolatable(t *testing.T) {
	sb := newSandbox(t, t.TempDir())

	scopes := claudecode.Scopes(sb)
	if len(scopes) == 0 {
		t.Fatalf("Scopes: got no scopes, want at least the sandbox and user scopes")
	}

	var sandboxSeen, userSeen bool
	for _, s := range scopes {
		if s.InSandbox {
			sandboxSeen = true
			continue
		}
		if s.Name == "user" {
			userSeen = true
			if !s.Isolatable {
				t.Errorf("Scopes: user scope must be Isolatable (CLAUDE_CONFIG_DIR relocates it)")
			}
		}
	}
	if !sandboxSeen {
		t.Errorf("Scopes: no scope has InSandbox true; the enumeration must be a complete description, including the sandbox itself")
	}
	if !userSeen {
		t.Errorf("Scopes: no scope named %q found among %+v", "user", scopes)
	}
}

// TestInspectScope_DetectsCompetingRewriteHook asserts that a scope document
// registering a PreToolUse hook against the intercepted tool is reported as
// RewritesInput.
func TestInspectScope_DetectsCompetingRewriteHook(t *testing.T) {
	scope := domain.ConfigScope{Name: "user", Path: "user-settings-with-rewriter.json", Isolatable: true}
	doc := readScopeFixture(t, "user-settings-with-rewriter.json")

	finding, err := claudecode.InspectScope(scope, doc)
	if err != nil {
		t.Fatalf("InspectScope: %v", err)
	}
	if !finding.RewritesInput {
		t.Errorf("InspectScope: RewritesInput = false, want true for a document registering a PreToolUse hook on the Task tool")
	}
	if finding.Detail == "" {
		t.Errorf("InspectScope: Detail is empty; a rewriting finding must explain itself to the operator")
	}
}

// TestInspectScope_CleanDocumentReportsNoRewrite asserts that a scope
// document with no competing PreToolUse hook is reported clean.
func TestInspectScope_CleanDocumentReportsNoRewrite(t *testing.T) {
	scope := domain.ConfigScope{Name: "user", Path: "user-settings-clean.json", Isolatable: true}
	doc := readScopeFixture(t, "user-settings-clean.json")

	finding, err := claudecode.InspectScope(scope, doc)
	if err != nil {
		t.Fatalf("InspectScope: %v", err)
	}
	if finding.RewritesInput {
		t.Errorf("InspectScope: RewritesInput = true for a document with no PreToolUse hook on the intercepted tool")
	}
}

// TestInspectScope_MalformedDocumentIsError asserts that a scope document
// that is not well-formed fails cleanly with an error, mirroring
// TestParseFragment_MalformedIsError: a malformed outside-scope document
// must never panic and must never silently read as clean, since a
// misjudged "clean" here is exactly the silently non-deterministic run
// AC10.7 forbids.
func TestInspectScope_MalformedDocumentIsError(t *testing.T) {
	scope := domain.ConfigScope{Name: "user", Path: "user-settings-malformed.json", Isolatable: true}
	doc := readScopeFixture(t, "user-settings-malformed.json")

	finding, err := claudecode.InspectScope(scope, doc)
	if err == nil {
		t.Fatalf("InspectScope: want error for a malformed document, got nil (finding=%+v)", finding)
	}
	if finding.RewritesInput {
		t.Errorf("InspectScope: a malformed document must not be reported as RewritesInput = true; an error path must not also carry a positive finding")
	}
}

// TestInspectScopes_AbsentScopeIsNotReportedAsClean asserts that a scope
// the probe reports as absent (ErrScopeAbsent) is never folded into the
// findings as an ordinary "inspected, no rewrite" result — a scope that was
// never inspected must never read downstream as "inspected and found
// clean". Either it is omitted from the findings, or it is present with a
// Detail explaining that it could not be read.
func TestInspectScopes_AbsentScopeIsNotReportedAsClean(t *testing.T) {
	probe := fixtureScopeProbe{docs: map[string][]byte{}} // every scope absent
	adapter := claudecode.New(claudecode.Options{ScopeProbe: probe})

	findings, err := adapter.InspectScopes(testContext())
	if err != nil {
		t.Fatalf("InspectScopes: %v", err)
	}

	for _, f := range findings {
		if f.Scope.Name == "user" && !f.RewritesInput && f.Detail == "" {
			t.Errorf("InspectScopes: absent scope %q was reported as clean with no explanation; an absent scope must never look like a clean inspection", f.Scope.Name)
		}
	}
}

// TestInspectScopes_RefusesOnCompetingRewriteHook asserts that a competing
// outside-scope hook causes InspectScopes to report a rewriting, unresolved
// finding rather than letting setup proceed silently.
func TestInspectScopes_RefusesOnCompetingRewriteHook(t *testing.T) {
	dir := t.TempDir()
	probe := fixtureScopeProbe{
		docs: map[string][]byte{
			"user": readScopeFixture(t, "user-settings-with-rewriter.json"),
		},
	}
	adapter := claudecode.New(claudecode.Options{ScopeProbe: probe})
	_ = newSandbox(t, dir)

	findings, err := adapter.InspectScopes(testContext())
	if err != nil {
		t.Fatalf("InspectScopes: %v", err)
	}

	var sawUnresolvedRewrite bool
	for _, f := range findings {
		if f.Scope.Name == "user" && f.RewritesInput && !f.Neutralized {
			sawUnresolvedRewrite = true
			if f.Detail == "" {
				t.Errorf("InspectScopes: un-neutralized rewriting finding for %q has empty Detail", f.Scope.Name)
			}
		}
	}
	if !sawUnresolvedRewrite {
		t.Errorf("InspectScopes: no un-neutralized rewriting finding reported for a user scope with a competing PreToolUse hook; findings=%+v", findings)
	}
}

// TestScopeProbe_NeverWritesOutsideSandbox asserts the structural property
// that ScopeProbe is read-only: the interface offers no write method, so a
// well-typed probe cannot write outside the sandbox by construction. This
// test exists so the obligation stated in the port's doc comment has a
// compile-time-checkable expression, not just prose.
func TestScopeProbe_NeverWritesOutsideSandbox(t *testing.T) {
	var _ claudecode.ScopeProbe = fixtureScopeProbe{}
	// fixtureScopeProbe (below) implements only Read; if ScopeProbe ever
	// grows a write method, this file fails to compile until every probe
	// implementation, including this fixture, supplies one under review.
}

// fixtureScopeProbe is a test-only ScopeProbe backed by an in-memory map,
// so scope inspection is testable with fixture documents rather than the
// real filesystem.
type fixtureScopeProbe struct {
	docs map[string][]byte
}

func (p fixtureScopeProbe) Read(scope domain.ConfigScope) ([]byte, error) {
	doc, ok := p.docs[scope.Name]
	if !ok {
		return nil, claudecode.ErrScopeAbsent
	}
	return doc, nil
}
