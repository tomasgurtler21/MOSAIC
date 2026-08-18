/**
 * handlers_messages.ts — Turn and usage_record event handlers.
 *
 * Produces two new event types from message-bus `message.updated` activity:
 *   - `turn`: one conversation turn (user or assistant) for the orchestrator or
 *     a subagent. A human prompt to the orchestrator is a `turn` with a user role.
 *   - `usage_record`: one API call's token usage as an independently attributable,
 *     deduplicable unit, carrying a stable record_id so the supersede rule can apply.
 *
 * Design rules enforced here:
 *   - `message.updated` is the single source of truth. The `chat.message` hook is
 *     deliberately not registered in plugin.ts to prevent double-emission.
 *   - Turn de-duplication via `store.claimTurnEmission` so repeated updates for the
 *     same message id emit exactly one turn.
 *   - Re-emitting the same `record_id` as values settle is correct (supersede rule);
 *     consumers total the final observed value per record_id.
 *   - Single-stream invariant: all observations of a given `record_id` route through
 *     `store.resolveEventSink`, so the same record_id can never appear on two streams.
 *   - No SDK calls, transcript reads, or directory scans — this is a higher-frequency
 *     path than lifecycle events.
 */

import type { HandlerDependencies, MessageInfo, OpenCodeEvent } from "./handlers_session.js";
import { currentTimestamp } from "./core.js";
import { extractTextFromParts } from "./handlers_invocation.js";

// ---------------------------------------------------------------------------
// Public factory
// ---------------------------------------------------------------------------

/**
 * Create the message-derived event handlers, closed over the shared dependency bundle.
 * Returns a single handler: `handleMessageEvent`, which is wired to every
 * `message.updated` (and related) bus events routed from the session handler.
 */
export function createMessageHandlers(deps: HandlerDependencies): {
  handleMessageEvent: (event: OpenCodeEvent) => Promise<void>;
} {
  const { store, paths, buildEvent, appendEvent } = deps;

  async function handleMessageEvent(event: OpenCodeEvent): Promise<void> {
    try {
      const props = event?.properties;
      if (!props) return;

      // Extract info and parts from the message.updated bus event.
      // Properties carry `info` (MessageInfo) and `parts` (MessagePart[]) directly.
      const infoRaw = props["info"];
      if (!infoRaw || typeof infoRaw !== "object" || Array.isArray(infoRaw)) return;
      const info = infoRaw as MessageInfo;

      const partsRaw = props["parts"];

      // Extract core identity fields from the message info
      const sessionId = typeof info.sessionID === "string" ? info.sessionID : undefined;
      const messageId = typeof info.id === "string" ? info.id : undefined;
      const role = typeof info.role === "string" ? info.role : undefined;

      // Both sessionId and role are required to produce meaningful events
      if (!sessionId || !role) return;

      // Resolve the event sink once — enforces the single-stream invariant.
      // All events for this message (both turn and usage_record) go to the same stream.
      const sink = store.resolveEventSink(sessionId, paths);

      // Common envelope fields
      const runId = store.getRunId(sessionId);
      const ts = currentTimestamp();

      // Extract text from text-typed parts only; non-text parts and empty-string text
      // parts are silently ignored. Uses the shared helper from handlers_invocation.ts
      // so the text extraction logic has a single source of truth across all call sites.
      const text = extractTextFromParts(partsRaw);

      // --- Turn event ---
      // De-duplicated per message id: only the first observation emits a turn.
      // When no message id is available, de-duplication is not possible so we always emit.
      const shouldEmitTurn = messageId ? store.claimTurnEmission(sessionId, messageId) : true;
      if (shouldEmitTurn) {
        const turnEvent = buildEvent(
          "turn",
          { sessionId, runId, timestamp: ts },
          {
            role,
            text, // undefined when no text parts — buildEvent/prune omits it
          },
        );
        await appendEvent(sink, turnEvent);
      }

      // --- Usage record event ---
      // Only for assistant messages. Re-emitted on every observation so the supersede
      // rule can apply: consumers use the final observed value per record_id.
      if (role === "assistant") {
        // Derive a stable record_id from the harness-native message id.
        // Where absent, fall back to info.time.created (a stable per-message creation
        // timestamp) so that two observations of the same underlying record yield the
        // same id — which is required for the supersede rule to work correctly.
        // Using the current timestamp as a fallback would generate a different id on
        // every call, breaking supersede; info.time.created is set once at record
        // creation and does not change across re-observations of the same message.
        // If even info.time.created is absent, use a session-level sentinel that causes
        // all no-id records in this session to supersede each other (last-writer wins).
        const createdAt = typeof info.time?.created === "number" ? info.time.created : undefined;
        const recordId =
          messageId
          ?? (createdAt !== undefined ? `${sessionId}:t${createdAt}` : `${sessionId}:no-id`);

        // 0-based ordinal among assistant records on this stream, stable across re-observations.
        // Keyed by the resolved sink path (not sessionId) so that multiple sessions sharing
        // one stream file (e.g. the degradation bucket) produce a single sequential ordinal.
        const recordIndex = store.usageRecordIndex(sink, recordId);

        // Source reflects forensic provenance: which transcript type this record came from.
        const isSubagent = store.isSubagentSession(sessionId);
        const source = isSubagent ? "agent_transcript" : "orchestrator_transcript";

        // agent_instance_id is present on invocation streams only, absent on orchestrator.
        const agentInstanceId = isSubagent
          ? store.get(sessionId)?.agentInstanceId
          : undefined;

        // Optional fields — emitted only when resolvable as non-empty strings.
        const model =
          typeof info.modelID === "string" && info.modelID !== ""
            ? info.modelID
            : undefined;

        // PROVISIONAL MAPPING — needs verification against a live OpenCode install.
        // info.mode (e.g. "build", "plan") appears to be OpenCode's agent operating
        // mode, which may differ from the API billing service_tier concept (e.g.
        // "standard"/"priority"/"batch") used by the claude-code adapter's homonymous
        // field. If OpenCode does not expose a billing-tier field, omit service_tier
        // rather than carrying an unrelated value; this mapping is kept here only until
        // the field semantics can be confirmed. Per the design's field-drift policy,
        // "never emit a wrong value" — if mode turns out not to be a billing tier,
        // remove this line and leave service_tier absent.
        const serviceTier =
          typeof info.mode === "string" && info.mode !== "" ? info.mode : undefined;

        // Token usage object — omitted entirely when nothing is resolvable.
        const tokenUsage = buildTokenUsage(info);

        const usageEvent = buildEvent(
          "usage_record",
          { sessionId, runId, timestamp: ts },
          {
            record_id: recordId,
            record_index: recordIndex,
            source,
            agent_instance_id: agentInstanceId, // undefined → omitted by buildEvent
            model,                               // undefined → omitted by buildEvent
            service_tier: serviceTier,           // undefined → omitted by buildEvent
            token_usage: tokenUsage,             // undefined → omitted by buildEvent
          },
        );
        await appendEvent(sink, usageEvent);
      }
    } catch {
      // Never throw into the host — degrade silently on any unexpected error.
    }
  }

  return { handleMessageEvent };
}

