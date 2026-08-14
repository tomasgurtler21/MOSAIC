package app_test

// render_new_roles_test.go covers the single-file render path's acceptance of the two
// new first-class frontmatter role values: "utility" and "standalone".
//
// Before the vocabulary expansion, ParseAgentRole rejected both values and renderAgent
// returned ErrRenderInvalidRole for any source file declaring them. After the expansion,
// both values are valid — renderAgent must succeed and report the correct role in its
// result, not return an error.
//
// These tests mirror the structure of TestRenderAgent_SubagentRole_ReportedInResult and
// TestRenderAgent_OrchestratorRole_ReportedInResult in render_service_test.go.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// minimalGenericUtilityBytes returns a byte-valid generic-form MOSAIC agent declaring
// `role: utility`. Used to verify that renderAgent accepts "utility" after the vocabulary
// expansion and does not return ErrRenderInvalidRole.
func minimalGenericUtilityBytes() []byte {
	return []byte("---\n" +
		"id: \"50\"\n" +
		"version: \"1.0.0\"\n" +
		"name: render-test-utility\n" +
		"description: A generic-form utility agent for render tests.\n" +
		"role: utility\n" +
		"model: \"{model-identifier}\"\n" +
		"tools: []\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"# RenderTest Utility Agent\n\n" +
		"You are the render test utility agent.\n" +
		"</Identity>\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"</CommunicationProtocol>\n")
}

// minimalGenericStandaloneBytes returns a byte-valid generic-form MOSAIC agent declaring
// `role: standalone`. Used to verify that renderAgent accepts "standalone" after the
// vocabulary expansion and does not return ErrRenderInvalidRole.
func minimalGenericStandaloneBytes() []byte {
	return []byte("---\n" +
		"id: \"51\"\n" +
		"version: \"1.0.0\"\n" +
		"name: render-test-standalone\n" +
		"description: A generic-form standalone agent for render tests.\n" +
		"role: standalone\n" +
		"model: \"{model-identifier}\"\n" +
		"tools: []\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"# RenderTest Standalone Agent\n\n" +
		"You are the render test standalone agent.\n" +
		"</Identity>\n" +
		"<CommunicationProtocol type=\"managed\">\n" +
		"</CommunicationProtocol>\n")
}

// newRenderDepsForNewRoles returns a minimal Deps wired for render tests of the new role
// values. It reuses the render test harness and registry already defined in render_service_test.go.
func newRenderDepsForNewRoles(t *testing.T) app.Deps {
	t.Helper()
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Registry = newRenderRegistry()
	deps.Catalog = newRenderCatalog()
	return deps
}

// ---------------------------------------------------------------------------
// Utility role — render path must accept it without error
// ---------------------------------------------------------------------------

// TestRenderAgent_UtilityRole_ReportedInResult verifies that a source file declaring
// `role: utility` is rendered successfully and reports Role "utility" in the result.
// Before the vocabulary expansion, "utility" was rejected with ErrRenderInvalidRole;
// after the expansion it is a valid first-class frontmatter value.
func TestRenderAgent_UtilityRole_ReportedInResult(t *testing.T) {
	// Arrange
	deps := newRenderDepsForNewRoles(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-utility.md", minimalGenericUtilityBytes())
	req := newPreAnsweredRenderRequest(src, filepath.Join(dir, "out-utility.md"))

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert: no error — "utility" is a recognised role value.
	if errors.Is(err, app.ErrRenderInvalidRole) {
		t.Fatal("RenderAgent returned ErrRenderInvalidRole for role \"utility\"; " +
			"\"utility\" is a valid frontmatter role value and must be accepted")
	}
	if err != nil {
		t.Fatalf("RenderAgent returned unexpected error: %v", err)
	}

	// Assert: the result reports the correct role.
	if result.Role != string(domain.RoleUtility) {
		t.Errorf("Role = %q, want %q", result.Role, string(domain.RoleUtility))
	}
}

// ---------------------------------------------------------------------------
// Standalone role — render path must accept it without error
// ---------------------------------------------------------------------------

// TestRenderAgent_StandaloneRole_ReportedInResult verifies that a source file declaring
// `role: standalone` is rendered successfully and reports Role "standalone" in the result.
// Before the vocabulary expansion, "standalone" was rejected with ErrRenderInvalidRole;
// after the expansion it is a valid first-class frontmatter value.
func TestRenderAgent_StandaloneRole_ReportedInResult(t *testing.T) {
	// Arrange
	deps := newRenderDepsForNewRoles(t)
	svc := app.New(deps)
	dir := t.TempDir()
	src := writeRenderSourceFile(t, dir, "render-test-standalone.md", minimalGenericStandaloneBytes())
	req := newPreAnsweredRenderRequest(src, filepath.Join(dir, "out-standalone.md"))

	// Act
	result, err := svc.RenderAgent(context.Background(), req)

	// Assert: no error — "standalone" is a recognised role value.
	if errors.Is(err, app.ErrRenderInvalidRole) {
		t.Fatal("RenderAgent returned ErrRenderInvalidRole for role \"standalone\"; " +
			"\"standalone\" is a valid frontmatter role value and must be accepted")
	}
	if err != nil {
		t.Fatalf("RenderAgent returned unexpected error: %v", err)
	}

	// Assert: the result reports the correct role.
	if result.Role != string(domain.RoleStandalone) {
		t.Errorf("Role = %q, want %q", result.Role, string(domain.RoleStandalone))
	}
}
