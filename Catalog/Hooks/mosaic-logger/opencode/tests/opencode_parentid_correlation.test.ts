/**
 * opencode_parentid_correlation.test.ts — Tests for OpenCode parentID correlation fixes.
 *
 * Covers:
 *   - extractSessionId and extractParentId reading the nested properties.info payload
 *     shape used by OpenCode's real bus events, alongside the existing flat shapes
 *   - session.created with a nested parentID registers the session as a subagent and
 *     suppresses lifecycle emission; without a parentID it registers as an orchestrator
 *   - session.updated contributes a parentID to the correlation store without emitting
 *     lifecycle events
 *   - Events that carry no parentID (e.g. session.status) do not clear an already-known
 *     parentId and do not throw
 *   - Downstream event-sink resolution: a subagent session whose parentID was learned
 *     from the nested shape routes to the invocation sink; orchestrator sessions are
 *     unaffected
 *
 * All tests that exercise not-yet-implemented behaviour fail until the implementation
 * is complete (TDD RED phase).
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import {
  createSessionHandlers,
  extractSessionId,
  extractParentId,
  type HandlerDependencies,
  type OpenCodeEvent,
  type SdkClient,
} from "../lib/handlers_session";
import { SessionCorrelationStore } from "../lib/correlation";
import { LogPaths, setDebugLogger } from "../lib/core";

// ---------------------------------------------------------------------------
// Shared test helpers
// ---------------------------------------------------------------------------

const ORCH_SESSION = "sess-orch-nested-001";
const SUB_SESSION = "sess-sub-nested-001";
const RUN_ID = "20260101T170000Z-a3f9";

let tmpDir: string;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(
    nodePath.join(nodeOs.tmpdir(), "mosaic-parentid-test-"),
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

type CollectedEvent = { filePath: string; event: Record<string, unknown> };

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

/** Build an event with properties.info as OpenCode's real bus emits it. */
function makeNestedEvent(
  type: string,
  info: Record<string, unknown>,
  extraProperties?: Record<string, unknown>,
): OpenCodeEvent {
  return {
    type,
    properties: {
      info,
      ...extraProperties,
    },
  };
}

/** Build an event with flat properties (the existing shape). */
function makeFlatEvent(
  type: string,
  properties?: Record<string, unknown>,
): OpenCodeEvent {
  return { type, properties };
}

// ---------------------------------------------------------------------------
// extractSessionId — nested and flat shapes
// ---------------------------------------------------------------------------

describe("extractSessionId — nested properties.info shape", () => {
  it("returns the session id from properties.info.id", () => {
    const event = makeNestedEvent("session.created", { id: SUB_SESSION });
    expect(extractSessionId(event)).toBe(SUB_SESSION);
  });

  it("returns the session id from properties.info.sessionID", () => {
    const event = makeNestedEvent("session.created", { sessionID: SUB_SESSION });
    expect(extractSessionId(event)).toBe(SUB_SESSION);
  });

  it("prefers properties.info.id over flat properties.sessionID", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: {
        info: { id: "info-id" },
        sessionID: "flat-id",
      },
    };
    expect(extractSessionId(event)).toBe("info-id");
  });

  it("falls back to flat properties.sessionID when properties.info is absent", () => {
    const event = makeFlatEvent("session.created", { sessionID: ORCH_SESSION });
    expect(extractSessionId(event)).toBe(ORCH_SESSION);
  });

  it("falls back to flat properties.sessionId when properties.info is absent", () => {
    const event = makeFlatEvent("session.created", { sessionId: ORCH_SESSION });
    expect(extractSessionId(event)).toBe(ORCH_SESSION);
  });

  it("falls back to flat properties.id when properties.info is absent", () => {
    const event = makeFlatEvent("session.created", { id: ORCH_SESSION });
    expect(extractSessionId(event)).toBe(ORCH_SESSION);
  });

  it("falls back to event root sessionID when properties carry nothing useful", () => {
    const event: OpenCodeEvent = { type: "session.created", sessionID: ORCH_SESSION };
    expect(extractSessionId(event)).toBe(ORCH_SESSION);
  });

  it("returns undefined when no level carries a session id", () => {
    const event = makeFlatEvent("session.created", {});
    expect(extractSessionId(event)).toBeUndefined();
  });

  it("returns undefined when properties.info is not an object (null)", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: { info: null } as unknown as Record<string, unknown>,
    };
    expect(extractSessionId(event)).toBeUndefined();
  });

  it("returns undefined when properties.info is not an object (array)", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: { info: [SUB_SESSION] } as unknown as Record<string, unknown>,
    };
    expect(extractSessionId(event)).toBeUndefined();
  });

  it("returns undefined when properties.info is not an object (string)", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: { info: "not-an-object" } as unknown as Record<string, unknown>,
    };
    expect(extractSessionId(event)).toBeUndefined();
  });

  it("does not throw when the event is an empty object", () => {
    expect(() => extractSessionId({} as OpenCodeEvent)).not.toThrow();
  });

  it("does not throw when properties is null", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: null as unknown as Record<string, unknown>,
    };
    expect(() => extractSessionId(event)).not.toThrow();
  });

  it("does not throw when properties is a primitive", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: 42 as unknown as Record<string, unknown>,
    };
    expect(() => extractSessionId(event)).not.toThrow();
  });

  it("ignores non-string values in properties.info.id", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: {
        info: { id: 12345 } as unknown as Record<string, unknown>,
        sessionID: ORCH_SESSION,
      },
    };
    // Non-string at info.id must be skipped; fall back to flat sessionID
    expect(extractSessionId(event)).toBe(ORCH_SESSION);
  });
});

