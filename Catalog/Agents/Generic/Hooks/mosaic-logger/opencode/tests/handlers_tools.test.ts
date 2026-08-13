/**
 * handlers_tools.test.ts — Tests for tool call event handlers.
 *
 * Covers:
 *   - handleToolBefore: emits tool_call_start with generated call_id and tool_name
 *   - handleToolBefore: routes orchestrator tool events to orchestrator events file
 *   - handleToolBefore: routes subagent tool events to the owning invocation events file
 *   - handleToolAfter: emits tool_call_end with matching call_id from the pending store
 *   - handleToolAfter: uses fallback call_id when no pending call_id is stored
 *   - call_id correlation: same call_id appears on both tool_call_start and tool_call_end
 *   - High-frequency path: handleToolBefore and handleToolAfter make no SDK calls
 *   - safeHandler (T4.4): errors do not propagate out of wrapped handlers
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import { createToolHandlers, deriveToolOutcome } from "../lib/handlers_tools";
import {
  type HandlerDependencies,
  type SdkClient,
  type ToolBeforeInput,
  type ToolBeforeOutput,
  type ToolAfterInput,
} from "../lib/handlers_session";
import { SessionCorrelationStore } from "../lib/correlation";
import { LogPaths, safeHandler, setDebugLogger } from "../lib/core";
import { taskDispatchPayload, toolBeforePayload, toolAfterPayload } from "./fixtures/opencode_api";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const RUN_ID = "20260101T170000Z-a3f9";
const ORCH_SESSION = "sess-orch-001";
const SUB_SESSION = "sess-sub-001";
const AGENT_INSTANCE_ID = "Research#1";
const TOOL_NAME = "bash";

let tmpDir: string;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "mosaic-ht-test-"));
  setDebugLogger(() => {});
});

afterEach(() => {
  nodeFs.rmSync(tmpDir, { recursive: true, force: true });
  setDebugLogger(() => {});
});

function makeStore(): SessionCorrelationStore {
  return new SessionCorrelationStore();
}

function makePaths(): LogPaths {
  return new LogPaths(tmpDir);
}

type CollectedEvent = { filePath: string; event: Record<string, unknown> };

/** Track whether the SDK client's methods were called. */
function makeSdkWithTracking(): {
  sdk: SdkClient;
  getCalled: () => boolean;
  messagesCalled: () => boolean;
} {
  const tracker = { getCalled: false, messagesCalled: false };
  return {
    sdk: {
      session: {
        get: async () => {
          tracker.getCalled = true;
          return undefined;
        },
        messages: async () => {
          tracker.messagesCalled = true;
          return [];
        },
      },
      app: { log: async () => {} },
    },
    getCalled: () => tracker.getCalled,
    messagesCalled: () => tracker.messagesCalled,
  };
}

let callIdCounter = 0;

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
      timestamp: envelope.timestamp,
      harness: "opencode",
      ...(envelope.sessionId ? { session_id: envelope.sessionId } : {}),
      ...(envelope.runId ? { run_id: envelope.runId } : {}),
      ...Object.fromEntries(
        Object.entries(fields).filter(([, v]) => v !== undefined && v !== null && v !== ""),
      ),
    }),
    appendEvent: async (filePath, event) => {
      collected.push({ filePath, event });
    },
    fallbackCallId: (toolName) => `${toolName ?? "tool"}_call_${++callIdCounter}`,
  };
}

function makeToolBeforeInput(tool: string, sessionID: string): ToolBeforeInput {
  return { tool, sessionID };
}

function makeToolBeforeOutput(args: Record<string, unknown> = {}): ToolBeforeOutput {
  return { args };
}

function makeToolAfterInput(tool: string, sessionID: string): ToolAfterInput {
  return { tool, sessionID };
}

// ---------------------------------------------------------------------------
// handleToolBefore — event emission
// ---------------------------------------------------------------------------

describe("handleToolBefore — tool_call_start emission", () => {
  it("emits a tool_call_start event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleToolBefore(
      makeToolBeforeInput(TOOL_NAME, ORCH_SESSION),
      makeToolBeforeOutput({ command: "echo hello" }),
    );

    const event = collected.find((e) => e.event.event === "tool_call_start");
    expect(event).toBeDefined();
    expect(event!.event.tool_name).toBe(TOOL_NAME);
  });

  it("includes tool_input from the output.args in the emitted event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const args = { command: "ls -la" };
    await handleToolBefore(
      makeToolBeforeInput(TOOL_NAME, ORCH_SESSION),
      makeToolBeforeOutput(args),
    );

    const event = collected.find((e) => e.event.event === "tool_call_start");
    expect(event!.event.tool_input).toEqual(args);
  });

  it("generates and includes a call_id in the event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, ORCH_SESSION), makeToolBeforeOutput());

    const event = collected.find((e) => e.event.event === "tool_call_start");
    expect(event!.event.call_id).toBeDefined();
    expect(typeof event!.event.call_id).toBe("string");
  });
});

