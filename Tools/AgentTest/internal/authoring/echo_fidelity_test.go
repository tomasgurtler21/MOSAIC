package authoring_test

// Tests for YAML deserialization of the echo_fidelity field in both test
// definitions and suite defaults, following the three-level settings
// precedence (suite defaults -> entry override -> definition settings).

import (
	"testing"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
)

// --- Test definition deserialization ---

func TestParseTestDefinition_EchoFidelity_Advisory_Parsed(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: subagent
echo_fidelity: advisory
subject:
  identity: researcher
  agent: researcher
  invocation_kind: subagent
`),
	}

	def, report := authoring.ParseTestDefinition(src)

	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if def.Settings.EchoFidelity == nil {
		t.Fatal("Settings.EchoFidelity is nil, want pointer to advisory")
	}
	if *def.Settings.EchoFidelity != domain.EchoFidelityAdvisory {
		t.Errorf("Settings.EchoFidelity = %q, want %q", *def.Settings.EchoFidelity, domain.EchoFidelityAdvisory)
	}
}

func TestParseTestDefinition_EchoFidelity_Required_Parsed(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: subagent
echo_fidelity: required
subject:
  identity: researcher
  agent: researcher
  invocation_kind: subagent
`),
	}

	def, report := authoring.ParseTestDefinition(src)

	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if def.Settings.EchoFidelity == nil {
		t.Fatal("Settings.EchoFidelity is nil, want pointer to required")
	}
	if *def.Settings.EchoFidelity != domain.EchoFidelityRequired {
		t.Errorf("Settings.EchoFidelity = %q, want %q", *def.Settings.EchoFidelity, domain.EchoFidelityRequired)
	}
}

func TestParseTestDefinition_EchoFidelity_Absent_IsNil(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: subagent
subject:
  identity: researcher
  agent: researcher
  invocation_kind: subagent
`),
	}

	def, report := authoring.ParseTestDefinition(src)

	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if def.Settings.EchoFidelity != nil {
		t.Errorf("Settings.EchoFidelity = %v, want nil when field is absent", def.Settings.EchoFidelity)
	}
}

// TestParseTestDefinition_EchoFidelity_NotRejectedByUnknownFieldCheck verifies
// that echo_fidelity is in the known-fields allowlist, so it does not trigger
// an unknown-field diagnostic.
func TestParseTestDefinition_EchoFidelity_NotRejectedByUnknownFieldCheck(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: subagent
echo_fidelity: advisory
subject:
  identity: researcher
  agent: researcher
  invocation_kind: subagent
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	for _, d := range report.Diagnostics {
		if d.Code == "unknown-field" {
			t.Errorf("unexpected unknown-field diagnostic for echo_fidelity: %+v", d)
		}
	}
}

// --- Suite defaults deserialization ---

func TestParseSuite_EchoFidelity_InDefaults_Parsed(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
id: s
defaults:
  echo_fidelity: advisory
tests:
  - path: tests/a.test.yaml
`),
	}

	suite, report := authoring.ParseSuite(src)

	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if suite.Defaults.EchoFidelity == nil {
		t.Fatal("Defaults.EchoFidelity is nil, want pointer to advisory")
	}
	if *suite.Defaults.EchoFidelity != domain.EchoFidelityAdvisory {
		t.Errorf("Defaults.EchoFidelity = %q, want %q", *suite.Defaults.EchoFidelity, domain.EchoFidelityAdvisory)
	}
}

