/**
 * lifecycle_e2e.test.ts — End-to-end invocation lifecycle tests.
 *
 * Covers (Stage 2, T2.3 and supplementary T2.4):
 *
 * T2.3 — End-to-end invocation lifecycle reachability:
 *   A subagent session.created followed by session.idle (with invocationCallbacks
 *   wired into the session handler) produces:
 *     - A matched invocation_start / invocation_end event pair
 *     - A written 02_output.md output artifact
 *     - A written 04_session.raw session transcript export
 *   These are all currently unreachable because the `stop` hook the invocation-end
 *   handler depends on does not exist in OpenCode's real Hooks interface.
 *
 * T2.4 (end-to-end aspect) — A second session.idle for the same already-ended
 *   subagent session emits no second invocation_end event.
 *
 * Wiring model:
 *   Tests create both session and invocation handler sets from a shared deps object,
 *   then late-bind deps.invocationCallbacks to the invocation handlers — exactly
 *   as the plugin factory will do after Stage 2 implementation. The session handler
 *   accesses invocationCallbacks via the deps reference (not a destructured copy),
 *   so the late assignment is visible inside the handler closures.
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import {
  createSessionHandlers,
  type HandlerDependencies,
  type SdkClient,
} from "../lib/handlers_session";
import { createInvocationHandlers } from "../lib/handlers_invocation";
import { SessionCorrelationStore } from "../lib/correlation";
import { LogPaths, setDebugLogger } from "../lib/core";
import {
  userMessageEntry,
  assistantMessageEntry,
  sessionCreatedEvent,
  sessionIdleEvent,
} from "./fixtures/opencode_api";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const RUN_ID = "20260101T170000Z-a3f9";
const ORCH_SESSION = "sess-orch-e2e-001";
const SUB_SESSION = "sess-sub-e2e-001";
const AGENT_INSTANCE_ID = "Research#1";

/**
 * A realistic dispatch prompt in the structured key-value form that
 * extractInstanceId / extractRunId already match. This is what the SDK
 * returns when the orchestrator queries the subagent session's messages.
 */
const DISPATCH_PROMPT = JSON.stringify({
  agent_instance_id: AGENT_INSTANCE_ID,
  run_id: RUN_ID,
  task_description: "Research the market landscape",
});

const FINAL_RESPONSE = `Here are the findings.

{"status_code": "SUCCESS", "status_message": "Research complete."}`;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let tmpDir: string;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(
    nodePath.join(nodeOs.tmpdir(), "mosaic-e2e-test-"),
  );
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

/**
 * SDK that returns the given messages for the subagent session and an empty
 * array for everything else.
 */
function makeSubagentSdk(
  subagentMessages: unknown[],
  orchestratorMessages?: unknown[],
): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async ({ path }) => {
        if (path.id === SUB_SESSION) return subagentMessages;
        if (path.id === ORCH_SESSION) return orchestratorMessages ?? [];
        return [];
      },
    },
    app: { log: async () => {} },
  };
}

type CollectedEvent = { filePath: string; event: Record<string, unknown> };

/**
 * Create a HandlerDependencies object whose appendEvent records all calls.
 * Does NOT set invocationCallbacks — the caller does that after creating
 * handler instances (late-binding, matching the plugin factory wiring).
 */
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
    adapterVersion: "0.1.0-test",
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
    fallbackCallId: (toolName) => `${toolName ?? "tool"}_fallback`,
    // invocationCallbacks intentionally absent here — set after handler creation
  };
}

// ---------------------------------------------------------------------------
// T2.3 — dispatch-then-idle produces invocation_start + invocation_end pair
// ---------------------------------------------------------------------------

