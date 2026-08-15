package claudecode

// Correlation mechanism for the claude-code adapter.
//
// # Live observation findings (the basis for the shipped mechanism)
//
// The previous mechanism planted a correlation token inside the prompt text at
// the pre-invocation point and attempted to recover it at the completion point
// by reading the transcript file named by the SubagentStop payload's
// agent_transcript_path field. That mechanism failed at runtime for one reason:
// the transcript file does not exist when the SubagentStop hook fires. The path
// is populated in the payload, but the file is written only after the hook
// completes, making every read an immediate not-found error and reducing every
// completion event to an empty correlation token.
//
// # The shipped mechanism
//
// The claude-code harness sends a tool_use_id field on every PreToolUse event
// that uniquely identifies the tool call within the session. The same field is
// echoed on the corresponding PostToolUse event. The SubagentStop event carries
// the same tool_use_id to identify which Task call's subagent has finished.
// This identifier is dispatch-scoped, unguessable, and carries no dictionary
// word, no prefix, and nothing naming this tool: the subject can see the call
// it is handed, and tool_use_id is a vendor-issued opaque identifier that does
// not tip the subject off that it is being exercised.
//
// CorrelationToken is therefore populated directly from the ToolUseID field on
// all three payload types (PreToolUsePayload, PostToolUsePayload,
// CompletionPayload). No token is minted, planted, or recovered from the prompt
// text. PlantToken, RecoverToken, tokenMarker, Options.NewToken and
// Options.TranscriptReader are all retired by this mechanism.
//
// An absent ToolUseID decodes to the zero value and is never an error: a
// completion event with no tool_use_id is a legitimate un-stubbed dispatch and
// produces an empty CorrelationToken, exactly as the per-phase contract allows.
