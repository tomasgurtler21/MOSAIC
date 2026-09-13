/**
 * handlers_notifications.ts — Notification and compaction event handlers.
 *
 * Handles:
 *   - permission.ask hook: emits a `notification` event to the appropriate event sink.
 *   - experimental.session.compacting hook: emits a `compaction` event.
 *
 * Both handlers are observe-only. Neither alters the permission decision nor
 * the compaction behaviour. Both degrade silently on unknown or empty payloads.
 */

import { currentTimestamp, effectiveRunId, debugLog } from "./core.js";
import type {
  HandlerDependencies,
  PermissionAskInput,
  SessionCompactingInput,
} from "./handlers_session.js";

/**
 * Create notification-related handlers: permission prompts and session compaction.
 */
export function createNotificationHandlers(deps: HandlerDependencies): {
  handlePermissionAsk: (input: PermissionAskInput, output?: unknown) => Promise<void>;
  handleSessionCompacting: (input: SessionCompactingInput, output?: unknown) => Promise<void>;
} {
  const { store, paths, buildEvent, appendEvent } = deps;

  async function handlePermissionAsk(
    input: PermissionAskInput,
    _output?: unknown,
  ): Promise<void> {
    const sessionId = typeof input?.sessionID === "string" ? input.sessionID : undefined;
    const ts = currentTimestamp();
    const runId = sessionId ? store.getRunId(sessionId) : undefined;
    const effectiveRun = effectiveRunId(runId);
    const sink = store.resolveEventSink(sessionId ?? "", paths);

    // notification_type is required; derive from payload or fall back to a stable literal
    const notificationType: string =
      typeof input?.type === "string" && input.type.length > 0
        ? input.type
        : "permission_prompt";

    const fields: Record<string, unknown> = {
      notification_type: notificationType,
    };

    // Optional fields — emit only when the payload supplies them
    if (typeof input?.title === "string" && input.title.length > 0) {
      fields["message"] = input.title;
    }

    const requiresResponse = (input as Record<string, unknown>)?.["requires_response"];
    if (typeof requiresResponse === "boolean") {
      fields["requires_response"] = requiresResponse;
    }

    const resolution = (input as Record<string, unknown>)?.["resolution"];
    if (typeof resolution === "string" && resolution.length > 0) {
      fields["resolution"] = resolution;
    }

    try {
      await appendEvent(
        sink,
        buildEvent("notification", { sessionId, runId: effectiveRun, timestamp: ts }, fields),
      );
    } catch (err) {
      debugLog("notification: appendEvent failed", err);
    }
  }

  async function handleSessionCompacting(
    input: SessionCompactingInput,
    _output?: unknown,
  ): Promise<void> {
    const sessionId = typeof input?.sessionID === "string" ? input.sessionID : undefined;
    const ts = currentTimestamp();
    const runId = sessionId ? store.getRunId(sessionId) : undefined;
    const effectiveRun = effectiveRunId(runId);
    const sink = store.resolveEventSink(sessionId ?? "", paths);

    // All compaction fields are optional — emit whichever the payload actually supports
    const fields: Record<string, unknown> = {};

    const trigger = (input as Record<string, unknown>)?.["trigger"];
    if (trigger === "auto" || trigger === "manual") {
      fields["trigger"] = trigger;
    }

    const tokensBefore = (input as Record<string, unknown>)?.["tokens_before"];
    if (typeof tokensBefore === "number") {
      fields["tokens_before"] = tokensBefore;
    }

    const tokensAfter = (input as Record<string, unknown>)?.["tokens_after"];
    if (typeof tokensAfter === "number") {
      fields["tokens_after"] = tokensAfter;
    }

    try {
      await appendEvent(
        sink,
        buildEvent("compaction", { sessionId, runId: effectiveRun, timestamp: ts }, fields),
      );
    } catch (err) {
      debugLog("compaction: appendEvent failed", err);
    }
  }

  return { handlePermissionAsk, handleSessionCompacting };
}
