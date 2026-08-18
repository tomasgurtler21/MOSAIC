/**
 * correlation.ts — In-memory session correlation store.
 *
 * Replaces the Claude Code adapter's on-disk pending-dispatch queue with an
 * in-memory Map appropriate to OpenCode's persistent in-process plugin model.
 *
 * Safe under async concurrency: JavaScript's single-threaded event loop
 * ensures Map operations are atomic between await points. The store does
 * not introduce shared mutable state patterns that could break under
 * microtask interleaving.
 *
 * Ordering assumption: OpenCode's session.created fires before any other
 * events for that session, so the store will be populated before
 * tool/lifecycle events reference the session.
 */

import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import { effectiveRunId, sanitizeComponent, debugLog, HARNESS, atomicReplaceSync } from "./core.js";
import type { LogPaths } from "./core.js";

// ---------------------------------------------------------------------------
// PendingDispatch
// ---------------------------------------------------------------------------

/**
 * Identity captured at `task` tool dispatch time, held pending until the
 * subagent session created under the dispatching parent claims it.
 *
 * Held as a per-parent FIFO queue so concurrent dispatches (orchestrator fires
 * several subagents before any session.created arrives) are attributed in order.
 */
export interface PendingDispatch {
  /** Native callID of the `task` tool call that carried this dispatch. */
  callId: string;
  /** Session that issued the dispatch (the orchestrator or a parent subagent). */
  parentSessionId: string;
  agentInstanceId?: string;
  agentType?: string;
  runId?: string;
  prompt?: string;
  dispatchedAt: string;
}

// ---------------------------------------------------------------------------
// PendingToolCall
// ---------------------------------------------------------------------------

/**
 * An in-flight tool call registered when tool.execute.before fires. Keyed by
 * the native OpenCode callID so parallel tool calls in one session each correlate
 * their start and end events by call id. Replaces the single-slot-per-session
 * pending call id, which cannot survive parallel tool use.
 */
export interface PendingToolCall {
  /** Native OpenCode callID where available, else a minted fallback. Registry key. */
  callId: string;
  sessionId: string;
  toolName: string;
  /** True for `task` dispatches, which may need synthetic closure. */
  isDispatch: boolean;
  startedAt: string;
}

// ---------------------------------------------------------------------------
// SessionRecord
// ---------------------------------------------------------------------------

/**
 * Per-session state tracked in the correlation store.
 * All fields except sessionId are optional and populated incrementally
 * as events for the session are observed.
 */
export interface SessionRecord {
  /** OpenCode's native session identifier */
  sessionId: string;

  /** Parent session ID, when this is a subagent session */
  parentId?: string;

  /** Extracted MOSAIC agent instance ID (e.g. "Research#1") */
  agentInstanceId?: string;

  /** Extracted MOSAIC run_id */
  runId?: string;

  /** The dispatch prompt text sent to this subagent */
  prompt?: string;

  /** Agent type (bare name, e.g. "Research") */
  agentType?: string;

  /** Whether this session has been marked as ended */
  ended: boolean;

  /** Timestamp when the session was first observed */
  createdAt: string;

  /** callID of the `task` dispatch that spawned this session, when known. */
  dispatchCallId?: string;

  /** True once invocation_start has been emitted, so invocation_end can stay symmetric. */
  invocationStarted?: boolean;
}

// ---------------------------------------------------------------------------
// SessionCorrelationStore
// ---------------------------------------------------------------------------

/**
 * In-memory store that tracks OpenCode sessions and their parent/child
 * relationships, replacing the Claude Code adapter's on-disk pending-dispatch
 * queue and agent-mapping store.
 */
export class SessionCorrelationStore {
  /** Primary session records store */
  private readonly _sessions = new Map<string, SessionRecord>();

  /** Per-session pending tool call_id (single-slot) */
  private readonly _pendingCallIds = new Map<string, string>();

  /** Per-parent FIFO queue of pending dispatch identities. */
  private readonly _pendingDispatches = new Map<string, PendingDispatch[]>();

