package claudecode_test

// Tests that a non-zero subject exit carries its exit code and stderr
// through decoding (T8.4). Today decodeFailure degrades every unclassified
// failure to DispositionSpawnFailed carrying only RawOutput; res.ExitCode
// and res.Stderr — both already present on harness.Response — are dropped.
// An authentication or environment failure ("Not logged in") must be
// diagnosable from the decoded domain.SubjectResult alone, without
// reproducing outside the tool.

import (
	"errors"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/harness/claudecode"
)

func TestDecodeEnvelope_CleanExit_CarriesZeroExitCode(t *testing.T) {
	res := commonharness.Response{
		ExitCode: 0,
		Stdout:   envelopeFixture(t, "completed.json"),
	}

	result, err := claudecode.DecodeEnvelope(res, nil)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("DecodeEnvelope: ExitCode = %d, want 0 for a clean exit", result.ExitCode)
	}
	if result.Stderr != "" {
		t.Errorf("DecodeEnvelope: Stderr = %q, want empty for a clean exit with none", result.Stderr)
	}
}

// TestDecodeEnvelope_FailureBeforeAnyOutput_CarriesExitCodeAndStderrThrough
// covers decodeFailure's default arm, which is what an authentication
// failure ("Not logged in") actually hits: the CLI exits non-zero having
// produced no usable output.
func TestDecodeEnvelope_FailureBeforeAnyOutput_CarriesExitCodeAndStderrThrough(t *testing.T) {
	const stderrText = "Not logged in. Run `claude login` to authenticate."
	res := commonharness.Response{
		ExitCode: 1,
		Stdout:   envelopeFixture(t, "no_output.json"),
		Stderr:   stderrText,
	}

	result, err := claudecode.DecodeEnvelope(res, commonharness.ErrEmptyResponse)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("DecodeEnvelope: ExitCode = %d, want 1", result.ExitCode)
	}
	if result.Stderr != stderrText {
		t.Errorf("DecodeEnvelope: Stderr = %q, want %q", result.Stderr, stderrText)
	}
}

// TestDecodeEnvelope_UnclassifiedFailure_CarriesExitCodeAndStderrThrough
// covers the same default arm for an error this function was not written
// to expect, mirroring TestDecodeEnvelope_UnclassifiedErrorIsStillHandleable.
func TestDecodeEnvelope_UnclassifiedFailure_CarriesExitCodeAndStderrThrough(t *testing.T) {
	const stderrText = "some unrelated stderr output"
	res := commonharness.Response{
		ExitCode: 127,
		Stderr:   stderrText,
	}

	result, err := claudecode.DecodeEnvelope(res, errors.New("some unrelated failure"))
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if result.ExitCode != 127 {
		t.Errorf("DecodeEnvelope: ExitCode = %d, want 127", result.ExitCode)
	}
	if result.Stderr != stderrText {
		t.Errorf("DecodeEnvelope: Stderr = %q, want %q", result.Stderr, stderrText)
	}
}
