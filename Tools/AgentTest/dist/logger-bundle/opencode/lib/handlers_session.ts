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
import type { SessionCorrelationStore, ClosedSessionRegistry } from "./correlation.js";

// ---------------------------------------------------------------------------
// Shared OpenCode hook-input types
// (Exported so plugin.ts and other handler modules can use them)
// ---------------------------------------------------------------------------

// --- SDK message shapes (client.session.messages) ---

/** Role discriminator on a message info object. */
export type MessageRole = "user" | "assistant";

/** Token counters on an assistant message. All fields unverified against a live install. */
export interface MessageTokens {
  input?: number;
  output?: number;
  reasoning?: number;
  cache?: { read?: number; write?: number };
  [key: string]: unknown;
}

/**
 * The `info` half of a message entry. Assistant-only fields are optional.
 * All fields declared here are shape hints for readers and tests; field-name drift
 * must degrade to absent data, never to a crash.
 */
export interface MessageInfo {
  id?: string;
  role?: MessageRole | string;
  sessionID?: string;
  parentID?: string;
  cost?: number;
  modelID?: string;
  providerID?: string;
  mode?: string;
  tokens?: MessageTokens;
  time?: { created?: number; completed?: number; [key: string]: unknown };
  [key: string]: unknown;
}

/**
 * One entry of a message part. `type` discriminates a 10-member union
 * (text | reasoning | file | tool | step-start | step-finish | snapshot | patch | agent | retry).
 * Only `text` parts carry a `text` field. There is no `content` field at any level.
 */
export interface MessagePart {
  id?: string;
  messageID?: string;
  sessionID?: string;
  type?: string;
  text?: string;
  name?: string;   // AgentPart
  source?: unknown; // AgentPart
  [key: string]: unknown;
}

/** One element of the array returned by client.session.messages(). */
export interface MessageEntry {
  info?: MessageInfo;
  parts?: MessagePart[];
  [key: string]: unknown;
}

// --- Bus event shapes (the `event` hook) ---

/** Session object carried under properties.info for created/updated/deleted. */
export interface SessionInfo {
  id?: string;
  parentID?: string;
  [key: string]: unknown;
}

/** Generic OpenCode bus event. Access all fields defensively. */
export interface OpenCodeEvent {
  type: string;
  properties?: Record<string, unknown>;
  [key: string]: unknown;
}

/** Input to tool.execute.before. */
export interface ToolBeforeInput {
  tool: string;
  sessionID: string;
  callID?: string;
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
  callID?: string;
  args?: Record<string, unknown>;
  [key: string]: unknown;
}

/** The after-hook's second parameter. Shape unconfirmed beyond "an object". */
export interface ToolAfterOutput {
  [key: string]: unknown;
}

/** permission.ask payload. Shape unconfirmed; every field optional by contract. */
export interface PermissionAskInput {
  sessionID?: string;
  type?: string;
  title?: string;
  metadata?: Record<string, unknown>;
  [key: string]: unknown;
}

/** experimental.session.compacting payload. Experimental hook; shape unconfirmed. */
export interface SessionCompactingInput {
  sessionID?: string;
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
// InvocationCallbacks
// ---------------------------------------------------------------------------

/**
 * Late-bound invocation entry points supplied by the plugin factory.
 *
 * Injected into the session handler so a `session.created` or `session.idle`
 * event for a subagent can drive invocation start/end without the session
 * handler importing the invocation module directly (which would create an
 * import cycle). Wired by the plugin factory after both handler sets are
 * constructed.
 */
export interface InvocationCallbacks {
  /** Handle a subagent session creation — emits invocation_start. */
  handleInvocationStart: (sessionId: string, parentId: string) => Promise<void>;
  /** Handle a subagent session going idle — emits invocation_end + artifacts. */
  handleInvocationEnd: (sessionId: string) => Promise<void>;
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

  /**
   * NEW — durable closed-session record. Consulted on every session close to
   * prevent a second session_end/run_end pair after a process restart.
   * Optional for backward compatibility with tests that do not supply it;
   * when absent the handler falls back to in-memory-only deduplication.
   */
  closedSessions?: ClosedSessionRegistry;