// ---------------------------------------------------------------------------
// handleToolBefore — event routing
// ---------------------------------------------------------------------------

describe("handleToolBefore — event routing", () => {
  it("routes orchestrator tool events to the orchestrator events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, ORCH_SESSION), makeToolBeforeOutput());

    const event = collected.find((e) => e.event.event === "tool_call_start");
    expect(event!.filePath).toBe(paths.orchestratorEvents(RUN_ID));
  });

  it("routes subagent tool events to the invocation events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, SUB_SESSION), makeToolBeforeOutput());

    const event = collected.find((e) => e.event.event === "tool_call_start");
    expect(event!.filePath).toBe(paths.invocationEvents(RUN_ID, AGENT_INSTANCE_ID));
  });

  it("never routes a subagent tool event to the orchestrator stream", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, SUB_SESSION), makeToolBeforeOutput());

    const event = collected.find((e) => e.event.event === "tool_call_start");
    expect(event!.filePath).not.toBe(paths.orchestratorEvents(RUN_ID));
  });
});

// ---------------------------------------------------------------------------
// handleToolAfter — event emission
// ---------------------------------------------------------------------------

describe("handleToolAfter — tool_call_end emission", () => {
  it("emits a tool_call_end event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, ORCH_SESSION), makeToolBeforeOutput());
    await handleToolAfter(makeToolAfterInput(TOOL_NAME, ORCH_SESSION));

    const event = collected.find((e) => e.event.event === "tool_call_end");
    expect(event).toBeDefined();
    expect(event!.event.status).toBe("success");
  });

  it("uses a fallback call_id when no pending call_id is stored", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    // Call handleToolAfter without a preceding handleToolBefore
    await handleToolAfter(makeToolAfterInput(TOOL_NAME, ORCH_SESSION));

    const event = collected.find((e) => e.event.event === "tool_call_end");
    expect(event).toBeDefined();
    expect(typeof event!.event.call_id).toBe("string");
  });
});

// ---------------------------------------------------------------------------
// call_id correlation
// ---------------------------------------------------------------------------

describe("call_id correlation between tool_call_start and tool_call_end", () => {
  it("emits the same call_id on both tool_call_start and tool_call_end", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, ORCH_SESSION), makeToolBeforeOutput());
    await handleToolAfter(makeToolAfterInput(TOOL_NAME, ORCH_SESSION));

    const startEvent = collected.find((e) => e.event.event === "tool_call_start");
    const endEvent = collected.find((e) => e.event.event === "tool_call_end");
    expect(startEvent!.event.call_id).toBe(endEvent!.event.call_id);
  });

  it("isolates call_ids across different sessions", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    // Two concurrent sessions, each calls a tool
    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, ORCH_SESSION), makeToolBeforeOutput());
    await handleToolBefore(makeToolBeforeInput("edit", SUB_SESSION), makeToolBeforeOutput());
    await handleToolAfter(makeToolAfterInput(TOOL_NAME, ORCH_SESSION));
    await handleToolAfter(makeToolAfterInput("edit", SUB_SESSION));

    const orchStart = collected.find(
      (e) => e.event.event === "tool_call_start" && e.event.tool_name === TOOL_NAME,
    );
    const orchEnd = collected.find(
      (e) => e.event.event === "tool_call_end" && e.filePath === paths.orchestratorEvents(RUN_ID),
    );
    const subStart = collected.find(
      (e) => e.event.event === "tool_call_start" && e.event.tool_name === "edit",
    );
    const subEnd = collected.find(
      (e) =>
        e.event.event === "tool_call_end" &&
        e.filePath === paths.invocationEvents(RUN_ID, AGENT_INSTANCE_ID),
    );

    expect(orchStart!.event.call_id).toBe(orchEnd!.event.call_id);
    expect(subStart!.event.call_id).toBe(subEnd!.event.call_id);
    // Different sessions get different call_ids
    expect(orchStart!.event.call_id).not.toBe(subStart!.event.call_id);
  });
});

// ---------------------------------------------------------------------------
// High-frequency path — no SDK calls
// ---------------------------------------------------------------------------