// ---------------------------------------------------------------------------
// Exported pure helper (directly testable)
// ---------------------------------------------------------------------------

/**
 * Build the `token_usage` object from an assistant message's `info.tokens` field.
 *
 * Rules:
 *   - Each sub-field is included only when its source value is a genuine number,
 *     including 0 (zero is a real measurement, not "absent").
 *   - Returns `undefined` when no sub-field is resolvable, so the `token_usage`
 *     key is omitted entirely from the emitted event (prune handles the rest).
 *   - Never throws for any input — treats field-name drift as absent data.
 *
 * Sub-field mapping:
 *   - `input_tokens`          ← `info.tokens.input`
 *   - `output_tokens`         ← `info.tokens.output`
 *   - `cache_read_tokens`     ← `info.tokens.cache.read`
 *   - `cache_creation_tokens` ← `info.tokens.cache.write`
 *
 * Exported for direct unit testing.
 */
export function buildTokenUsage(
  info: MessageInfo | undefined,
):
  | {
      input_tokens?: number;
      output_tokens?: number;
      cache_read_tokens?: number;
      cache_creation_tokens?: number;
    }
  | undefined {
  if (!info) return undefined;

  const tokens = info.tokens;
  if (!tokens) return undefined;

  const result: {
    input_tokens?: number;
    output_tokens?: number;
    cache_read_tokens?: number;
    cache_creation_tokens?: number;
  } = {};

  if (typeof tokens.input === "number") {
    result.input_tokens = tokens.input;
  }
  if (typeof tokens.output === "number") {
    result.output_tokens = tokens.output;
  }

  const cache = tokens.cache;
  if (cache && typeof cache === "object" && !Array.isArray(cache)) {
    if (typeof cache.read === "number") {
      result.cache_read_tokens = cache.read;
    }
    if (typeof cache.write === "number") {
      result.cache_creation_tokens = cache.write;
    }
  }

  // Return undefined when no sub-field was resolved — omits the object entirely.
  if (Object.keys(result).length === 0) return undefined;
  return result;
}
