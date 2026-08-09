package opencode

import (
	"errors"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
)

// DecodeEnvelope maps a completed invocation of the OpenCode CLI onto the
// subject's result. It is an exported package-level function rather than a
// method because the neutral launcher (internal/launch) consumes it as a
// value.
//
// The single load-bearing difference from the Claude Code decoder: a zero
// exit code carries no success/failure information for this harness —
// `opencode run` exits 0 even when the run failed. The err == nil branch
// therefore always parses the event stream before deciding anything, and a
// stream that itself reports failure, or ends without a terminal event, maps
// to DispositionSpawnFailed rather than DispositionCompleted.
//
// No turn-limit disposition arm exists: the documented event stream
// distinguishes no such condition, so none is invented here.
//
// DispositionEarlyExit is never produced here: early exit is observed by the
// supervisor that cancelled the run, not by the envelope.
//
// This function never returns a non-nil error: every failure degrades to a
// disposition.
func DecodeEnvelope(res commonharness.Response, err error) (domain.SubjectResult, error) {
	if err != nil {
		return decodeFailure(res, err), nil
	}

	text, perr := commonharness.ParseOpenCodeEnvelope(res.Stdout)
	if perr != nil {
		// A stream-reported failure (ErrOpenCodeStreamError) and an
		// incomplete stream (ErrOpenCodeStreamIncomplete) both land here:
		// neither is completion, and the exit code gave no signal either
		// way.
		return domain.SubjectResult{
			Disposition: domain.DispositionSpawnFailed,
			RawOutput:   string(res.Stdout),
			Duration:    res.Duration,
		}, nil
	}

	protocolMessage := text
	if protocol, perr := commonharness.ExtractProtocolJSON(text); perr == nil {
		protocolMessage = string(protocol)
	}

	return domain.SubjectResult{
		Disposition:     domain.DispositionCompleted,
		ProtocolMessage: protocolMessage,
		RawOutput:       string(res.Stdout),
		Duration:        res.Duration,
	}, nil
}

// decodeFailure classifies a non-nil error from the shared spawning module.
// Every arm degrades to a disposition rather than propagating the error, per
// DecodeEnvelope's own contract.
func decodeFailure(res commonharness.Response, err error) domain.SubjectResult {
	switch {
	case errors.Is(err, commonharness.ErrTimeout):
		return domain.SubjectResult{Disposition: domain.DispositionTimedOut, Duration: res.Duration}
	case errors.Is(err, commonharness.ErrExecutableNotFound):
		return domain.SubjectResult{Disposition: domain.DispositionSpawnFailed}
	default:
		// Every other error this function may be handed — a non-zero exit,
		// a parent-context cancellation, or an error this function was not
		// written to expect — degrades to the same safe disposition: the
		// run cannot be trusted as completed, but it is not the
		// supervisor-observed early exit either.
		return domain.SubjectResult{
			Disposition: domain.DispositionSpawnFailed,
			RawOutput:   string(res.Stdout),
			Duration:    res.Duration,
		}
	}
}
