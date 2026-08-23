package claudecode

// Correlation mechanism for the claude-code adapter.
//
// # Live observation findings (harness version 2.1.240)
//
// These findings rest on 40 hook firings captured across seven sessions, under
// both the default and a relocated configuration home. Raw capture:
// Tools/AgentTest/dist/hook-payload-capture-20260822-claudecode-2.1.240.jsonl.
// Package-local fixture (one representative entry per hook type):
// testdata/live_payloads_claudecode_2.1.240.jsonl.
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
// # Pre- and post-invocation correlation (tool_use_id)
//
// The claude-code harness sends a tool_use_id field on every PreToolUse event
// that uniquely identifies the tool call within the session. The same field is
// echoed on the corresponding PostToolUse event. This identifier is
// dispatch-scoped, unguessable, and carries no dictionary word, no prefix, and
// nothing naming this tool: the subject can see the call it is handed, and
// tool_use_id is a vendor-issued opaque identifier that does not tip the
// subject off that it is being exercised.
//
// CorrelationToken is populated directly from the ToolUseID field on
// PreToolUsePayload and PostToolUsePayload. No token is minted, planted, or
// recovered from the prompt text. PlantToken, RecoverToken, tokenMarker,
// Options.NewToken and Options.TranscriptReader are all retired by this
// mechanism.
//
// # Completion correlation (agent_id)
//
// SubagentStop does NOT carry tool_use_id. The field is absent from the payload
// entirely — not merely empty — on every observed firing against harness version
// 2.1.240, under both the default and a relocated configuration home. The
// observed key set on SubagentStop is: session_id, transcript_path, cwd,
// prompt_id, permission_mode, agent_id, agent_type, effort, hook_event_name,
// stop_hook_active, agent_transcript_path, last_assistant_message,
// background_tasks, session_crons.
//
// agent_id is the only identifier on SubagentStop that distinguishes one
// dispatch from another. CorrelationToken is therefore left empty on the
// completion phase; the completion event is correlated back to its dispatch
// through the agent_id field, via the agent-start association the
// SubagentStart phase establishes. See the design (ContractsDesign.md,
// section B) for the full mechanism. The association rests on the SubagentStart
// event firing before SubagentStop for the same collaborator, which was
// observed consistently in the live capture.
//
// These are empirical observations on harness 2.1.240, not documented vendor
// contracts. A future harness version that changes the payload shape will be
// caught by the fixture-driven tests in live_capture_test.go.
