/**
 * handlers_session.test.ts — Tests for session/run lifecycle event handlers.
 *
 * Covers:
 *   - session.created (top-level): registers session, emits session_start + run_start
 *     to the orchestrator events file
 *   - session.created (subagent, with parentID): registers session in store, does NOT
 *     emit session_start/run_start (invocation handler's responsibility)
 *   - session.deleted / session.idle (top-level): emits session_end + run_end
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
  extractSessionId,
  extractParentId,
  type HandlerDependencies,
  type InvocationCallbacks,
  type OpenCodeEvent,
  type SdkClient,
} from "../lib/handlers_session";
import { SessionCorrelationStore } from "../lib/correlation";
import { LogPaths, setDebugLogger } from "../lib/core";
import {
  sessionCreatedEvent,
  sessionUpdatedEvent,
  sessionDeletedEvent,
  sessionIdleEvent,
  userMessageEntry,
} from "./fixtures/opencode_api";

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

/**
 * An SDK client whose messages() returns content containing a run_id.
 * Uses the real { info, parts } shape via the fixture builder so no message
 * is constructed as { role, content } anywhere in the suite.
 */
function makeSdkWithRunId(runId: string): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async () => [
        userMessageEntry(`run_id: "${runId}"`),
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

// ---------------------------------------------------------------------------
// T1.4 — Session event shapes: extractSessionId / extractParentId across all
// four bus event types, using the real fixture shapes. The session.idle
// asymmetry (no properties.info) is the critical case.
// ---------------------------------------------------------------------------

describe("extractSessionId — all four session event types via fixture events", () => {
  it("extracts session id from session.created using nested properties.info.id", () => {
    const event = sessionCreatedEvent(ORCH_SESSION);
    expect(extractSessionId(event)).toBe(ORCH_SESSION);
  });

  it("extracts session id from session.created with a parentID", () => {
    const event = sessionCreatedEvent(SUB_SESSION, ORCH_SESSION);
    expect(extractSessionId(event)).toBe(SUB_SESSION);
  });

  it("extracts session id from session.updated using nested properties.info.id", () => {
    const event = sessionUpdatedEvent(ORCH_SESSION);
    expect(extractSessionId(event)).toBe(ORCH_SESSION);
  });

  it("extracts session id from session.deleted using nested properties.info.id", () => {
    const event = sessionDeletedEvent(ORCH_SESSION);
    expect(extractSessionId(event)).toBe(ORCH_SESSION);
  });

  it("extracts session id from session.idle via properties.sessionID (no info object)", () => {
    // session.idle is structurally different: it carries ONLY properties.sessionID,
    // not properties.info. extractSessionId must fall through to the flat fallback.
    const event = sessionIdleEvent(ORCH_SESSION);
    expect(extractSessionId(event)).toBe(ORCH_SESSION);
  });

  it("session.idle event has no properties.info (shape asymmetry is explicit)", () => {
    const event = sessionIdleEvent(ORCH_SESSION);
    // Directly verify the fixture shape: no `info` under properties
    expect((event.properties as Record<string, unknown>)?.["info"]).toBeUndefined();
    // But sessionID is there
    expect((event.properties as Record<string, unknown>)?.["sessionID"]).toBe(ORCH_SESSION);
  });
});

describe("extractParentId — all four session event types via fixture events", () => {
  it("extracts parentId from session.created with parent in properties.info.parentID", () => {
    const event = sessionCreatedEvent(SUB_SESSION, ORCH_SESSION);
    expect(extractParentId(event)).toBe(ORCH_SESSION);
  });

  it("returns undefined for session.created with no parentID", () => {
    const event = sessionCreatedEvent(ORCH_SESSION);
    expect(extractParentId(event)).toBeUndefined();
  });

  it("returns undefined for session.updated (fixture carries no parent)", () => {
    const event = sessionUpdatedEvent(ORCH_SESSION);
    expect(extractParentId(event)).toBeUndefined();
  });

  it("returns undefined for session.deleted (fixture carries no parent)", () => {
    const event = sessionDeletedEvent(ORCH_SESSION);
    expect(extractParentId(event)).toBeUndefined();
  });

  it("returns undefined for session.idle (no info object, so no parentID)", () => {
    // session.idle carries only properties.sessionID — no info, no parentID.
    const event = sessionIdleEvent(ORCH_SESSION);
    expect(extractParentId(event)).toBeUndefined();
  });
});

describe("handleEvent — session.created with real fixture event shape", () => {
  it("registers orchestrator session from fixture sessionCreatedEvent (no parent)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(sessionCreatedEvent(ORCH_SESSION));

    expect(store.get(ORCH_SESSION)).toBeDefined();
    expect(store.isSubagentSession(ORCH_SESSION)).toBe(false);
  });

  it("emits session_start and run_start for orchestrator from fixture sessionCreatedEvent", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(sessionCreatedEvent(ORCH_SESSION));

    expect(collected.some((e) => e.event.event === "session_start")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_start")).toBe(true);
  });

  it("registers subagent from fixture sessionCreatedEvent with parentId", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(sessionCreatedEvent(SUB_SESSION, ORCH_SESSION));

    const record = store.get(SUB_SESSION);
    expect(record).toBeDefined();
    expect(record?.parentId).toBe(ORCH_SESSION);
    expect(store.isSubagentSession(SUB_SESSION)).toBe(true);
  });

  it("does not emit lifecycle events for subagent from fixture sessionCreatedEvent", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(sessionCreatedEvent(SUB_SESSION, ORCH_SESSION));

    expect(collected.some((e) => e.event.event === "session_start")).toBe(false);
    expect(collected.some((e) => e.event.event === "run_start")).toBe(false);
  });
});

