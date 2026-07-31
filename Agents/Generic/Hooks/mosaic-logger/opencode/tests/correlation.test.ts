/**
 * correlation.test.ts — Tests for the in-memory session correlation store.
 *
 * Covers:
 *   - Session registration and retrieval
 *   - Merge semantics: existing fields preserved, specified fields overwritten
 *   - Orchestrator vs subagent session distinction
 *   - Synchronous parent-chain resolution
 *   - Asynchronous parent-chain resolution with SDK fallback
 *   - run_id resolution through the parent chain
 *   - Event sink routing (orchestrator vs subagent, unknown sessions, fallbacks)
 *   - unmapped_{sessionId} fallback when agentInstanceId not yet known
 *   - effectiveRunId applied internally (undefined run_id maps to "unknown-run")
 *   - Tool call_id correlation (storePendingCallId / popPendingCallId)
 *   - Session end tracking (markEnded / isEnded)
 *
 * All tests fail until the implementation is complete (TDD RED phase).
 */

import { describe, it, expect, beforeEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeOs from "node:os";

import { SessionCorrelationStore, type SessionRecord } from "../lib/correlation";
import { LogPaths } from "../lib/core";

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

const TS = "2026-01-01T17:00:00.000Z";
const RUN_ID = "20260101T170000Z-a3f9";

function makeStore(): SessionCorrelationStore {
  return new SessionCorrelationStore();
}

function makeRecord(overrides: Partial<SessionRecord> = {}): Partial<SessionRecord> {
  return {
    ended: false,
    createdAt: TS,
    ...overrides,
  };
}

// A minimal LogPaths for event-sink resolution tests.
// We use a temp-dir-like path; no actual I/O is performed.
const TEST_ROOT = nodePath.join(nodeOs.tmpdir(), "mosaic-logger-corr-test");

function makePaths(): LogPaths {
  return new LogPaths(TEST_ROOT);
}

// SDK lookup stub that resolves known session parent relationships.
function makeInMemorySdkLookup(
  map: Record<string, string | undefined>,
): (sessionId: string) => Promise<{ parentID?: string } | undefined> {
  return async (id: string) => {
    if (id in map) {
      const parentID = map[id];
      return { parentID };
    }
    return undefined;
  };
}

// ---------------------------------------------------------------------------
// Registration and retrieval
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — registration and retrieval", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
  });

  it("returns undefined for a session that has not been registered", () => {
    expect(store.get("unknown-session")).toBeUndefined();
  });

  it("stores and retrieves a registered session by its sessionId", () => {
    store.register("session-a", makeRecord({ sessionId: "session-a" }));
    const record = store.get("session-a");
    expect(record).toBeDefined();
    expect(record!.sessionId).toBe("session-a");
  });

  it("stores parentId when provided", () => {
    store.register(
      "session-child",
      makeRecord({ sessionId: "session-child", parentId: "session-parent" }),
    );
    const record = store.get("session-child");
    expect(record!.parentId).toBe("session-parent");
  });

  it("stores agentInstanceId when provided", () => {
    store.register(
      "session-a",
      makeRecord({ sessionId: "session-a", agentInstanceId: "Researcher#1" }),
    );
    expect(store.get("session-a")!.agentInstanceId).toBe("Researcher#1");
  });

  it("stores runId when provided", () => {
    store.register(
      "session-a",
      makeRecord({ sessionId: "session-a", runId: RUN_ID }),
    );
    expect(store.get("session-a")!.runId).toBe(RUN_ID);
  });

  it("stores prompt text when provided", () => {
    const prompt = '{"agent_instance_id": "TestWriter#5", "task": "write tests"}';
    store.register("session-a", makeRecord({ sessionId: "session-a", prompt }));
    expect(store.get("session-a")!.prompt).toBe(prompt);
  });

  it("stores agentType when provided", () => {
    store.register(
      "session-a",
      makeRecord({ sessionId: "session-a", agentType: "TestWriter" }),
    );
    expect(store.get("session-a")!.agentType).toBe("TestWriter");
  });

  it("stores createdAt timestamp", () => {
    store.register("session-a", makeRecord({ sessionId: "session-a", createdAt: TS }));
    expect(store.get("session-a")!.createdAt).toBe(TS);
  });

  it("stores multiple sessions independently", () => {
    store.register("session-x", makeRecord({ sessionId: "session-x" }));
    store.register("session-y", makeRecord({ sessionId: "session-y" }));
    expect(store.get("session-x")!.sessionId).toBe("session-x");
    expect(store.get("session-y")!.sessionId).toBe("session-y");
  });
});

