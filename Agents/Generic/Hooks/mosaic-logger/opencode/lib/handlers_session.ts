/**
 * handlers_session.ts — Session/run lifecycle event handlers.
 *
 * Also owns the shared HandlerDependencies interface and hook-input types,
 * since these are needed by all three handler modules.
 *
 * Handles:
 *   - session.created events (via generic event hook): emit session_start + run_start
 *     for top-level sessions; register all sessions in the correlation store
 *   - session end detection (via stop hook or session.deleted/session.idle events):
 *     emit session_end + run_end for top-level sessions
 */

import { currentTimestamp, effectiveRunId } from "./core.js";
import { extractRunId } from "./runstate.js";
import type { LogPaths } from "./core.js";
import type { SessionCorrelationStore } from "./correlation.js";

// ---------------------------------------------------------------------------
// Shared OpenCode hook-input types
// (Exported so plugin.ts and other handler modules can use them)
// ---------------------------------------------------------------------------

/** Generic OpenCode bus event. Access all fields defensively. */
export interface OpenCodeEvent {
  type: string;
  properties?: Record<string, unknown>;
  [key: string]: unknown;
}

/** Input to the stop hook. */
export interface StopInput {
  sessionID: string;
  [key: string]: unknown;
}

/** Input to tool.execute.before. */
export interface ToolBeforeInput {
  tool: string;
  sessionID: string;
  [key: string]: unknown;
}

/** Mutable output to tool.execute.before. */
export interface ToolBeforeOutput {
  args: Record<string, unknown>;
  [key: string]: unknown;
}

/** Input to tool.execute.after. */
export interface ToolAfterInput {
  tool: string;
  sessionID: string;
  args?: Record<string, unknown>;
  [key: string]: unknown;
}

// ---------------------------------------------------------------------------
// Minimal SdkClient type contract
// ---------------------------------------------------------------------------

/** Minimal type contract for the OpenCode SDK client. */
export interface SdkClient {
  session: {
    get(params: { path: { id: string } }): Promise<{ parentID?: string } | undefined>;
    messages(params: { path: { id: string } }): Promise<unknown>;
  };
  app: {
    log(params: {
      body: {
        service: string;
        level: "debug" | "info" | "warn" | "error";
        message: string;
        extra?: Record<string, unknown>;
      };
    }): Promise<void>;
  };
}

// ---------------------------------------------------------------------------
// HandlerDependencies
// ---------------------------------------------------------------------------

/**
 * Shared dependency bundle injected into all handler factory functions.
 * Created once in the plugin factory and passed to each createXxxHandlers call.
 * Enables testing each handler in isolation via dependency injection.
 */