describe("handleEvent — session.deleted with real fixture event shape", () => {
  it("emits session_end + run_end for orchestrator from fixture sessionDeletedEvent", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    await handleEvent(sessionDeletedEvent(ORCH_SESSION));

    expect(collected.some((e) => e.event.event === "session_end")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_end")).toBe(true);
  });

  it("does not emit end events for subagent from fixture sessionDeletedEvent", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleEvent(sessionDeletedEvent(SUB_SESSION));

    expect(collected).toHaveLength(0);
  });
});

describe("handleEvent — session.idle with real fixture event shape (info-less)", () => {
  it("emits session_end + run_end for orchestrator from fixture sessionIdleEvent", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    // session.idle carries only properties.sessionID — no properties.info
    await handleEvent(sessionIdleEvent(ORCH_SESSION));

    expect(collected.some((e) => e.event.event === "session_end")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_end")).toBe(true);
  });

  it("does not throw when session.idle event has no properties.info", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    await expect(handleEvent(sessionIdleEvent(ORCH_SESSION))).resolves.toBeUndefined();
  });

  it("does not emit end events for a subagent session.idle (subagent handling is separate)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleEvent(sessionIdleEvent(SUB_SESSION));

    // Subagent idle routes to invocation end (not session end) — no session_end/run_end here
    expect(collected.some((e) => e.event.event === "session_end")).toBe(false);
    expect(collected.some((e) => e.event.event === "run_end")).toBe(false);
  });

  it("session.idle end is idempotent — second event is suppressed", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    await handleEvent(sessionIdleEvent(ORCH_SESSION));
    await handleEvent(sessionIdleEvent(ORCH_SESSION));

    const endEvents = collected.filter((e) => e.event.event === "session_end");
    expect(endEvents).toHaveLength(1);
  });

  it("degrades gracefully for session.idle when session is unknown to the store", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    // session.idle for a session the store has never seen — must not throw
    await expect(
      handleEvent(sessionIdleEvent("completely-unknown-session")),
    ).resolves.toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Stage 2: T2.2 — session.idle routing by session kind via invocationCallbacks
//
// After Stage 2 implementation:
//   - session.idle for a subagent → invocationCallbacks.handleInvocationEnd called
//   - session.idle for top-level → session_end + run_end emitted (no callback)
//   - session.idle for unknown session → degrade without throw
//   - invocationCallbacks absent → subagent idle degrades without throw
// ---------------------------------------------------------------------------

/**
 * Build a HandlerDependencies object that includes an invocationCallbacks bundle.
 * The callbacks are supplied as plain objects so tests can spy on them.
 */
function makeDepsWithCallbacks(
  store: SessionCorrelationStore,
  paths: LogPaths,
  sdkClient: SdkClient,
  collected: CollectedEvent[],
  invocationCallbacks: InvocationCallbacks,
): HandlerDependencies {
  return {
    ...makeDeps(store, paths, sdkClient, collected),
    invocationCallbacks,
  };
}

describe("session.idle routing — subagent session calls invocationCallbacks.handleInvocationEnd (T2.2)", () => {
  it("calls handleInvocationEnd with the subagent sessionId when idle fires for a registered subagent", async () => {
    const store = makeStore();
    const paths = makePaths();
    let endedSessionId: string | undefined;
    let callCount = 0;

    const callbacks: InvocationCallbacks = {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async (sessionId) => {
        callCount++;
        endedSessionId = sessionId;
      },
    };

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), [], callbacks);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleEvent(sessionIdleEvent(SUB_SESSION));

    // FAILS until I2.2: the session handler currently does not use invocationCallbacks
    expect(callCount).toBe(1);
    expect(endedSessionId).toBe(SUB_SESSION);
  });

  it("does NOT emit session_end or run_end when idle fires for a subagent session", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), collected, {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async () => {},
    });
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleEvent(sessionIdleEvent(SUB_SESSION));

    // session_end and run_end must never be emitted for subagent idle events
    expect(collected.some((e) => e.event.event === "session_end")).toBe(false);
    expect(collected.some((e) => e.event.event === "run_end")).toBe(false);
  });

  it("emits session_end + run_end for top-level idle and does NOT call handleInvocationEnd", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    let invocationEndCalled = false;

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), collected, {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async () => {
        invocationEndCalled = true;
      },
    });
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    await handleEvent(sessionIdleEvent(ORCH_SESSION));

    // Top-level idle → session end path (existing behaviour, unchanged by Stage 2)
    expect(collected.some((e) => e.event.event === "session_end")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_end")).toBe(true);
    // Invocation callback must not be called for top-level idle
    expect(invocationEndCalled).toBe(false);
  });

  it("degrades without throwing when idle fires for an unknown session (callbacks wired)", async () => {
    const store = makeStore();
    const paths = makePaths();

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), [], {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async () => {},
    });
    const { handleEvent } = createSessionHandlers(deps);

    // No session registered at all — must not throw
    await expect(
      handleEvent(sessionIdleEvent("completely-unknown-session-xyz")),
    ).resolves.toBeUndefined();
  });

  it("degrades without throwing when idle fires for a subagent and invocationCallbacks is absent", async () => {
    // invocationCallbacks is optional — handler modules must no-op when absent
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []); // no invocationCallbacks
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await expect(handleEvent(sessionIdleEvent(SUB_SESSION))).resolves.toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Stage 2: T2.4 — repeat-idle idempotency at the session-handler routing layer
//
// session.idle can fire more than once for the same session. The first call
// should route to handleInvocationEnd (or session end). The second must be
// suppressed so that no duplicate events are produced.
// ---------------------------------------------------------------------------

describe("repeat-idle idempotency — subagent session (T2.4)", () => {
  it("calls handleInvocationEnd exactly once when the same subagent fires idle twice", async () => {
    const store = makeStore();
    const paths = makePaths();
    let callCount = 0;

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), [], {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async (sessionId) => {
        callCount++;
        // Simulate what the real handler does: mark the session ended
        store.markEnded(sessionId);
      },
    });
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleEvent(sessionIdleEvent(SUB_SESSION));
    await handleEvent(sessionIdleEvent(SUB_SESSION));

    // FAILS until I2.2: currently handleInvocationEnd is never called (callCount = 0)
    expect(callCount).toBe(1);
  });

  it("calls handleInvocationEnd exactly once when idle fires for an already-ended subagent", async () => {
    const store = makeStore();
    const paths = makePaths();
    let callCount = 0;

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), [], {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async () => {
        callCount++;
      },
    });
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    store.markEnded(SUB_SESSION); // already ended before idle fires

    await handleEvent(sessionIdleEvent(SUB_SESSION));

    // An already-ended session must not trigger a second invocation end callback
    expect(callCount).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// Stage 2: T2.5 — session.deleted routing: top-level sessions and double-close
//
// session.deleted is a different lifecycle moment (record was removed, not idle).
// It must route to session-end for top-level sessions and must NOT trigger
// invocation-end handling for subagent sessions, regardless of the idle path.
// ---------------------------------------------------------------------------

describe("session.deleted routing — top-level sessions (T2.5)", () => {
  it("session.deleted for a top-level session emits session_end and run_end", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), collected, {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async () => {},
    });
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    await handleEvent(sessionDeletedEvent(ORCH_SESSION));

    expect(collected.some((e) => e.event.event === "session_end")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_end")).toBe(true);
  });

  it("session.deleted for a top-level session emits session_end before run_end", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), collected, {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async () => {},
    });
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    await handleEvent(sessionDeletedEvent(ORCH_SESSION));

    const names = collected.map((e) => e.event.event as string);
    expect(names.indexOf("session_end")).toBeLessThan(names.indexOf("run_end"));
  });
});

