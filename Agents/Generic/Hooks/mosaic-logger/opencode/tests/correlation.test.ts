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

import { SessionCorrelationStore, type SessionRecord, type PendingToolCall } from "../lib/correlation";
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

// ---------------------------------------------------------------------------
// Pending dispatch queue (per-parent FIFO) — Stage 3
//
// These tests specify the new pushPendingDispatch / claimPendingDispatch /
// pendingDispatchCount / clearPendingDispatches methods that do not yet exist
// on SessionCorrelationStore. All tests in this section are RED until the
// implementation adds the dispatch-queue capability to the store.
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — pending dispatch queue", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
  });

  it("returns undefined when no dispatch has been pushed for a parent", () => {
    expect(store.claimPendingDispatch("parent-a")).toBeUndefined();
  });

  it("returns the pushed dispatch and consumes it", () => {
    store.pushPendingDispatch("parent-a", {
      callId: "call-123",
      parentSessionId: "parent-a",
      agentInstanceId: "TestAgent#1",
      runId: "20260101T000000Z-aaaa",
      prompt: "do some work",
      dispatchedAt: TS,
    });
    const claimed = store.claimPendingDispatch("parent-a");
    expect(claimed).toBeDefined();
    expect(claimed!.agentInstanceId).toBe("TestAgent#1");
    expect(claimed!.runId).toBe("20260101T000000Z-aaaa");
  });

  it("second claim after consuming returns undefined", () => {
    store.pushPendingDispatch("parent-a", {
      callId: "call-abc",
      parentSessionId: "parent-a",
      dispatchedAt: TS,
    });
    store.claimPendingDispatch("parent-a"); // consume
    expect(store.claimPendingDispatch("parent-a")).toBeUndefined();
  });

  it("returns dispatches in FIFO order when multiple are queued", () => {
    store.pushPendingDispatch("parent-a", {
      callId: "call-first",
      parentSessionId: "parent-a",
      agentInstanceId: "Agent#1",
      dispatchedAt: TS,
    });
    store.pushPendingDispatch("parent-a", {
      callId: "call-second",
      parentSessionId: "parent-a",
      agentInstanceId: "Agent#2",
      dispatchedAt: TS,
    });
    store.pushPendingDispatch("parent-a", {
      callId: "call-third",
      parentSessionId: "parent-a",
      agentInstanceId: "Agent#3",
      dispatchedAt: TS,
    });

    expect(store.claimPendingDispatch("parent-a")!.agentInstanceId).toBe("Agent#1");
    expect(store.claimPendingDispatch("parent-a")!.agentInstanceId).toBe("Agent#2");
    expect(store.claimPendingDispatch("parent-a")!.agentInstanceId).toBe("Agent#3");
    expect(store.claimPendingDispatch("parent-a")).toBeUndefined();
  });

  it("pendingDispatchCount returns 0 for a parent with no pending dispatches", () => {
    expect(store.pendingDispatchCount("parent-a")).toBe(0);
  });

  it("pendingDispatchCount returns the correct count as dispatches are pushed", () => {
    store.pushPendingDispatch("parent-a", { callId: "c1", parentSessionId: "parent-a", dispatchedAt: TS });
    expect(store.pendingDispatchCount("parent-a")).toBe(1);
    store.pushPendingDispatch("parent-a", { callId: "c2", parentSessionId: "parent-a", dispatchedAt: TS });
    expect(store.pendingDispatchCount("parent-a")).toBe(2);
  });

  it("pendingDispatchCount decrements after a claim", () => {
    store.pushPendingDispatch("parent-a", { callId: "c1", parentSessionId: "parent-a", dispatchedAt: TS });
    store.pushPendingDispatch("parent-a", { callId: "c2", parentSessionId: "parent-a", dispatchedAt: TS });
    store.claimPendingDispatch("parent-a");
    expect(store.pendingDispatchCount("parent-a")).toBe(1);
  });

  it("clearPendingDispatches removes all pending entries for a parent", () => {
    store.pushPendingDispatch("parent-a", { callId: "c1", parentSessionId: "parent-a", dispatchedAt: TS });
    store.pushPendingDispatch("parent-a", { callId: "c2", parentSessionId: "parent-a", dispatchedAt: TS });
    store.clearPendingDispatches("parent-a");
    expect(store.pendingDispatchCount("parent-a")).toBe(0);
    expect(store.claimPendingDispatch("parent-a")).toBeUndefined();
  });

  it("clearPendingDispatches on a parent with no entries does not throw", () => {
    expect(() => store.clearPendingDispatches("parent-never-used")).not.toThrow();
  });

  it("queues for different parents are independent", () => {
    store.pushPendingDispatch("parent-a", {
      callId: "call-a",
      parentSessionId: "parent-a",
      agentInstanceId: "Agent#1",
      dispatchedAt: TS,
    });
    store.pushPendingDispatch("parent-b", {
      callId: "call-b",
      parentSessionId: "parent-b",
      agentInstanceId: "Agent#2",
      dispatchedAt: TS,
    });

    expect(store.claimPendingDispatch("parent-a")!.agentInstanceId).toBe("Agent#1");
    expect(store.claimPendingDispatch("parent-b")!.agentInstanceId).toBe("Agent#2");
    expect(store.claimPendingDispatch("parent-a")).toBeUndefined();
    expect(store.claimPendingDispatch("parent-b")).toBeUndefined();
  });

  it("clearing one parent does not affect another parent's queue", () => {
    store.pushPendingDispatch("parent-a", { callId: "ca1", parentSessionId: "parent-a", dispatchedAt: TS });
    store.pushPendingDispatch("parent-b", { callId: "cb1", parentSessionId: "parent-b", agentInstanceId: "Agent#9", dispatchedAt: TS });
    store.clearPendingDispatches("parent-a");
    expect(store.claimPendingDispatch("parent-b")!.agentInstanceId).toBe("Agent#9");
  });

  it("preserves all dispatch fields through push and claim", () => {
    const dispatch = {
      callId: "call-xyz",
      parentSessionId: "parent-a",
      agentInstanceId: "Planner#3",
      agentType: "Planner",
      runId: "20260808T120000Z-abcd",
      prompt: '{"agent_instance_id":"Planner#3","run_id":"20260808T120000Z-abcd"}',
      dispatchedAt: "2026-08-08T12:00:00.000Z",
    };
    store.pushPendingDispatch("parent-a", dispatch);
    const claimed = store.claimPendingDispatch("parent-a");
    expect(claimed).toEqual(dispatch);
  });

  it("handles a dispatch without agentInstanceId or runId (unresolved identity)", () => {
    store.pushPendingDispatch("parent-a", {
      callId: "call-no-id",
      parentSessionId: "parent-a",
      dispatchedAt: TS,
      // no agentInstanceId, no runId — identity could not be extracted from the prompt
    });
    const claimed = store.claimPendingDispatch("parent-a");
    expect(claimed).toBeDefined();
    expect(claimed!.agentInstanceId).toBeUndefined();
    expect(claimed!.runId).toBeUndefined();
  });

  it("concurrent dispatches under one parent are attributed in dispatch order (FIFO)", () => {
    // Simulates an orchestrator that fires three subagents before any session.created
    // arrives. The three pending entries must be claimed in the exact order they were
    // pushed, regardless of how much time passes between push and claim.
    const parents = ["parent-root"];
    const agents = ["Writer#1", "Reviewer#2", "Analyst#3"];
    for (let i = 0; i < agents.length; i++) {
      store.pushPendingDispatch(parents[0], {
        callId: `call-${i}`,
        parentSessionId: parents[0],
        agentInstanceId: agents[i],
        dispatchedAt: TS,
      });
    }
    for (const expectedAgent of agents) {
      expect(store.claimPendingDispatch(parents[0])!.agentInstanceId).toBe(expectedAgent);
    }
    expect(store.claimPendingDispatch(parents[0])).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// adoptRunId — run-id propagation to orchestrator chain head — Stage 3
//
// These tests specify the new adoptRunId method that does not yet exist on
// SessionCorrelationStore. All tests in this section are RED until the
// implementation adds adoptRunId.
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — adoptRunId", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
  });

  it("sets the run id on the orchestrator session at the head of the chain", () => {
    store.register("orch", makeRecord({ sessionId: "orch" }));
    store.register("sub", makeRecord({ sessionId: "sub", parentId: "orch" }));

    store.adoptRunId("sub", RUN_ID);

    expect(store.get("orch")!.runId).toBe(RUN_ID);
  });

  it("is a no-op when the chain head already carries a run id", () => {
    const originalRunId = "20260101T000000Z-orig";
    store.register("orch", makeRecord({ sessionId: "orch", runId: originalRunId }));
    store.register("sub", makeRecord({ sessionId: "sub", parentId: "orch" }));

    store.adoptRunId("sub", "20260808T000000Z-new1");

    // Original run id must be preserved
    expect(store.get("orch")!.runId).toBe(originalRunId);
  });

  it("sets run id directly when the session is the chain head (orchestrator itself)", () => {
    store.register("orch", makeRecord({ sessionId: "orch" }));

    store.adoptRunId("orch", RUN_ID);

    expect(store.get("orch")!.runId).toBe(RUN_ID);
  });

  it("is a no-op for an unknown session and does not throw", () => {
    expect(() => store.adoptRunId("not-registered", RUN_ID)).not.toThrow();
  });

  it("propagates run id through a two-hop chain to the root", () => {
    store.register("root", makeRecord({ sessionId: "root" }));
    store.register("mid", makeRecord({ sessionId: "mid", parentId: "root" }));
    store.register("leaf", makeRecord({ sessionId: "leaf", parentId: "mid" }));

    store.adoptRunId("leaf", RUN_ID);

    expect(store.get("root")!.runId).toBe(RUN_ID);
    // Intermediate sessions are not the target of adoption
    expect(store.get("mid")!.runId).toBeUndefined();
    expect(store.get("leaf")!.runId).toBeUndefined();
  });

  it("after adoptRunId, getRunId resolves correctly for all sessions in the chain", () => {
    store.register("root", makeRecord({ sessionId: "root" }));
    store.register("sub", makeRecord({ sessionId: "sub", parentId: "root" }));

    store.adoptRunId("sub", RUN_ID);

    // Both sessions now resolve the same run id through the chain
    expect(store.getRunId("root")).toBe(RUN_ID);
    expect(store.getRunId("sub")).toBe(RUN_ID);
  });
});

