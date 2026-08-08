package claudecode

import (
	"encoding/json"
	"errors"

	"mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
)

// turnLimitSubtype is the CLI's --output-format json result entry subtype
// reported when the harness itself stops a run at its own turn limit. It is
// checked ahead of ordinary result decoding because the entry's "result"
// field carries a human-readable message rather than a protocol response,
// so attempting protocol extraction on it first would misclassify a turn
// limit as a malformed completion.
const turnLimitSubtype = "error_max_turns"

// cliResultEntry is the minimal shape read out of the CLI's --output-format
// json array to classify a completed invocation ahead of, or instead of,
// full protocol extraction.
type cliResultEntry struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

// DecodeEnvelope maps a completed invocation of this harness's CLI onto the
// subject's result: its own final protocol message, its raw output, its
// exit disposition and its duration.
//
// It is an exported package-level function rather than a method because the
// neutral launcher (internal/launch) consumes it as a value — that is what
// keeps the launcher free of any harness name while leaving envelope
// knowledge in this adapter's package. The composition root is the only
// place the two meet.
//
// Disposition mapping:
//   - a clean exit with a recoverable protocol message → DispositionCompleted
//   - a timeout from the shared spawning module            → DispositionTimedOut
//   - the harness reporting its turn limit reached          → DispositionTurnLimit
//   - the executable missing, or a failure before the
//     process produced any output                           → DispositionSpawnFailed
//   - any other, unclassified failure                        → DispositionSpawnFailed,
//     a safe degradation rather than a panic or an unclassifiable disposition
//
// DispositionEarlyExit is never produced here: early exit is observed by
// the supervisor that cancelled the run, not by the envelope, and a decoder
// that inferred it would make a deliberately short single-decision test read
// as a fault.
func DecodeEnvelope(res harness.Response, err error) (domain.SubjectResult, error) {
	if err != nil {
		return decodeFailure(res, err), nil
	}

	if isTurnLimitEnvelope(res.Stdout) {
		return domain.SubjectResult{
			Disposition: domain.DispositionTurnLimit,
			RawOutput:   string(res.Stdout),
			Duration:    res.Duration,
		}, nil
	}

	text, perr := harness.ParseClaudeCodeEnvelope(res.Stdout)
	if perr != nil {
		return domain.SubjectResult{
			Disposition: domain.DispositionSpawnFailed,
			RawOutput:   string(res.Stdout),
			Duration:    res.Duration,
		}, nil
	}

	protocolMessage := text
	if protocol, perr := harness.ExtractProtocolJSON(text); perr == nil {
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
// this function's contract: DecodeEnvelope itself never fails.
func decodeFailure(res harness.Response, err error) domain.SubjectResult {
	switch {
	case errors.Is(err, harness.ErrTimeout):
		return domain.SubjectResult{Disposition: domain.DispositionTimedOut, Duration: res.Duration}
	case errors.Is(err, harness.ErrExecutableNotFound):
		return domain.SubjectResult{Disposition: domain.DispositionSpawnFailed}
	default:
		// Every other error this function may be handed — an empty
		// response, a non-zero exit, a parent-context cancellation, or an
		// error this function was not written to expect — degrades to the
		// same safe disposition: the run cannot be trusted as completed,
		// but it is not the supervisor-observed early exit either.
		return domain.SubjectResult{
			Disposition: domain.DispositionSpawnFailed,
			RawOutput:   string(res.Stdout),
			Duration:    res.Duration,
		}
	}
}

// isTurnLimitEnvelope reports whether data is a CLI output-format json array
// containing a result entry reporting the harness's own turn limit.
// Malformed or non-array input reports false rather than erroring; ordinary
// decoding classifies it instead.
func isTurnLimitEnvelope(data []byte) bool {
	var entries []cliResultEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return false
	}
	for _, e := range entries {
		if e.Type == "result" && e.Subtype == turnLimitSubtype {
			return true
		}
	}
	return false
}