// ---------------------------------------------------------------------------
// extractParentId — nested and flat shapes
// ---------------------------------------------------------------------------

describe("extractParentId — nested properties.info shape", () => {
  it("returns the parent id from properties.info.parentID", () => {
    const event = makeNestedEvent("session.created", {
      id: SUB_SESSION,
      parentID: ORCH_SESSION,
    });
    expect(extractParentId(event)).toBe(ORCH_SESSION);
  });

  it("returns the parent id from properties.info.parentId (camelCase variant)", () => {
    const event = makeNestedEvent("session.created", {
      id: SUB_SESSION,
      parentId: ORCH_SESSION,
    });
    expect(extractParentId(event)).toBe(ORCH_SESSION);
  });

  it("prefers properties.info.parentID over flat properties.parentID", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: {
        info: { parentID: "nested-parent" },
        parentID: "flat-parent",
      },
    };
    expect(extractParentId(event)).toBe("nested-parent");
  });

  it("falls back to flat properties.parentID when properties.info carries no parent", () => {
    const event = makeFlatEvent("session.created", { parentID: ORCH_SESSION });
    expect(extractParentId(event)).toBe(ORCH_SESSION);
  });

  it("falls back to flat properties.parentId when properties.info carries no parent", () => {
    const event = makeFlatEvent("session.created", { parentId: ORCH_SESSION });
    expect(extractParentId(event)).toBe(ORCH_SESSION);
  });

  it("falls back to event root parentID when properties carry nothing", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      parentID: ORCH_SESSION,
    };
    expect(extractParentId(event)).toBe(ORCH_SESSION);
  });

  it("falls back to event root parentId when properties carry nothing", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      parentId: ORCH_SESSION,
    };
    expect(extractParentId(event)).toBe(ORCH_SESSION);
  });

  it("returns undefined when no level carries a parent id", () => {
    const event = makeNestedEvent("session.created", { id: SUB_SESSION });
    expect(extractParentId(event)).toBeUndefined();
  });

  it("returns undefined when properties.info is null", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: { info: null } as unknown as Record<string, unknown>,
    };
    expect(extractParentId(event)).toBeUndefined();
  });

  it("returns undefined when properties.info is an array", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: { info: [ORCH_SESSION] } as unknown as Record<string, unknown>,
    };
    expect(extractParentId(event)).toBeUndefined();
  });

  it("does not throw for an empty event object", () => {
    expect(() => extractParentId({} as OpenCodeEvent)).not.toThrow();
  });

  it("does not throw when properties is null", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: null as unknown as Record<string, unknown>,
    };
    expect(() => extractParentId(event)).not.toThrow();
  });

  it("ignores non-string values in properties.info.parentID", () => {
    const event: OpenCodeEvent = {
      type: "session.created",
      properties: {
        info: { parentID: true } as unknown as Record<string, unknown>,
        parentID: ORCH_SESSION,
      },
    };
    // Non-string at info.parentID must be skipped; fall through to flat
    expect(extractParentId(event)).toBe(ORCH_SESSION);
  });
});

// ---------------------------------------------------------------------------
// session.created — nested parentID registers subagent vs orchestrator
// ---------------------------------------------------------------------------

