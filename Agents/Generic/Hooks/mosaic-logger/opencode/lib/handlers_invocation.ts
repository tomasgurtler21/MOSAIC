/**
 * handlers_invocation.ts — Invocation (subagent) lifecycle event handlers.
 *
 * Handles:
 *   - Subagent session creation (session.created with parentID): emit invocation_start,
 *     extract agent_instance_id and run_id from the dispatch prompt, write 01_input.md,
 *     and refresh the orchestrator transcript.
 *   - Subagent session end (via stop hook): emit invocation_end with extracted status_code,
 *     write 02_output.md, export subagent session transcript, refresh orchestrator transcript.
 *
 * This is the integration layer that bridges the OpenCode session model to the
 * MOSAIC invocation log format.
 */

import { currentTimestamp, effectiveRunId, debugLog } from "./core.js";
import { extractInstanceId, extractRunId, extractStatusCode } from "./runstate.js";
import { renderInput, renderOutput, writeArtifact } from "./artifacts.js";
import { exportSession, exportOrchestratorTranscript } from "./export.js";
import type { HandlerDependencies } from "./handlers_session.js";

// ---------------------------------------------------------------------------
// Message text extraction helpers
// ---------------------------------------------------------------------------

/**
 * Extract plain text from an OpenCode message's content field.
 * Handles both string content and the array-of-parts format.
 */
function extractTextFromContent(content: unknown): string | undefined {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return undefined;

  const parts: string[] = [];
  for (const part of content) {
    if (typeof part === "string") {
      parts.push(part);
    } else if (typeof part === "object" && part !== null) {
      const p = part as Record<string, unknown>;
      if (typeof p.text === "string") parts.push(p.text);
    }
  }
  return parts.length > 0 ? parts.join("") : undefined;
}

/**
 * Extract text from the first user message in a messages array.
 * The first user message is the dispatch prompt sent by the orchestrator.
 */
function extractFirstUserText(messages: unknown): string | undefined {
  if (!Array.isArray(messages)) return undefined;
  for (const msg of messages) {
    if (typeof msg !== "object" || msg === null) continue;
    const m = msg as Record<string, unknown>;
    if (m.role === "user") {
      return extractTextFromContent(m.content);
    }
  }
  return undefined;
}

/**
 * Extract text from the last assistant message in a messages array.
 * The last assistant message is the subagent's final response.
 */
function extractLastAssistantText(messages: unknown): string | undefined {
  if (!Array.isArray(messages)) return undefined;
  let lastText: string | undefined;
  for (const msg of messages) {
    if (typeof msg !== "object" || msg === null) continue;
    const m = msg as Record<string, unknown>;
    if (m.role === "assistant") {
      const text = extractTextFromContent(m.content);
      if (text !== undefined) lastText = text;
    }
  }
  return lastText;
}

// ---------------------------------------------------------------------------
// Handler factory
// ---------------------------------------------------------------------------

/**
 * Create invocation (subagent) lifecycle handlers.
 *
 * Returns individual handler functions. `handleInvocationStart` is called from
 * plugin.ts when a session.created event has a parentID. `handleInvocationEnd`
 * is called from plugin.ts when the stop hook fires for a subagent session.
 */
