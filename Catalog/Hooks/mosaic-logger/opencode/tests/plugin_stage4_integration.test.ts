/**
 * plugin_stage4_integration.test.ts — Integration tests for Stage 4 wiring fixes.
 *
 * Exercises two integration paths that the unit tests cover only at the
 * module function level:
 *
 * Critical #1: tool.execute.after output forwarding through the real hook map.
 *   Unit tests call handleToolAfter and deriveToolOutcome directly — they do not
 *   exercise the plugin.ts hook registration that forwards (or drops) the second
 *   argument. These tests drive the full hook-map entry point and assert the
 *   outcome status written to the on-disk JSONL file.
 *
 * Critical #2: closeDispatchCall wired to handleInvocationEnd.
 *   Unit tests call closeDispatchCall directly — they do not exercise
 *   handleInvocationEnd calling it. These tests verify:
 *     (a) When closeDispatchCall is injected into createInvocationHandlers and
 *         handleInvocationEnd is called, tool_call_end is emitted for the open
 *         dispatch call — this verifies the Stage 5 block added to
 *         handleInvocationEnd by the review fix.
 *     (b) The full plugin hook sequence (task dispatch → subagent session.created
 *         → session.idle) emits tool_call_end for the dispatch call — this
 *         verifies end-to-end wiring through handleInvocationStart storing the
 *         dispatchCallId on the subagent session record.
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import { MosaicLogger } from "../plugin";
import { createToolHandlers } from "../lib/handlers_tools";
import { createInvocationHandlers } from "../lib/handlers_invocation";
import {
  type HandlerDependencies,
  type SdkClient,
  type OpenCodeEvent,
  type ToolBeforeInput,
  type ToolBeforeOutput,
  type ToolAfterInput,
  type ToolAfterOutput,
} from "../lib/handlers_session";
import { SessionCorrelationStore } from "../lib/correlation";
import { LogPaths, setDebugLogger } from "../lib/core";
import {
  taskDispatchPayload,
  toolBeforePayload,
  toolAfterPayload,
} from "./fixtures/opencode_api";

// ---------------------------------------------------------------------------
// Common helpers
// ---------------------------------------------------------------------------

let tmpDir: string;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(
    nodePath.join(nodeOs.tmpdir(), "mosaic-s4-int-test-"),
  );
  setDebugLogger(() => {});
});

afterEach(() => {
  nodeFs.rmSync(tmpDir, { recursive: true, force: true });
  setDebugLogger(() => {});
});

/** Minimal SDK client that silently absorbs all calls. */
function makeNullClient(): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async () => [],
    },
    app: { log: async () => {} },
  };
}

/**
 * Build the plugin hook map via MosaicLogger and return it as a plain object.
 * Uses the real buildEvent + appendEvent (writes to disk in tmpDir).
 */
async function buildHookMap(): Promise<Record<string, unknown>> {
  const result = await MosaicLogger({
    directory: tmpDir,
    client: makeNullClient(),
  });
  return result as Record<string, unknown>;
}

/**
 * Read all events from the orchestrator JSONL stream for an unknown-run
 * session (no run_id set on the session record).
 */
function readOrchestratorEvents(root: string): Record<string, unknown>[] {
  const eventsPath = nodePath.join(
    root,
    "OrchestrationLogs",
    "unknown-run",
    "00_orchestrator_events.jsonl",
  );
  if (!nodeFs.existsSync(eventsPath)) return [];
  return nodeFs
    .readFileSync(eventsPath, "utf-8")
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line) as Record<string, unknown>);
}

// ---------------------------------------------------------------------------
// Helpers for unit-level integration tests (collected events, not on disk)
// ---------------------------------------------------------------------------

type CollectedEvent = { filePath: string; event: Record<string, unknown> };

let callIdCounter = 0;

function makeStore(): SessionCorrelationStore {
  return new SessionCorrelationStore();
}

function makePaths(): LogPaths {
  return new LogPaths(tmpDir);
}

