// Envelope handling for the OpenCode CLI lives in its own file, sitting
// beside envelope.go rather than being forced through it: the OpenCode
// `opencode run --format json` output is a newline-delimited JSON event
// stream, structurally unlike the Claude Code CLI's single-document
// envelope, and has nothing in common with it beyond feeding the same
// ExtractProtocolJSON.
package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors specific to the OpenCode event-stream verdict. See
// harness.go for the shared spawn/parse taxonomy these sit alongside.
var (
	// ErrOpenCodeStreamError reports that the event stream itself declared
	// the run failed: an error event, or a terminal step whose reason is
	// neither the success reason nor the intermediate tool-call reason. It
	// exists because the process exit code cannot report this — `opencode
	// run` exits 0 on a failed run.
	ErrOpenCodeStreamError = errors.New("harness: opencode run reported an error")

	// ErrOpenCodeStreamIncomplete reports an event stream that ended without
	// any terminal event. The run may have succeeded, failed, or been cut
	// off mid-flight; nothing in the output distinguishes them, so this is
	// never reported as success.
	ErrOpenCodeStreamIncomplete = errors.New("harness: opencode event stream ended without a terminal event")
)

// Recognised discriminator values in the OpenCode event stream.
const (
	ocEventStepStart  = "step_start"
	ocEventToolUse    = "tool_use"
	ocEventText       = "text"
	ocEventStepFinish = "step_finish"
	ocEventError      = "error"

	ocReasonStop      = "stop"
	ocReasonToolCalls = "tool-calls"
)

// openCodeEventLine is one line of the event stream, decoded to only the
// fields the parser reads. Everything else in a line — timestamps, session
// identifiers, snapshots, costs, token counts — is deliberately not decoded:
// a field the parser does not read is a field that cannot break it.
type openCodeEventLine struct {
	Type  string         `json:"type"`
	Part  openCodePart   `json:"part"`
	Error *openCodeError `json:"error"`
}

type openCodePart struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

type openCodeError struct {
	Name string `json:"name"`
	Data struct {
		Message string `json:"message"`
	} `json:"data"`
}

// ParseOpenCodeEnvelope reads `opencode run --format json` output as a
// newline-delimited JSON event stream and returns the assistant text
// accumulated from its text events.
//
// It never sees, receives or reasons about a process exit code: success and
// failure are derived from the stream's own content.
func ParseOpenCodeEnvelope(data []byte) (string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return "", ErrEmptyResponse
	}

	var accumulated strings.Builder
	linesDecoded := 0

	lines := strings.Split(string(data), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		var event openCodeEventLine
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// A malformed line is skipped without aborting: it never
			// converts a failure into a success and never produces a
			// terminal verdict of its own.
			continue
		}
		linesDecoded++

		switch event.Type {
		case ocEventText:
			accumulated.WriteString(event.Part.Text)

		case ocEventStepFinish:
			switch event.Part.Reason {
			case ocReasonToolCalls:
				// Intermediate step; parsing continues.
			case ocReasonStop:
				return accumulated.String(), nil
			default:
				return "", fmt.Errorf("%w: step_finish reason %q", ErrOpenCodeStreamError, event.Part.Reason)
			}

		case ocEventError:
			name, message := "", ""
			if event.Error != nil {
				name = event.Error.Name
				message = event.Error.Data.Message
			}
			return "", fmt.Errorf("%w: %s: %s", ErrOpenCodeStreamError, name, message)

		case ocEventStepStart, ocEventToolUse:
			// Recognised; contributes nothing to the accumulator.
		}
	}

	if linesDecoded == 0 {
		trimmed := strings.TrimSpace(string(data))
		if len(trimmed) > rawOutputCap {
			total := len(trimmed)
			trimmed = trimmed[:rawOutputCap] + fmt.Sprintf("... [truncated, %d bytes total]", total)
		}
		return "", fmt.Errorf("%w: %s", ErrMalformedJSON, trimmed)
	}

	return "", ErrOpenCodeStreamIncomplete
}