// ---------------------------------------------------------------------------
// Merge semantics
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — merge semantics on re-registration", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
    // Seed a session with some initial fields.
    store.register("session-a", {
      sessionId: "session-a",
      agentInstanceId: "TestWriter#1",
      runId: RUN_ID,
      ended: false,
      createdAt: TS,
    });
  });

  it("overwrites a field when the key is present in the update record", () => {
    store.register("session-a", { agentInstanceId: "TestWriter#99" });
    expect(store.get("session-a")!.agentInstanceId).toBe("TestWriter#99");
  });

  it("preserves a field that is absent from the update record", () => {
    store.register("session-a", { agentType: "TestWriter" });
    // runId was not in the update record — must be preserved.
    expect(store.get("session-a")!.runId).toBe(RUN_ID);
  });

  it("allows clearing a field by passing undefined explicitly", () => {
    store.register("session-a", { agentInstanceId: undefined });
    expect(store.get("session-a")!.agentInstanceId).toBeUndefined();
  });

  it("adds a new field to an existing record", () => {
    store.register("session-a", { prompt: "dispatch text" });
    expect(store.get("session-a")!.prompt).toBe("dispatch text");
    // Previously stored field still present.
    expect(store.get("session-a")!.runId).toBe(RUN_ID);
  });
});

// ---------------------------------------------------------------------------
// Orchestrator vs subagent distinction
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — orchestrator vs subagent distinction", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
  });

  it("identifies a session with no parentId as an orchestrator session", () => {
    store.register("orch-session", makeRecord({ sessionId: "orch-session" }));
    expect(store.isSubagentSession("orch-session")).toBe(false);
  });

  it("identifies a session with a parentId as a subagent session", () => {
    store.register(
      "sub-session",
      makeRecord({ sessionId: "sub-session", parentId: "orch-session" }),
    );
    expect(store.isSubagentSession("sub-session")).toBe(true);
  });

  it("returns false for an unknown session (treat as orchestrator for safety)", () => {
    expect(store.isSubagentSession("completely-unknown")).toBe(false);
  });

  it("correctly distinguishes when multiple sessions are registered", () => {
    store.register("orch", makeRecord({ sessionId: "orch" }));
    store.register(
      "sub-a",
      makeRecord({ sessionId: "sub-a", parentId: "orch" }),
    );
    store.register(
      "sub-b",
      makeRecord({ sessionId: "sub-b", parentId: "orch" }),
    );

    expect(store.isSubagentSession("orch")).toBe(false);
    expect(store.isSubagentSession("sub-a")).toBe(true);
    expect(store.isSubagentSession("sub-b")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Synchronous parent-chain resolution
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — resolveOrchestratorSession (synchronous)", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
    // Chain: session-c -> session-b -> session-a (orchestrator)
    store.register("session-a", makeRecord({ sessionId: "session-a" }));
    store.register(
      "session-b",
      makeRecord({ sessionId: "session-b", parentId: "session-a" }),
    );
    store.register(
      "session-c",
      makeRecord({ sessionId: "session-c", parentId: "session-b" }),
    );
  });

  it("returns the session itself when it has no parent (orchestrator session)", () => {
    expect(store.resolveOrchestratorSession("session-a")).toBe("session-a");
  });

  it("returns the orchestrator session from a direct child", () => {
    expect(store.resolveOrchestratorSession("session-b")).toBe("session-a");
  });

  it("returns the orchestrator session by walking a two-hop chain", () => {
    expect(store.resolveOrchestratorSession("session-c")).toBe("session-a");
  });

  it("returns undefined for an unknown session", () => {
    expect(store.resolveOrchestratorSession("not-registered")).toBeUndefined();
  });

  it("handles a single-level chain with no grandparent", () => {
    const store2 = makeStore();
    store2.register("root", makeRecord({ sessionId: "root" }));
    store2.register("child", makeRecord({ sessionId: "child", parentId: "root" }));
    expect(store2.resolveOrchestratorSession("child")).toBe("root");
  });
});

