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
import { extractInstanceId, extractRunId, extractStatusCode, fallbackInstanceId } from "./runstate.js";
import { renderInput, renderOutput, writeArtifact } from "./artifacts.js";
import { exportSession, exportOrchestratorTranscript } from "./export.js";
import type { HandlerDependencies } from "./handlers_session.js";

// ---------------------------------------------------------------------------
// Message text extraction helpers
// ---------------------------------------------------------------------------

/**
 * Concatenate the text of every `type: "text"` part in the given parts array,
 * joining multiple text parts with "\n". Returns undefined if none are found.
 *
 * Accepts `unknown` and returns `undefined` for anything that is not a usable
 * array — including null, non-arrays, empty arrays, and arrays whose entries
 * carry no text-typed part with a non-empty string. Never throws.
 */
export function extractTextFromParts(parts: unknown): string | undefined {
  if (!Array.isArray(parts)) return undefined;

  const texts: string[] = [];
  for (const part of parts) {
    if (typeof part !== "object" || part === null) continue;
    const p = part as Record<string, unknown>;
    if (p["type"] === "text" && typeof p["text"] === "string" && p["text"].length > 0) {
      texts.push(p["text"]);
    }
  }
  return texts.length > 0 ? texts.join("\n") : undefined;
}

/**
 * Extract text from the first user message in a messages array.
 * Role is read from entry.info.role (the real OpenCode shape); text is assembled
 * from text-typed parts via extractTextFromParts.
 * Returns undefined for any malformed or non-matching input. Never throws.
 */
export function extractFirstUserText(messages: unknown): string | undefined {
  if (!Array.isArray(messages)) return undefined;
  for (const msg of messages) {
    if (typeof msg !== "object" || msg === null) continue;
    const m = msg as Record<string, unknown>;
    const info = m["info"];
    if (typeof info !== "object" || info === null) continue;
    const infoObj = info as Record<string, unknown>;
    if (infoObj["role"] === "user") {
      return extractTextFromParts(m["parts"]);
    }
  }
  return undefined;
}

/**
 * Extract text from the last assistant message in a messages array.
 * Role is read from entry.info.role (the real OpenCode shape); text is assembled
 * from text-typed parts via extractTextFromParts.
 * Returns undefined for any malformed or non-matching input. Never throws.
 */
