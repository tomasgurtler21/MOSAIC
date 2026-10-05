/**
 * runner_invocation.ts — subagent-role invocation lifecycle for Runner mode.
 *
 * The top-level session is one invocation: invocation_start once the prompt is
 * known, and exactly one invocation_end written synchronously (before any
 * await) so it survives a process exit right after session.idle.
 */

import { currentTimestamp, effectiveRunId } from "./core.js";
import { extractRunId, extractStatusCode } from "./runstate.js";
import { renderInput, renderOutput, writeArtifact } from "./artifacts.js";
import type { HandlerDependencies } from "./handlers_session.js";
import type { RunnerMode } from "./runner_mode.js";
import type { MessageAccumulator } from "./runner_messages.js";
import { appendEventSync } from "./sync_append.js";

export interface InvocationLifecycle {
  /** Emit invocation_start once. Waits for the prompt unless forced. */
  start(sessionId: string, force: boolean): void;
  /** Emit invocation_end once, then queue artifacts and a transcript refresh. */
  end(sessionId: string): void;
}

export function createInvocationLifecycle(
  deps: HandlerDependencies,
  mode: RunnerMode,
  messages: MessageAccumulator,
  addTask: (task: Promise<unknown>) => void,
  refreshTranscript: (sessionId: string) => void,
): InvocationLifecycle {
  const { store, paths, buildEvent } = deps;

  function start(sessionId: string, force: boolean): void {
    if (store.get(sessionId)?.invocationStarted) return;
    const prompt = messages.findMessageText(sessionId, "user", false);
    if (prompt === undefined && !force) return;

    if (!mode.runId) {
      const promptRunId = extractRunId(prompt);
      if (promptRunId) store.adoptRunId(sessionId, promptRunId);
    }
    store.register(sessionId, { invocationStarted: true, prompt });

    const instanceId = mode.agentInstanceId ?? "unknown";
    const runId = store.getRunId(sessionId);
    const effectiveRun = effectiveRunId(runId);
    const timestamp = currentTimestamp();
    appendEventSync(
      paths.invocationEvents(effectiveRun, instanceId),
      buildEvent(
        "invocation_start",
        { sessionId, runId, timestamp },
        { agent_instance_id: instanceId, agent_type: mode.agentType, prompt },
      ),
    );
    addTask(
      writeArtifact(
        paths.invocationInput(effectiveRun, instanceId),
        renderInput({
          agentInstanceId: instanceId,
          agentType: mode.agentType,
          sessionId,
          runId,
          timestamp,
          prompt,
        }),
      ),
    );
  }

  function end(sessionId: string): void {
    if (store.isEnded(sessionId)) return;
    start(sessionId, true);
    store.markEnded(sessionId);

    const instanceId = mode.agentInstanceId ?? "unknown";
    const runId = store.getRunId(sessionId);
    const effectiveRun = effectiveRunId(runId);
    const timestamp = currentTimestamp();
    const response = messages.findMessageText(sessionId, "assistant", true);
    const statusCode = extractStatusCode(response);

    // Synchronous and first: nothing after the first await survives process exit.
    appendEventSync(
      paths.invocationEvents(effectiveRun, instanceId),
      buildEvent(
        "invocation_end",
        { sessionId, runId, timestamp },
        { agent_instance_id: instanceId, status_code: statusCode, response },
      ),
    );

    addTask(
      writeArtifact(
        paths.invocationOutput(effectiveRun, instanceId),
        renderOutput({
          agentInstanceId: instanceId,
          agentType: mode.agentType,
          sessionId,
          runId,
          timestamp,
          statusCode,
          response,
        }),
      ),
    );
    refreshTranscript(sessionId);
  }

  return { start, end };
}