// ---------------------------------------------------------------------------
// Asynchronous parent-chain resolution with SDK fallback
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — resolveOrchestratorSessionAsync", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
    store.register("session-a", makeRecord({ sessionId: "session-a" }));
    store.register(
      "session-b",
      makeRecord({ sessionId: "session-b", parentId: "session-a" }),
    );
  });

  it("resolves an orchestrator from in-memory data without calling the SDK", async () => {
    let sdkCallCount = 0;
    const sdkLookup = async (_id: string) => {
      sdkCallCount++;
      return undefined;
    };

    const result = await store.resolveOrchestratorSessionAsync("session-b", sdkLookup);
    expect(result).toBe("session-a");
    expect(sdkCallCount).toBe(0);
  });

  it("falls back to the SDK for a parent session not yet in the store", async () => {
    // "session-c" has parent "session-a" (already in store), but
    // "session-c" itself is not registered. The SDK should be consulted.
    const sdkLookup = makeInMemorySdkLookup({
      "session-c": "session-a",
    });

    const result = await store.resolveOrchestratorSessionAsync(
      "session-c",
      sdkLookup,
    );
    expect(result).toBe("session-a");
  });

  it("returns the root via SDK when the whole chain is outside the store", async () => {
    const sdkLookup = makeInMemorySdkLookup({
      "ext-child": "ext-parent",
      "ext-parent": undefined, // root — no further parent
    });

    const result = await store.resolveOrchestratorSessionAsync(
      "ext-child",
      sdkLookup,
    );
    expect(result).toBe("ext-parent");
  });

  it("returns the sessionId itself when neither in-memory nor SDK can find a parent", async () => {
    const sdkLookup = makeInMemorySdkLookup({});
    const result = await store.resolveOrchestratorSessionAsync(
      "orphan-session",
      sdkLookup,
    );
    expect(result).toBe("orphan-session");
  });

  it("uses in-memory data for the first hop and SDK for the second hop", async () => {
    // session-b is in store with parent "ext-root" which is NOT in store.
    const storePartial = makeStore();
    storePartial.register(
      "session-b",
      makeRecord({ sessionId: "session-b", parentId: "ext-root" }),
    );

    const sdkLookup = makeInMemorySdkLookup({
      "ext-root": undefined, // root — no further parent
    });

    const result = await storePartial.resolveOrchestratorSessionAsync(
      "session-b",
      sdkLookup,
    );
    expect(result).toBe("ext-root");
  });

  it("gracefully handles an SDK lookup that throws", async () => {
    const sdkLookup = async (_id: string): Promise<{ parentID?: string } | undefined> => {
      throw new Error("SDK unavailable");
    };

    // Should not throw — gracefully fall back; returns the input sessionId itself
    await expect(
      store.resolveOrchestratorSessionAsync("session-c-unknown", sdkLookup),
    ).resolves.toBe("session-c-unknown");
  });

  it("resolves the orchestrator session itself when given the orchestrator's sessionId", async () => {
    const sdkLookup = makeInMemorySdkLookup({});
    const result = await store.resolveOrchestratorSessionAsync("session-a", sdkLookup);
    expect(result).toBe("session-a");
  });
});

// ---------------------------------------------------------------------------
// run_id resolution through parent chain
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — getRunId", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
    store.register(
      "session-orch",
      makeRecord({ sessionId: "session-orch", runId: RUN_ID }),
    );
    store.register(
      "session-sub",
      makeRecord({ sessionId: "session-sub", parentId: "session-orch" }),
    );
  });

  it("returns the run_id stored directly on an orchestrator session", () => {
    expect(store.getRunId("session-orch")).toBe(RUN_ID);
  });

  it("returns the orchestrator's run_id when queried from a subagent session", () => {
    expect(store.getRunId("session-sub")).toBe(RUN_ID);
  });

  it("returns undefined for an unknown session", () => {
    expect(store.getRunId("not-registered")).toBeUndefined();
  });

  it("returns undefined when the orchestrator has no run_id set yet", () => {
    const store2 = makeStore();
    store2.register("orch-no-run", makeRecord({ sessionId: "orch-no-run" }));
    store2.register(
      "sub",
      makeRecord({ sessionId: "sub", parentId: "orch-no-run" }),
    );
    expect(store2.getRunId("sub")).toBeUndefined();
  });

  it("resolves run_id through a two-hop parent chain", () => {
    store.register(
      "session-sub2",
      makeRecord({ sessionId: "session-sub2", parentId: "session-sub" }),
    );
    expect(store.getRunId("session-sub2")).toBe(RUN_ID);
  });
});

