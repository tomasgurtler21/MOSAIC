// Envelope handling for the GHCP CLI lives in its own file, sitting beside
// envelope.go and envelope_opencode.go rather than being forced through either:
// the `copilot -p ... --output-format json` output is a newline-delimited JSON
// event stream, structurally unlike both the Claude Code single-document envelope
// and the OpenCode event stream, and shares nothing with either beyond feeding
// the same ExtractProtocolJSON. The schema is undocumented upstream; what follows
// was captured empirically from copilot CLI v1.0.77 on Windows, and that version
// is noted here so a future schema divergence is diagnosable rather than mysterious.
package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors specific to the GHCP CLI event-stream verdict. See harness.go
// for the shared spawn/parse taxonomy these sit alongside.
var (
	// ErrGHCPCLIStreamError reports that the event stream's terminal result
	// event declared the run failed (non-zero exitCode on the result line).
	// It exists separately from the shared taxonomy because the process exit
	// code is not a trustworthy verdict for this CLI — copilot has been
	// observed to exit 0 after producing no output — so the stream's own
	// result line is the authoritative verdict, and a non-zero value there
	// has no shared sentinel that can express it.
	ErrGHCPCLIStreamError = errors.New("harness: ghcp-cli run reported an error")

	// ErrGHCPCLIStreamIncomplete reports an event stream that ended without a
	// terminal result event, leaving the run's outcome unknown. It exists
	// separately from the shared taxonomy for the same reason: a process exit
	// code of zero is not sufficient for success, so a stream that ends with
	// no result line can never be reported as success, and that condition has
	// no other way to be expressed.
	ErrGHCPCLIStreamIncomplete = errors.New("harness: ghcp-cli event stream ended without a terminal event")
)

// Recognised type discriminators in the GHCP CLI JSONL event stream.
// Only ghcpEventAssistantMessage and ghcpEventResult change the parser's state.
// ghcpEventUserMessage and ghcpEventAssistantMessageDelta are named explicitly
// because they are the two events a future reader is most likely to add
// accumulation for by mistake; naming them makes their deliberate exclusion
// from accumulation explicit. Every other observed type falls through as
// unrecognised, contributing nothing and never producing an error.
const (
	ghcpEventUserMessage           = "user.message"
	ghcpEventAssistantMessage      = "assistant.message"
	ghcpEventAssistantMessageDelta = "assistant.message_delta"
	ghcpEventResult                = "result"
)

// ghcpCLIEventLine decodes one stdout line to the fields the parser acts on.
// Note that ExitCode is top-level, not nested under Data: the terminal result
// event has no data wrapper. Every other observed field — timestamps, ids,
// parent ids, session ids, model names, token counts, request ids, usage
// totals, skill lists, MCP server status — is deliberately left undecoded: a
// field the parser does not read is a field that cannot break it, and this
// schema is undocumented and actively changing.
type ghcpCLIEventLine struct {
	Type     string           `json:"type"`
	Data     ghcpCLIEventData `json:"data"`
	ExitCode int              `json:"exitCode"`
}

type ghcpCLIEventData struct {
	Content string `json:"content"`
}

// ParseGHCPCLIEnvelope reads `copilot -p ... --output-format json` output as a
// newline-delimited JSON event stream and returns the assistant text
// accumulated from its assistant.message events.
//
// stderr and exitCode are diagnostic context only: they are attached to the
// failure paths' error messages and can never produce a success verdict.
func ParseGHCPCLIEnvelope(stdout, stderr []byte, exitCode int) (string, error) {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "", ErrEmptyResponse
	}

	var accumulated strings.Builder
	linesDecoded := 0
	stderrStr := strings.TrimSpace(string(stderr))

	lines := strings.Split(string(stdout), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		var event ghcpCLIEventLine
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// A malformed line is skipped without aborting: it never
			// converts a failure into a success and never produces a
			// terminal verdict of its own.
			continue
		}
		linesDecoded++

		switch event.Type {
		case ghcpEventAssistantMessage:
			accumulated.WriteString(event.Data.Content)

		case ghcpEventResult:
			// The first result line is terminal; subsequent lines are not read.
			if event.ExitCode != 0 {
				return "", fmt.Errorf("%w: stream exit code %d (stderr: %s; process exit code: %d)",
					ErrGHCPCLIStreamError, event.ExitCode, stderrStr, exitCode)
			}
			return accumulated.String(), nil

		case ghcpEventUserMessage, ghcpEventAssistantMessageDelta:
			// Recognised; contributes nothing to the accumulator. user.message
			// also carries data.content (the prompt text); accumulating it would
			// echo the request back as the response. assistant.message_delta
			// streams the response chunk by chunk; the subsequent assistant.message
			// carries the same text complete, so accumulating both would double it.

		default:
			// Unrecognised type: ignored without error.
		}
	}

	if linesDecoded == 0 {
		trimmed := strings.TrimSpace(string(stdout))
		if len(trimmed) > rawOutputCap {
			total := len(trimmed)
			trimmed = trimmed[:rawOutputCap] + fmt.Sprintf("... [truncated, %d bytes total]", total)
		}
		return "", fmt.Errorf("%w: %s", ErrMalformedJSON, trimmed)
	}

	return "", fmt.Errorf("%w (stderr: %s; process exit code: %d)",
		ErrGHCPCLIStreamIncomplete, stderrStr, exitCode)
}
