/**
 * handlers_session.test.ts — Tests for session/run lifecycle event handlers.
 *
 * Covers:
 *   - session.created (top-level): registers session, emits session_start + run_start
 *     to the orchestrator events file
 *   - session.created (subagent, with parentID): registers session in store, does NOT
 *     emit session_start/run_start (invocation handler's responsibility)
 *   - session.deleted / session.idle (top-level): emits session_end + run_end
 *   - handleStop (top-level session): emits session_end + run_end
 *   - handleStop (subagent session): ignored (invocation handler handles this)
 *   - Idempotent end detection: second session_end is suppressed via isEnded
 *   - run_id extraction from session messages updates the orchestrator session record
 *   - Unhandled event types are silently ignored
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import {
  createSessionHandlers,
  type HandlerDependencies,
  type OpenCodeEvent,
  type SdkClient,
} from "../lib/handlers_session";
import { SessionCorrelationStore } from "../lib/correlation";
import { LogPaths, setDebugLogger } from "../lib/core";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const RUN_ID = "20260101T170000Z-a3f9";
const ORCH_SESSION = "sess-orch-001";
const SUB_SESSION = "sess-sub-001";

let tmpDir: string;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "mosaic-hs-test-"));
  // Install a no-op debug logger to avoid cross-test state leakage
  setDebugLogger(() => {});
});

afterEach(() => {
  nodeFs.rmSync(tmpDir, { recursive: true, force: true });
  // Reset debug logger
  setDebugLogger(() => {});
});

function makeStore(): SessionCorrelationStore {
  return new SessionCorrelationStore();
}

function makePaths(): LogPaths {
  return new LogPaths(tmpDir);
}

/** A no-op SDK client whose messages() returns an empty array. */
function makeNullSdk(): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async () => [],
    },
    app: {
      log: async () => {},
    },
  };
}

/** An SDK client whose messages() returns content containing a run_id. */
function makeSdkWithRunId(runId: string): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async () => [
        {
          role: "user",
          content: [{ type: "text", text: `run_id: "${runId}"` }],
        },
      ],
    },
    app: {
      log: async () => {},
    },
  };
}

/** An SDK client whose messages() throws. */
function makeThrowingSdk(): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async () => {
        throw new Error("SDK failure");
      },
    },
    app: {
      log: async () => {},
    },
  };
}

/** Collected events written by appendEvent stub. */
type CollectedEvent = { filePath: string; event: Record<string, unknown> };