  /**
   * Per-session set of message ids for which a turn has already been emitted.
   * Scoped by session so the set is bounded and can be dropped when a session ends,
   * preventing unbounded growth across many sessions in a long-lived process.
   */
  private readonly _seenTurns = new Map<string, Set<string>>();

  /**
   * Per-stream map of record_id → 0-based ordinal, tracking assistant record order.
   * Keyed by the resolved stream path (sink) rather than by session id so that
   * multiple sessions sharing the same physical stream file (e.g. two different
   * unknown/unregistered sessions both routing to the same degradation bucket file)
   * share one counter and produce a single incrementing sequence, preserving the
   * "in file order" contract.
   */
  private readonly _usageRecordOrdinals = new Map<string, Map<string, number>>();

  /**
   * Call-id-keyed registry of in-flight tool calls.
   * Replaces the single-slot per-session approach so parallel tool calls in one
   * session each correlate their start and end by native call id. Keyed by
   * entry.callId; entries are removed by closeToolCall on first closure.
   */
  private readonly _pendingToolCallRegistry = new Map<string, PendingToolCall>();

  /**
   * Register a session. Called on session.created / session.updated events.
   * If the session already exists, merges new fields using these rules:
   *   - Any key present in `record` overwrites the stored value (even if
   *     the new value is undefined).
   *   - Keys absent from `record` are left untouched.
   */
  register(sessionId: string, record: Partial<SessionRecord>): void {
    const existing = this._sessions.get(sessionId);
    if (existing) {
      // Merge: only keys present in `record` are applied.
      for (const key of Object.keys(record) as Array<keyof SessionRecord>) {
        (existing as Record<string, unknown>)[key] = record[key];
      }
    } else {
      // New record. Ensure required fields have defaults.
      const now = new Date().toISOString();
      const newRecord: SessionRecord = {
        sessionId,
        ended: false,
        createdAt: now,
        ...record,
      };
      this._sessions.set(sessionId, newRecord);
    }
  }

  /**
   * Retrieve the full record for a session. Returns undefined for unknown sessions.
   */
  get(sessionId: string): SessionRecord | undefined {
    return this._sessions.get(sessionId);
  }

  /**
   * Determine whether a session is a subagent (has a parentID) or an
   * orchestrator (top-level, no parentID). Returns false for unknown sessions.
   */
  isSubagentSession(sessionId: string): boolean {
    const record = this._sessions.get(sessionId);
    if (!record) return false;
    return record.parentId !== undefined;
  }

  /**
   * Return the orchestrator (root) session ID for any session in the chain.
   * Walks the in-memory parent chain. Returns the input sessionId itself
   * if it has no parent (i.e., it is the orchestrator session).
   * Returns undefined only if the sessionId is completely unknown.
   */
  resolveOrchestratorSession(sessionId: string): string | undefined {
    const visited = new Set<string>();
    let current = sessionId;
    while (true) {
      if (visited.has(current)) {
        // Cycle detected — break at current node
        return current;
      }
      visited.add(current);
      const record = this._sessions.get(current);
      if (!record) {
        // Unknown session
        if (current === sessionId) return undefined;
        // We walked into an unknown parent — return the last known node
        return current;
      }
      if (!record.parentId) {
        // This is the root (orchestrator) session
        return current;
      }
      current = record.parentId;
    }
  }

  /**
   * Walk the parent chain using in-memory data first, falling back to the
   * SDK client for sessions not yet seen. The SDK lookup callback is
   * provided by the caller (the plugin factory wires it to client.session.get).
   *
   * Returns the root (orchestrator) session ID, or the input sessionId
   * if no parent can be resolved.
   */
  async resolveOrchestratorSessionAsync(
    sessionId: string,
    sdkLookup: (sessionId: string) => Promise<{ parentID?: string } | undefined>,
  ): Promise<string> {
    const visited = new Set<string>();
    let current = sessionId;

    while (true) {
      if (visited.has(current)) {
        // Cycle detected
        return current;
      }
      visited.add(current);

      // Try in-memory first
      const record = this._sessions.get(current);
      if (record !== undefined) {
        if (!record.parentId) {
          // Root session found in memory
          return current;
        }
        current = record.parentId;
        continue;
      }

      // Not in memory — try SDK fallback
      try {
        const info = await sdkLookup(current);
        if (!info) {
          // SDK returned nothing — treat current as root
          return current;
        }
        if (!info.parentID) {
          // This session has no parent — it is root
          return current;
        }
        current = info.parentID;
      } catch {
        // SDK threw — treat current as root
        return current;
      }
    }
  }

