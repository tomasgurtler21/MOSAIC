/**
 * handlers_runner.ts — Runner-mode lifecycle handlers.
 *
 * Active only when the Runner set MOSAIC_ROLE (see runner_mode.ts). The Runner
 * owns run_start/run_end, so this module never writes them.
 *
 *   subagent role:      the top-level session is ONE invocation. The session is
 *                       registered under a synthetic root record carrying the
 *                       run id, so the shared handlers (turns, usage, tools)
 *                       route it to the invocation folder as a native subagent
 *                       session. invocation_start is written when the prompt is
 *                       known; invocation_end is written synchronously, before
 *                       the first await of the session.idle path, from state
 *                       accumulated during the session (runner_invocation.ts).
 *   orchestrator role:  the top-level session writes run-level events to the
 *                       orchestrator stream (session_start / session_end only).
 *
 * Both roles refresh the session transcript while the session is running,
 * because `opencode run` does not await the session.idle handler at exit.
 *
 * usage_record, turn and tool events are not written here: they still come from
 * the native handlers, which see the session through the synthetic
 * `runner-root:*` registration (subagent role) or the root registration
 * (orchestrator role).
 *
 * Known Runner-mode field gaps (omitted rather than fabricated; compare with a
 * native OpenCode run):
 *   - subagent role: no run_start, run_end, session_start or session_end events;
 *     nothing is written to 00_orchestrator_events.jsonl.
 *   - subagent role: invocation_end carries agent_instance_id, status_code and
 *     response only; response is the last assistant text observed through
 *     message events, not read back from the SDK, so it can lack text the
 *     harness never surfaced as events.
 *   - subagent role: no synthetic closing tool_call_end for a dispatching task
 *     call (there is no dispatching parent session in the process).
 *   - subagent role: no orchestrator transcript refresh on invocation start/end.
 *   - orchestrator role: no run_start/run_end; session_start omits any run id
 *     extracted from session history (MOSAIC_RUN_ID is authoritative); no
 *     durable closed-session marker (exactly-once is per process).
 *   - 04_session.raw / 04_session.meta.json (and the session-scoped orchestrator
 *     transcript) exist only when the SDK returns messages during the session;
 *     the last completed refresh remains, so the final assistant message can be
 *     missing if the process exits before its refresh completes.
 *   - 02_output.md is best-effort and may be missing when the process exits
 *     right after session.idle.
 */

import { currentTimestamp, effectiveRunId, debugLog } from "./core.js";
import { extractSessionId, extractParentId } from "./handlers_session.js";
import type { HandlerDependencies, OpenCodeEvent } from "./handlers_session.js";
import type { RunnerMode } from "./runner_mode.js";
import { asRecord, createMessageAccumulator } from "./runner_messages.js";
import { createInvocationLifecycle } from "./runner_invocation.js";
import { createSessionOwnership } from "./runner_ownership.js";
import { appendEventSync } from "./sync_append.js";
import {
  createTranscriptRefresher,
  runnerOrchestratorRawPath,
  type TranscriptRefresher,
} from "./runner_transcript.js";

export interface RunnerEventResult {
  consumed: boolean;
  followUp?: Promise<void>;
}

export interface RunnerHandlers {
  /**
   * Synchronous: all exit-critical writes happen before this returns. A
   * consumed event must not be routed to the native handlers. followUp holds
   * best-effort asynchronous work (artifacts) for the caller to await.
   */
  handleEvent(event: OpenCodeEvent): RunnerEventResult;
  noteToolExecuted(sessionId: string): void;
}