// ---------------------------------------------------------------------------
// Event sink routing
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — resolveEventSink", () => {
  let store: SessionCorrelationStore;
  let paths: LogPaths;

  beforeEach(() => {
    store = makeStore();
    paths = makePaths();
    // Register orchestrator with a known run_id
    store.register(
      "session-orch",
      makeRecord({ sessionId: "session-orch", runId: RUN_ID }),
    );
    // Register subagent with known agentInstanceId
    store.register("session-sub", makeRecord({
      sessionId: "session-sub",
      parentId: "session-orch",
      agentInstanceId: "Researcher#1",
    }));
  });

  it("routes an orchestrator session to the orchestrator events file", () => {
    const sink = store.resolveEventSink("session-orch", paths);
    expect(sink).toBe(paths.orchestratorEvents(RUN_ID));
  });

  it("routes a subagent session to the invocation events file", () => {
    const sink = store.resolveEventSink("session-sub", paths);
    expect(sink).toBe(paths.invocationEvents(RUN_ID, "Researcher#1"));
  });

  it("uses the orchestrator's run_id (not the subagent's) when routing a subagent event", () => {
    // The subagent itself has no runId stored — it resolves through the parent chain.
    const store2 = makeStore();
    store2.register(
      "orch",
      makeRecord({ sessionId: "orch", runId: "20260101T120000Z-bbbb" }),
    );
    store2.register("sub", makeRecord({
      sessionId: "sub",
      parentId: "orch",
      agentInstanceId: "PlanWriter#2",
      // deliberately no runId on sub
    }));

    const sink = store2.resolveEventSink("sub", paths);
    expect(sink).toBe(paths.invocationEvents("20260101T120000Z-bbbb", "PlanWriter#2"));
  });

  it("falls back to unmapped_{sessionId} when a subagent's agentInstanceId is not yet known", () => {
    store.register("session-sub-unmapped", makeRecord({
      sessionId: "session-sub-unmapped",
      parentId: "session-orch",
      // no agentInstanceId yet
    }));
    const sink = store.resolveEventSink("session-sub-unmapped", paths);
    expect(sink).toBe(paths.invocationEvents(RUN_ID, "unmapped_session-sub-unmapped"));
  });

  it("routes a completely unknown session to the orchestrator stream under unknown-run", () => {
    const sink = store.resolveEventSink("completely-unknown-session", paths);
    expect(sink).toBe(paths.orchestratorEvents("unknown-run"));
  });

  it("applies effectiveRunId: maps undefined run_id to unknown-run for orchestrator routing", () => {
    const store2 = makeStore();
    store2.register("orch-no-run", makeRecord({ sessionId: "orch-no-run" }));
    // Orchestrator has no run_id — resolveEventSink must use "unknown-run"
    const sink = store2.resolveEventSink("orch-no-run", paths);
    expect(sink).toBe(paths.orchestratorEvents("unknown-run"));
  });

  it("applies effectiveRunId: maps undefined run_id to unknown-run for subagent routing", () => {
    const store2 = makeStore();
    store2.register("orch-no-run", makeRecord({ sessionId: "orch-no-run" }));
    store2.register("sub", makeRecord({
      sessionId: "sub",
      parentId: "orch-no-run",
      agentInstanceId: "TestWriter#3",
    }));
    // Orchestrator has no run_id — subagent event sink must use "unknown-run"
    const sink = store2.resolveEventSink("sub", paths);
    expect(sink).toBe(paths.invocationEvents("unknown-run", "TestWriter#3"));
  });

  it("never routes a subagent event to the orchestrator stream", () => {
    const sink = store.resolveEventSink("session-sub", paths);
    expect(sink).not.toBe(paths.orchestratorEvents(RUN_ID));
    expect(sink).not.toContain("00_orchestrator_events");
  });
});

// ---------------------------------------------------------------------------
// Tool call_id correlation
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — tool call_id correlation", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
  });

  it("stores a pending call_id and retrieves it via pop", () => {
    store.storePendingCallId("session-a", "call-id-abc123");
    expect(store.popPendingCallId("session-a")).toBe("call-id-abc123");
  });

  it("returns undefined when no call_id has been stored for a session", () => {
    expect(store.popPendingCallId("session-no-callid")).toBeUndefined();
  });

  it("overwrites the stored call_id on a second store call (single-slot)", () => {
    store.storePendingCallId("session-a", "first-call-id");
    store.storePendingCallId("session-a", "second-call-id");
    expect(store.popPendingCallId("session-a")).toBe("second-call-id");
  });

  it("isolates call_ids across different sessions", () => {
    store.storePendingCallId("session-x", "call-x");
    store.storePendingCallId("session-y", "call-y");
    expect(store.popPendingCallId("session-x")).toBe("call-x");
    expect(store.popPendingCallId("session-y")).toBe("call-y");
  });

  it("returns undefined on a second pop after the call_id was already retrieved", () => {
    store.storePendingCallId("session-a", "call-id-xyz");
    store.popPendingCallId("session-a"); // consume it
    expect(store.popPendingCallId("session-a")).toBeUndefined();
  });

  it("does not affect other session data when storing a call_id", () => {
    store.register(
      "session-a",
      makeRecord({ sessionId: "session-a", agentInstanceId: "Agent#1" }),
    );
    store.storePendingCallId("session-a", "call-id");
    // The registered record must be unaffected
    expect(store.get("session-a")!.agentInstanceId).toBe("Agent#1");
  });
});

