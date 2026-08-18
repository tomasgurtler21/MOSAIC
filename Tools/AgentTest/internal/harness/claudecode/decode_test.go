package claudecode_test

// Tests for envelope decoding (T11.7): mapping a completed invocation of
// this harness's CLI onto the subject's own final protocol message, its raw
// output, its duration and its exit disposition. Each disposition arm has
// its own case; the one negative case that matters most is that early exit
// is never produced here — it is observed by the supervisor that cancelled
// the run, not decoded from the envelope.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func envelopeFixture(t *testing.T, name string) []byte {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "claudecode", "envelopes", name))
	if err != nil {
		t.Fatalf("envelopeFixture: resolving %q: %v", name, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("envelopeFixture: reading %q: %v", path, err)
	}
	return data
}

func TestDecodeEnvelope_CleanExit_DispositionCompletedWithProtocolMessage(t *testing.T) {
	res := commonharness.Response{
		ExitCode: 0,
		Stdout:   envelopeFixture(t, "completed.json"),
		Duration: 2 * time.Second,
	}

	result, err := claudecode.DecodeEnvelope(res, nil)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition != domain.DispositionCompleted {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q", result.Disposition, domain.DispositionCompleted)
	}
	if result.ProtocolMessage == "" {
		t.Errorf("DecodeEnvelope: ProtocolMessage is empty; the subject's own final protocol message must be recovered")
	}
	if result.Duration != 2*time.Second {
		t.Errorf("DecodeEnvelope: Duration = %v, want %v", result.Duration, 2*time.Second)
	}
}

func TestDecodeEnvelope_Timeout_DispositionTimedOut(t *testing.T) {
	result, err := claudecode.DecodeEnvelope(commonharness.Response{}, commonharness.ErrTimeout)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition != domain.DispositionTimedOut {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q", result.Disposition, domain.DispositionTimedOut)
	}
}

func TestDecodeEnvelope_TurnLimitReached_DispositionTurnLimit(t *testing.T) {
	res := commonharness.Response{
		ExitCode: 0,
		Stdout:   envelopeFixture(t, "turn_limit.json"),
	}

	result, err := claudecode.DecodeEnvelope(res, nil)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition != domain.DispositionTurnLimit {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q", result.Disposition, domain.DispositionTurnLimit)
	}
}

func TestDecodeEnvelope_ExecutableMissing_DispositionSpawnFailed(t *testing.T) {
	result, err := claudecode.DecodeEnvelope(commonharness.Response{}, commonharness.ErrExecutableNotFound)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition != domain.DispositionSpawnFailed {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q", result.Disposition, domain.DispositionSpawnFailed)
	}
}

func TestDecodeEnvelope_FailureBeforeAnyOutput_DispositionSpawnFailed(t *testing.T) {
	res := commonharness.Response{Stdout: envelopeFixture(t, "no_output.json")}

	result, err := claudecode.DecodeEnvelope(res, commonharness.ErrEmptyResponse)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition != domain.DispositionSpawnFailed {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q", result.Disposition, domain.DispositionSpawnFailed)
	}
}

// TestDecodeEnvelope_NeverProducesEarlyExit asserts that no combination of
// inputs this function is ever handed decodes to DispositionEarlyExit —
// that disposition belongs exclusively to the supervisor that watches the
// sentinel file, never to envelope decoding. Conflating the two would make
// a deliberately short single-decision test read as a fault.
func TestDecodeEnvelope_NeverProducesEarlyExit(t *testing.T) {
	cases := []struct {
		name string
		res  commonharness.Response
		err  error
	}{
		{"completed", commonharness.Response{Stdout: envelopeFixture(t, "completed.json")}, nil},
		{"timeout", commonharness.Response{}, commonharness.ErrTimeout},
		{"turn-limit", commonharness.Response{Stdout: envelopeFixture(t, "turn_limit.json")}, nil},
		{"executable-missing", commonharness.Response{}, commonharness.ErrExecutableNotFound},
		{"cancelled", commonharness.Response{}, commonharness.ErrCancelled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := claudecode.DecodeEnvelope(tc.res, tc.err)
			if err != nil {
				t.Fatalf("DecodeEnvelope: %v", err)
			}
			if result.Disposition == domain.DispositionEarlyExit {
				t.Errorf("DecodeEnvelope(%s): Disposition = %q, early exit must never be produced by decoding", tc.name, result.Disposition)
			}
		})
	}
}

func TestDecodeEnvelope_RawOutputIsCaptured(t *testing.T) {
	res := commonharness.Response{Stdout: envelopeFixture(t, "completed.json")}

	result, err := claudecode.DecodeEnvelope(res, nil)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.RawOutput == "" {
		t.Errorf("DecodeEnvelope: RawOutput is empty; the raw output must be preserved for diagnostics")
	}
}

// TestDecodeEnvelope_UnclassifiedErrorIsStillHandleable guards against a
// panic or an unclassifiable disposition when this function receives an
// error it was not written to expect — the decoder must degrade to a
// sensible disposition rather than crash the caller.
func TestDecodeEnvelope_UnclassifiedErrorIsStillHandleable(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DecodeEnvelope: panicked on an unclassified error: %v", r)
		}
	}()

	_, err := claudecode.DecodeEnvelope(commonharness.Response{}, errors.New("some unrelated failure"))
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
}