export function createInvocationHandlers(deps: HandlerDependencies): {
  /**
   * Handle a subagent session creation. Called by plugin.ts when a
   * session.created event has a parentID. Emits invocation_start.
   */
  handleInvocationStart: (sessionId: string, parentId: string) => Promise<void>;

  /**
   * Handle a subagent session end. Emits invocation_end with status_code
   * extracted from the last message, writes artifacts, exports transcript.
   */
  handleInvocationEnd: (sessionId: string) => Promise<void>;
} {
  const { store, paths, sdkClient, buildEvent, appendEvent } = deps;

  async function handleInvocationStart(sessionId: string, _parentId: string): Promise<void> {
    // Session is already registered in the correlation store by session handler.
    const ts = currentTimestamp();

    // Fetch subagent's messages to extract identity from the dispatch prompt.
    let messages: unknown;
    try {
      messages = await sdkClient.session.messages({ path: { id: sessionId } });
    } catch (err) {
      debugLog(`handleInvocationStart: SDK messages() failed for ${sessionId}`, err);
      // Continue — identity will be unknown for this invocation
    }

    const promptText = extractFirstUserText(messages);
    const agentInstanceId = extractInstanceId(promptText) ?? `unmapped_${sessionId}`;

    // Guard: skip unmapped invocations (agent_instance_id could not be extracted).
    // Mirrors the Python adapter's symmetric guard that skips both start and end
    // when ctx.agent_id is absent, ensuring event-pair symmetry in the log.
    if (agentInstanceId.startsWith("unmapped_")) {
      debugLog(`handleInvocationStart: skipping unmapped session ${sessionId} (no agent_instance_id in prompt)`);
      return;
    }

    const agentType = agentInstanceId.includes("#")
      ? agentInstanceId.split("#")[0]
      : undefined;
    const extractedRunId = extractRunId(promptText);

    // Store extracted identity in the correlation store
    store.register(sessionId, {
      agentInstanceId,
      agentType,
      prompt: promptText,
      runId: extractedRunId,
    });

    // If we extracted a run_id, also update the orchestrator session record so that
    // store.getRunId() resolves correctly for all sessions in the chain.
    if (extractedRunId) {
      const orchestratorId = store.resolveOrchestratorSession(sessionId);
      if (orchestratorId && orchestratorId !== sessionId) {
        store.register(orchestratorId, { runId: extractedRunId });
      }
    }

    const runId = store.getRunId(sessionId);
    const effectiveRun = effectiveRunId(runId);
    const sink = paths.invocationEvents(effectiveRun, agentInstanceId);

    // Emit invocation_start
    await appendEvent(
      sink,
      buildEvent(
        "invocation_start",
        { sessionId, runId, timestamp: ts },
        {
          agent_instance_id: agentInstanceId,
          agent_type: agentType,
          prompt: promptText,
        },
      ),
    );

    // Write 01_input.md (best-effort; failure doesn't prevent event emission)
    await writeArtifact(
      paths.invocationInput(effectiveRun, agentInstanceId),
      renderInput({
        agentInstanceId,
        agentType,
        sessionId,
        runId,
        timestamp: ts,
        prompt: promptText,
      }),
    );

    // Refresh orchestrator transcript (best-effort)
    const orchestratorId = store.resolveOrchestratorSession(sessionId);
    if (orchestratorId) {
      await exportOrchestratorTranscript(sdkClient, orchestratorId, paths, effectiveRun);
    }
  }

  async function handleInvocationEnd(sessionId: string): Promise<void> {
    if (store.isEnded(sessionId)) return;

    const record = store.get(sessionId);
    if (!record) return;

    // Guard: skip if no agent_instance_id was extracted.
    // handleInvocationStart returns early (without emitting invocation_start) when
    // extraction fails, so no matching invocation_start exists for this session.
    // This check is a symmetric defensive guard ensuring invocation_end is never
    // emitted without a preceding invocation_start.
    const agentInstanceId = record.agentInstanceId;
    if (!agentInstanceId || agentInstanceId.startsWith("unmapped_")) {
      debugLog(`handleInvocationEnd: skipping unmapped session ${sessionId} (no agent_instance_id extracted at start)`);
      return;
    }

    store.markEnded(sessionId);

    const ts = currentTimestamp();
    const runId = store.getRunId(sessionId);
    const effectiveRun = effectiveRunId(runId);
    const sink = paths.invocationEvents(effectiveRun, agentInstanceId);

    // Fetch session messages to read the last assistant response.
    let messages: unknown;
    try {
      messages = await sdkClient.session.messages({ path: { id: sessionId } });
    } catch (err) {
      debugLog(`handleInvocationEnd: SDK messages() failed for ${sessionId}`, err);
      // Continue — response fields will be absent from the event
    }

    const responseText = extractLastAssistantText(messages);
    const statusCode = extractStatusCode(responseText);

    // Stage 1: emit invocation_end
    await appendEvent(
      sink,
      buildEvent(
        "invocation_end",
        { sessionId, runId, timestamp: ts },
        {
          agent_instance_id: agentInstanceId,
          status_code: statusCode,
          response: responseText,
        },
      ),
    );

    // Stage 2: write 02_output.md (independent — failure doesn't block stage 3+)
    try {
      await writeArtifact(
        paths.invocationOutput(effectiveRun, agentInstanceId),
        renderOutput({
          agentInstanceId,
          agentType: record.agentType,
          sessionId,
          runId,
          timestamp: ts,
          statusCode,
          response: responseText,
        }),
      );
    } catch (err) {
      debugLog(`handleInvocationEnd: writeArtifact failed for ${sessionId}`, err);
    }

    // Stage 3: export subagent session transcript (independent)
    try {
      await exportSession(
        sdkClient,
        sessionId,
        paths.invocationRaw(effectiveRun, agentInstanceId),
      );
    } catch (err) {
      debugLog(`handleInvocationEnd: exportSession failed for ${sessionId}`, err);
    }

    // Stage 4: refresh orchestrator transcript (independent)
    try {
      const orchestratorId = store.resolveOrchestratorSession(sessionId);
      if (orchestratorId) {
        await exportOrchestratorTranscript(sdkClient, orchestratorId, paths, effectiveRun);
      }
    } catch (err) {
      debugLog(`handleInvocationEnd: exportOrchestratorTranscript failed for ${sessionId}`, err);
    }
  }

  return { handleInvocationStart, handleInvocationEnd };
}
