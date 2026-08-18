/**
 * opencode_api.ts — Shared fixture builders for OpenCode API payloads.
 *
 * Every test that constructs an OpenCode message, session event, or tool-hook
 * payload must build it through the functions in this module. This ensures that
 * when a real-install check reveals a field-name difference, the correction lands
 * in one place rather than in every test file.
 *
 * All shapes follow the REAL OpenCode SDK and bus shapes, not the imagined ones:
 *   - Messages are `{ info: MessageInfo, parts: MessagePart[] }[]`
 *   - There is NO `content` field anywhere; text lives only in parts with type "text"
 *   - Role lives at `entry.info.role`, not at `entry.role`
 *   - session.idle carries ONLY `properties.sessionID` — no `properties.info`
 */

import type {
  MessageEntry,
  MessageInfo,
  MessagePart,
  OpenCodeEvent,
  ToolBeforeInput,
  ToolBeforeOutput,
  ToolAfterInput,
  ToolAfterOutput,
  PermissionAskInput,
  SessionCompactingInput,
} from "../../lib/handlers_session";

// ---------------------------------------------------------------------------
// Message entry builders
// ---------------------------------------------------------------------------

/**
 * Build a MessageEntry for a user-role message. `text` may be a single string
 * (one text part) or an array of strings (multiple text parts, concatenated by
 * the extraction helpers). Non-text parts are NOT included unless the caller
 * builds them separately and merges.
 */
export function userMessageEntry(
  text: string | string[],
  overrides?: Partial<MessageInfo>,
): MessageEntry {
  const texts = Array.isArray(text) ? text : [text];
  const parts: MessagePart[] = texts.map((t, i) => ({
    id: `part-user-${i}`,
    messageID: "msg-user",
    sessionID: "sess-fixture",
    type: "text",
    text: t,
  }));
  return {
    info: {
      id: "msg-user",
      role: "user",
      sessionID: "sess-fixture",
      ...overrides,
    },
    parts,
  };
}

/**
 * Build a MessageEntry for an assistant-role message. `text` may be a single
 * string (one text part) or an array of strings (multiple text parts).
 */
export function assistantMessageEntry(
  text: string | string[],
  overrides?: Partial<MessageInfo>,
): MessageEntry {
  const texts = Array.isArray(text) ? text : [text];
  const parts: MessagePart[] = texts.map((t, i) => ({
    id: `part-asst-${i}`,
    messageID: "msg-assistant",
    sessionID: "sess-fixture",
    type: "text",
    text: t,
  }));
  return {
    info: {
      id: "msg-assistant",
      role: "assistant",
      sessionID: "sess-fixture",
      ...overrides,
    },
    parts,
  };
}

/**
 * Build a MessageEntry with an empty parts array, so extractTextFromParts
 * returns undefined and there are no part-level payloads of any kind.
 * Use this fixture rather than an inline object literal whenever a test
 * needs a zero-parts edge case, so the shape stays synchronized here if
 * the MessageEntry contract ever changes.
 */
export function emptyPartsMessageEntry(
  overrides?: Partial<MessageInfo>,
): MessageEntry {
  return {
    info: {
      id: "msg-empty",
      role: "user",
      sessionID: "sess-fixture",
      ...overrides,
    },
    parts: [],
  };
}

/**
 * Build a MessageEntry whose parts contain ONLY non-text types, so that
 * extractTextFromParts returns undefined for it. The default part types are
 * ["reasoning", "tool", "step-start"] — representative non-text members of
 * OpenCode's 10-member part union.
 */
export function nonTextMessageEntry(partTypes?: string[]): MessageEntry {
  const types = partTypes ?? ["reasoning", "tool", "step-start"];
  const parts: MessagePart[] = types.map((t, i) => ({
    id: `part-nontext-${i}`,
    messageID: "msg-nontext",
    sessionID: "sess-fixture",
    type: t,
    // Deliberately NO `text` field — these are non-text part types
  }));
  return {
    info: {
      id: "msg-nontext",
      role: "assistant",
      sessionID: "sess-fixture",
    },
    parts,
  };
}

/**
 * Build a MessageEntry with a mix of text parts and non-text parts interleaved.
 * The caller controls the part sequence. Text parts carry the provided text;
 * non-text parts carry only `type`.
 *
 * Example: interleavedMessageEntry("user", ["reasoning", "Hello", "step-start", "World"])
 * produces parts: [reasoning, text("Hello"), step-start, text("World")]
 */
export function interleavedMessageEntry(
  role: "user" | "assistant",
  sequence: string[],
): MessageEntry {
  const textTypes = new Set(["text"]);
  const nonTextTypes = new Set([
    "reasoning", "file", "tool", "step-start", "step-finish",
    "snapshot", "patch", "agent", "retry",
  ]);

  const parts: MessagePart[] = sequence.map((item, i) => {
    if (nonTextTypes.has(item)) {
      return {
        id: `part-${i}`,
        messageID: "msg-interleaved",
        sessionID: "sess-fixture",
        type: item,
      };
    }
    // Treat anything not recognised as a non-text type as text content
    if (textTypes.has(item) || !nonTextTypes.has(item)) {
      return {
        id: `part-${i}`,
        messageID: "msg-interleaved",
        sessionID: "sess-fixture",
        type: "text",
        text: item,
      };
    }
    return {
      id: `part-${i}`,
      messageID: "msg-interleaved",
      sessionID: "sess-fixture",
      type: item,
    };
  });

  return {
    info: {
      id: "msg-interleaved",
      role,
      sessionID: "sess-fixture",
    },
    parts,
  };
}