export interface HandlerDependencies {
  /** The in-memory session correlation store */
  store: SessionCorrelationStore;
  /** On-disk path layout, initialized from workspace root */
  paths: LogPaths;
  /** OpenCode SDK client for session data retrieval */
  sdkClient: SdkClient;
  /** Adapter version read from hook.yaml at init time */
  adapterVersion: string | undefined;
  /** Build a complete event object (envelope + per-event fields). */
  buildEvent: (
    event: string,
    envelope: { sessionId?: string; runId?: string; timestamp: string },
    fields: Record<string, unknown>,
  ) => Record<string, unknown>;
  /** Append one JSON event line to a JSONL file. Never throws. */
  appendEvent: (filePath: string, event: Record<string, unknown>) => Promise<void>;
  /** Generate a fallback tool call_id. */
  fallbackCallId: (toolName?: string) => string;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Extract the sessionId from an OpenCode event, trying common field names/locations.
 * OpenCode event payloads may carry the session identifier under different keys.
 * The nested `properties.info` level (OpenCode's real bus payload shape) is checked
 * first; flat levels are retained as fallbacks for backward compatibility.
 *
 * Exported so plugin.ts can reuse this logic without duplicating the field-cascade.
 */
export function extractSessionId(event: OpenCodeEvent): string | undefined {
  const props = event.properties;

  // Resolve properties.info only when it is a plain (non-array) object.
  const infoRaw =
    props != null && typeof props === "object" ? props["info"] : undefined;
  const info: Record<string, unknown> | undefined =
    infoRaw != null && typeof infoRaw === "object" && !Array.isArray(infoRaw)
      ? (infoRaw as Record<string, unknown>)
      : undefined;

  // Nested levels — type-guarded to skip non-string values.
  const infoId = typeof info?.["id"] === "string" ? (info["id"] as string) : undefined;
  const infoSessionID =
    typeof info?.["sessionID"] === "string" ? (info["sessionID"] as string) : undefined;

  return (
    infoId ||
    infoSessionID ||
    (props?.["sessionID"] as string | undefined) ||
    (props?.["sessionId"] as string | undefined) ||
    (props?.["id"] as string | undefined) ||
    (event["sessionID"] as string | undefined) ||
    (event["sessionId"] as string | undefined)
  );
}

/**
 * Extract the parentID from a session event.
 * The nested `properties.info` level (OpenCode's real bus payload shape) is checked
 * first; flat levels are retained as fallbacks for backward compatibility.
 *
 * Exported so plugin.ts can reuse this logic without duplicating the field-cascade.
 */
export function extractParentId(event: OpenCodeEvent): string | undefined {
  const props = event.properties;

  // Resolve properties.info only when it is a plain (non-array) object.
  const infoRaw =
    props != null && typeof props === "object" ? props["info"] : undefined;
  const info: Record<string, unknown> | undefined =
    infoRaw != null && typeof infoRaw === "object" && !Array.isArray(infoRaw)
      ? (infoRaw as Record<string, unknown>)
      : undefined;

  // Nested levels — type-guarded to skip non-string values.
  const infoParentID =
    typeof info?.["parentID"] === "string" ? (info["parentID"] as string) : undefined;
  const infoParentId =
    typeof info?.["parentId"] === "string" ? (info["parentId"] as string) : undefined;

  return (
    infoParentID ||
    infoParentId ||
    (props?.["parentID"] as string | undefined) ||
    (props?.["parentId"] as string | undefined) ||
    (event["parentID"] as string | undefined) ||
    (event["parentId"] as string | undefined)
  );
}

// ---------------------------------------------------------------------------
// Handler factory
// ---------------------------------------------------------------------------

/**
 * Create session/run lifecycle handlers.
 *
 * Returns individual handler functions that the plugin factory wires into
 * the hook registration object.
 */
export function createSessionHandlers(deps: HandlerDependencies): {
  /**
   * Handle a generic event bus event. Dispatches to session.created,
   * session.deleted, session.idle based on event.type.
   * For subagent sessions (parentID present on session.created), registers
   * in the correlation store but defers invocation events to the invocation
   * handler (wired by plugin.ts).
   */
  handleEvent: (event: OpenCodeEvent) => Promise<void>;

  /**
   * Handle the stop hook for an orchestrator (top-level) session.
   * Emits session_end + run_end. Subagent session ends are routed to the
   * invocation handler by plugin.ts before this is called.
   */
  handleStop: (input: StopInput) => Promise<void>;
} {
  const { store, paths, sdkClient, adapterVersion, buildEvent, appendEvent } = deps;

  /** Emit session_end + run_end for a top-level session. Idempotent. */
  async function emitSessionEnd(sessionId: string, reason?: string): Promise<void> {
    if (store.isEnded(sessionId)) return;
    store.markEnded(sessionId);

    const ts = currentTimestamp();
    const runId = store.getRunId(sessionId);
    const effectiveRun = effectiveRunId(runId);
    const sink = paths.orchestratorEvents(effectiveRun);

    await appendEvent(
      sink,
      buildEvent("session_end", { sessionId, runId, timestamp: ts }, { reason }),
    );

    await appendEvent(
      sink,
      buildEvent("run_end", { sessionId, runId, timestamp: ts }, {}),
    );
  }

  /**
   * Handle session.created for an orchestrator (top-level, no parentID) session.
   * Session is already registered in the correlation store by handleEvent.
   */
  async function handleOrchestratorSessionCreated(sessionId: string): Promise<void> {
    // Try to extract run_id from initial session messages (best-effort).
    // This will usually return nothing for a brand-new session, but handles
    // resumed sessions that already have message history.
    let extractedRunId: string | undefined;
    try {
      const messages = await sdkClient.session.messages({ path: { id: sessionId } });
      if (messages !== null && messages !== undefined) {
        extractedRunId = extractRunId(JSON.stringify(messages));
      }
    } catch {
      // SDK failure is non-fatal — run_id will be updated when subagents are dispatched
    }

    if (extractedRunId) {
      store.register(sessionId, { runId: extractedRunId });
    }

    const ts = currentTimestamp();
    const runId = store.getRunId(sessionId);
    const effectiveRun = effectiveRunId(runId);
    const sink = paths.orchestratorEvents(effectiveRun);

    // Emit session_start (session_id is required per MosaicLogFormat.md)
    await appendEvent(
      sink,
      buildEvent(
        "session_start",
        { sessionId, runId, timestamp: ts },
        { session_id: sessionId, adapter_version: adapterVersion },
      ),
    );

    // Emit run_start (run_id is required per MosaicLogFormat.md)
    await appendEvent(
      sink,
      buildEvent("run_start", { sessionId, runId, timestamp: ts }, { adapter_version: adapterVersion }),
    );
  }

  async function handleEvent(event: OpenCodeEvent): Promise<void> {
    const type = event?.type;
    if (!type) return;

    const sessionId = extractSessionId(event);

    switch (type) {
      case "session.created": {
        const parentId = extractParentId(event);
        if (sessionId) {
          // Always register in the correlation store (both orchestrator and subagent sessions).
          // Omit the parentId key entirely when no parent was extracted — passing
          // { parentId: undefined } would overwrite a parentId already stored from an
          // earlier event, which must be preserved.
          const record = parentId !== undefined ? { parentId } : {};
          store.register(sessionId, record);

          // For top-level (orchestrator) sessions: emit session_start + run_start
          // For subagent sessions: invocation handler takes over (wired in plugin.ts)
          if (!parentId) {
            await handleOrchestratorSessionCreated(sessionId);
          }
        }
        break;
      }

      case "session.updated": {
        // A session.updated event is a second opportunity to learn a parentID.
        // Register only when the parentID is present or the session is already known —
        // an updated event for an unseen session without a parentID must not create a
        // parent-less record that would misclassify a subagent as an orchestrator.
        // No lifecycle events are emitted here; session_start/run_start are exclusive
        // to the session.created path.
        if (sessionId) {
          const parentId = extractParentId(event);
          const alreadyKnown = store.get(sessionId) !== undefined;
          if (parentId !== undefined || alreadyKnown) {
            const record = parentId !== undefined ? { parentId } : {};
            store.register(sessionId, record);
          }
        }
        break;
      }

      case "session.deleted":
      case "session.idle": {
        // Only emit end events for orchestrator sessions; subagent ends handled elsewhere
        if (sessionId && !store.isSubagentSession(sessionId)) {
          await emitSessionEnd(sessionId, type);
        }
        break;
      }

      default:
        // All other event types are silently ignored by the session handler
        break;
    }
  }

  async function handleStop(input: StopInput): Promise<void> {
    const sessionId = input?.sessionID;
    if (!sessionId) return;

    // Defensive guard: plugin.ts already routes subagent stop events to
    // handleInvocationEnd before calling this function. This check protects
    // against future mis-wiring where handleStop might be called directly for
    // a subagent session. Under the current plugin.ts routing contract, this
    // branch is never taken in production.
    if (store.isSubagentSession(sessionId)) return;

    await emitSessionEnd(sessionId);
  }

  return { handleEvent, handleStop };
}