/**
 * Build a HandlerDependencies object with real store/paths and a stubbed appendEvent.
 * The `appendEvent` stub records all calls so tests can inspect emitted events.
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
    fallbackCallId: (toolName) => `${toolName ?? "tool"}_fallback`,
  };
}

function makeEvent(
  type: string,
  properties?: Record<string, unknown>,
): OpenCodeEvent {
  return { type, properties };
}

// ---------------------------------------------------------------------------
// session.created — top-level (orchestrator) session
// ---------------------------------------------------------------------------

describe("session.created — top-level orchestrator session", () => {
  it("registers the session in the correlation store", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(makeEvent("session.created", { sessionID: ORCH_SESSION }));

    expect(store.get(ORCH_SESSION)).toBeDefined();
  });

  it("emits session_start to the orchestrator events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(makeEvent("session.created", { sessionID: ORCH_SESSION }));

    const sessionStart = collected.find((e) => e.event.event === "session_start");
    expect(sessionStart).toBeDefined();
    expect(sessionStart!.event.session_id).toBe(ORCH_SESSION);
  });

  it("emits run_start to the orchestrator events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(makeEvent("session.created", { sessionID: ORCH_SESSION }));

    const runStart = collected.find((e) => e.event.event === "run_start");
    expect(runStart).toBeDefined();
  });

  it("routes session_start and run_start to the orchestrator events file path", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(makeEvent("session.created", { sessionID: ORCH_SESSION }));

    const expectedSink = paths.orchestratorEvents("unknown-run");
    for (const e of collected) {
      expect(e.filePath).toBe(expectedSink);
    }
  });

  it("uses extracted run_id in event routing when SDK returns a run_id", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeSdkWithRunId(RUN_ID), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(makeEvent("session.created", { sessionID: ORCH_SESSION }));

    const expectedSink = paths.orchestratorEvents(RUN_ID);
    // All events should be routed to the run_id-named directory
    for (const e of collected) {
      expect(e.filePath).toBe(expectedSink);
    }
    // The store should have the run_id
    expect(store.get(ORCH_SESSION)?.runId).toBe(RUN_ID);
  });

  it("degrades gracefully when SDK messages() throws — still emits session_start + run_start", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeThrowingSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    // Should not throw
    await expect(handleEvent(makeEvent("session.created", { sessionID: ORCH_SESSION }))).resolves.toBeUndefined();

    expect(collected.some((e) => e.event.event === "session_start")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_start")).toBe(true);
  });

  it("emits session_start and run_start in that order", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(makeEvent("session.created", { sessionID: ORCH_SESSION }));

    const eventNames = collected.map((e) => e.event.event);
    const startIdx = eventNames.indexOf("session_start");
    const runIdx = eventNames.indexOf("run_start");
    expect(startIdx).toBeGreaterThanOrEqual(0);
    expect(runIdx).toBeGreaterThanOrEqual(0);
    expect(startIdx).toBeLessThan(runIdx);
  });

  it("accepts sessionID from the event root level (not just properties)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    const event: OpenCodeEvent = { type: "session.created", sessionID: ORCH_SESSION };
    await handleEvent(event);

    expect(collected.some((e) => e.event.event === "session_start")).toBe(true);
  });

  it("does not register or emit when sessionID is absent from the event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(makeEvent("session.created", {}));

    expect(collected).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// session.created — subagent session (parentID present)
// ---------------------------------------------------------------------------

describe("session.created — subagent session (parentID present)", () => {
  it("registers the subagent session in the correlation store with its parentId", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(
      makeEvent("session.created", { sessionID: SUB_SESSION, parentID: ORCH_SESSION }),
    );

    const record = store.get(SUB_SESSION);
    expect(record).toBeDefined();
    expect(record?.parentId).toBe(ORCH_SESSION);
  });

  it("does NOT emit session_start or run_start for subagent sessions", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(
      makeEvent("session.created", { sessionID: SUB_SESSION, parentID: ORCH_SESSION }),
    );

    expect(collected.some((e) => e.event.event === "session_start")).toBe(false);
    expect(collected.some((e) => e.event.event === "run_start")).toBe(false);
  });

  it("correctly identifies the subagent session as a subagent after registration", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(
      makeEvent("session.created", { sessionID: SUB_SESSION, parentID: ORCH_SESSION }),
    );

    expect(store.isSubagentSession(SUB_SESSION)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// session.deleted and session.idle — orchestrator session end
// ---------------------------------------------------------------------------

describe("session.deleted — orchestrator session end", () => {
  it("emits session_end to the orchestrator events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    // Register session first
    store.register(ORCH_SESSION, {});

    await handleEvent(makeEvent("session.deleted", { sessionID: ORCH_SESSION }));

    expect(collected.some((e) => e.event.event === "session_end")).toBe(true);
  });

  it("emits run_end after session_end", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    await handleEvent(makeEvent("session.deleted", { sessionID: ORCH_SESSION }));

    const names = collected.map((e) => e.event.event);
    expect(names).toContain("session_end");
    expect(names).toContain("run_end");
    expect(names.indexOf("session_end")).toBeLessThan(names.indexOf("run_end"));
  });

  it("does NOT emit end events for subagent sessions on session.deleted", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleEvent(makeEvent("session.deleted", { sessionID: SUB_SESSION }));

    expect(collected).toHaveLength(0);
  });

  it("suppresses duplicate end events (idempotent)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    await handleEvent(makeEvent("session.deleted", { sessionID: ORCH_SESSION }));
    await handleEvent(makeEvent("session.deleted", { sessionID: ORCH_SESSION }));

    const endEvents = collected.filter((e) => e.event.event === "session_end");
    expect(endEvents).toHaveLength(1);
  });
});

describe("session.idle — orchestrator session end", () => {
  it("emits session_end + run_end for session.idle event on orchestrator", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    await handleEvent(makeEvent("session.idle", { sessionID: ORCH_SESSION }));

    expect(collected.some((e) => e.event.event === "session_end")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_end")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// handleStop
// ---------------------------------------------------------------------------

describe("handleStop", () => {
  it("emits session_end + run_end when stop fires for an orchestrator session", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleStop } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleStop({ sessionID: ORCH_SESSION });

    expect(collected.some((e) => e.event.event === "session_end")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_end")).toBe(true);
  });

  it("routes events to the correct run_id directory", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleStop } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleStop({ sessionID: ORCH_SESSION });

    const expectedSink = paths.orchestratorEvents(RUN_ID);
    for (const e of collected) {
      expect(e.filePath).toBe(expectedSink);
    }
  });

  it("ignores stop events for subagent sessions", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleStop } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleStop({ sessionID: SUB_SESSION });

    expect(collected).toHaveLength(0);
  });

  it("suppresses duplicate stop events (idempotent)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleStop } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleStop({ sessionID: ORCH_SESSION });
    await handleStop({ sessionID: ORCH_SESSION });

    const endEvents = collected.filter((e) => e.event.event === "session_end");
    expect(endEvents).toHaveLength(1);
  });

  it("does not throw when sessionID is missing", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleStop } = createSessionHandlers(deps);

    // sessionID is missing / empty
    await expect(handleStop({ sessionID: "" })).resolves.toBeUndefined();
    expect(collected).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// Unhandled event types
// ---------------------------------------------------------------------------

describe("unhandled event types", () => {
  it("silently ignores unknown event types", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await expect(handleEvent(makeEvent("message.created", { sessionID: ORCH_SESSION }))).resolves.toBeUndefined();
    await expect(handleEvent(makeEvent("something.unknown", {}))).resolves.toBeUndefined();

    expect(collected).toHaveLength(0);
  });

  it("does not throw when event has no type field", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    // Cast to bypass type checker intentionally for defensive test
    await expect(handleEvent({ type: "" } as OpenCodeEvent)).resolves.toBeUndefined();
  });
});