describe("high-frequency path — no SDK calls", () => {
  it("handleToolBefore does not call client.session.get()", async () => {
    const store = makeStore();
    const paths = makePaths();
    const tracking = makeSdkWithTracking();
    const deps = makeDeps(store, paths, tracking.sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, ORCH_SESSION), makeToolBeforeOutput());

    expect(tracking.getCalled()).toBe(false);
  });

  it("handleToolBefore does not call client.session.messages()", async () => {
    const store = makeStore();
    const paths = makePaths();
    const tracking = makeSdkWithTracking();
    const deps = makeDeps(store, paths, tracking.sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, ORCH_SESSION), makeToolBeforeOutput());

    expect(tracking.messagesCalled()).toBe(false);
  });

  it("handleToolAfter does not call client.session.get()", async () => {
    const store = makeStore();
    const paths = makePaths();
    const tracking = makeSdkWithTracking();
    const deps = makeDeps(store, paths, tracking.sdk, []);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, ORCH_SESSION), makeToolBeforeOutput());
    const getCalledAfterBefore = tracking.getCalled();
    await handleToolAfter(makeToolAfterInput(TOOL_NAME, ORCH_SESSION));

    // getCalled() should not change after handleToolAfter — it must be false both times
    expect(tracking.getCalled()).toBe(getCalledAfterBefore);
    expect(tracking.getCalled()).toBe(false);
  });

  it("handleToolAfter does not call client.session.messages()", async () => {
    const store = makeStore();
    const paths = makePaths();
    const tracking = makeSdkWithTracking();
    const deps = makeDeps(store, paths, tracking.sdk, []);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleToolBefore(makeToolBeforeInput(TOOL_NAME, ORCH_SESSION), makeToolBeforeOutput());
    await handleToolAfter(makeToolAfterInput(TOOL_NAME, ORCH_SESSION));

    expect(tracking.messagesCalled()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// T4.4 — Defensive error handling via safeHandler
// ---------------------------------------------------------------------------

describe("safeHandler — defensive error wrapping", () => {
  it("does not propagate errors thrown by the wrapped handler", async () => {
    const failingHandler = async (_input: string) => {
      throw new Error("Intentional handler failure");
    };

    const wrapped = safeHandler("test-handler", failingHandler);

    // Must not throw
    await expect(wrapped("some input")).resolves.toBeUndefined();
  });

  it("preserves the return type signature after wrapping", async () => {
    let wasCalled = false;
    const handler = async (_x: number, _y: string) => {
      wasCalled = true;
    };

    const wrapped = safeHandler("noop", handler);
    await wrapped(42, "hello");

    expect(wasCalled).toBe(true);
  });

  it("logs the error via debugLog and does not re-throw", async () => {
    const loggedMessages: string[] = [];
    setDebugLogger((msg) => {
      loggedMessages.push(msg);
    });

    const failingHandler = async () => {
      throw new Error("boom");
    };

    const wrapped = safeHandler("my-handler", failingHandler);
    await wrapped();

    // At least one log message should mention the handler name
    expect(loggedMessages.some((m) => m.includes("my-handler"))).toBe(true);

    // Reset logger
    setDebugLogger(() => {});
  });

  it("allows subsequent calls to succeed after a previous call threw", async () => {
    let callCount = 0;
    const handler = async () => {
      callCount++;
      if (callCount === 1) throw new Error("first call fails");
      // second call succeeds
    };

    const wrapped = safeHandler("retry-handler", handler);
    await wrapped(); // first call: throws internally, swallowed
    await wrapped(); // second call: should succeed

    expect(callCount).toBe(2);
  });

  it("a real tool handler never propagates errors to the caller", async () => {
    // Simulate a crashing appendEvent
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();

    const crashingDeps: HandlerDependencies = {
      store,
      paths,
      sdkClient: sdk,
      adapterVersion: undefined,
      buildEvent: () => ({ event: "tool_call_start", schema_version: "1.0.0", timestamp: "t", harness: "opencode" }),
      appendEvent: async () => {
        throw new Error("disk full");
      },
      fallbackCallId: () => "call-id-1",
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const { handleToolBefore } = createToolHandlers(crashingDeps);
    const safeToolBefore = safeHandler(
      "tool.execute.before",
      async (input: ToolBeforeInput, output: ToolBeforeOutput) =>
        handleToolBefore(input, output),
    );

    await expect(
      safeToolBefore(makeToolBeforeInput(TOOL_NAME, ORCH_SESSION), makeToolBeforeOutput()),
    ).resolves.toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// task dispatch identity capture — Stage 3
//
// These tests specify that handleToolBefore, when the tool is "task", reads
// identity fields from the dispatch args and pushes a PendingDispatch entry
// onto the per-parent queue via store.pushPendingDispatch(). All tests in this
// section are RED because:
//   1. SessionCorrelationStore does not yet have pushPendingDispatch /
//      claimPendingDispatch / pendingDispatchCount — TypeScript compilation fails.
//   2. handleToolBefore does not yet call store.pushPendingDispatch for task calls.
// ---------------------------------------------------------------------------

describe("handleToolBefore — task dispatch captures identity into pending dispatch queue", () => {
  it("pushes a pending dispatch when the tool is 'task'", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [input, output] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      callID: "call-dispatch-001",
      agentInstanceId: "Research#1",
      runId: RUN_ID,
      subagentType: "codebase-research",
    });

    await handleToolBefore(input, output);

    expect(store.pendingDispatchCount(ORCH_SESSION)).toBe(1);
  });

  it("extracts agentInstanceId from the task dispatch prompt and stores it in the pending dispatch", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [input, output] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      agentInstanceId: "Planner#5",
      runId: RUN_ID,
    });

    await handleToolBefore(input, output);

    const dispatch = store.claimPendingDispatch(ORCH_SESSION);
    expect(dispatch!.agentInstanceId).toBe("Planner#5");
  });

  it("extracts runId from the task dispatch prompt and stores it in the pending dispatch", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [input, output] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      agentInstanceId: "Reviewer#2",
      runId: "20260808T120000Z-beef",
    });

    await handleToolBefore(input, output);

    const dispatch = store.claimPendingDispatch(ORCH_SESSION);
    expect(dispatch!.runId).toBe("20260808T120000Z-beef");
  });

  it("reads agentType from the separate subagent_type field rather than splitting the instance id", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [input, output] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      agentInstanceId: "codebase-research#3",
      runId: RUN_ID,
      subagentType: "codebase-research",
    });

    await handleToolBefore(input, output);

    const dispatch = store.claimPendingDispatch(ORCH_SESSION);
    expect(dispatch!.agentType).toBe("codebase-research");
  });

  it("stores the prompt text in the pending dispatch for use as 01_input.md content", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [input, output] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      agentInstanceId: "TestAgent#1",
      runId: RUN_ID,
    });

    await handleToolBefore(input, output);

    const dispatch = store.claimPendingDispatch(ORCH_SESSION);
    expect(typeof dispatch!.prompt).toBe("string");
    expect(dispatch!.prompt).toContain("TestAgent#1");
  });

  it("stores the parentSessionId in the pending dispatch", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [input, output] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      agentInstanceId: "TestAgent#1",
      runId: RUN_ID,
    });

    await handleToolBefore(input, output);

    const dispatch = store.claimPendingDispatch(ORCH_SESSION);
    expect(dispatch!.parentSessionId).toBe(ORCH_SESSION);
  });

  it("still pushes a dispatch when the task prompt carries no recognisable identity", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    // A task call whose prompt has no agent_instance_id or run_id
    const [input, output] = toolBeforePayload({
      tool: "task",
      sessionID: ORCH_SESSION,
      callID: "call-no-identity",
      args: { prompt: "Just run the thing with no structured identity." },
    });

    await handleToolBefore(input, output);

    const dispatch = store.claimPendingDispatch(ORCH_SESSION);
    expect(dispatch).toBeDefined();
    expect(dispatch!.agentInstanceId).toBeUndefined();
    expect(dispatch!.runId).toBeUndefined();
  });

  it("does not push a pending dispatch for non-task tools", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleToolBefore(
      makeToolBeforeInput("bash", ORCH_SESSION),
      makeToolBeforeOutput({ command: "echo hello" }),
    );

    expect(store.pendingDispatchCount(ORCH_SESSION)).toBe(0);
    expect(store.claimPendingDispatch(ORCH_SESSION)).toBeUndefined();
  });

  it("uses the native input.callID as the dispatch callId when available", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [input, output] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      callID: "native-call-id-xyz",
      agentInstanceId: "TestAgent#1",
      runId: RUN_ID,
    });

    await handleToolBefore(input, output);

    const dispatch = store.claimPendingDispatch(ORCH_SESSION);
    expect(dispatch!.callId).toBe("native-call-id-xyz");
  });

  it("does not call any SDK methods when processing a task dispatch (high-frequency path)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const tracking = makeSdkWithTracking();
    const deps = makeDeps(store, paths, tracking.sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [input, output] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      agentInstanceId: "TestAgent#1",
      runId: RUN_ID,
    });

    await handleToolBefore(input, output);

    expect(tracking.getCalled()).toBe(false);
    expect(tracking.messagesCalled()).toBe(false);
  });

  it("queues multiple task dispatches from one session in the order they are received", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [input1, output1] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      agentInstanceId: "Writer#1",
      runId: RUN_ID,
    });
    const [input2, output2] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      agentInstanceId: "Reviewer#2",
      runId: RUN_ID,
    });

    await handleToolBefore(input1, output1);
    await handleToolBefore(input2, output2);

    expect(store.pendingDispatchCount(ORCH_SESSION)).toBe(2);
    expect(store.claimPendingDispatch(ORCH_SESSION)!.agentInstanceId).toBe("Writer#1");
    expect(store.claimPendingDispatch(ORCH_SESSION)!.agentInstanceId).toBe("Reviewer#2");
  });

  it("still emits tool_call_start normally for a task dispatch", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [input, output] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      agentInstanceId: "Research#1",
      runId: RUN_ID,
    });

    await handleToolBefore(input, output);

    const startEvent = collected.find((e) => e.event.event === "tool_call_start");
    expect(startEvent).toBeDefined();
    expect(startEvent!.event.tool_name).toBe("task");
  });
});

