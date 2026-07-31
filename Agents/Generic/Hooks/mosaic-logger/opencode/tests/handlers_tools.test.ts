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

import { createToolHandlers } from "../lib/handlers_tools";
import {
  type HandlerDependencies,
  type SdkClient,
  type ToolBeforeInput,
  type ToolBeforeOutput,
  type ToolAfterInput,
} from "../lib/handlers_session";
import { SessionCorrelationStore } from "../lib/correlation";
import { LogPaths, safeHandler, setDebugLogger } from "../lib/core";

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