  /**
   * Get the run_id associated with the orchestrator session for a given
   * session (resolves the chain first). Returns undefined if no run_id
   * has been extracted for the orchestrator session.
   */
  getRunId(sessionId: string): string | undefined {
    const orchestratorId = this.resolveOrchestratorSession(sessionId);
    if (!orchestratorId) return undefined;
    const record = this._sessions.get(orchestratorId);
    return record?.runId;
  }

  /**
   * Get the event file path where events for this session should be routed.
   *
   * Resolution logic:
   *   1. Resolve the run_id for this session's orchestrator chain via
   *      getRunId(). Apply effectiveRunId() so missing run_id maps to
   *      "unknown-run".
   *   2. For orchestrator sessions (no parentId): return
   *      paths.orchestratorEvents(effectiveRun).
   *   3. For subagent sessions (has parentId): use the session's stored
   *      agentInstanceId. If not yet known, fall back to
   *      "unmapped_{sessionId}" as the directory name.
   *   4. For completely unknown sessions: treat as orchestrator and return
   *      paths.orchestratorEvents("unknown-run").
   */
  resolveEventSink(sessionId: string, paths: LogPaths): string {
    const record = this._sessions.get(sessionId);

    // Unknown session — degrade to orchestrator stream under unknown-run
    if (!record) {
      return paths.orchestratorEvents(effectiveRunId(undefined));
    }

    const runId = effectiveRunId(this.getRunId(sessionId));

    // Orchestrator session
    if (!record.parentId) {
      return paths.orchestratorEvents(runId);
    }

    // Subagent session
    const agentInstanceId = record.agentInstanceId ?? `unmapped_${sessionId}`;
    return paths.invocationEvents(runId, agentInstanceId);
  }

  // -----------------------------------------------------------------------
  // run-id adoption — propagates a discovered run id to the chain head
  // -----------------------------------------------------------------------

  /**
   * Record a run id against the orchestrator session at the head of this
   * session's parent chain, so every session in the chain resolves the same
   * run. No-op when the chain head is unknown or already carries a run id.
   */
  adoptRunId(sessionId: string, runId: string): void {
    const head = this.resolveOrchestratorSession(sessionId);
    if (!head) return;
    const record = this._sessions.get(head);
    if (!record) return;
    if (record.runId !== undefined) return; // already set — preserve existing value
    record.runId = runId;
  }

  // -----------------------------------------------------------------------
  // Pending dispatch queue (per-parent FIFO)
  // -----------------------------------------------------------------------

  /**
   * Push a pending dispatch identity to the tail of the per-parent queue.
   * Called by handleToolBefore when the tool is "task".
   */
  pushPendingDispatch(parentSessionId: string, dispatch: PendingDispatch): void {
    let queue = this._pendingDispatches.get(parentSessionId);
    if (!queue) {
      queue = [];
      this._pendingDispatches.set(parentSessionId, queue);
    }
    queue.push(dispatch);
  }

  /**
   * Remove and return the oldest unclaimed dispatch for this parent (FIFO).
   * Returns undefined when no pending dispatch exists for the parent.
   */
  claimPendingDispatch(parentSessionId: string): PendingDispatch | undefined {
    const queue = this._pendingDispatches.get(parentSessionId);
    if (!queue || queue.length === 0) return undefined;
    return queue.shift();
  }

  /**
   * Return the number of unclaimed dispatches for a parent.
   * Diagnostic and test aid; carries no side effects.
   */
  pendingDispatchCount(parentSessionId: string): number {
    return this._pendingDispatches.get(parentSessionId)?.length ?? 0;
  }