describe("session.created — nested parentID registers subagent session", () => {
  it("registers a subagent session when parentID is inside properties.info", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(
      makeNestedEvent("session.created", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    const record = store.get(SUB_SESSION);
    expect(record).toBeDefined();
    expect(record?.parentId).toBe(ORCH_SESSION);
  });

  it("identifies the session as a subagent after nested-parentID registration", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(
      makeNestedEvent("session.created", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    expect(store.isSubagentSession(SUB_SESSION)).toBe(true);
  });

  it("does NOT emit session_start or run_start when parentID is in properties.info", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(
      makeNestedEvent("session.created", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    expect(collected.some((e) => e.event.event === "session_start")).toBe(false);
    expect(collected.some((e) => e.event.event === "run_start")).toBe(false);
  });

  it("registers an orchestrator session when properties.info has no parentID", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(
      makeNestedEvent("session.created", { id: ORCH_SESSION }),
    );

    const record = store.get(ORCH_SESSION);
    expect(record).toBeDefined();
    expect(record?.parentId).toBeUndefined();
    expect(store.isSubagentSession(ORCH_SESSION)).toBe(false);
  });

  it("emits session_start and run_start for an orchestrator session from properties.info.id", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(
      makeNestedEvent("session.created", { id: ORCH_SESSION }),
    );

    expect(collected.some((e) => e.event.event === "session_start")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_start")).toBe(true);
  });

  it("does not throw when called with a deeply nested-shaped event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    await expect(
      handleEvent(
        makeNestedEvent("session.created", {
          id: SUB_SESSION,
          parentID: ORCH_SESSION,
          extra: { nested: true },
        }),
      ),
    ).resolves.toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// session.updated — contributes parentID to the correlation store
// ---------------------------------------------------------------------------

describe("session.updated — contributes parentID to the correlation store", () => {
  it("registers a parentId from a session.updated event with nested parentID", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    // First: session.created (no parentID yet — seen as orchestrator initially)
    await handleEvent(
      makeNestedEvent("session.created", { id: SUB_SESSION }),
    );

    // Now: session.updated arrives carrying the parentID
    await handleEvent(
      makeNestedEvent("session.updated", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    const record = store.get(SUB_SESSION);
    expect(record?.parentId).toBe(ORCH_SESSION);
    expect(store.isSubagentSession(SUB_SESSION)).toBe(true);
  });

  it("registers a parentId from a session.updated event when session was not previously registered", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    // session.updated arrives before session.created has been seen locally
    await handleEvent(
      makeNestedEvent("session.updated", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    const record = store.get(SUB_SESSION);
    expect(record).toBeDefined();
    expect(record?.parentId).toBe(ORCH_SESSION);
  });

  it("does NOT emit session_start or run_start on session.updated", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(
      makeNestedEvent("session.updated", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    expect(collected.some((e) => e.event.event === "session_start")).toBe(false);
    expect(collected.some((e) => e.event.event === "run_start")).toBe(false);
  });

  it("a session that receives session.created (no parent) then session.updated (with parent) emits lifecycle events only once", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleEvent } = createSessionHandlers(deps);

    // session.created fires with no parentID → emits session_start + run_start
    await handleEvent(
      makeNestedEvent("session.created", { id: SUB_SESSION }),
    );

    const afterCreate = collected.filter(
      (e) => e.event.event === "session_start" || e.event.event === "run_start",
    ).length;

    // session.updated arrives and reveals parentID (late discovery)
    await handleEvent(
      makeNestedEvent("session.updated", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    const afterUpdate = collected.filter(
      (e) => e.event.event === "session_start" || e.event.event === "run_start",
    ).length;

    // No additional lifecycle events should have been emitted by session.updated
    expect(afterUpdate).toBe(afterCreate);
  });

  it("does not register from session.updated when no parentID and session is not already known", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    // session.updated with no parentID and session not yet in store
    // This must NOT create a parent-less record that misclassifies the session as orchestrator
    await handleEvent(
      makeNestedEvent("session.updated", { id: SUB_SESSION }),
    );

    // Session should remain unregistered (no speculative parent-less registration)
    expect(store.get(SUB_SESSION)).toBeUndefined();
  });

  it("does not throw when session.updated carries no session id", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    await expect(
      handleEvent(makeNestedEvent("session.updated", { parentID: ORCH_SESSION })),
    ).resolves.toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Events without parentID do not clear an already-known parentId
// ---------------------------------------------------------------------------

describe("events without parentID do not clear a known parentId", () => {
  it("session.status does not clear a parentId learned from session.created", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    // Establish the subagent registration via session.created
    await handleEvent(
      makeNestedEvent("session.created", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    expect(store.get(SUB_SESSION)?.parentId).toBe(ORCH_SESSION);

    // Fire a session.status event — which carries no parentID by design
    await handleEvent(makeFlatEvent("session.status", { sessionID: SUB_SESSION }));

    // The parentId must be preserved
    expect(store.get(SUB_SESSION)?.parentId).toBe(ORCH_SESSION);
    expect(store.isSubagentSession(SUB_SESSION)).toBe(true);
  });

  it("session.status does not throw", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, {});

    await expect(
      handleEvent(makeFlatEvent("session.status", { sessionID: ORCH_SESSION })),
    ).resolves.toBeUndefined();
  });

  it("a session.created re-registration without parentID does not erase an existing parentId", async () => {
    // This test guards the registration call: building { parentId: undefined } as the
    // record when no parentId was extracted must NOT happen — it would overwrite the
    // stored value. The key must be omitted entirely from the record.
    const store = makeStore();

    // Register with parentId already known
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    expect(store.get(SUB_SESSION)?.parentId).toBe(ORCH_SESSION);

    // Re-register with a record that omits the parentId key (correct behaviour)
    // vs one that passes { parentId: undefined } (the bug to prevent)
    store.register(SUB_SESSION, {}); // omit key — must NOT clear parentId

    expect(store.get(SUB_SESSION)?.parentId).toBe(ORCH_SESSION);
  });

  it("a session.created re-registration with explicit undefined DOES clear parentId (store contract preserved)", () => {
    // This is the existing merge semantic — explicit undefined clears.
    // Kept as a regression guard to ensure the store contract is not changed.
    const store = makeStore();

    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    store.register(SUB_SESSION, { parentId: undefined }); // explicit clear

    expect(store.get(SUB_SESSION)?.parentId).toBeUndefined();
  });

  it("handleEvent session.created without parentID does not pass { parentId: undefined } to register", async () => {
    // Observable via the store: after session.created with no parentID,
    // a subsequent re-registration that omits the key should preserve any
    // parentId that was loaded another way before handleEvent ran.
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    // Pre-load the store with a known parentId (simulating out-of-order arrival)
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    // session.created arrives for the same session but without parentID
    await handleEvent(makeFlatEvent("session.created", { sessionID: SUB_SESSION }));

    // The pre-loaded parentId must not have been cleared
    expect(store.get(SUB_SESSION)?.parentId).toBe(ORCH_SESSION);
    expect(store.isSubagentSession(SUB_SESSION)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Downstream sink resolution: subagent events route to invocation sink
// ---------------------------------------------------------------------------

describe("downstream event-sink resolution after nested parentID registration", () => {
  it("subagent session routes to the invocation events sink once parentID is known", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    // Register the orchestrator with a known run_id
    store.register(ORCH_SESSION, { runId: RUN_ID });

    // Register the subagent via nested session.created
    await handleEvent(
      makeNestedEvent("session.created", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    // Supply the agentInstanceId (normally done by the invocation handler)
    store.register(SUB_SESSION, { agentInstanceId: "TestWriter#5" });

    const sink = store.resolveEventSink(SUB_SESSION, paths);
    expect(sink).toBe(paths.invocationEvents(RUN_ID, "TestWriter#5"));
  });

  it("subagent event sink is NOT the orchestrator events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleEvent(
      makeNestedEvent("session.created", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    store.register(SUB_SESSION, { agentInstanceId: "Planner#1" });

    const sink = store.resolveEventSink(SUB_SESSION, paths);
    expect(sink).not.toBe(paths.orchestratorEvents(RUN_ID));
    expect(sink).not.toContain("00_orchestrator_events");
  });

  it("orchestrator session routing is unaffected by subagent registration", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleEvent(
      makeNestedEvent("session.created", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    const orchSink = store.resolveEventSink(ORCH_SESSION, paths);
    expect(orchSink).toBe(paths.orchestratorEvents(RUN_ID));
  });

  it("uses unmapped fallback until agentInstanceId is supplied, then resolves correctly", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleEvent(
      makeNestedEvent("session.created", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    // Before agentInstanceId is known
    const sinkBefore = store.resolveEventSink(SUB_SESSION, paths);
    expect(sinkBefore).toBe(
      paths.invocationEvents(RUN_ID, `unmapped_${SUB_SESSION}`),
    );

    // After identity extraction
    store.register(SUB_SESSION, { agentInstanceId: "Reviewer#3" });
    const sinkAfter = store.resolveEventSink(SUB_SESSION, paths);
    expect(sinkAfter).toBe(paths.invocationEvents(RUN_ID, "Reviewer#3"));
  });

  it("parentId learned via session.updated causes subagent routing after the update", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeNullSdk(), []);
    const { handleEvent } = createSessionHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });

    // session.created arrives with no parentID — session looks like an orchestrator
    await handleEvent(
      makeNestedEvent("session.created", { id: SUB_SESSION }),
    );

    // Before parentID is known, session routes as orchestrator
    const sinkBefore = store.resolveEventSink(SUB_SESSION, paths);
    expect(sinkBefore).toBe(paths.orchestratorEvents("unknown-run"));

    // session.updated reveals the parentID
    await handleEvent(
      makeNestedEvent("session.updated", {
        id: SUB_SESSION,
        parentID: ORCH_SESSION,
      }),
    );

    store.register(SUB_SESSION, { agentInstanceId: "TestWriter#9" });

    // After parentID is known, session routes to invocation sink
    const sinkAfter = store.resolveEventSink(SUB_SESSION, paths);
    expect(sinkAfter).toBe(paths.invocationEvents(RUN_ID, "TestWriter#9"));
    expect(sinkAfter).not.toContain("00_orchestrator_events");
  });
});