func TestParseSuite_EchoFidelity_Absent_IsNil(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
id: s
defaults:
  turn_limit: 10
tests:
  - path: tests/a.test.yaml
`),
	}

	suite, report := authoring.ParseSuite(src)

	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if suite.Defaults.EchoFidelity != nil {
		t.Errorf("Defaults.EchoFidelity = %v, want nil when field is absent", suite.Defaults.EchoFidelity)
	}
}

// TestParseSuite_EchoFidelity_EntryOverridesDefault verifies that a suite
// entry can declare its own echo_fidelity, independently of the suite default.
func TestParseSuite_EchoFidelity_EntryOverridesDefault(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
id: s
defaults:
  echo_fidelity: advisory
tests:
  - path: tests/a.test.yaml
  - path: tests/b.test.yaml
    echo_fidelity: required
`),
	}

	suite, report := authoring.ParseSuite(src)

	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if len(suite.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(suite.Entries))
	}

	// Entry 0 does not override; its per-entry settings should have nil EchoFidelity.
	if suite.Entries[0].Settings.EchoFidelity != nil {
		t.Errorf("Entries[0].Settings.EchoFidelity = %v, want nil (not overridden)", suite.Entries[0].Settings.EchoFidelity)
	}

	// Entry 1 overrides with "required".
	if suite.Entries[1].Settings.EchoFidelity == nil {
		t.Fatal("Entries[1].Settings.EchoFidelity is nil, want pointer to required")
	}
	if *suite.Entries[1].Settings.EchoFidelity != domain.EchoFidelityRequired {
		t.Errorf("Entries[1].Settings.EchoFidelity = %q, want %q", *suite.Entries[1].Settings.EchoFidelity, domain.EchoFidelityRequired)
	}
}

// TestParseSuite_EchoFidelity_NotRejectedByUnknownFieldCheck verifies that
// echo_fidelity in suite defaults does not trigger an unknown-field diagnostic.
func TestParseSuite_EchoFidelity_NotRejectedByUnknownFieldCheck(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
id: s
defaults:
  echo_fidelity: advisory
tests:
  - path: tests/a.test.yaml
`),
	}

	_, report := authoring.ParseSuite(src)

	for _, d := range report.Diagnostics {
		if d.Code == "unknown-field" {
			t.Errorf("unexpected unknown-field diagnostic for echo_fidelity in suite defaults: %+v", d)
		}
	}
}

// TestParseSuite_EchoFidelity_EntryLevel_NotRejectedByUnknownFieldCheck
// verifies that echo_fidelity on a suite entry (not in suite defaults) does
// not produce an unknown-field diagnostic. The test is analogous to
// TestParseSuite_EchoFidelity_NotRejectedByUnknownFieldCheck for suite
// defaults; having an explicit guard at the entry level documents that the
// allowlist covers both locations.
func TestParseSuite_EchoFidelity_EntryLevel_NotRejectedByUnknownFieldCheck(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
id: s
tests:
  - path: tests/a.test.yaml
    echo_fidelity: required
`),
	}

	_, report := authoring.ParseSuite(src)

	for _, d := range report.Diagnostics {
		if d.Code == "unknown-field" {
			t.Errorf("unexpected unknown-field diagnostic for echo_fidelity on suite entry: %+v", d)
		}
	}
}

// TestParseTestDefinition_EchoFidelity_InvalidValue_StoredAsLiteral documents
// the deliberate no-validation design: an unrecognized echo_fidelity value is
// stored as the literal string without producing an unknown-field diagnostic or
// coercing the pointer to nil. The field is a *string, so YAML deserialization
// accepts any string literal. At evaluation time, the unrecognized value will
// not match domain.EchoFidelityAdvisory, so it falls through to behave as
// required — the stricter default — without any special handling.
func TestParseTestDefinition_EchoFidelity_InvalidValue_StoredAsLiteral(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: subagent
echo_fidelity: typo-value
subject:
  identity: researcher
  agent: researcher
  invocation_kind: subagent
`),
	}

	def, report := authoring.ParseTestDefinition(src)

	// Must not produce an unknown-field diagnostic for echo_fidelity; the key
	// is in the known-fields allowlist regardless of its value.
	for _, d := range report.Diagnostics {
		if d.Code == "unknown-field" {
			t.Errorf("unexpected unknown-field diagnostic for echo_fidelity with unrecognized value: %+v", d)
		}
	}

	// The value must be stored as a non-nil pointer to the literal string, not
	// coerced to nil. A future implementer adding validation here would break
	// this test, surfacing the design decision explicitly.
	if def.Settings.EchoFidelity == nil {
		t.Fatal("Settings.EchoFidelity is nil; want non-nil pointer to literal \"typo-value\" " +
			"(no validation, no coercion — unrecognized values behave as required at evaluation time)")
	}
	if *def.Settings.EchoFidelity != "typo-value" {
		t.Errorf("Settings.EchoFidelity = %q, want %q; "+
			"unrecognized value must be stored as-is without coercion",
			*def.Settings.EchoFidelity, "typo-value")
	}
}