describe("session.deleted routing — subagent sessions do NOT trigger invocation-end (T2.5)", () => {
  it("session.deleted for a subagent session does NOT call handleInvocationEnd", async () => {
    const store = makeStore();
    const paths = makePaths();
    let invocationEndCalled = false;

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), [], {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async () => {
        invocationEndCalled = true;
      },
    });
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    // session.deleted for a subagent — must not trigger invocation end
    // (session.deleted is "record removed", not "session went idle")
    await handleEvent(sessionDeletedEvent(SUB_SESSION));

    expect(invocationEndCalled).toBe(false);
  });

  it("session.deleted after subagent idle does not produce a second invocation-end callback", async () => {
    const store = makeStore();
    const paths = makePaths();
    let callCount = 0;

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), [], {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async (sessionId) => {
        callCount++;
        store.markEnded(sessionId);
      },
    });
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    // First signal: idle (should call the callback once in the GREEN state)
    await handleEvent(sessionIdleEvent(SUB_SESSION));
    // Second signal: deleted (must not call the callback again)
    await handleEvent(sessionDeletedEvent(SUB_SESSION));

    // Exactly one invocation-end callback must be fired: idle drives it, deleted does not.
    // FAILS in RED state (callCount = 0) until session.idle routing is wired for subagents.
    // A callCount of 2 would indicate a double-close; a count of 0 indicates the idle→end
    // wire is missing entirely — both defects are caught by the exact-equality assertion.
    expect(callCount).toBe(1);
  });

  it("session.deleted for top-level after top-level idle emits session_end exactly once total", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];

    const deps = makeDepsWithCallbacks(store, paths, makeNullSdk(), collected, {
      handleInvocationStart: async () => {},
      handleInvocationEnd: async () => {},
    });
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    // First: idle closes the top-level session
    await handleEvent(sessionIdleEvent(ORCH_SESSION));
    // Second: deleted fires — must be a no-op (already ended)
    await handleEvent(sessionDeletedEvent(ORCH_SESSION));

    const endEvents = collected.filter((e) => e.event.event === "session_end");
    expect(endEvents).toHaveLength(1);
  });
});