describe("end-to-end lifecycle — session.created then session.idle (T2.3)", () => {
  it("subagent session.created triggers invocation_start via invocationCallbacks", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const sdk = makeSubagentSdk(
      [userMessageEntry(DISPATCH_PROMPT)],
    );

    const deps = makeDeps(store, paths, sdk, collected);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    // Late-bind invocationCallbacks — this is how the plugin factory wires them
    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    // Register orchestrator and fire subagent session.created
    store.register(ORCH_SESSION, { runId: RUN_ID });
    await sessionHandlers.handleEvent(sessionCreatedEvent(SUB_SESSION, ORCH_SESSION));

    // FAILS until I2.2: the session handler currently does not call
    // invocationCallbacks.handleInvocationStart on session.created for subagents
    const invStart = collected.find((e) => e.event.event === "invocation_start");
    expect(invStart).toBeDefined();
    expect(invStart?.event.agent_instance_id).toBe(AGENT_INSTANCE_ID);
  });

  it("subagent session.idle triggers invocation_end via invocationCallbacks", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const sdk = makeSubagentSdk(
      [userMessageEntry(DISPATCH_PROMPT), assistantMessageEntry(FINAL_RESPONSE)],
    );

    const deps = makeDeps(store, paths, sdk, collected);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    // Set up a session that has already been through invocation start
    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
      agentType: "Research",
    });

    // Fire session.idle for the subagent
    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    // FAILS until I2.2: the session handler currently does not call
    // invocationCallbacks.handleInvocationEnd on session.idle for subagents
    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invEnd).toBeDefined();
    expect(invEnd?.event.agent_instance_id).toBe(AGENT_INSTANCE_ID);
  });

  it("session.created + session.idle produces a matched invocation_start / invocation_end pair", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const sdk = makeSubagentSdk(
      [userMessageEntry(DISPATCH_PROMPT), assistantMessageEntry(FINAL_RESPONSE)],
    );

    const deps = makeDeps(store, paths, sdk, collected);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });

    // Simulate the full subagent lifecycle through session events
    await sessionHandlers.handleEvent(sessionCreatedEvent(SUB_SESSION, ORCH_SESSION));
    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    const invStart = collected.find((e) => e.event.event === "invocation_start");
    const invEnd = collected.find((e) => e.event.event === "invocation_end");

    // FAILS until I2.2: both callbacks are currently unreachable
    expect(invStart).toBeDefined();
    expect(invEnd).toBeDefined();

    // The pair must reference the same agent_instance_id
    expect(invStart?.event.agent_instance_id).toBe(invEnd?.event.agent_instance_id);
  });

  it("invocation_end carries a status_code extracted from the final assistant response", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const sdk = makeSubagentSdk(
      [userMessageEntry(DISPATCH_PROMPT), assistantMessageEntry(FINAL_RESPONSE)],
    );

    const deps = makeDeps(store, paths, sdk, collected);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
      agentType: "Research",
    });

    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    // FAILS until I2.2
    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invEnd).toBeDefined();
    expect(invEnd?.event.status_code).toBe("SUCCESS");
  });
});

// ---------------------------------------------------------------------------
// T2.3 — output artifact and session transcript written on idle
// ---------------------------------------------------------------------------

describe("end-to-end lifecycle — output artifacts written on session.idle (T2.3)", () => {
  it("02_output.md is written when a subagent session goes idle", async () => {
    const store = makeStore();
    const paths = makePaths();

    const sdk = makeSubagentSdk(
      [userMessageEntry(DISPATCH_PROMPT), assistantMessageEntry(FINAL_RESPONSE)],
    );

    const deps = makeDeps(store, paths, sdk, []);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
      agentType: "Research",
    });

    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    // FAILS until I2.2: 02_output.md is written inside handleInvocationEnd which
    // is currently unreachable via session.idle
    const outputPath = paths.invocationOutput(RUN_ID, AGENT_INSTANCE_ID);
    expect(nodeFs.existsSync(outputPath)).toBe(true);
  });

  it("04_session.raw session transcript is exported when a subagent session goes idle", async () => {
    const store = makeStore();
    const paths = makePaths();

    const sdk = makeSubagentSdk(
      [userMessageEntry(DISPATCH_PROMPT), assistantMessageEntry(FINAL_RESPONSE)],
    );

    const deps = makeDeps(store, paths, sdk, []);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
      agentType: "Research",
    });

    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    // FAILS until I2.2: 04_session.raw is exported inside handleInvocationEnd which
    // is currently unreachable via session.idle
    const rawPath = paths.invocationRaw(RUN_ID, AGENT_INSTANCE_ID);
    expect(nodeFs.existsSync(rawPath)).toBe(true);
  });

  it("the orchestrator transcript is refreshed when a subagent session goes idle", async () => {
    const store = makeStore();
    const paths = makePaths();

    const sdk = makeSubagentSdk(
      [userMessageEntry(DISPATCH_PROMPT), assistantMessageEntry(FINAL_RESPONSE)],
      [userMessageEntry("dispatch message")],
    );

    const deps = makeDeps(store, paths, sdk, []);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
      agentType: "Research",
    });

    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    // FAILS until I2.2: orchestrator transcript refresh is inside handleInvocationEnd
    const orchRawPath = paths.orchestratorRaw(RUN_ID);
    expect(nodeFs.existsSync(orchRawPath)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// T2.4 — end-to-end repeat-idle idempotency
//
// A second session.idle for the same subagent must not emit a second
// invocation_end or write a second set of artifacts.
// ---------------------------------------------------------------------------

describe("end-to-end repeat-idle idempotency (T2.4)", () => {
  it("second session.idle for the same subagent emits invocation_end exactly once", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const sdk = makeSubagentSdk(
      [userMessageEntry(DISPATCH_PROMPT), assistantMessageEntry(FINAL_RESPONSE)],
    );

    const deps = makeDeps(store, paths, sdk, collected);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
      agentType: "Research",
    });

    // First idle — should produce invocation_end once
    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));
    // Second idle — must be a no-op
    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    const endEvents = collected.filter((e) => e.event.event === "invocation_end");
    // FAILS until I2.2 (no events currently emitted, so endEvents.length = 0, not 1)
    expect(endEvents).toHaveLength(1);
  });

  it("second session.idle does not overwrite the output artifact written by the first", async () => {
    const store = makeStore();
    const paths = makePaths();

    const sdk = makeSubagentSdk(
      [userMessageEntry(DISPATCH_PROMPT), assistantMessageEntry(FINAL_RESPONSE)],
    );

    const deps = makeDeps(store, paths, sdk, []);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
      agentType: "Research",
    });

    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    const outputPath = paths.invocationOutput(RUN_ID, AGENT_INSTANCE_ID);
    const existsAfterFirst = nodeFs.existsSync(outputPath);
    let mtimeAfterFirst: number | undefined;
    if (existsAfterFirst) {
      mtimeAfterFirst = nodeFs.statSync(outputPath).mtimeMs;
    }

    // Small delay to ensure mtime would differ if re-written
    await new Promise((r) => setTimeout(r, 10));

    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    if (existsAfterFirst && mtimeAfterFirst !== undefined) {
      // If first idle produced the artifact, the second idle must not modify it
      const mtimeAfterSecond = nodeFs.statSync(outputPath).mtimeMs;
      expect(mtimeAfterSecond).toBe(mtimeAfterFirst);
    } else {
      // If first idle didn't produce artifact (RED phase), mark the test as showing
      // the absent state — the file simply doesn't exist yet
      expect(existsAfterFirst).toBe(true); // FAILS in RED phase to signal the issue
    }
  });
});

