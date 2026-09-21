package codextoml_test

// op_enforcement_test.go covers the Op enforcement rules in the Codex translator's
// encode path. These are the exhaustive unit tests that live here because the
// translator is the raise site; Stage 7 adds seam-level regression tests confirming
// the errors propagate through the pipeline.
//
// The three enforcement rules apply only to the Codex translator. The Markdown
// identity translator accepts any Op value without error (proved by the mirror test
// in the agentformat package test suite).
//
// Rules:
//   (a) Op == OpUnspecified (zero value) yields ErrUnspecifiedOperation.
//   (b) Op == OpCreate with non-nil PriorDeployed yields ErrUnspecifiedOperation
//       (a create must not carry prior bytes; their presence contradicts the operation).
//   (c) Op == OpUpdate with nil PriorDeployed yields ErrMissingPriorBytes
//       (an update without the preservation channel is a contract violation).
//   (d) Op == OpCreate with nil PriorDeployed succeeds (the normal create path).
//   (e) Op == OpUpdate with non-nil PriorDeployed succeeds (the normal update path).

import (
	"errors"
	"testing"

	"mosaic-deploy/internal/agentformat"
)

// minimalCanonicalForOp builds a minimal canonical document suitable for Op enforcement
// tests. It includes name, description and a non-empty body so the encode path reaches
// the Op check without failing for other reasons.
func minimalCanonicalForOp(agentKey string) []byte {
	return makeCanonical(
		"name: "+agentKey+"\ndescription: A test agent.\n",
		"Body text for op enforcement test.\n",
	)
}

// minimalPriorBytes returns a minimal valid Codex TOML byte slice for use as
// PriorDeployed in OpUpdate tests.
func minimalPriorBytes(agentKey string) []byte {
	return []byte("name = \"" + agentKey + "\"\ndescription = \"A test agent.\"\nsandbox_mode = \"read-only\"\ndeveloper_instructions = \"Body text for op enforcement test.\"\n")
}

// TestOpEnforcement_OpUnspecified_ReturnsErrUnspecifiedOperation verifies that
// encoding with Op == OpUnspecified (the zero value of Operation) returns an error
// wrapping ErrUnspecifiedOperation.
func TestOpEnforcement_OpUnspecified_ReturnsErrUnspecifiedOperation(t *testing.T) {
	canonical := minimalCanonicalForOp("op-agent")
	tr := codexTranslator(t)

	ctx := agentformat.ArtifactContext{
		AgentKey:      "op-agent",
		Op:            agentformat.OpUnspecified,
		PriorDeployed: nil,
	}

	_, _, err := tr.Encode(canonical, ctx)
	if err == nil {
		t.Fatal("Encode with OpUnspecified returned nil error; want ErrUnspecifiedOperation")
	}
	if !errors.Is(err, agentformat.ErrUnspecifiedOperation) {
		t.Errorf("Encode with OpUnspecified returned %v; want error wrapping ErrUnspecifiedOperation", err)
	}
}

// TestOpEnforcement_OpCreate_WithPriorBytes_ReturnsErrUnspecifiedOperation verifies
// that Op == OpCreate with non-nil PriorDeployed is rejected with ErrUnspecifiedOperation.
// A create operation must not carry prior bytes; their presence contradicts the stated
// operation.
func TestOpEnforcement_OpCreate_WithPriorBytes_ReturnsErrUnspecifiedOperation(t *testing.T) {
	canonical := minimalCanonicalForOp("op-agent")
	tr := codexTranslator(t)

	ctx := agentformat.ArtifactContext{
		AgentKey:      "op-agent",
		Op:            agentformat.OpCreate,
		PriorDeployed: minimalPriorBytes("op-agent"), // non-nil on a create is the contradiction
	}

	_, _, err := tr.Encode(canonical, ctx)
	if err == nil {
		t.Fatal("Encode with OpCreate + non-nil PriorDeployed returned nil error; want ErrUnspecifiedOperation")
	}
	if !errors.Is(err, agentformat.ErrUnspecifiedOperation) {
		t.Errorf("Encode with OpCreate + non-nil PriorDeployed returned %v; want error wrapping ErrUnspecifiedOperation", err)
	}
}

// TestOpEnforcement_OpUpdate_WithNilPriorBytes_ReturnsErrMissingPriorBytes verifies
// that Op == OpUpdate with nil PriorDeployed is rejected with ErrMissingPriorBytes.
// An update without the preservation channel is a contract violation.
func TestOpEnforcement_OpUpdate_WithNilPriorBytes_ReturnsErrMissingPriorBytes(t *testing.T) {
	canonical := minimalCanonicalForOp("op-agent")
	tr := codexTranslator(t)

	ctx := agentformat.ArtifactContext{
		AgentKey:      "op-agent",
		Op:            agentformat.OpUpdate,
		PriorDeployed: nil, // the contract violation
	}

	_, _, err := tr.Encode(canonical, ctx)
	if err == nil {
		t.Fatal("Encode with OpUpdate + nil PriorDeployed returned nil error; want ErrMissingPriorBytes")
	}
	if !errors.Is(err, agentformat.ErrMissingPriorBytes) {
		t.Errorf("Encode with OpUpdate + nil PriorDeployed returned %v; want error wrapping ErrMissingPriorBytes", err)
	}
}

// TestOpEnforcement_OpCreate_WithNilPriorBytes_Succeeds verifies that Op == OpCreate
// with nil PriorDeployed does not trigger an Op-related error. This is the normal
// create path and must produce valid TOML output.
func TestOpEnforcement_OpCreate_WithNilPriorBytes_Succeeds(t *testing.T) {
	const agentKey = "op-create-agent"
	canonical := minimalCanonicalForOp(agentKey)
	tr := codexTranslator(t)

	ctx := agentformat.ArtifactContext{
		AgentKey:      agentKey,
		Op:            agentformat.OpCreate,
		PriorDeployed: nil,
	}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode with OpCreate + nil PriorDeployed returned error %v; want success", err)
	}
	parseTomlMap(t, out) // must produce parseable TOML
}

// TestOpEnforcement_OpUpdate_WithPriorBytes_Succeeds verifies that Op == OpUpdate
// with non-nil PriorDeployed does not trigger an Op-related error. This is the normal
// update path.
func TestOpEnforcement_OpUpdate_WithPriorBytes_Succeeds(t *testing.T) {
	const agentKey = "op-update-agent"
	canonical := minimalCanonicalForOp(agentKey)
	tr := codexTranslator(t)

	ctx := agentformat.ArtifactContext{
		AgentKey:      agentKey,
		Op:            agentformat.OpUpdate,
		PriorDeployed: minimalPriorBytes(agentKey),
	}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode with OpUpdate + non-nil PriorDeployed returned error %v; want success", err)
	}
	parseTomlMap(t, out) // must produce parseable TOML
}

// TestOpEnforcement_ZeroValueOperation_MatchesOpUnspecified confirms that the zero
// value of the Operation type equals OpUnspecified and therefore triggers the same
// enforcement. This guards against a future refactor that changes the zero value.
func TestOpEnforcement_ZeroValueOperation_MatchesOpUnspecified(t *testing.T) {
	var zeroOp agentformat.Operation
	if zeroOp != agentformat.OpUnspecified {
		t.Errorf("zero value of Operation is %q; want OpUnspecified (%q) -- the enforcement depends on this equality", zeroOp, agentformat.OpUnspecified)
	}
}