// ---------------------------------------------------------------------------
// PendingToolCall registry (call-id-keyed)
//
// These tests specify the new registerToolCall / closeToolCall / peekToolCall /
// openDispatchCallFor methods that do not yet exist on SessionCorrelationStore.
// All tests in this section are RED until the implementation adds the
// call-id-keyed tool call registry to the store.
// ---------------------------------------------------------------------------

describe("SessionCorrelationStore — PendingToolCall registry", () => {
  let store: SessionCorrelationStore;

  beforeEach(() => {
    store = makeStore();
  });

  function makePendingToolCall(overrides: Partial<PendingToolCall> = {}): PendingToolCall {
    return {
      callId: "call-default-1",
      sessionId: "sess-default",
      toolName: "bash",
      isDispatch: false,
      startedAt: TS,
      ...overrides,
    };
  }

  it("registerToolCall stores an in-flight tool call, retrievable via peekToolCall", () => {
    const entry = makePendingToolCall({ callId: "call-reg-1" });
    store.registerToolCall(entry);
    const peeked = store.peekToolCall("call-reg-1");
    expect(peeked).toBeDefined();
    expect(peeked!.callId).toBe("call-reg-1");
  });

  it("peekToolCall does not consume the entry — a second peek returns the same entry", () => {
    store.registerToolCall(makePendingToolCall({ callId: "call-peek-1" }));
    const first = store.peekToolCall("call-peek-1");
    const second = store.peekToolCall("call-peek-1");
    expect(first).toBeDefined();
    expect(second).toBeDefined();
    expect(second!.callId).toBe("call-peek-1");
  });

  it("closeToolCall returns the entry on the first call", () => {
    store.registerToolCall(makePendingToolCall({ callId: "call-close-1" }));
    const result = store.closeToolCall("call-close-1");
    expect(result).toBeDefined();
    expect(result!.callId).toBe("call-close-1");
  });

  it("closeToolCall returns undefined on the second call, making closure idempotent", () => {
    store.registerToolCall(makePendingToolCall({ callId: "call-idem-1" }));
    store.closeToolCall("call-idem-1");
    const second = store.closeToolCall("call-idem-1");
    expect(second).toBeUndefined();
  });

  it("peekToolCall returns undefined after the call has been closed", () => {
    store.registerToolCall(makePendingToolCall({ callId: "call-peek-after-close" }));
    store.closeToolCall("call-peek-after-close");
    expect(store.peekToolCall("call-peek-after-close")).toBeUndefined();
  });

  it("closeToolCall returns undefined for a call id that was never registered", () => {
    expect(store.closeToolCall("call-never-registered")).toBeUndefined();
  });

  it("peekToolCall returns undefined for a call id that was never registered", () => {
    expect(store.peekToolCall("call-never-registered")).toBeUndefined();
  });

  it("different call ids are tracked independently — closing one does not affect the other", () => {
    store.registerToolCall(makePendingToolCall({ callId: "call-indep-A" }));
    store.registerToolCall(makePendingToolCall({ callId: "call-indep-B" }));
    store.closeToolCall("call-indep-A");
    expect(store.peekToolCall("call-indep-B")).toBeDefined();
    expect(store.closeToolCall("call-indep-B")).toBeDefined();
    expect(store.peekToolCall("call-indep-A")).toBeUndefined();
  });

  it("preserves all PendingToolCall fields through registration and retrieval", () => {
    const entry: PendingToolCall = {
      callId: "call-fields-1",
      sessionId: "sess-preserve",
      toolName: "task",
      isDispatch: true,
      startedAt: "2026-08-11T12:00:00.000Z",
    };
    store.registerToolCall(entry);
    const peeked = store.peekToolCall("call-fields-1");
    expect(peeked).toEqual(entry);
  });

  it("closing and re-registering the same call id makes the new entry retrievable", () => {
    store.registerToolCall(makePendingToolCall({ callId: "call-re-reg", toolName: "bash" }));
    store.closeToolCall("call-re-reg");
    // Re-register the same call id (edge case: re-use after close)
    store.registerToolCall(makePendingToolCall({ callId: "call-re-reg", toolName: "edit" }));
    const peeked = store.peekToolCall("call-re-reg");
    expect(peeked).toBeDefined();
    expect(peeked!.toolName).toBe("edit");
  });

  it("openDispatchCallFor returns the dispatch tool call linked to a child session", () => {
    store.register(
      "child-sess-dispatch",
      makeRecord({ sessionId: "child-sess-dispatch", dispatchCallId: "call-dispatch-open" }),
    );
    store.registerToolCall(
      makePendingToolCall({ callId: "call-dispatch-open", toolName: "task", isDispatch: true }),
    );
    const result = store.openDispatchCallFor("child-sess-dispatch");
    expect(result).toBeDefined();
    expect(result!.callId).toBe("call-dispatch-open");
    expect(result!.isDispatch).toBe(true);
  });

  it("openDispatchCallFor returns undefined when the child session has no dispatch call id", () => {
    store.register(
      "child-no-dispatch",
      makeRecord({ sessionId: "child-no-dispatch" }),
    );
    expect(store.openDispatchCallFor("child-no-dispatch")).toBeUndefined();
  });

  it("openDispatchCallFor returns undefined once the dispatch call has been closed", () => {
    store.register(
      "child-closed-dispatch",
      makeRecord({ sessionId: "child-closed-dispatch", dispatchCallId: "call-closed-dispatch" }),
    );
    store.registerToolCall(
      makePendingToolCall({ callId: "call-closed-dispatch", toolName: "task", isDispatch: true }),
    );
    store.closeToolCall("call-closed-dispatch");
    expect(store.openDispatchCallFor("child-closed-dispatch")).toBeUndefined();
  });

  it("openDispatchCallFor returns undefined for an unknown child session", () => {
    expect(store.openDispatchCallFor("unknown-child-session")).toBeUndefined();
  });

  it("parallel task calls from one session each have independent entries in the registry", () => {
    // An orchestrator can dispatch multiple subagents before any completes.
    store.registerToolCall(
      makePendingToolCall({ callId: "par-dispatch-1", toolName: "task", isDispatch: true }),
    );
    store.registerToolCall(
      makePendingToolCall({ callId: "par-dispatch-2", toolName: "task", isDispatch: true }),
    );
    // Close the first dispatch.
    store.closeToolCall("par-dispatch-1");
    // The second must still be open.
    expect(store.peekToolCall("par-dispatch-2")).toBeDefined();
    expect(store.closeToolCall("par-dispatch-2")).toBeDefined();
  });
});