  /**
   * Discard all pending dispatches for a parent. Called when the parent
   * session record is dropped so a dispatch whose subagent never materialises
   * cannot leak beyond the parent's lifetime.
   */
  clearPendingDispatches(parentSessionId: string): void {
    this._pendingDispatches.delete(parentSessionId);
  }

  // -----------------------------------------------------------------------
  // Tool call_id correlation (legacy single-slot — preserved for existing tests)
  // -----------------------------------------------------------------------

  /**
   * Store a pending tool call_id for a session (per-session single-slot).
   * Called by handleToolBefore so handleToolAfter can retrieve the same
   * value. Overwrites any previously stored call_id for the session.
   *
   * @deprecated Superseded by the call-id-keyed registry below, which
   * supports parallel tool calls. Retained so existing callers compile.
   */
  storePendingCallId(sessionId: string, callId: string): void {
    this._pendingCallIds.set(sessionId, callId);
  }

  /**
   * Retrieve and clear the pending tool call_id for a session.
   * Returns undefined if no call_id has been stored for this session.
   *
   * @deprecated See storePendingCallId.
   */
  popPendingCallId(sessionId: string): string | undefined {
    const callId = this._pendingCallIds.get(sessionId);
    this._pendingCallIds.delete(sessionId);
    return callId;
  }

  // -----------------------------------------------------------------------
  // PendingToolCall registry (keyed by native call id)
  //
  // Replaces the single-slot approach above. Keying by the native callID
  // lets parallel tool calls in one session each correlate their start and
  // end by call id and gives dispatch closure an idempotent handle usable
  // from either the real after-hook or the synthetic invocation-end path.
  // -----------------------------------------------------------------------

  /**
   * Register an in-flight tool call. Keyed by entry.callId.
   * Called by handleToolBefore for every tool call start.
   * Re-registering an existing callId overwrites the previous entry (edge case:
   * re-use of a call id after its prior close).
   */
  registerToolCall(entry: PendingToolCall): void {
    this._pendingToolCallRegistry.set(entry.callId, entry);
  }

  /**
   * Close a tool call exactly once. Returns the entry on the first call for
   * a given call id and undefined on every subsequent call, making closure
   * idempotent across the real-after-hook and synthetic-closure paths.
   */
  closeToolCall(callId: string): PendingToolCall | undefined {
    const entry = this._pendingToolCallRegistry.get(callId);
    if (!entry) return undefined;
    this._pendingToolCallRegistry.delete(callId);
    return entry;
  }

  /**
   * Inspect without closing. Returns undefined once the call has been closed.
   */
  peekToolCall(callId: string): PendingToolCall | undefined {
    return this._pendingToolCallRegistry.get(callId);
  }

  /**
   * Close the first open tool call for a session that matches the given tool name,
   * or any tool call for the session when toolName is undefined. Used by
   * handleToolAfter when no native callID is present so correlation is maintained
   * through the keyed registry rather than the per-session single-slot.
   *
   * For parallel calls to the same tool with no native callID, FIFO insertion order
   * determines which entry is claimed — the limitation is inherent in having no
   * per-call identifier. Parallel calls WITH a native callID are unaffected.
   */
  closeToolCallBySession(
    sessionId: string,
    toolName?: string,
  ): PendingToolCall | undefined {
    for (const [callId, entry] of this._pendingToolCallRegistry) {
      if (entry.sessionId !== sessionId) continue;
      if (toolName !== undefined && entry.toolName !== toolName) continue;
      this._pendingToolCallRegistry.delete(callId);
      return entry;
    }
    return undefined;
  }

  /**
   * The still-open `task` dispatch call that spawned this subagent session,
   * if any. Used by closeDispatchCall to find the matching pending tool call.
   * Returns undefined when no open dispatch is associated with this child session,
   * or when the dispatch call has already been closed (real or synthetic closure
   * already won the race).
   */
  openDispatchCallFor(childSessionId: string): PendingToolCall | undefined {
    const record = this._sessions.get(childSessionId);
    if (!record?.dispatchCallId) return undefined;
    return this._pendingToolCallRegistry.get(record.dispatchCallId);
  }

  // -----------------------------------------------------------------------
  // Turn de-duplication
  // -----------------------------------------------------------------------