export function createRunnerHandlers(
  deps: HandlerDependencies,
  mode: RunnerMode,
): RunnerHandlers {
  const { store, paths, sdkClient, buildEvent } = deps;
  const isSubagentRole = mode.role === "subagent";

  const messages = createMessageAccumulator();
  const ownership = createSessionOwnership(deps, mode);
  const refreshers = new Map<string, TranscriptRefresher>();
  /** Best-effort async work collected during one synchronous handleEvent call. */
  let tasks: Promise<unknown>[] = [];

  function refreshTranscript(sessionId: string): void {
    let refresher = refreshers.get(sessionId);
    if (!refresher) {
      const effectiveRun = effectiveRunId(store.getRunId(sessionId));
      const target = isSubagentRole
        ? paths.invocationRaw(effectiveRun, mode.agentInstanceId ?? "unknown")
        : runnerOrchestratorRawPath(paths, effectiveRun, sessionId);
      refresher = createTranscriptRefresher(sdkClient, sessionId, target);
      refreshers.set(sessionId, refresher);
    }
    refresher.request();
  }

  const invocation = createInvocationLifecycle(
    deps,
    mode,
    messages,
    (task) => tasks.push(task),
    refreshTranscript,
  );

  function endOrchestratorSession(sessionId: string, reason: string): void {
    if (store.isEnded(sessionId)) return;
    store.markEnded(sessionId);
    const runId = store.getRunId(sessionId);
    appendEventSync(
      paths.orchestratorEvents(effectiveRunId(runId)),
      buildEvent("session_end", { sessionId, runId, timestamp: currentTimestamp() }, { reason }),
    );
    refreshTranscript(sessionId);
  }

  function handleSessionEvent(event: OpenCodeEvent): boolean {
    const sessionId = extractSessionId(event);
    if (!sessionId) return false;

    if (event.type === "session.created") {
      if (extractParentId(event) !== undefined) return false;
      ownership.claimTopLevel(sessionId);
      return true;
    }

    // session.idle / session.deleted
    if (!ownership.isTopLevel(sessionId)) return false;
    if (isSubagentRole) {
      // Only idle closes an invocation; deletion is a no-op, as natively.
      if (event.type === "session.idle") {
        ownership.claimTopLevel(sessionId);
        invocation.end(sessionId);
      }
    } else {
      ownership.claimTopLevel(sessionId);
      endOrchestratorSession(sessionId, event.type);
    }
    return true;
  }

  function handleMessageUpdated(event: OpenCodeEvent): void {
    const info = asRecord(event.properties?.["info"]);
    const sessionId = typeof info?.["sessionID"] === "string" ? info["sessionID"] : undefined;
    if (!info || !sessionId || !ownership.isTopLevel(sessionId)) return;
    ownership.claimTopLevel(sessionId);

    messages.recordMessageUpdate(sessionId, info, event.properties?.["parts"]);
    if (isSubagentRole) invocation.start(sessionId, info["role"] === "assistant");

    // A completed assistant message is the checkpoint for a transcript refresh.
    const time = asRecord(info["time"]);
    if (info["role"] === "assistant" && typeof time?.["completed"] === "number") {
      refreshTranscript(sessionId);
    }
  }

  function handleMessagePartUpdated(event: OpenCodeEvent): void {
    const part = asRecord(event.properties?.["part"]);
    const sessionId = typeof part?.["sessionID"] === "string" ? part["sessionID"] : undefined;
    if (!part || !sessionId || !ownership.isOwned(sessionId)) return;
    messages.recordPartUpdate(sessionId, part);
    if (isSubagentRole) invocation.start(sessionId, false);
  }

  function handleEvent(event: OpenCodeEvent): RunnerEventResult {
    tasks = [];
    let consumed = false;
    try {
      switch (event?.type) {
        case "session.created":
        case "session.idle":
        case "session.deleted":
          consumed = handleSessionEvent(event);
          break;
        case "message.updated":
          handleMessageUpdated(event);
          break;
        case "message.part.updated":
          handleMessagePartUpdated(event);
          break;
        default:
          break;
      }
    } catch (err) {
      debugLog("runner handleEvent failed", err);
    }
    const pending = tasks;
    tasks = [];
    const followUp =
      pending.length > 0
        ? Promise.allSettled(pending).then(() => undefined)
        : undefined;
    return { consumed, followUp };
  }

  function noteToolExecuted(sessionId: string): void {
    try {
      if (ownership.isOwned(sessionId)) refreshTranscript(sessionId);
    } catch (err) {
      debugLog("runner noteToolExecuted failed", err);
    }
  }

  return { handleEvent, noteToolExecuted };
}