// ---------------------------------------------------------------------------
// deriveToolOutcome — defensive outcome status derivation
//
// These tests specify the new deriveToolOutcome export that does not yet exist
// as a real implementation. All tests below are RED because deriveToolOutcome
// currently throws "not yet implemented".
// ---------------------------------------------------------------------------

describe("deriveToolOutcome — tool call outcome derivation", () => {
  it("returns error status with captured detail when the payload carries an error field", () => {
    const [input] = toolAfterPayload({
      tool: TOOL_NAME,
      sessionID: ORCH_SESSION,
      callID: "c-err-1",
      output: { error: "command not found" },
    });
    const outcome = deriveToolOutcome(input, { error: "command not found" });
    expect(outcome.status).toBe("error");
    expect(outcome.error).toBe("command not found");
  });

  it("returns error status when the payload status field equals 'error'", () => {
    const [input] = toolAfterPayload({ tool: TOOL_NAME, sessionID: ORCH_SESSION });
    const outcome = deriveToolOutcome(input, { status: "error" });
    expect(outcome.status).toBe("error");
  });

  it("returns success status when the payload status field equals 'success'", () => {
    const [input] = toolAfterPayload({ tool: TOOL_NAME, sessionID: ORCH_SESSION });
    const outcome = deriveToolOutcome(input, { status: "success" });
    expect(outcome.status).toBe("success");
  });

  it("returns success status when the payload carries success: true", () => {
    const [input] = toolAfterPayload({ tool: TOOL_NAME, sessionID: ORCH_SESSION });
    const outcome = deriveToolOutcome(input, { success: true });
    expect(outcome.status).toBe("success");
  });

  it("omits status entirely when the payload carries no recognizable outcome signal", () => {
    const [input] = toolAfterPayload({ tool: TOOL_NAME, sessionID: ORCH_SESSION });
    const outcome = deriveToolOutcome(input, { something: "unrecognized payload structure" });
    expect("status" in outcome).toBe(false);
  });

  it("omits status entirely when the output is undefined", () => {
    const [input] = toolAfterPayload({ tool: TOOL_NAME, sessionID: ORCH_SESSION });
    const outcome = deriveToolOutcome(input, undefined);
    expect("status" in outcome).toBe(false);
  });

  it("captures error detail in the error field alongside error status", () => {
    const [input] = toolAfterPayload({ tool: TOOL_NAME, sessionID: ORCH_SESSION });
    const outcome = deriveToolOutcome(input, { status: "error", error: "permission denied" });
    expect(outcome.status).toBe("error");
    expect(typeof outcome.error).toBe("string");
    expect(outcome.error!.length).toBeGreaterThan(0);
  });

  it("does not set error detail when the outcome is success", () => {
    const [input] = toolAfterPayload({ tool: TOOL_NAME, sessionID: ORCH_SESSION });
    const outcome = deriveToolOutcome(input, { status: "success" });
    expect(outcome.error).toBeUndefined();
  });

  it("does not set error detail when the outcome is ambiguous", () => {
    const [input] = toolAfterPayload({ tool: TOOL_NAME, sessionID: ORCH_SESSION });
    const outcome = deriveToolOutcome(input, { output: "some text" });
    expect(outcome.error).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// handleToolAfter — status derived from payload, not hardcoded
//
// These tests specify that handleToolAfter must use deriveToolOutcome to
// determine the status field, not hardcode "success". They are RED because:
//   1. deriveToolOutcome currently throws.
//   2. Even if deriveToolOutcome were implemented, the current handleToolAfter
//      ignores the output parameter and hardcodes status: "success", so error
//      and ambiguous payloads would produce the wrong status.
// ---------------------------------------------------------------------------

describe("handleToolAfter — status field derived from payload, not hardcoded", () => {
  it("emits error status when the after-hook output clearly signals failure", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [beforeInput, beforeOutput] = toolBeforePayload({
      tool: TOOL_NAME,
      sessionID: ORCH_SESSION,
      callID: "call-err-status",
    });
    await handleToolBefore(beforeInput, beforeOutput);

    const [afterInput, afterOutput] = toolAfterPayload({
      tool: TOOL_NAME,
      sessionID: ORCH_SESSION,
      callID: "call-err-status",
      output: { error: "exit code 1" },
    });
    await handleToolAfter(afterInput, afterOutput);

    const endEvent = collected.find((e) => e.event.event === "tool_call_end");
    expect(endEvent).toBeDefined();
    expect(endEvent!.event.status).toBe("error");
    expect(endEvent!.event.error).toBeTruthy();
  });

  it("omits status when the after-hook output signals neither success nor failure", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [beforeInput, beforeOutput] = toolBeforePayload({
      tool: TOOL_NAME,
      sessionID: ORCH_SESSION,
      callID: "call-ambig-status",
    });
    await handleToolBefore(beforeInput, beforeOutput);

    const [afterInput, afterOutput] = toolAfterPayload({
      tool: TOOL_NAME,
      sessionID: ORCH_SESSION,
      callID: "call-ambig-status",
      output: { output: "some output text without any status signal" },
    });
    await handleToolAfter(afterInput, afterOutput);

    const endEvent = collected.find((e) => e.event.event === "tool_call_end");
    expect(endEvent).toBeDefined();
    // Must not unconditionally claim success when the payload gives no indication
    expect("status" in endEvent!.event).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Parallel tool calls in one session — call-id correlation
//
// Each tool call is registered in the keyed registry under its native callID so
// parallel calls in one session each correlate their start and end independently.
// ---------------------------------------------------------------------------

describe("parallel tool calls in one session — call-id correlation", () => {
  it("each end event carries its own start's native call_id when two calls run concurrently", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    // Two tool calls start in the same session before either ends.
    const [beforeInputA, beforeOutputA] = toolBeforePayload({
      tool: "bash",
      sessionID: ORCH_SESSION,
      callID: "parallel-call-A",
    });
    const [beforeInputB, beforeOutputB] = toolBeforePayload({
      tool: "edit",
      sessionID: ORCH_SESSION,
      callID: "parallel-call-B",
    });
    await handleToolBefore(beforeInputA, beforeOutputA);
    await handleToolBefore(beforeInputB, beforeOutputB);

    // After-hooks arrive: first for A, then for B.
    const [afterInputA, afterOutputA] = toolAfterPayload({
      tool: "bash",
      sessionID: ORCH_SESSION,
      callID: "parallel-call-A",
    });
    const [afterInputB, afterOutputB] = toolAfterPayload({
      tool: "edit",
      sessionID: ORCH_SESSION,
      callID: "parallel-call-B",
    });
    await handleToolAfter(afterInputA, afterOutputA);
    await handleToolAfter(afterInputB, afterOutputB);

    const endEvents = collected.filter((e) => e.event.event === "tool_call_end");
    expect(endEvents).toHaveLength(2);

    const endA = endEvents.find((e) => e.event.call_id === "parallel-call-A");
    const endB = endEvents.find((e) => e.event.call_id === "parallel-call-B");

    // Each end event must carry its own start's call_id — not the other's.
    expect(endA).toBeDefined();
    expect(endB).toBeDefined();
  });

  it("closing one parallel call does not produce an end event for the other", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [beforeInputA, beforeOutputA] = toolBeforePayload({
      tool: "bash",
      sessionID: ORCH_SESSION,
      callID: "par-call-X",
    });
    const [beforeInputB, beforeOutputB] = toolBeforePayload({
      tool: "read",
      sessionID: ORCH_SESSION,
      callID: "par-call-Y",
    });
    await handleToolBefore(beforeInputA, beforeOutputA);
    await handleToolBefore(beforeInputB, beforeOutputB);

    // Only close call Y — call X remains open.
    const [afterInputB, afterOutputB] = toolAfterPayload({
      tool: "read",
      sessionID: ORCH_SESSION,
      callID: "par-call-Y",
    });
    await handleToolAfter(afterInputB, afterOutputB);

    const endEvents = collected.filter((e) => e.event.event === "tool_call_end");
    expect(endEvents).toHaveLength(1);
    expect(endEvents[0]!.event.call_id).toBe("par-call-Y");
    expect(endEvents[0]!.event.call_id).not.toBe("par-call-X");
  });

  it("three parallel calls in the same session each emit the correct call_id", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const callIds = ["multi-call-1", "multi-call-2", "multi-call-3"];
    const tools = ["bash", "edit", "read"];

    // All three start before any ends.
    for (let i = 0; i < 3; i++) {
      const [beforeInput, beforeOutput] = toolBeforePayload({
        tool: tools[i]!,
        sessionID: ORCH_SESSION,
        callID: callIds[i]!,
      });
      await handleToolBefore(beforeInput, beforeOutput);
    }

    // End them in reverse order to stress the correlation.
    for (let i = 2; i >= 0; i--) {
      const [afterInput, afterOutput] = toolAfterPayload({
        tool: tools[i]!,
        sessionID: ORCH_SESSION,
        callID: callIds[i]!,
      });
      await handleToolAfter(afterInput, afterOutput);
    }

    const endEvents = collected.filter((e) => e.event.event === "tool_call_end");
    expect(endEvents).toHaveLength(3);

    for (const expectedCallId of callIds) {
      const match = endEvents.find((e) => e.event.call_id === expectedCallId);
      expect(match).toBeDefined();
    }
  });
});

// ---------------------------------------------------------------------------
// Real dispatch closure — after-hook on task call
//
// These tests specify that when the real tool.execute.after arrives for a
// `task` call, exactly one tool_call_end is emitted carrying the original
// call_id and closure is idempotent.
//
// Some tests in this section are RED because:
//   1. The idempotency tests (calling handleToolAfter twice) emit two events
//      with the current single-slot implementation — the first call pops the
//      stored call_id, the second call finds an empty slot and uses a fallback.
//   2. Tests involving parallel tool calls interleaved with the task call fail
//      because the single-slot approach overwrites the task's call_id with the
//      second tool's call_id.
// ---------------------------------------------------------------------------

describe("real dispatch closure — after-hook on task call", () => {
  it("emits exactly one tool_call_end when after-hook arrives for a task dispatch", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [beforeInput, beforeOutput] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      callID: "dispatch-after-real",
      agentInstanceId: "Research#1",
      runId: RUN_ID,
    });
    await handleToolBefore(beforeInput, beforeOutput);

    const [afterInput, afterOutput] = toolAfterPayload({
      tool: "task",
      sessionID: ORCH_SESSION,
      callID: "dispatch-after-real",
    });
    await handleToolAfter(afterInput, afterOutput);

    const endEvents = collected.filter((e) => e.event.event === "tool_call_end");
    expect(endEvents).toHaveLength(1);
    expect(endEvents[0]!.event.call_id).toBe("dispatch-after-real");
  });

  it("calling after-hook twice for the same task call emits only one tool_call_end", async () => {
    // The second call must be a no-op: the call has already been closed.
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const [beforeInput, beforeOutput] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      callID: "dispatch-idem-real",
      agentInstanceId: "Research#1",
      runId: RUN_ID,
    });
    await handleToolBefore(beforeInput, beforeOutput);

    const [afterInput, afterOutput] = toolAfterPayload({
      tool: "task",
      sessionID: ORCH_SESSION,
      callID: "dispatch-idem-real",
    });
    await handleToolAfter(afterInput, afterOutput); // first — should emit
    await handleToolAfter(afterInput, afterOutput); // second — must be a no-op

    const endEvents = collected.filter((e) => e.event.event === "tool_call_end");
    expect(endEvents).toHaveLength(1);
  });

  it("task after-hook carries the dispatch's native call_id even when another tool was interleaved", async () => {
    // A bash tool call starts between the task start and the task end.
    // The task's end must still use the task's native call_id, not the bash call's.
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    // Task dispatch starts.
    const [taskBefore, taskOutput] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      callID: "task-call-interleaved",
      agentInstanceId: "Research#1",
      runId: RUN_ID,
    });
    await handleToolBefore(taskBefore, taskOutput);

    // Bash starts while the task is still running.
    const [bashBefore, bashOutput] = toolBeforePayload({
      tool: "bash",
      sessionID: ORCH_SESSION,
      callID: "bash-call-interleaved",
    });
    await handleToolBefore(bashBefore, bashOutput);

    // Task after-hook arrives before bash ends.
    const [taskAfter, taskAfterOutput] = toolAfterPayload({
      tool: "task",
      sessionID: ORCH_SESSION,
      callID: "task-call-interleaved",
    });
    await handleToolAfter(taskAfter, taskAfterOutput);

    const taskEndEvent = collected.find(
      (e) => e.event.event === "tool_call_end" && e.event.call_id === "task-call-interleaved",
    );
    expect(taskEndEvent).toBeDefined();

    // The bash call must still be open — no end event for it yet.
    const bashEndEvent = collected.find(
      (e) => e.event.event === "tool_call_end" && e.event.call_id === "bash-call-interleaved",
    );
    expect(bashEndEvent).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Synthetic dispatch closure — closeDispatchCall on invocation end