  /**
   * Claim turn emission for a (sessionId, messageId) pair. Returns true the
   * first time a given message id is seen within a session, recording it so
   * subsequent observations return false. This ensures repeated `message.updated`
   * bus firings for the same message emit exactly one turn event.
   *
   * Scoped per session so the set is bounded. The session's entry is removed
   * by markEnded so memory does not grow without bound across many sessions
   * in a long-lived OpenCode process.
   */
  claimTurnEmission(sessionId: string, messageId: string): boolean {
    let sessionTurns = this._seenTurns.get(sessionId);
    if (!sessionTurns) {
      sessionTurns = new Set<string>();
      this._seenTurns.set(sessionId, sessionTurns);
    }
    if (sessionTurns.has(messageId)) return false;
    sessionTurns.add(messageId);
    return true;
  }

  // -----------------------------------------------------------------------
  // Usage record ordinals
  // -----------------------------------------------------------------------

  /**
   * Return the 0-based ordinal of a usage record among assistant records on the
   * resolved stream file, in first-observation order. Re-observing the same
   * `recordId` returns the same index, so the supersede rule preserves a stable
   * ordinal across multiple observations of the same record.
   *
   * `streamKey` must be the resolved sink path (i.e. the result of
   * `resolveEventSink` for the session that produced the record). Callers should
   * resolve the sink once and pass it here rather than passing a session id,
   * because multiple session ids can route to the same physical stream file (the
   * degradation-bucket scenario) and the ordinal must be sequential within the
   * file, not within each session separately.
   *
   * The ordinal is a diagnostic and ordering aid only; it carries no attribution
   * or totalling weight.
   */
  usageRecordIndex(streamKey: string, recordId: string): number {
    let streamOrdinals = this._usageRecordOrdinals.get(streamKey);
    if (!streamOrdinals) {
      streamOrdinals = new Map<string, number>();
      this._usageRecordOrdinals.set(streamKey, streamOrdinals);
    }
    if (streamOrdinals.has(recordId)) {
      return streamOrdinals.get(recordId)!;
    }
    const index = streamOrdinals.size;
    streamOrdinals.set(recordId, index);
    return index;
  }

  // -----------------------------------------------------------------------
  // Session lifecycle
  // -----------------------------------------------------------------------

  /**
   * Mark a session as ended. Idempotent. Used to prevent duplicate
   * invocation_end / session_end events.
   */
  markEnded(sessionId: string): void {
    const record = this._sessions.get(sessionId);
    if (record) {
      record.ended = true;
    }
    // Drop the per-session turn-dedup set — once a session ends its messages
    // will not be re-observed, and releasing the set prevents unbounded growth.
    this._seenTurns.delete(sessionId);
    // For unknown sessions — silently ignore (idempotent, never throw)
  }

  /** Check if a session has already been marked as ended. */
  isEnded(sessionId: string): boolean {
    const record = this._sessions.get(sessionId);
    if (!record) return false;
    return record.ended;
  }
}

// ---------------------------------------------------------------------------
// ClosedSessionMarker
// ---------------------------------------------------------------------------

/**
 * The on-disk content of a durable session-closure marker.
 * Its mere existence is the signal; the fields are diagnostic only.
 * Written atomically under {root}/.session-ends/{sanitize(sessionId)}.json.
 */
export interface ClosedSessionMarker {
  session_id: string;
  closed_at: string;
  harness: string;
}

// ---------------------------------------------------------------------------
// ClosedSessionRegistry
// ---------------------------------------------------------------------------

/**
 * Abstraction over the durable closed-session record. Two implementations:
 * FileClosedSessionRegistry (production) and MemoryClosedSessionRegistry
 * (test double and degradation target).
 *
 * Never throws from any method. All I/O errors degrade to the documented
 * return values.
 */
export interface ClosedSessionRegistry {
  /**
   * True when a close has already been recorded for this session.
   * Answers false on any read failure (degrade, never throw).
   */
  isClosed(sessionId: string): Promise<boolean>;