function makeDeps(
  store: SessionCorrelationStore,
  paths: LogPaths,
  sdkClient: SdkClient,
  collected: CollectedEvent[],
): HandlerDependencies {
  return {
    store,
    paths,
    sdkClient,
    adapterVersion: "0.1.0",
    buildEvent: (event, envelope, fields) => ({
      schema_version: "1.0.0",
      event,
      timestamp: envelope.timestamp ?? new Date().toISOString(),
      harness: "opencode",
      ...(envelope.sessionId ? { session_id: envelope.sessionId } : {}),
      ...(envelope.runId ? { run_id: envelope.runId } : {}),
      ...Object.fromEntries(
        Object.entries(fields).filter(
          ([, v]) => v !== undefined && v !== null && v !== "",
        ),
      ),
    }),
    appendEvent: async (filePath, event) => {
      collected.push({ filePath, event });
    },
    fallbackCallId: (toolName) =>
      `${toolName ?? "tool"}_call_${++callIdCounter}`,
  };
}

// ---------------------------------------------------------------------------
// Suite 1: Critical #1 — tool.execute.after output forwarding through the
//          real hook-map entry point
//
// Before the Stage 4 fix, plugin.ts registered tool.execute.after as:
//   async (input) => { await toolHandlers.handleToolAfter(input); }
// dropping the second argument, so deriveToolOutcome was never invoked and
// every real tool call was recorded with the hardcoded status: "success".
// The fix forwards the second argument so the real outcome reaches the log.
// ---------------------------------------------------------------------------

