package cli_test

// Tests for the merge-metadata extension to EncodeRunTotal.
//
// These tests verify that the JSON wire shape for the basic total-per-run
// contract correctly includes the unknown-run merge signal fields:
//
//   - unknown_run_merged is present in the JSON output when
//     RunTotal.UnknownRunMerged is non-zero.
//   - unknown_run_residual is present in the JSON output when
//     RunTotal.UnknownRunResidual is non-zero.
//   - Both fields are absent from the JSON output when their values are zero
//     (omitempty behaviour: backward compatible for consumers of older builds).
//   - Both fields encode as JSON numbers, not strings.
//   - All pre-existing fields (schema_version, run_id, provisional, currency,
//     tokens, money, complete) are unchanged when the merge fields are present.

import (
	"bytes"
	"encoding/json"
	"testing"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/cli"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// EncodeRunTotal: unknown_run_merged field
// ---------------------------------------------------------------------------

func TestEncodeRunTotal_UnknownRunMerged_PresentWhenNonZero(t *testing.T) {
	// When RunTotal.UnknownRunMerged is greater than zero, the JSON output
	// must contain the "unknown_run_merged" field with the correct count.
	total := app.RunTotal{
		RunID:            cliTestRunID,
		Money:            domain.KnownMoney(domain.Money(1_000_000_000)),
		Complete:         true,
		UnknownRunMerged: 7,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	merged, ok := m["unknown_run_merged"]
	if !ok {
		t.Fatalf("unknown_run_merged is absent from output; want it present when UnknownRunMerged = 7\noutput: %s", buf.String())
	}
	if val, _ := merged.(float64); int(val) != 7 {
		t.Errorf("unknown_run_merged = %v, want 7", merged)
	}
}

func TestEncodeRunTotal_UnknownRunMerged_AbsentWhenZero(t *testing.T) {
	// When RunTotal.UnknownRunMerged is zero, "unknown_run_merged" must be
	// absent from the JSON output so that older consumers reading output from
	// a build that did not yet have the merge feature see no change.
	total := app.RunTotal{
		RunID:            cliTestRunID,
		Money:            domain.KnownMoney(domain.Money(1_000_000_000)),
		Complete:         true,
		UnknownRunMerged: 0,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	if _, present := m["unknown_run_merged"]; present {
		t.Errorf("unknown_run_merged is present when UnknownRunMerged = 0; want the key absent (omitempty)")
	}
}

func TestEncodeRunTotal_UnknownRunMerged_IsJSONNumber(t *testing.T) {
	// unknown_run_merged must encode as a JSON number, not as a string.
	total := app.RunTotal{
		RunID:            cliTestRunID,
		Money:            domain.KnownMoney(domain.Money(500_000_000)),
		Complete:         true,
		UnknownRunMerged: 3,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	merged, ok := m["unknown_run_merged"]
	if !ok {
		t.Fatalf("unknown_run_merged is absent from output")
	}
	if _, isNumber := merged.(float64); !isNumber {
		t.Errorf("unknown_run_merged is not a JSON number; got %T = %v", merged, merged)
	}
}

// ---------------------------------------------------------------------------
// EncodeRunTotal: unknown_run_residual field
// ---------------------------------------------------------------------------

func TestEncodeRunTotal_UnknownRunResidual_PresentWhenNonZero(t *testing.T) {
	// When RunTotal.UnknownRunResidual is greater than zero, the JSON output
	// must contain the "unknown_run_residual" field with the correct count.
	total := app.RunTotal{
		RunID:              cliTestRunID,
		Money:              domain.KnownMoney(domain.Money(1_000_000_000)),
		Complete:           true,
		UnknownRunResidual: 2,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	residual, ok := m["unknown_run_residual"]
	if !ok {
		t.Fatalf("unknown_run_residual is absent from output; want it present when UnknownRunResidual = 2\noutput: %s", buf.String())
	}
	if val, _ := residual.(float64); int(val) != 2 {
		t.Errorf("unknown_run_residual = %v, want 2", residual)
	}
}

func TestEncodeRunTotal_UnknownRunResidual_AbsentWhenZero(t *testing.T) {
	// When RunTotal.UnknownRunResidual is zero, "unknown_run_residual" must
	// be absent from the JSON output (omitempty, backward compatible).
	total := app.RunTotal{
		RunID:              cliTestRunID,
		Money:              domain.KnownMoney(domain.Money(1_000_000_000)),
		Complete:           true,
		UnknownRunResidual: 0,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	if _, present := m["unknown_run_residual"]; present {
		t.Errorf("unknown_run_residual is present when UnknownRunResidual = 0; want the key absent (omitempty)")
	}
}

// ---------------------------------------------------------------------------
// EncodeRunTotal: both merge fields together
// ---------------------------------------------------------------------------

func TestEncodeRunTotal_BothMergeFields_PresentWhenBothNonZero(t *testing.T) {
	// When both UnknownRunMerged and UnknownRunResidual are non-zero, both
	// fields must appear in the output with their correct values.
	total := app.RunTotal{
		RunID:              cliTestRunID,
		Money:              domain.KnownMoney(domain.Money(500_000_000)),
		Complete:           true,
		UnknownRunMerged:   10,
		UnknownRunResidual: 3,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	if _, ok := m["unknown_run_merged"]; !ok {
		t.Errorf("unknown_run_merged is absent; want it present when value is 10")
	}
	if _, ok := m["unknown_run_residual"]; !ok {
		t.Errorf("unknown_run_residual is absent; want it present when value is 3")
	}

	if val, _ := m["unknown_run_merged"].(float64); int(val) != 10 {
		t.Errorf("unknown_run_merged = %v, want 10", m["unknown_run_merged"])
	}
	if val, _ := m["unknown_run_residual"].(float64); int(val) != 3 {
		t.Errorf("unknown_run_residual = %v, want 3", m["unknown_run_residual"])
	}
}

func TestEncodeRunTotal_BothMergeFields_AbsentWhenBothZero(t *testing.T) {
	// When both merge fields are zero (the common case: no unknown-run bucket
	// or merge was skipped), neither field must appear in the JSON output.
	// This ensures backward compatibility for consumers that built against
	// an older wire shape.
	total := app.RunTotal{
		RunID:    cliTestRunID,
		Money:    domain.KnownMoney(domain.Money(1_000_000_000)),
		Complete: true,
		// UnknownRunMerged: 0 (zero value)
		// UnknownRunResidual: 0 (zero value)
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	mergeKeys := []string{"unknown_run_merged", "unknown_run_residual"}
	for _, key := range mergeKeys {
		if _, present := m[key]; present {
			t.Errorf("field %q is present when value is 0; want it absent (omitempty for backward compatibility)", key)
		}
	}
}

// ---------------------------------------------------------------------------
// EncodeRunTotal: pre-existing fields unchanged by merge metadata
// ---------------------------------------------------------------------------

func TestEncodeRunTotal_MergeFieldsPresent_PreExistingFieldsUnchanged(t *testing.T) {
	// When merge metadata fields are present, all pre-existing fields must
	// still be in the output with the same names, types, and encoding rules.
	// No pre-existing field may be removed, retyped, or renamed.
	total := app.RunTotal{
		RunID:       cliTestRunID,
		Provisional: false,
		Tokens: domain.TokenUsage{
			Input:  domain.Tokens(1_000_000),
			Output: domain.Tokens(400_000),
		},
		Money:            domain.KnownMoney(domain.Money(2_000_000_000)),
		Complete:         true,
		UnknownRunMerged: 5,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	preExisting := []string{
		"schema_version",
		"run_id",
		"provisional",
		"currency",
		"tokens",
		"money",
		"complete",
	}
	for _, field := range preExisting {
		if _, ok := m[field]; !ok {
			t.Errorf("pre-existing field %q is absent from EncodeRunTotal output when merge fields are also present", field)
		}
	}

	// Spot-check encoding rules for two key pre-existing fields.
	if ver, _ := m["schema_version"].(string); ver != "1" {
		t.Errorf("schema_version = %q, want %q after adding merge fields", ver, "1")
	}
	money, ok := m["money"].(map[string]any)
	if !ok {
		t.Fatalf("money field missing or not an object")
	}
	if state, _ := money["state"].(string); state != "known" {
		t.Errorf("money.state = %q, want %q after adding merge fields", state, "known")
	}
}