  /**
   * Record a close. Idempotent — an already-present record is success, not
   * an error. Returns true on success, false when the record could not be
   * persisted. Never throws.
   */
  markClosed(sessionId: string): Promise<boolean>;
}

// ---------------------------------------------------------------------------
// MemoryClosedSessionRegistry
// ---------------------------------------------------------------------------

/**
 * In-memory implementation of ClosedSessionRegistry.
 * Used as the test double and as the degradation target when file I/O fails.
 */
export class MemoryClosedSessionRegistry implements ClosedSessionRegistry {
  private readonly _closed = new Set<string>();

  async isClosed(sessionId: string): Promise<boolean> {
    return this._closed.has(sessionId);
  }

  async markClosed(sessionId: string): Promise<boolean> {
    this._closed.add(sessionId);
    return true;
  }
}

// ---------------------------------------------------------------------------
// FileClosedSessionRegistry
// ---------------------------------------------------------------------------

/**
 * File-backed implementation of ClosedSessionRegistry, scoped to one log root.
 *
 * Marker placement: {root}/.session-ends/{sanitize(sessionId)}.json
 *
 * The directory is dot-prefixed and lives directly under the logs root —
 * never inside a run folder and never as a non-dot sibling of run folders.
 * Downstream log consumers that skip dot-prefixed directories will never
 * encounter the marker directory as run data.
 *
 * Writes are atomic (temp-file + rename) and the class never throws.
 * Any I/O failure degrades gracefully: isClosed returns false (proceed with
 * the close) and markClosed returns false (durability lost, close still happens).
 *
 * Implementation note: synchronous I/O is used intentionally. The session-close
 * path is low-frequency (once per session end, never on the tool-call path), so
 * blocking the event loop briefly is affordable and avoids multi-tick Promise
 * chains that complicate error-boundary reasoning.
 */
export class FileClosedSessionRegistry implements ClosedSessionRegistry {
  private readonly _stateDir: string;

  constructor(paths: LogPaths) {
    this._stateDir = nodePath.join(paths.root, ".session-ends");
  }

  private _markerPath(sessionId: string): string {
    return nodePath.join(this._stateDir, sanitizeComponent(sessionId) + ".json");
  }

  /**
   * Check whether a marker file exists and contains valid JSON.
   * Returns a pre-resolved Promise so callers that await it see the result
   * in the next microtask without additional I/O-level scheduling delays.
   * Degrades to false on any error — never throws.
   */
  isClosed(sessionId: string): Promise<boolean> {
    try {
      const raw = nodeFs.readFileSync(this._markerPath(sessionId), "utf-8");
      JSON.parse(raw); // validate — corrupt file degrades to false
      return Promise.resolve(true);
    } catch {
      return Promise.resolve(false);
    }
  }

  /**
   * Write a marker file atomically using the shared atomicReplaceSync primitive
   * from core.ts. Returns a pre-resolved Promise so callers see the result in
   * the next microtask. Returns false when the write cannot be persisted (I/O
   * error), but never throws — closure must not be blocked by a registry failure.
   *
   * When atomicReplaceSync returns false (rename failed — common on Windows when
   * the target already exists), an existsSync check distinguishes a concurrent-close
   * race (target written by another process → idempotent success) from a genuine
   * write failure.
   */
  markClosed(sessionId: string): Promise<boolean> {
    try {
      nodeFs.mkdirSync(this._stateDir, { recursive: true });

      const marker: ClosedSessionMarker = {
        session_id: sessionId,
        closed_at: new Date().toISOString(),
        harness: HARNESS,
      };
      const data = Buffer.from(JSON.stringify(marker, null, 2), "utf-8");
      const markerPath = this._markerPath(sessionId);

      const written = atomicReplaceSync(markerPath, data);
      if (!written) {
        // rename failed — treat an existing marker as idempotent success so
        // concurrent processes closing the same session are safe.
        return Promise.resolve(nodeFs.existsSync(markerPath));
      }
      return Promise.resolve(true);
    } catch (err) {
      debugLog(`FileClosedSessionRegistry: markClosed failed for session ${sessionId}`, err);
      return Promise.resolve(false);
    }
  }
}