//
// These tests specify the new closeDispatchCall function that does not yet
// exist as a real implementation. All tests below are RED because
// closeDispatchCall currently throws "not yet implemented".
// ---------------------------------------------------------------------------

describe("synthetic dispatch closure — closeDispatchCall on invocation end", () => {
  it("emits a tool_call_end with the original dispatch call_id", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, closeDispatchCall } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      dispatchCallId: "dispatch-synth-1",
    });

    const [beforeInput, beforeOutput] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      callID: "dispatch-synth-1",
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
    });
    await handleToolBefore(beforeInput, beforeOutput);

    // Invocation ends without the real after-hook firing.
    await closeDispatchCall(SUB_SESSION);

    const endEvents = collected.filter((e) => e.event.event === "tool_call_end");
    expect(endEvents).toHaveLength(1);
    expect(endEvents[0]!.event.call_id).toBe("dispatch-synth-1");
  });

  it("synthetic tool_call_end carries no tool_output (the synthetic path has no output payload)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, closeDispatchCall } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      dispatchCallId: "dispatch-noout-1",
    });

    const [beforeInput, beforeOutput] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      callID: "dispatch-noout-1",
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
    });
    await handleToolBefore(beforeInput, beforeOutput);
    await closeDispatchCall(SUB_SESSION);

    const endEvent = collected.find((e) => e.event.event === "tool_call_end");
    expect(endEvent).toBeDefined();
    expect(endEvent!.event.tool_output).toBeUndefined();
  });

  it("is a no-op when no dispatch call is open for the child session", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { closeDispatchCall } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      // No dispatchCallId — no pending dispatch.
    });

    await closeDispatchCall(SUB_SESSION);

    const endEvents = collected.filter((e) => e.event.event === "tool_call_end");
    expect(endEvents).toHaveLength(0);
  });

  it("does not make any SDK calls (high-frequency path constraint)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const tracking = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, tracking.sdk, collected);
    const { handleToolBefore, closeDispatchCall } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      dispatchCallId: "dispatch-nosdk",
    });

    const [beforeInput, beforeOutput] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      callID: "dispatch-nosdk",
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
    });
    await handleToolBefore(beforeInput, beforeOutput);
    await closeDispatchCall(SUB_SESSION);

    expect(tracking.getCalled()).toBe(false);
    expect(tracking.messagesCalled()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Dispatch closure race — first-wins idempotency
//
// These tests specify that whichever closure path arrives first wins, and the
// second path must be a no-op so exactly one tool_call_end is always emitted.
// All tests below are RED because closeDispatchCall throws "not yet implemented".
// ---------------------------------------------------------------------------

describe("dispatch closure race — first-wins idempotency", () => {
  it("emits exactly one tool_call_end when the real after-hook arrives before synthetic closure", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter, closeDispatchCall } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      dispatchCallId: "dispatch-race-real-first",
    });

    const [beforeInput, beforeOutput] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      callID: "dispatch-race-real-first",
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
    });
    await handleToolBefore(beforeInput, beforeOutput);

    // Real after-hook arrives first.
    const [afterInput, afterOutput] = toolAfterPayload({
      tool: "task",
      sessionID: ORCH_SESSION,
      callID: "dispatch-race-real-first",
    });
    await handleToolAfter(afterInput, afterOutput);

    // Synthetic closure also fires (e.g. invocation-end arrives too).
    await closeDispatchCall(SUB_SESSION);

    const endEvents = collected.filter((e) => e.event.event === "tool_call_end");
    expect(endEvents).toHaveLength(1);
  });

  it("emits exactly one tool_call_end when synthetic closure arrives before the real after-hook", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleToolBefore, handleToolAfter, closeDispatchCall } = createToolHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      dispatchCallId: "dispatch-race-synth-first",
    });

    const [beforeInput, beforeOutput] = taskDispatchPayload({
      sessionID: ORCH_SESSION,
      callID: "dispatch-race-synth-first",
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
    });
    await handleToolBefore(beforeInput, beforeOutput);

    // Synthetic closure arrives first.
    await closeDispatchCall(SUB_SESSION);

    // Real after-hook also arrives — must be a no-op.
    const [afterInput, afterOutput] = toolAfterPayload({
      tool: "task",
      sessionID: ORCH_SESSION,
      callID: "dispatch-race-synth-first",
    });
    await handleToolAfter(afterInput, afterOutput);

    const endEvents = collected.filter((e) => e.event.event === "tool_call_end");
    expect(endEvents).toHaveLength(1);
  });

  it("the winning event carries the dispatch's original call_id regardless of which path fires first", async () => {
    // Run both orderings and verify the emitted call_id is the dispatch's.
    async function runRace(realFirst: boolean): Promise<string | undefined> {
      const store = makeStore();
      const paths = makePaths();
      const { sdk } = makeSdkWithTracking();
      const collected: CollectedEvent[] = [];
      const deps = makeDeps(store, paths, sdk, collected);
      const { handleToolBefore, handleToolAfter, closeDispatchCall } = createToolHandlers(deps);

      const callId = realFirst ? "race-call-real-first" : "race-call-synth-first";

      store.register(ORCH_SESSION, { runId: RUN_ID });
      store.register(SUB_SESSION, {
        parentId: ORCH_SESSION,
        agentInstanceId: AGENT_INSTANCE_ID,
        dispatchCallId: callId,
      });

      const [beforeInput, beforeOutput] = taskDispatchPayload({
        sessionID: ORCH_SESSION,
        callID: callId,
        agentInstanceId: AGENT_INSTANCE_ID,
        runId: RUN_ID,
      });
      await handleToolBefore(beforeInput, beforeOutput);

      const [afterInput, afterOutput] = toolAfterPayload({
        tool: "task",
        sessionID: ORCH_SESSION,
        callID: callId,
      });

      if (realFirst) {
        await handleToolAfter(afterInput, afterOutput);
        await closeDispatchCall(SUB_SESSION);
      } else {
        await closeDispatchCall(SUB_SESSION);
        await handleToolAfter(afterInput, afterOutput);
      }

      const endEvents = collected.filter((e) => e.event.event === "tool_call_end");
      return typeof endEvents[0]?.event.call_id === "string"
        ? endEvents[0].event.call_id
        : undefined;
    }

    const realFirstCallId = await runRace(true);
    const synthFirstCallId = await runRace(false);

    expect(realFirstCallId).toBe("race-call-real-first");
    expect(synthFirstCallId).toBe("race-call-synth-first");
  });
});
