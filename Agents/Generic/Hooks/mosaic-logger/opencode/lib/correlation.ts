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

import { effectiveRunId } from "./core.js";
import type { LogPaths } from "./core.js";

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
  // Tool call_id correlation
  // -----------------------------------------------------------------------

  /**
   * Store a pending tool call_id for a session (per-session single-slot).
   * Called by handleToolBefore so handleToolAfter can retrieve the same
   * value. Overwrites any previously stored call_id for the session.
   */
  storePendingCallId(sessionId: string, callId: string): void {
    this._pendingCallIds.set(sessionId, callId);
  }

  /**
   * Retrieve and clear the pending tool call_id for a session.
   * Returns undefined if no call_id has been stored for this session.
   */
  popPendingCallId(sessionId: string): string | undefined {
    const callId = this._pendingCallIds.get(sessionId);
    this._pendingCallIds.delete(sessionId);
    return callId;
  }

  /**
   * Mark a session as ended. Idempotent. Used to prevent duplicate
   * invocation_end / session_end events.
   */
  markEnded(sessionId: string): void {
    const record = this._sessions.get(sessionId);
    if (record) {
      record.ended = true;
    }
    // For unknown sessions — silently ignore (idempotent, never throw)
  }

  /** Check if a session has already been marked as ended. */
  isEnded(sessionId: string): boolean {
    const record = this._sessions.get(sessionId);
    if (!record) return false;
    return record.ended;
  }
}