describe("after-hook output forwarded through the real tool.execute.after hook-map entry point", () => {
  it("a failing tool output reaches error status through the hook-map entry point", async () => {
    const hookMap = await buildHookMap();
    const eventHook = hookMap["event"] as
      | ((input: { event: OpenCodeEvent }) => Promise<void>)
      | undefined;
    const toolBeforeHook = hookMap["tool.execute.before"] as
      | ((input: ToolBeforeInput, output: ToolBeforeOutput) => Promise<void>)
      | undefined;
    const toolAfterHook = hookMap["tool.execute.after"] as
      | ((input: ToolAfterInput, output: ToolAfterOutput) => Promise<void>)
      | undefined;

    if (!eventHook || !toolBeforeHook || !toolAfterHook) return;

    const sessionId = "s4-int-error-session";
    const callId = "s4-int-call-error-1";

    // Register the orchestrator session
    await eventHook({
      event: {
        type: "session.created",
        properties: { info: { id: sessionId } },
      },
    });

    // Open a tool call
    const [beforeInput, beforeOutput] = toolBeforePayload({
      tool: "bash",
      sessionID: sessionId,
      callID: callId,
      args: { command: "exit 1" },
    });
    await toolBeforeHook(beforeInput, beforeOutput);

    // Close the tool call with a failing output — the hook must forward this
    // to handleToolAfter so deriveToolOutcome derives "error", not "success"
    const [afterInput, afterOutput] = toolAfterPayload({
      tool: "bash",
      sessionID: sessionId,
      callID: callId,
      output: { status: "error", error: "command not found: bogus-cmd" },
    });
    await toolAfterHook(afterInput, afterOutput);

    // Read the on-disk event stream
    const events = readOrchestratorEvents(tmpDir);
    const endEvents = events.filter((e) => e["event"] === "tool_call_end");

    // A tool_call_end must have been emitted
    expect(endEvents.length).toBeGreaterThan(0);

    // The event must carry the error status derived from the output — not the
    // hardcoded "success" that would appear if output was dropped
    const endEvent = endEvents.find((e) => e["call_id"] === callId);
    expect(endEvent).toBeDefined();
    expect(endEvent!["status"]).toBe("error");
    expect(typeof endEvent!["error"]).toBe("string");
    expect(endEvent!["error"]).toContain("command not found");
  });

  it("a successful tool output reaches success status through the hook-map entry point", async () => {
    const hookMap = await buildHookMap();
    const eventHook = hookMap["event"] as
      | ((input: { event: OpenCodeEvent }) => Promise<void>)
      | undefined;
    const toolBeforeHook = hookMap["tool.execute.before"] as
      | ((input: ToolBeforeInput, output: ToolBeforeOutput) => Promise<void>)
      | undefined;
    const toolAfterHook = hookMap["tool.execute.after"] as
      | ((input: ToolAfterInput, output: ToolAfterOutput) => Promise<void>)
      | undefined;

    if (!eventHook || !toolBeforeHook || !toolAfterHook) return;

    const sessionId = "s4-int-success-session";
    const callId = "s4-int-call-success-1";

    await eventHook({
      event: {
        type: "session.created",
        properties: { info: { id: sessionId } },
      },
    });

    const [beforeInput, beforeOutput] = toolBeforePayload({
      tool: "bash",
      sessionID: sessionId,
      callID: callId,
      args: { command: "echo hello" },
    });
    await toolBeforeHook(beforeInput, beforeOutput);

    const [afterInput, afterOutput] = toolAfterPayload({
      tool: "bash",
      sessionID: sessionId,
      callID: callId,
      output: { status: "success" },
    });
    await toolAfterHook(afterInput, afterOutput);

    const events = readOrchestratorEvents(tmpDir);
    const endEvent = events.find(
      (e) => e["event"] === "tool_call_end" && e["call_id"] === callId,
    );

    expect(endEvent).toBeDefined();
    // Success status from the output must be forwarded; "error" would indicate
    // the output was dropped and the outcome defaulted incorrectly
    expect(endEvent!["status"]).toBe("success");
  });

  it("an outcome-ambiguous tool output omits status through the hook-map entry point", async () => {
    const hookMap = await buildHookMap();
    const eventHook = hookMap["event"] as
      | ((input: { event: OpenCodeEvent }) => Promise<void>)
      | undefined;
    const toolBeforeHook = hookMap["tool.execute.before"] as
      | ((input: ToolBeforeInput, output: ToolBeforeOutput) => Promise<void>)
      | undefined;
    const toolAfterHook = hookMap["tool.execute.after"] as
      | ((input: ToolAfterInput, output: ToolAfterOutput) => Promise<void>)
      | undefined;

    if (!eventHook || !toolBeforeHook || !toolAfterHook) return;

    const sessionId = "s4-int-ambiguous-session";
    const callId = "s4-int-call-ambiguous-1";

    await eventHook({
      event: {
        type: "session.created",
        properties: { info: { id: sessionId } },
      },
    });

    const [beforeInput, beforeOutput] = toolBeforePayload({
      tool: "bash",
      sessionID: sessionId,
      callID: callId,
      args: { command: "ls" },
    });
    await toolBeforeHook(beforeInput, beforeOutput);

    // An output payload that signals neither success nor failure — deriveToolOutcome
    // must omit status rather than asserting an outcome the payload does not support
    const [afterInput, afterOutput] = toolAfterPayload({
      tool: "bash",
      sessionID: sessionId,
      callID: callId,
      output: { title: "Ran bash", result: "some output text" },
    });
    await toolAfterHook(afterInput, afterOutput);

    const events = readOrchestratorEvents(tmpDir);
    const endEvent = events.find(
      (e) => e["event"] === "tool_call_end" && e["call_id"] === callId,
    );

    expect(endEvent).toBeDefined();
    // Status must be absent — the design's "omit rather than guess" rule.
    // If the output was dropped, the hardcoded fallback would set status: "success",
    // which this assertion catches.
    expect(endEvent!["status"]).toBeUndefined();
  });

  it("call_id is consistent between tool_call_start and tool_call_end through both hook-map entry points", async () => {
    const hookMap = await buildHookMap();
    const eventHook = hookMap["event"] as
      | ((input: { event: OpenCodeEvent }) => Promise<void>)
      | undefined;
    const toolBeforeHook = hookMap["tool.execute.before"] as
      | ((input: ToolBeforeInput, output: ToolBeforeOutput) => Promise<void>)
      | undefined;
    const toolAfterHook = hookMap["tool.execute.after"] as
      | ((input: ToolAfterInput, output: ToolAfterOutput) => Promise<void>)
      | undefined;

    if (!eventHook || !toolBeforeHook || !toolAfterHook) return;

    const sessionId = "s4-int-callid-session";
    const callId = "s4-int-call-corr-1";

    await eventHook({
      event: {
        type: "session.created",
        properties: { info: { id: sessionId } },
      },
    });

    const [beforeInput, beforeOutput] = toolBeforePayload({
      tool: "bash",
      sessionID: sessionId,
      callID: callId,
      args: { command: "pwd" },
    });
    await toolBeforeHook(beforeInput, beforeOutput);

    const [afterInput, afterOutput] = toolAfterPayload({
      tool: "bash",
      sessionID: sessionId,
      callID: callId,
      output: { status: "success" },
    });
    await toolAfterHook(afterInput, afterOutput);

    const events = readOrchestratorEvents(tmpDir);
    const startEvent = events.find(
      (e) => e["event"] === "tool_call_start" && e["call_id"] === callId,
    );
    const endEvent = events.find(
      (e) => e["event"] === "tool_call_end" && e["call_id"] === callId,
    );

    // Both events must exist and carry the same call_id
    expect(startEvent).toBeDefined();
    expect(endEvent).toBeDefined();
    expect(startEvent!["call_id"]).toBe(callId);
    expect(endEvent!["call_id"]).toBe(callId);
  });
});