// ---------------------------------------------------------------------------
// Session bus event builders
// ---------------------------------------------------------------------------

/**
 * Build a `session.created` bus event. Uses the real nested shape:
 *   { type: "session.created", properties: { info: { id, parentID? } } }
 * When `parentId` is provided the resulting event represents a subagent session.
 */
export function sessionCreatedEvent(id: string, parentId?: string): OpenCodeEvent {
  return {
    type: "session.created",
    properties: {
      info: {
        id,
        ...(parentId !== undefined ? { parentID: parentId } : {}),
      },
    },
  };
}

/**
 * Build a `session.updated` bus event. Uses the real nested shape:
 *   { type: "session.updated", properties: { info: { id } } }
 */
export function sessionUpdatedEvent(id: string): OpenCodeEvent {
  return {
    type: "session.updated",
    properties: {
      info: { id },
    },
  };
}

/**
 * Build a `session.deleted` bus event. Uses the real nested shape:
 *   { type: "session.deleted", properties: { info: { id } } }
 */
export function sessionDeletedEvent(id: string): OpenCodeEvent {
  return {
    type: "session.deleted",
    properties: {
      info: { id },
    },
  };
}

/**
 * Build a `session.idle` bus event.
 *
 * IMPORTANT ASYMMETRY: `session.idle` is structurally different from the other
 * three session event types. It carries ONLY `properties.sessionID` as a bare
 * string and has NO `properties.info` object at all.
 *
 *   { type: "session.idle", properties: { sessionID: id } }
 *
 * Code that assumes `properties.info` is always present will silently do nothing
 * for this event type. This fixture makes that asymmetry explicit and testable.
 */
export function sessionIdleEvent(id: string): OpenCodeEvent {
  return {
    type: "session.idle",
    properties: {
      sessionID: id,
      // Deliberately NO `info` key — this is the real session.idle shape
    },
  };
}

/**
 * Build a `message.updated` bus event wrapping the given message entry.
 * The entry's `info` and `parts` are surfaced under `properties`.
 */
export function messageUpdatedEvent(entry: MessageEntry): OpenCodeEvent {
  return {
    type: "message.updated",
    properties: {
      info: entry.info,
      parts: entry.parts,
    },
  };
}

// ---------------------------------------------------------------------------
// Tool hook payload builders
// ---------------------------------------------------------------------------

/**
 * Build a [ToolBeforeInput, ToolBeforeOutput] payload pair for tool.execute.before.
 * `output.args` is the mutable args object the hook may read.
 */
export function toolBeforePayload(params: {
  tool: string;
  sessionID: string;
  callID?: string;
  args?: Record<string, unknown>;
}): [ToolBeforeInput, ToolBeforeOutput] {
  const input: ToolBeforeInput = {
    tool: params.tool,
    sessionID: params.sessionID,
    ...(params.callID !== undefined ? { callID: params.callID } : {}),
  };
  const output: ToolBeforeOutput = {
    args: params.args ?? {},
  };
  return [input, output];
}

/**
 * Build a [ToolAfterInput, ToolAfterOutput] payload pair for tool.execute.after.
 * The output shape is unconfirmed beyond "an object"; callers may inject any fields.
 */
export function toolAfterPayload(params: {
  tool: string;
  sessionID: string;
  callID?: string;
  args?: Record<string, unknown>;
  output?: Record<string, unknown>;
}): [ToolAfterInput, ToolAfterOutput] {
  const input: ToolAfterInput = {
    tool: params.tool,
    sessionID: params.sessionID,
    ...(params.callID !== undefined ? { callID: params.callID } : {}),
    ...(params.args !== undefined ? { args: params.args } : {}),
  };
  const output: ToolAfterOutput = params.output ?? {};
  return [input, output];
}

/**
 * Build a realistic `task` dispatch payload for tool.execute.before.
 * The dispatch prompt is a JSON object containing `agent_instance_id` and `run_id`
 * in the structured key-value form that `extractInstanceId` / `extractRunId` already
 * match. This is the primary identity source introduced in Stage 1.
 */
export function taskDispatchPayload(params: {
  sessionID: string;
  callID?: string;
  agentInstanceId?: string;
  runId?: string;
  subagentType?: string;
}): [ToolBeforeInput, ToolBeforeOutput] {
  const agentInstanceId = params.agentInstanceId ?? "TestAgent#1";
  const runId = params.runId ?? "20260101T000000Z-0000";
  const prompt = JSON.stringify({
    agent_instance_id: agentInstanceId,
    run_id: runId,
    task_description: "Test task",
  });
  const input: ToolBeforeInput = {
    tool: "task",
    sessionID: params.sessionID,
    ...(params.callID !== undefined ? { callID: params.callID } : {}),
  };
  const output: ToolBeforeOutput = {
    args: {
      prompt,
      ...(params.subagentType !== undefined
        ? { subagent_type: params.subagentType }
        : {}),
    },
  };
  return [input, output];
}

// ---------------------------------------------------------------------------
// Notification / compaction payload builders
// ---------------------------------------------------------------------------

/**
 * Build a PermissionAskInput payload. All fields optional; callers supply only
 * what a specific test scenario needs.
 */
export function permissionAskPayload(
  overrides?: Partial<PermissionAskInput>,
): PermissionAskInput {
  return {
    sessionID: "sess-fixture",
    type: "permission_request",
    ...overrides,
  };
}

/**
 * Build a SessionCompactingInput payload. The hook is experimental; all fields
 * are optional by design.
 */
export function sessionCompactingPayload(
  overrides?: Partial<SessionCompactingInput>,
): SessionCompactingInput {
  return {
    sessionID: "sess-fixture",
    ...overrides,
  };
}
