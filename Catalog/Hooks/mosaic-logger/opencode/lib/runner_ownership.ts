/**
 * runner_ownership.ts — top-level session ownership for Runner mode.
 *
 * Tracks which sessions the Runner drives and registers them in the session
 * store (subagent role: under a synthetic root carrying the run id, so shared
 * handlers route them as a native subagent session; orchestrator role: as a
 * root session that emits session_start).
 */

import { currentTimestamp, effectiveRunId } from "./core.js";
import type { HandlerDependencies } from "./handlers_session.js";
import type { RunnerMode } from "./runner_mode.js";
import { appendEventSync } from "./sync_append.js";

export interface SessionOwnership {
  /** True when the session is Runner-owned. */
  isOwned(sessionId: string): boolean;
  /** A session is Runner-driven when owned already or not yet known (no parent seen). */
  isTopLevel(sessionId: string): boolean;
  /** Claim a top-level session. Returns true when newly claimed. */
  claimTopLevel(sessionId: string): boolean;
}

export function createSessionOwnership(
  deps: HandlerDependencies,
  mode: RunnerMode,
): SessionOwnership {
  const { store, paths, buildEvent } = deps;
  const isSubagentRole = mode.role === "subagent";
  const rootId = `runner-root:${mode.runId ?? "unknown-run"}`;
  const owned = new Set<string>();

  function emitSessionStart(sessionId: string): void {
    const runId = store.getRunId(sessionId);
    appendEventSync(
      paths.orchestratorEvents(effectiveRunId(runId)),
      buildEvent(
        "session_start",
        { sessionId, runId, timestamp: currentTimestamp() },
        { session_id: sessionId, adapter_version: deps.adapterVersion },
      ),
    );
  }

  function claimTopLevel(sessionId: string): boolean {
    if (owned.has(sessionId)) return false;
    owned.add(sessionId);
    if (isSubagentRole) {
      store.register(rootId, mode.runId ? { runId: mode.runId } : {});
      store.register(sessionId, {
        parentId: rootId,
        agentInstanceId: mode.agentInstanceId,
        agentType: mode.agentType,
      });
    } else {
      store.register(sessionId, mode.runId ? { runId: mode.runId } : {});
      emitSessionStart(sessionId);
    }
    return true;
  }

  return {
    isOwned: (sessionId) => owned.has(sessionId),
    isTopLevel: (sessionId) => owned.has(sessionId) || store.get(sessionId) === undefined,
    claimTopLevel,
  };
}