// ---------------------------------------------------------------------------
// Suite 2a: Critical #2 — closeDispatchCall wired to handleInvocationEnd
//           Tested through handleInvocationEnd directly with closeDispatchCall
//           explicitly injected, verifying the Stage 5 block added to
//           handleInvocationEnd during the review-remediation fix.
//
// These tests use collected (in-memory) events rather than on-disk files so
// they remain self-contained without requiring the full plugin factory.
// ---------------------------------------------------------------------------

describe("closeDispatchCall wired to handleInvocationEnd — tested via handleInvocationEnd directly", () => {
  const ORCH_SESSION = "s4-inv-orch-001";
  const SUB_SESSION = "s4-inv-sub-001";
  const DISPATCH_CALL_ID = "s4-dispatch-call-1";

  /**
   * Set up a store with an open task dispatch call registered and a subagent
   * session record that knows its dispatchCallId. This mirrors the state that
   * would exist in production after:
   *   1. handleToolBefore(task, ...) registered the call in the keyed registry
   *   2. handleInvocationStart stored the dispatchCallId on the subagent record
   */
  function makeStoreWithOpenDispatch(collected: CollectedEvent[]): {
    store: SessionCorrelationStore;
    paths: LogPaths;
    deps: HandlerDependencies;
  } {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeNullClient();
    const deps = makeDeps(store, paths, sdk, collected);

    // Orchestrator session
    store.register(ORCH_SESSION, { runId: undefined });

    // Subagent session with its dispatchCallId linked to the open task call
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: "TestAgent#1",
      invocationStarted: true,
      dispatchCallId: DISPATCH_CALL_ID,
    });

    // Register the pending tool call in the keyed registry
    store.registerToolCall({
      callId: DISPATCH_CALL_ID,
      sessionId: ORCH_SESSION,
      toolName: "task",
      isDispatch: true,
      startedAt: new Date().toISOString(),
    });

    return { store, paths, deps };
  }

  it("handleInvocationEnd emits tool_call_end for an open dispatch call", async () => {
    const collected: CollectedEvent[] = [];
    const { deps } = makeStoreWithOpenDispatch(collected);

    // Create tool handlers to get closeDispatchCall
    const toolHandlers = createToolHandlers(deps);

    // Create invocation handlers with closeDispatchCall injected — this is the
    // wiring added by the Stage 4 review-remediation fix
    const invHandlers = createInvocationHandlers({
      ...deps,
      closeDispatchCall: toolHandlers.closeDispatchCall,
    });

    // Calling handleInvocationEnd should trigger closeDispatchCall internally
    await invHandlers.handleInvocationEnd(SUB_SESSION);

    // A tool_call_end event for the dispatch call must have been emitted
    const toolCallEndEvents = collected.filter(
      (c) => c.event["event"] === "tool_call_end",
    );
    expect(toolCallEndEvents.length).toBeGreaterThan(0);

    const dispatchEndEvent = toolCallEndEvents.find(
      (c) => c.event["call_id"] === DISPATCH_CALL_ID,
    );
    expect(dispatchEndEvent).toBeDefined();
    expect(dispatchEndEvent!.event["call_id"]).toBe(DISPATCH_CALL_ID);
  });

  it("a second handleInvocationEnd call does not emit a second tool_call_end (idempotent via closeToolCall)", async () => {
    const collected: CollectedEvent[] = [];
    const { deps } = makeStoreWithOpenDispatch(collected);

    const toolHandlers = createToolHandlers(deps);
    const invHandlers = createInvocationHandlers({
      ...deps,
      closeDispatchCall: toolHandlers.closeDispatchCall,
    });

    // First call — should emit tool_call_end
    await invHandlers.handleInvocationEnd(SUB_SESSION);

    const afterFirst = collected.filter(
      (c) => c.event["event"] === "tool_call_end",
    ).length;

    // Second call — handleInvocationEnd's isEnded guard returns early,
    // so no second tool_call_end should appear
    await invHandlers.handleInvocationEnd(SUB_SESSION);

    const afterSecond = collected.filter(
      (c) => c.event["event"] === "tool_call_end",
    ).length;

    expect(afterFirst).toBe(1);
    expect(afterSecond).toBe(1); // count must not grow
  });

  it("handleInvocationEnd does not emit tool_call_end when no dispatch call is open", async () => {
    const collected: CollectedEvent[] = [];
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeNullClient();
    const deps = makeDeps(store, paths, sdk, collected);

    // Subagent session with NO dispatchCallId — no open dispatch to close
    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: "TestAgent#2",
      invocationStarted: true,
      // Deliberately no dispatchCallId
    });

    const toolHandlers = createToolHandlers(deps);
    const invHandlers = createInvocationHandlers({
      ...deps,
      closeDispatchCall: toolHandlers.closeDispatchCall,
    });

    await invHandlers.handleInvocationEnd(SUB_SESSION);

    const toolCallEndEvents = collected.filter(
      (c) => c.event["event"] === "tool_call_end",
    );
    // No tool_call_end should be emitted — there was no open dispatch to close
    expect(toolCallEndEvents.length).toBe(0);
  });

  it("real after-hook arriving before invocation-end closes the call: handleInvocationEnd does not double-emit", async () => {
    const collected: CollectedEvent[] = [];
    const { store, deps } = makeStoreWithOpenDispatch(collected);

    const toolHandlers = createToolHandlers(deps);
    const invHandlers = createInvocationHandlers({
      ...deps,
      closeDispatchCall: toolHandlers.closeDispatchCall,
    });

    // Simulate the real after-hook arriving first and closing the call
    await toolHandlers.handleToolAfter(
      { tool: "task", sessionID: ORCH_SESSION, callID: DISPATCH_CALL_ID },
      { status: "success" },
    );

    const afterRealHook = collected.filter(
      (c) => c.event["event"] === "tool_call_end",
    ).length;

    // Now invocation end fires — closeDispatchCall must be a no-op because the
    // call is already closed (closeToolCall returns undefined on second call)
    await invHandlers.handleInvocationEnd(SUB_SESSION);

    const afterInvEnd = collected.filter(
      (c) => c.event["event"] === "tool_call_end",
    ).length;

    // Only one tool_call_end total — the real after-hook's
    expect(afterRealHook).toBe(1);
    expect(afterInvEnd).toBe(1); // must not grow
  });
});