  /**
   * NEW — invocation lifecycle callbacks, injected by the plugin factory after
   * both handler sets are constructed (late binding via object mutation). The
   * session handler accesses this via the `deps` reference, not a destructured
   * copy, so the factory can assign it after `createSessionHandlers` returns.
   *
   * Optional: handler modules must treat this as possibly absent (for test
   * isolation) and no-op when it is undefined.
   */
  invocationCallbacks?: InvocationCallbacks;

  /**
   * Close an open `task` dispatch call from a signal other than the after-hook.
   * Injected by the plugin factory after `createToolHandlers` returns (late binding
   * via object mutation, same pattern as invocationCallbacks). Called by
   * `handleInvocationEnd` so that a subagent's end event synthesizes the closing
   * `tool_call_end` for the dispatching `task` call when the real after-hook was
   * never delivered.
   *
   * Optional: invocation handlers must treat this as possibly absent (for test
   * isolation) and no-op when it is undefined.
   */
  closeDispatchCall?: (childSessionId: string) => Promise<void>;
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
   * in the correlation store and calls invocationCallbacks.handleInvocationStart.
   * For session.idle on subagent sessions, calls invocationCallbacks.handleInvocationEnd.
   */
  handleEvent: (event: OpenCodeEvent) => Promise<void>;
} {
  const { store, paths, sdkClient, adapterVersion, buildEvent, appendEvent } = deps;

  /** Emit session_end + run_end for a top-level session. Idempotent. */
  async function emitSessionEnd(sessionId: string, reason?: string): Promise<void> {
    // Step 1: in-memory short-circuit (synchronous — guards concurrent same-process calls).
    if (store.isEnded(sessionId)) return;

    // Step 2: claim the close synchronously so a second concurrent call that
    // hasn't reached its first await yet will see isEnded = true and bail out.
    // This is the critical ordering: markEnded runs before any await so it is
    // always atomic with respect to the in-memory check above.
    store.markEnded(sessionId);

    // Step 3: durable check — if a previous process already closed this session,
    // the marker file will exist and we suppress the duplicate close pair.
    const registry = deps.closedSessions;
    if (registry) {
      const alreadyClosed = await registry.isClosed(sessionId);
      if (alreadyClosed) return;
    }

    // Step 4: emit the close pair.
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

    // Step 5: persist the durable marker so a future process restart won't
    // emit a second close pair. Never throws; write failure only means
    // durability is lost, not that the close itself is blocked.
    if (registry) {
      await registry.markClosed(sessionId);
    }
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

          if (!parentId) {
            // Top-level (orchestrator) session: emit session_start + run_start
            await handleOrchestratorSessionCreated(sessionId);
          } else {
            // Subagent session: drive invocation start via the late-bound callbacks.
            // deps.invocationCallbacks is set by the plugin factory after all handler
            // sets are constructed; the session handler reads it via the deps reference
            // (not a destructured copy) so the late assignment is visible here.
            await deps.invocationCallbacks?.handleInvocationStart(sessionId, parentId);
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

      case "session.idle": {
        // session.idle fires when a session's agent loop goes quiet — this is the
        // replacement signal for the nonexistent stop hook.
        // Route by session kind: subagents go to invocation-end handling; top-level
        // sessions (and unknown sessions that default to non-subagent) go to session end.
        if (sessionId) {
          if (store.isSubagentSession(sessionId)) {
            // Subagent: drive invocation end via callbacks. Guard with isEnded so
            // repeated idle events for the same session are no-ops after the first.
            if (!store.isEnded(sessionId)) {
              await deps.invocationCallbacks?.handleInvocationEnd(sessionId);
            }
          } else {
            // Top-level (or unknown) session: emit session_end + run_end
            await emitSessionEnd(sessionId, type);
          }
        }
        break;
      }

      case "session.deleted": {
        // session.deleted is a different lifecycle moment (record removed, not idle).
        // Route to session end for top-level sessions only — it is retained as a
        // second closing signal in case idle never fires for the top-level session.
        // Subagent session.deleted must NOT trigger invocation-end handling: that
        // path belongs exclusively to session.idle.
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

  return { handleEvent };
}