// ---------------------------------------------------------------------------
// Session end tracking
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — session end tracking", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
    store.register("session-a", makeRecord({ sessionId: "session-a" }));
  });

  it("returns false for a newly registered session that has not ended", () => {
    expect(store.isEnded("session-a")).toBe(false);
  });

  it("returns false for an unknown session", () => {
    expect(store.isEnded("not-registered")).toBe(false);
  });

  it("returns true after markEnded is called", () => {
    store.markEnded("session-a");
    expect(store.isEnded("session-a")).toBe(true);
  });

  it("markEnded is idempotent — calling it twice does not throw", () => {
    expect(() => {
      store.markEnded("session-a");
      store.markEnded("session-a");
    }).not.toThrow();
  });

  it("markEnded on unknown session does not throw", () => {
    expect(() => store.markEnded("never-registered")).not.toThrow();
  });

  it("end state is independent across sessions", () => {
    store.register("session-b", makeRecord({ sessionId: "session-b" }));
    store.markEnded("session-a");
    expect(store.isEnded("session-a")).toBe(true);
    expect(store.isEnded("session-b")).toBe(false);
  });

  it("ended flag persists on the stored record", () => {
    store.markEnded("session-a");
    const record = store.get("session-a");
    expect(record!.ended).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// End-to-end parent chain scenario
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — end-to-end parent chain scenario", () => {
  it("correctly routes C→B→A (orchestrator) chain from any session in the chain", () => {
    const store = makeStore();
    const paths = makePaths();

    // Register three-level chain
    store.register(
      "session-a",
      makeRecord({ sessionId: "session-a", runId: RUN_ID }),
    );
    store.register(
      "session-b",
      makeRecord({
        sessionId: "session-b",
        parentId: "session-a",
        agentInstanceId: "Researcher#1",
      }),
    );
    store.register(
      "session-c",
      makeRecord({
        sessionId: "session-c",
        parentId: "session-b",
        agentInstanceId: "Reviewer#2",
      }),
    );

    // Root resolution
    expect(store.resolveOrchestratorSession("session-a")).toBe("session-a");
    expect(store.resolveOrchestratorSession("session-b")).toBe("session-a");
    expect(store.resolveOrchestratorSession("session-c")).toBe("session-a");

    // run_id resolution (all sessions see orchestrator's run_id)
    expect(store.getRunId("session-a")).toBe(RUN_ID);
    expect(store.getRunId("session-b")).toBe(RUN_ID);
    expect(store.getRunId("session-c")).toBe(RUN_ID);

    // Event sink routing
    expect(store.resolveEventSink("session-a", paths)).toBe(
      paths.orchestratorEvents(RUN_ID),
    );
    expect(store.resolveEventSink("session-b", paths)).toBe(
      paths.invocationEvents(RUN_ID, "Researcher#1"),
    );
    expect(store.resolveEventSink("session-c", paths)).toBe(
      paths.invocationEvents(RUN_ID, "Reviewer#2"),
    );
  });

  it("updates agentInstanceId incrementally as identity extraction completes", () => {
    const store = makeStore();
    const paths = makePaths();

    // First: session created but identity not yet extracted
    store.register(
      "session-orch",
      makeRecord({ sessionId: "session-orch", runId: RUN_ID }),
    );
    store.register(
      "session-sub",
      makeRecord({ sessionId: "session-sub", parentId: "session-orch" }),
    );

    // Before identity extraction: must use unmapped fallback
    const sinkBefore = store.resolveEventSink("session-sub", paths);
    expect(sinkBefore).toBe(
      paths.invocationEvents(RUN_ID, "unmapped_session-sub"),
    );

    // Identity extraction completes: update agentInstanceId
    store.register("session-sub", { agentInstanceId: "TestWriter#5" });

    // After identity extraction: must use correct agentInstanceId
    const sinkAfter = store.resolveEventSink("session-sub", paths);
    expect(sinkAfter).toBe(paths.invocationEvents(RUN_ID, "TestWriter#5"));

    // Other fields still preserved after the update
    expect(store.get("session-sub")!.parentId).toBe("session-orch");
  });
});
