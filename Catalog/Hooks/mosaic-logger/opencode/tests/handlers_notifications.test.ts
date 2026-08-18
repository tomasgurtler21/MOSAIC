/**
 * handlers_notifications.test.ts — Tests for notification and compaction event handlers.
 *
 * Covers:
 *   - handlePermissionAsk: notification_type is always populated (from payload or stable fallback)
 *   - handlePermissionAsk: optional fields appear only when the payload supplies them
 *   - handlePermissionAsk: no notification_id is minted for a single-shot notification
 *   - handlePermissionAsk: requires_response and resolution are emitted when the payload supplies them
 *   - handlePermissionAsk: routing follows session scope (orchestrator vs invocation stream)
 *   - handleSessionCompacting: emits a compaction event with whichever fields the payload supports
 *   - handleSessionCompacting: minimal valid event when the payload is sparse or empty
 *   - handleSessionCompacting: routing follows session scope
 *   - Defensive handling: neither handler throws for null, undefined, empty, or malformed payloads
 *   - Defensive handling: appendEvent errors do not propagate out of either handler
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import { createNotificationHandlers } from "../lib/handlers_notifications";
import {
  type HandlerDependencies,
  type SdkClient,
  type PermissionAskInput,
  type SessionCompactingInput,
} from "../lib/handlers_session";
import { SessionCorrelationStore } from "../lib/correlation";
import { LogPaths, setDebugLogger } from "../lib/core";
import {
  permissionAskPayload,
  sessionCompactingPayload,
} from "./fixtures/opencode_api";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const RUN_ID = "20260101T170000Z-a3f9";
const ORCH_SESSION = "sess-orch-001";
const SUB_SESSION = "sess-sub-001";
const AGENT_INSTANCE_ID = "Research#1";

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

let tmpDir: string;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "mosaic-hn-test-"));
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
    app: { log: async () => {} },
  };
}

type CollectedEvent = { filePath: string; event: Record<string, unknown> };

function makeDeps(
  store: SessionCorrelationStore,
  paths: LogPaths,
  collected: CollectedEvent[],
  appendEventOverride?: (filePath: string, event: Record<string, unknown>) => Promise<void>,
): HandlerDependencies {
  return {
    store,
    paths,
    sdkClient: makeNullSdk(),
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
    appendEvent: appendEventOverride ?? (async (filePath, event) => {
      collected.push({ filePath, event });
    }),
    fallbackCallId: (toolName) => `${toolName ?? "tool"}_call_fallback`,
  };
}

// ---------------------------------------------------------------------------
// handlePermissionAsk — notification_type population
// ---------------------------------------------------------------------------

describe("handlePermissionAsk — notification_type population", () => {
  it("emits a notification event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handlePermissionAsk(permissionAskPayload({ sessionID: ORCH_SESSION }));

    const event = collected.find((e) => e.event.event === "notification");
    expect(event).toBeDefined();
  });

  it("sets notification_type from the payload type field when present", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handlePermissionAsk(
      permissionAskPayload({ sessionID: ORCH_SESSION, type: "read_file" }),
    );

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.event.notification_type).toBe("read_file");
  });

  it("falls back to a stable literal when the payload carries no type", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    // No `type` field in payload
    await handlePermissionAsk(permissionAskPayload({ sessionID: ORCH_SESSION, type: undefined }));

    const event = collected.find((e) => e.event.event === "notification");
    expect(typeof event!.event.notification_type).toBe("string");
    expect((event!.event.notification_type as string).length).toBeGreaterThan(0);
  });

  it("falls back to a stable literal when the payload type is an empty string", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handlePermissionAsk(permissionAskPayload({ sessionID: ORCH_SESSION, type: "" }));

    const event = collected.find((e) => e.event.event === "notification");
    expect(typeof event!.event.notification_type).toBe("string");
    expect((event!.event.notification_type as string).length).toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// handlePermissionAsk — optional field presence
// ---------------------------------------------------------------------------

describe("handlePermissionAsk — optional field presence", () => {
  it("includes message when the payload supplies a title", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handlePermissionAsk(
      permissionAskPayload({ sessionID: ORCH_SESSION, title: "Allow file access?" }),
    );

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.event.message).toBe("Allow file access?");
  });

  it("omits message when the payload supplies no title", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handlePermissionAsk(permissionAskPayload({ sessionID: ORCH_SESSION, title: undefined }));

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.event.message).toBeUndefined();
  });

  it("includes requires_response when the payload supplies it as true", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const payload: PermissionAskInput = {
      sessionID: ORCH_SESSION,
      type: "read_file",
      requires_response: true,
    };
    await handlePermissionAsk(payload);

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.event.requires_response).toBe(true);
  });

  it("includes requires_response when the payload supplies it as false", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const payload: PermissionAskInput = {
      sessionID: ORCH_SESSION,
      type: "write_file",
      requires_response: false,
    };
    await handlePermissionAsk(payload);

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.event.requires_response).toBe(false);
  });

  it("omits requires_response when the payload does not supply it", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handlePermissionAsk(permissionAskPayload({ sessionID: ORCH_SESSION }));

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.event.requires_response).toBeUndefined();
  });

  it("includes resolution when the payload supplies it", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const payload: PermissionAskInput = {
      sessionID: ORCH_SESSION,
      type: "read_file",
      resolution: "approved",
    };
    await handlePermissionAsk(payload);

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.event.resolution).toBe("approved");
  });

  it("omits resolution when the payload does not supply it", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handlePermissionAsk(permissionAskPayload({ sessionID: ORCH_SESSION }));

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.event.resolution).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// handlePermissionAsk — single-shot notification model
// ---------------------------------------------------------------------------

describe("handlePermissionAsk — single-shot notification model", () => {
  it("does not mint a notification_id for a single-shot notification", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handlePermissionAsk(permissionAskPayload({ sessionID: ORCH_SESSION }));

    const event = collected.find((e) => e.event.event === "notification");
    // For single-shot visibility no correlation id should be present
    expect(event!.event.notification_id).toBeUndefined();
  });

  it("emits exactly one notification event per permission-ask invocation", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handlePermissionAsk(permissionAskPayload({ sessionID: ORCH_SESSION }));

    const notificationEvents = collected.filter((e) => e.event.event === "notification");
    expect(notificationEvents).toHaveLength(1);
  });
});

// ---------------------------------------------------------------------------
// handlePermissionAsk — routing by session scope
// ---------------------------------------------------------------------------

describe("handlePermissionAsk — routing by session scope", () => {
  it("routes an orchestrator-session notification to the orchestrator events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handlePermissionAsk(permissionAskPayload({ sessionID: ORCH_SESSION }));

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.filePath).toBe(paths.orchestratorEvents(RUN_ID));
  });

  it("routes a subagent-session notification to the invocation events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handlePermissionAsk(permissionAskPayload({ sessionID: SUB_SESSION }));

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.filePath).toBe(paths.invocationEvents(RUN_ID, AGENT_INSTANCE_ID));
  });

  it("never routes a subagent notification to the orchestrator stream", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handlePermissionAsk(permissionAskPayload({ sessionID: SUB_SESSION }));

    const event = collected.find((e) => e.event.event === "notification");
    expect(event!.filePath).not.toBe(paths.orchestratorEvents(RUN_ID));
  });

  it("still emits the event when the session is unknown (degrades rather than drops)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    // No store.register() — session identity is unknown
    await handlePermissionAsk(permissionAskPayload({ sessionID: "sess-unknown" }));

    const event = collected.find((e) => e.event.event === "notification");
    // Should still emit — unknown sessions degrade to the orchestrator/bucket path, never drop
    expect(event).toBeDefined();
  });
});

// ---------------------------------------------------------------------------
// handleSessionCompacting — compaction event emission
// ---------------------------------------------------------------------------

describe("handleSessionCompacting — compaction event emission", () => {
  it("emits a compaction event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleSessionCompacting(sessionCompactingPayload({ sessionID: ORCH_SESSION }));

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event).toBeDefined();
  });

  it("includes trigger 'auto' when the payload supplies it", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const payload: SessionCompactingInput = { sessionID: ORCH_SESSION, trigger: "auto" };
    await handleSessionCompacting(payload);

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.event.trigger).toBe("auto");
  });

  it("includes trigger 'manual' when the payload supplies it", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const payload: SessionCompactingInput = { sessionID: ORCH_SESSION, trigger: "manual" };
    await handleSessionCompacting(payload);

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.event.trigger).toBe("manual");
  });

  it("omits trigger when the payload supplies an unrecognised value", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const payload: SessionCompactingInput = { sessionID: ORCH_SESSION, trigger: "unknown_trigger" };
    await handleSessionCompacting(payload);

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.event.trigger).toBeUndefined();
  });

  it("omits trigger when the payload supplies no trigger field", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleSessionCompacting(sessionCompactingPayload({ sessionID: ORCH_SESSION }));

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.event.trigger).toBeUndefined();
  });

  it("includes tokens_before when the payload supplies it as a number", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const payload: SessionCompactingInput = { sessionID: ORCH_SESSION, tokens_before: 8500 };
    await handleSessionCompacting(payload);

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.event.tokens_before).toBe(8500);
  });

  it("includes tokens_after when the payload supplies it as a number", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const payload: SessionCompactingInput = { sessionID: ORCH_SESSION, tokens_after: 3200 };
    await handleSessionCompacting(payload);

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.event.tokens_after).toBe(3200);
  });

  it("omits tokens_before when the payload supplies no numeric value", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleSessionCompacting(sessionCompactingPayload({ sessionID: ORCH_SESSION }));

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.event.tokens_before).toBeUndefined();
  });

  it("omits tokens_after when the payload supplies no numeric value", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleSessionCompacting(sessionCompactingPayload({ sessionID: ORCH_SESSION }));

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.event.tokens_after).toBeUndefined();
  });

  it("includes all three optional fields when the payload supplies them all", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const payload: SessionCompactingInput = {
      sessionID: ORCH_SESSION,
      trigger: "auto",
      tokens_before: 12000,
      tokens_after: 4000,
    };
    await handleSessionCompacting(payload);

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.event.trigger).toBe("auto");
    expect(event!.event.tokens_before).toBe(12000);
    expect(event!.event.tokens_after).toBe(4000);
  });

  it("produces a minimal valid compaction event when the payload is sparse (no optional fields)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    // Only sessionID supplied — all compaction-specific fields absent
    await handleSessionCompacting({ sessionID: ORCH_SESSION });

    const event = collected.find((e) => e.event.event === "compaction");
    // The event must exist and carry the standard envelope
    expect(event).toBeDefined();
    expect(event!.event.event).toBe("compaction");
    // Compaction-specific optional fields must be absent
    expect(event!.event.trigger).toBeUndefined();
    expect(event!.event.tokens_before).toBeUndefined();
    expect(event!.event.tokens_after).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// handleSessionCompacting — routing by session scope
// ---------------------------------------------------------------------------

describe("handleSessionCompacting — routing by session scope", () => {
  it("routes an orchestrator-session compaction to the orchestrator events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await handleSessionCompacting(sessionCompactingPayload({ sessionID: ORCH_SESSION }));

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.filePath).toBe(paths.orchestratorEvents(RUN_ID));
  });

  it("routes a subagent-session compaction to the invocation events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handleSessionCompacting(sessionCompactingPayload({ sessionID: SUB_SESSION }));

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event!.filePath).toBe(paths.invocationEvents(RUN_ID, AGENT_INSTANCE_ID));
  });

  it("still emits when the session is unknown (degrades rather than drops)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    await handleSessionCompacting(sessionCompactingPayload({ sessionID: "sess-unknown" }));

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event).toBeDefined();
  });
});

// ---------------------------------------------------------------------------
// Defensive handling — handlePermissionAsk
// ---------------------------------------------------------------------------

describe("handlePermissionAsk — defensive handling of unexpected payloads", () => {
  it("does not throw when called with null as the input", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    await expect(
      handlePermissionAsk(null as unknown as PermissionAskInput),
    ).resolves.toBeUndefined();
  });

  it("does not throw when called with undefined as the input", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    await expect(
      handlePermissionAsk(undefined as unknown as PermissionAskInput),
    ).resolves.toBeUndefined();
  });

  it("does not throw when called with an empty object as the input", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    await expect(handlePermissionAsk({} as PermissionAskInput)).resolves.toBeUndefined();
  });

  it("still emits a notification event for an empty-object payload", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    await handlePermissionAsk({} as PermissionAskInput);

    const event = collected.find((e) => e.event.event === "notification");
    // Even with no payload data, a minimal notification event must be emitted
    expect(event).toBeDefined();
    expect(typeof event!.event.notification_type).toBe("string");
  });

  it("does not throw when appendEvent rejects (I/O error)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const crashingAppend = async () => {
      throw new Error("disk full");
    };
    const { handlePermissionAsk } = createNotificationHandlers(
      makeDeps(store, paths, [], crashingAppend),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await expect(
      handlePermissionAsk(permissionAskPayload({ sessionID: ORCH_SESSION })),
    ).resolves.toBeUndefined();
  });

  it("does not produce a malformed event for a payload with only unknown extra fields", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handlePermissionAsk } = createNotificationHandlers(makeDeps(store, paths, collected));

    store.register(ORCH_SESSION, { runId: RUN_ID });

    // Payload carries only unrecognised fields — all named fields absent
    const strangePaylod: PermissionAskInput = {
      sessionID: ORCH_SESSION,
      unrecognised_field_xyz: 42,
      another_unknown: { nested: true },
    } as PermissionAskInput;
    await handlePermissionAsk(strangePaylod);

    const event = collected.find((e) => e.event.event === "notification");
    // The event must exist and notification_type must still be a non-empty string
    expect(event).toBeDefined();
    expect(typeof event!.event.notification_type).toBe("string");
    expect((event!.event.notification_type as string).length).toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// Defensive handling — handleSessionCompacting
// ---------------------------------------------------------------------------

describe("handleSessionCompacting — defensive handling of unexpected payloads", () => {
  it("does not throw when called with null as the input", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    await expect(
      handleSessionCompacting(null as unknown as SessionCompactingInput),
    ).resolves.toBeUndefined();
  });

  it("does not throw when called with undefined as the input", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    await expect(
      handleSessionCompacting(undefined as unknown as SessionCompactingInput),
    ).resolves.toBeUndefined();
  });

  it("does not throw when called with an empty object as the input", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    await expect(
      handleSessionCompacting({} as SessionCompactingInput),
    ).resolves.toBeUndefined();
  });

  it("still emits a compaction event for an empty-object payload", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    await handleSessionCompacting({} as SessionCompactingInput);

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event).toBeDefined();
  });

  it("does not throw when appendEvent rejects (I/O error)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const crashingAppend = async () => {
      throw new Error("disk full");
    };
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, [], crashingAppend),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    await expect(
      handleSessionCompacting(sessionCompactingPayload({ sessionID: ORCH_SESSION })),
    ).resolves.toBeUndefined();
  });

  it("omits compaction-specific fields for a payload that carries only unrecognised keys", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const strangePayload: SessionCompactingInput = {
      sessionID: ORCH_SESSION,
      unknown_field_abc: "surprise",
    } as SessionCompactingInput;
    await handleSessionCompacting(strangePayload);

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event).toBeDefined();
    // No compaction-specific fields should be fabricated
    expect(event!.event.trigger).toBeUndefined();
    expect(event!.event.tokens_before).toBeUndefined();
    expect(event!.event.tokens_after).toBeUndefined();
  });

  it("does not produce a malformed event for a tokens_before value that is not a number", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const { handleSessionCompacting } = createNotificationHandlers(
      makeDeps(store, paths, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });

    const payload: SessionCompactingInput = {
      sessionID: ORCH_SESSION,
      tokens_before: "not-a-number" as unknown as number,
      tokens_after: null as unknown as number,
    };
    await handleSessionCompacting(payload);

    const event = collected.find((e) => e.event.event === "compaction");
    expect(event).toBeDefined();
    // Non-numeric values must be omitted, not emitted as-is
    expect(event!.event.tokens_before).toBeUndefined();
    expect(event!.event.tokens_after).toBeUndefined();
  });
});