export function extractLastAssistantText(messages: unknown): string | undefined {
  if (!Array.isArray(messages)) return undefined;
  let lastText: string | undefined;
  for (const msg of messages) {
    if (typeof msg !== "object" || msg === null) continue;
    const m = msg as Record<string, unknown>;
    const info = m["info"];
    if (typeof info !== "object" || info === null) continue;
    const infoObj = info as Record<string, unknown>;
    if (infoObj["role"] === "assistant") {
      const text = extractTextFromParts(m["parts"]);
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

  async function handleInvocationStart(sessionId: string, parentId: string): Promise<void> {
    // Session is already registered in the correlation store by session handler.
    const ts = currentTimestamp();

    // Resolution order for identity:
    //   1. Pending dispatch — identity captured at task tool dispatch time (primary)
    //   2. session.messages() extraction — secondary fallback only when no dispatch queued
    const pendingDispatch = store.claimPendingDispatch(parentId);

    let promptText: string | undefined;
    let agentInstanceId: string | undefined;
    let agentType: string | undefined;
    let extractedRunId: string | undefined;
    // Track whether the secondary (session.messages) path was taken so that
    // orchestrator transcript refresh (which itself calls session.messages) is
    // only done in that path, keeping the primary dispatch path SDK-call-free.
    let usedSecondaryPath = false;

    if (pendingDispatch) {
      // Primary path: dispatch payload intercepted at tool.execute.before time.
      // No SDK call needed — identity is already in memory.
      agentInstanceId = pendingDispatch.agentInstanceId;
      agentType = pendingDispatch.agentType;
      extractedRunId = pendingDispatch.runId;
      promptText = pendingDispatch.prompt;
    } else {
      // Secondary path: fetch the session's messages and extract identity from the
      // user-role prompt text. Used when the dispatch payload was unavailable or
      // the pending queue for this parent is empty.
      usedSecondaryPath = true;
      let messages: unknown;
      try {
        messages = await sdkClient.session.messages({ path: { id: sessionId } });
      } catch (err) {
        debugLog(`handleInvocationStart: SDK messages() failed for ${sessionId}`, err);
        // Continue — identity will be resolved from whatever we have
      }
      promptText = extractFirstUserText(messages);
      agentInstanceId = extractInstanceId(promptText);
      extractedRunId = extractRunId(promptText);
      // Derive agent type from the instance id when no separate agentType field
      if (agentInstanceId?.includes("#")) {
        agentType = agentInstanceId.split("#")[0];
      }
    }

    // Apply fallback naming when identity remains unresolved. Per MosaicLogFormat.md
    // §3.2, invocation_start is mandatory and must be emitted even when identity is
    // unknown — a degraded event under unknown-run beats a missing one.
    if (!agentInstanceId) {
      agentInstanceId = fallbackInstanceId();
    }

    // Store resolved identity in the correlation store and mark that invocation_start
    // was emitted so handleInvocationEnd can stay symmetric.
    // When a pending dispatch was claimed, record its callId on this session so that
    // openDispatchCallFor(sessionId) can find the open tool call and close it
    // synthetically when handleInvocationEnd fires.
    store.register(sessionId, {
      agentInstanceId,
      agentType,
      prompt: promptText,
      runId: extractedRunId,
      invocationStarted: true,
      ...(pendingDispatch ? { dispatchCallId: pendingDispatch.callId } : {}),
    });

    // Propagate the resolved run id to the orchestrator session at the chain head
    // so every session in the chain resolves the same run folder.
    if (extractedRunId) {
      store.adoptRunId(sessionId, extractedRunId);
    }

    const runId = store.getRunId(sessionId);
    const effectiveRun = effectiveRunId(runId);
    const sink = paths.invocationEvents(effectiveRun, agentInstanceId);

    // Emit invocation_start unconditionally — degraded events land in unknown-run,
    // which is the spec's intended outcome for unresolvable identity.
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

    // Refresh orchestrator transcript only when the secondary (session.messages) path
    // was taken, because exportOrchestratorTranscript internally calls session.messages()
    // and the primary dispatch path must remain SDK-call-free after identity is captured.
    if (usedSecondaryPath) {
      const orchestratorId = store.resolveOrchestratorSession(sessionId);
      if (orchestratorId) {
        await exportOrchestratorTranscript(sdkClient, orchestratorId, paths, effectiveRun, orchestratorId);
      }
    }
  }

  async function handleInvocationEnd(sessionId: string): Promise<void> {
    if (store.isEnded(sessionId)) return;

    const record = store.get(sessionId);
    if (!record) return;

    const agentInstanceId = record.agentInstanceId;

    // Guard: emit invocation_end only when invocation_start was previously emitted
    // (tracked by the invocationStarted flag set in handleInvocationStart). For
    // sessions that carry a known agentInstanceId from direct registration (a legacy
    // path used by tests that bypass handleInvocationStart), allow emission when
    // agentInstanceId is set to a non-fallback value — this preserves the previous
    // behaviour for those sessions without requiring a mandatory handleInvocationStart
    // call in every test that exercises handleInvocationEnd.
    if (!record.invocationStarted && (!agentInstanceId || agentInstanceId.startsWith("unmapped_"))) {
      debugLog(`handleInvocationEnd: skipping session ${sessionId} — no invocation_start was emitted`);
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
        await exportOrchestratorTranscript(sdkClient, orchestratorId, paths, effectiveRun, orchestratorId);
      }
    } catch (err) {
      debugLog(`handleInvocationEnd: exportOrchestratorTranscript failed for ${sessionId}`, err);
    }

    // Stage 5: synthesize closure for the task dispatch that spawned this subagent.
    // Idempotent — if the real after-hook already fired, closeToolCall returns
    // undefined and no second event is emitted. If the after-hook never fires (the
    // confirmed OpenCode bug scenario), this is the only path that closes the call.
    try {
      await deps.closeDispatchCall?.(sessionId);
    } catch (err) {
      debugLog(`handleInvocationEnd: closeDispatchCall failed for ${sessionId}`, err);
    }
  }

  return { handleInvocationStart, handleInvocationEnd };
}