// ---------------------------------------------------------------------------
// T2.3 — invocation_end carries AC2.5: status_code extraction is exercised
// ---------------------------------------------------------------------------

describe("end-to-end — status_code extraction is exercised on invocation_end (AC2.5)", () => {
  it("invocation_end omits status_code when no structured response is present", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const sdk = makeSubagentSdk(
      [
        userMessageEntry(DISPATCH_PROMPT),
        assistantMessageEntry("Some unstructured response without a status code."),
      ],
    );

    const deps = makeDeps(store, paths, sdk, collected);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
      agentType: "Research",
    });

    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    // FAILS until I2.2: invocation_end not currently emitted via session.idle
    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invEnd).toBeDefined();
    // status_code should be absent when not extractable (not defaulted to a guess)
    expect(invEnd?.event.status_code).toBeUndefined();
  });

  it("invocation_end is emitted even when SDK returns an empty messages array", async () => {
    // The handler must emit invocation_end unconditionally; an absent status_code
    // is correct degradation, not a reason to suppress the mandatory event.
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const sdk = makeSubagentSdk([]); // no messages

    const deps = makeDeps(store, paths, sdk, collected);
    const sessionHandlers = createSessionHandlers(deps);
    const invHandlers = createInvocationHandlers(deps);

    deps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
      agentType: "Research",
    });

    await sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION));

    // FAILS until I2.2
    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invEnd).toBeDefined();
  });

  it("no hook call propagates a throw into the calling scope (safeHandler wrapping)", async () => {
    // AC2.8: every registered hook must be wrapped in safeHandler so that no
    // handler error can propagate to the OpenCode host process.
    // Tested here by checking that a handler error is silently swallowed.
    const store = makeStore();
    const paths = makePaths();

    const throwingDeps = makeDeps(store, paths, {
      session: {
        get: async () => undefined,
        messages: async () => {
          throw new Error("SDK failure inside hook");
        },
      },
      app: { log: async () => {} },
    }, []);

    const sessionHandlers = createSessionHandlers(throwingDeps);
    const invHandlers = createInvocationHandlers(throwingDeps);

    throwingDeps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      runId: RUN_ID,
      agentType: "Research",
    });

    // session.idle must not throw even when the SDK and handler both fail
    await expect(
      sessionHandlers.handleEvent(sessionIdleEvent(SUB_SESSION)),
    ).resolves.toBeUndefined();
  });
});