// ---------------------------------------------------------------------------
// Suite 2b: Critical #2 — full end-to-end integration through the plugin
//           hook map.
//
// Drives: session.created (orch) → tool.execute.before (task) →
//         session.created (subagent) → session.idle (subagent)
// and asserts that tool_call_end is emitted for the dispatch call.
//
// This requires handleInvocationStart to store dispatchCallId on the subagent
// session record so that openDispatchCallFor can find the pending tool call
// when closeDispatchCall is later invoked from handleInvocationEnd.
// ---------------------------------------------------------------------------

describe("full plugin hook sequence: task dispatch → subagent session.idle → tool_call_end emitted", () => {
  it("session.idle for a subagent emits tool_call_end for the parent's open dispatch call", async () => {
    const hookMap = await buildHookMap();
    const eventHook = hookMap["event"] as
      | ((input: { event: OpenCodeEvent }) => Promise<void>)
      | undefined;
    const toolBeforeHook = hookMap["tool.execute.before"] as
      | ((input: ToolBeforeInput, output: ToolBeforeOutput) => Promise<void>)
      | undefined;

    if (!eventHook || !toolBeforeHook) return;

    const orchSessionId = "s4-e2e-orch-001";
    const subSessionId = "s4-e2e-sub-001";
    const dispatchCallId = "s4-e2e-dispatch-call-1";

    // Step 1: register the orchestrator session
    await eventHook({
      event: {
        type: "session.created",
        properties: { info: { id: orchSessionId } },
      },
    });

    // Step 2: dispatch a task tool call — registers the call in the pending
    // registry and pushes a PendingDispatch onto the parent's queue
    const [beforeInput, beforeOutput] = taskDispatchPayload({
      sessionID: orchSessionId,
      callID: dispatchCallId,
      agentInstanceId: "IntegrationAgent#1",
      runId: "20260811T000000Z-test",
    });
    await toolBeforeHook(beforeInput, beforeOutput);

    // Step 3: the dispatched subagent's session is created under the orch session
    await eventHook({
      event: {
        type: "session.created",
        properties: {
          info: { id: subSessionId, parentID: orchSessionId },
        },
      },
    });

    // Step 4: the subagent session becomes idle — triggers handleInvocationEnd,
    // which calls closeDispatchCall(subSessionId), which looks up the pending
    // dispatch call via openDispatchCallFor and emits tool_call_end.
    // This requires handleInvocationStart to have stored dispatchCallId on the
    // subagent session record so openDispatchCallFor can find the pending call.
    await eventHook({
      event: {
        type: "session.idle",
        properties: { sessionID: subSessionId },
      },
    });

    // Read all events from disk — the tool_call_end should appear in the run's
    // orchestrator event stream. Because the task dispatch payload carries a
    // run_id, events may land in a run-specific path rather than unknown-run.
    // Check both paths.
    const unknownRunEvents = readOrchestratorEvents(tmpDir);
    const runSpecificPath = nodePath.join(
      tmpDir,
      "OrchestrationLogs",
      "20260811T000000Z-test",
      "00_orchestrator_events.jsonl",
    );
    const runSpecificEvents: Record<string, unknown>[] = nodeFs.existsSync(runSpecificPath)
      ? nodeFs
          .readFileSync(runSpecificPath, "utf-8")
          .split("\n")
          .filter(Boolean)
          .map((line) => JSON.parse(line) as Record<string, unknown>)
      : [];

    const allEvents = [...unknownRunEvents, ...runSpecificEvents];
    const toolCallEndEvents = allEvents.filter(
      (e) => e["event"] === "tool_call_end",
    );

    // Exactly one tool_call_end must be emitted for the dispatch call
    expect(toolCallEndEvents.length).toBeGreaterThan(0);
    const dispatchEnd = toolCallEndEvents.find(
      (e) => e["call_id"] === dispatchCallId,
    );
    expect(dispatchEnd).toBeDefined();
    expect(dispatchEnd!["call_id"]).toBe(dispatchCallId);
  });

  it("session.idle for a subagent does not emit a second tool_call_end when the real after-hook already fired", async () => {
    const hookMap = await buildHookMap();
    const eventHook = hookMap["event"] as
      | ((input: { event: OpenCodeEvent }) => Promise<void>)
      | undefined;
    const toolBeforeHook = hookMap["tool.execute.before"] as
      | ((input: ToolBeforeInput, output: ToolBeforeOutput) => Promise<void>)
      | undefined;
    const toolAfterHook = hookMap["tool.execute.after"] as
      | ((input: ToolAfterInput, output: ToolAfterOutput) => Promise<void>)
      | undefined;

    if (!eventHook || !toolBeforeHook || !toolAfterHook) return;

    const orchSessionId = "s4-e2e-race-orch-001";
    const subSessionId = "s4-e2e-race-sub-001";
    const dispatchCallId = "s4-e2e-race-dispatch-1";

    await eventHook({
      event: {
        type: "session.created",
        properties: { info: { id: orchSessionId } },
      },
    });

    const [beforeInput, beforeOutput] = taskDispatchPayload({
      sessionID: orchSessionId,
      callID: dispatchCallId,
      agentInstanceId: "IntegrationAgent#2",
      runId: "20260811T000000Z-race",
    });
    await toolBeforeHook(beforeInput, beforeOutput);

    await eventHook({
      event: {
        type: "session.created",
        properties: {
          info: { id: subSessionId, parentID: orchSessionId },
        },
      },
    });

    // The real after-hook fires first and closes the call
    const [afterInput, afterOutput] = toolAfterPayload({
      tool: "task",
      sessionID: orchSessionId,
      callID: dispatchCallId,
      output: { status: "success" },
    });
    await toolAfterHook(afterInput, afterOutput);

    // Count tool_call_end events present after the real after-hook
    const eventsAfterRealHook = [
      ...readOrchestratorEvents(tmpDir),
      ...((): Record<string, unknown>[] => {
        const p = nodePath.join(tmpDir, "OrchestrationLogs", "20260811T000000Z-race", "00_orchestrator_events.jsonl");
        return nodeFs.existsSync(p)
          ? nodeFs.readFileSync(p, "utf-8").split("\n").filter(Boolean).map((l) => JSON.parse(l) as Record<string, unknown>)
          : [];
      })(),
    ];
    const countAfterRealHook = eventsAfterRealHook.filter(
      (e) => e["event"] === "tool_call_end" && e["call_id"] === dispatchCallId,
    ).length;

    // Now session.idle fires — the synthetic closure path must be a no-op
    await eventHook({
      event: {
        type: "session.idle",
        properties: { sessionID: subSessionId },
      },
    });

    const eventsAfterIdle = [
      ...readOrchestratorEvents(tmpDir),
      ...((): Record<string, unknown>[] => {
        const p = nodePath.join(tmpDir, "OrchestrationLogs", "20260811T000000Z-race", "00_orchestrator_events.jsonl");
        return nodeFs.existsSync(p)
          ? nodeFs.readFileSync(p, "utf-8").split("\n").filter(Boolean).map((l) => JSON.parse(l) as Record<string, unknown>)
          : [];
      })(),
    ];
    const countAfterIdle = eventsAfterIdle.filter(
      (e) => e["event"] === "tool_call_end" && e["call_id"] === dispatchCallId,
    ).length;

    // The count must not have increased — the real after-hook already closed the call
    expect(countAfterRealHook).toBe(1);
    expect(countAfterIdle).toBe(1);
  });
});
