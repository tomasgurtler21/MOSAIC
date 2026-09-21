package main

// translator_imports_test.go verifies that the import-boundary guard enforces
// the translator layer's purity and downward-only dependency direction.
//
// Background: the translator layer (internal/agentformat and its sub-packages)
// must be a pure function with no I/O, no clock, and no randomness, and must
// depend only downward -- it must never import a harness package, the application
// layer, transform, the frontends, or any infrastructure package.
//
// This is an imports-only rule and does not require call-level AST inspection.
// The rules are per-directory (sub-packages are not covered by a parent's rule),
// so the guard carries one entry per agentformat directory:
//   - internal/agentformat
//   - internal/agentformat/codextoml
//   - internal/agentformat/formatid (forbidAllModuleImports, like internal/domain)
//   - internal/agentformat/all      (permits only blank imports of translator sub-packages)
//
// Test strategy: each test passes a small fixture directory (under testdata/) to
// runChecks as its module root and asserts rejection or acceptance outcomes only,
// not the rule table's contents.
//
// The I/O import rejection test fails today because the guard has no rule for
// internal/agentformat. The harness import rejection test may pass today via the
// existing harness-isolation check. The acceptance test passes today because no
// rule fires. These tests are written before the translator-layer rules are added.

import (
	"path/filepath"
	"testing"
)

// TestTranslatorImports_IOImport_IsRejected asserts that a file in
// internal/agentformat that imports an I/O package ("os") is rejected by the
// guard. The translator layer must be a pure function; an I/O import violates
// that purity and must be caught at the guard rather than at code-review.
//
// This test is written before the translator-layer rules are added to the guard
// and fails today because the guard has no rule for internal/agentformat.
func TestTranslatorImports_IOImport_IsRejected(t *testing.T) {
	assertGuardRejectsFixture(t,
		filepath.Join("testdata", "translator_imports", "io_import_in_agentformat"),
		"I/O import (os) in internal/agentformat should be rejected by the translator-layer purity rule",
	)
}

// TestTranslatorImports_HarnessImport_IsRejected asserts that a file in
// internal/agentformat that imports a harness package is rejected by the guard.
// The translator layer must depend only downward; a harness-package import inverts
// the dependency direction and violates the architectural boundary.
//
// Note: the existing harness-isolation check already catches this class of import
// across the whole source tree. This test additionally proves the translator-layer
// direction rule fires for the same scenario once it is added, providing a named
// architectural reason in the violation message.
func TestTranslatorImports_HarnessImport_IsRejected(t *testing.T) {
	assertGuardRejectsFixture(t,
		filepath.Join("testdata", "translator_imports", "harness_import_in_agentformat"),
		"harness-package import in internal/agentformat should be rejected by the translator-layer direction rule",
	)
}

// TestTranslatorImports_PermittedImports_IsAccepted asserts that a file in
// internal/agentformat that uses only the permitted import set is accepted by
// the guard. The permitted set includes internal/domain, internal/agentfields,
// mosaic-common/docformat, and mosaic-common/mosaic.
//
// internal/agentfields is on the permitted list deliberately: it is the
// repository's authority for MOSAIC-owned and stamp key vocabulary, and
// forbidding it would force the translator into a duplicated key list, which is
// the failure this permission exists to prevent.
//
// This test is expected to pass both before and after the translator-layer rules
// are added: no violations are expected for the permitted import set.
func TestTranslatorImports_PermittedImports_IsAccepted(t *testing.T) {
	assertGuardAcceptsFixture(t,
		filepath.Join("testdata", "translator_imports", "permitted_imports"),
		"permitted imports (internal/domain, internal/agentfields, mosaic-common/docformat) in internal/agentformat should be accepted",
	)
}
