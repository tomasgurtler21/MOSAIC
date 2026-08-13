/**
 * closed_session_registry.test.ts — Tests for durable session-end deduplication.
 *
 * Covers:
 *   - MemoryClosedSessionRegistry: basic contract (isClosed, markClosed, idempotency)
 *   - FileClosedSessionRegistry: on-disk persistence, restart simulation, marker content
 *   - First-close path: a session never closed before gets exactly one close pair
 *   - Durability: a session closed by one registry instance is recognized by a fresh
 *     instance over the same log root, emitting no second close pair
 *   - Degradation: an unavailable or unwritable registry never blocks closure, never
 *     throws, and falls back to in-memory behavior
 *   - Concurrent closure: multiple concurrent markClosed calls on the same session
 *     are safe; isClosed remains true and no error escapes
 *   - Artifact placement: the marker directory is dot-prefixed, lives directly under
 *     the log root, and is never inside a run folder where a log consumer would see it
 *
 * All tests fail until the implementation is complete (TDD RED phase).
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeFsPromises from "node:fs/promises";
import * as nodeOs from "node:os";

// These imports reference types and classes that do not exist in the current
// implementation. The tests will fail to compile until Stage 5 is implemented.
import {
  type ClosedSessionRegistry,
  type ClosedSessionMarker,
  FileClosedSessionRegistry,
  MemoryClosedSessionRegistry,
} from "../lib/correlation";

import { LogPaths, sanitizeComponent, setDebugLogger } from "../lib/core";

// These imports reference the new `closedSessions` field on HandlerDependencies,
// which does not exist until Stage 5 extends the interface.
import {
  createSessionHandlers,
  type HandlerDependencies,
  type SdkClient,
} from "../lib/handlers_session";
import { SessionCorrelationStore } from "../lib/correlation";
import {
  sessionIdleEvent,
  sessionDeletedEvent,
  sessionCreatedEvent,
} from "./fixtures/opencode_api";

// ---------------------------------------------------------------------------
// Shared constants and helpers
// ---------------------------------------------------------------------------

const RUN_ID = "20260101T170000Z-a3f9";
const ORCH_SESSION = "sess-orch-durability-001";
const SESSION_ALPHA = "sess-alpha-001";
const SESSION_BETA = "sess-beta-002";

let tmpDir: string;
let paths: LogPaths;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "mosaic-csr-test-"));
  paths = new LogPaths(tmpDir);
  setDebugLogger(() => {});
});

afterEach(() => {
  nodeFs.rmSync(tmpDir, { recursive: true, force: true });
  setDebugLogger(() => {});
});

function makeStore(): SessionCorrelationStore {
  return new SessionCorrelationStore();
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

/**
 * Build HandlerDependencies with a ClosedSessionRegistry injected.
 * The `closedSessions` field is new in Stage 5; this will cause a compile error
 * until HandlerDependencies is extended.
 */
function makeDepsWithRegistry(
  store: SessionCorrelationStore,
  logPaths: LogPaths,
  sdk: SdkClient,
  collected: CollectedEvent[],
  registry: ClosedSessionRegistry,
): HandlerDependencies {
  return {
    store,
    paths: logPaths,
    sdkClient: sdk,
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
    // NEW field: Stage 5 extends HandlerDependencies with closedSessions.
    // This line will cause a compile error until HandlerDependencies is updated.
    closedSessions: registry,
  };
}

// ---------------------------------------------------------------------------
// MemoryClosedSessionRegistry — basic contract
// ---------------------------------------------------------------------------

describe("MemoryClosedSessionRegistry — basic contract", () => {
  it("isClosed returns false for a session that has never been closed", async () => {
    const registry = new MemoryClosedSessionRegistry();
    expect(await registry.isClosed("session-never-closed")).toBe(false);
  });

  it("markClosed returns true on a successful first close", async () => {
    const registry = new MemoryClosedSessionRegistry();
    const result = await registry.markClosed(SESSION_ALPHA);
    expect(result).toBe(true);
  });

  it("isClosed returns true after markClosed has been called", async () => {
    const registry = new MemoryClosedSessionRegistry();
    await registry.markClosed(SESSION_ALPHA);
    expect(await registry.isClosed(SESSION_ALPHA)).toBe(true);
  });

  it("markClosed is idempotent: second call on the same session still returns true", async () => {
    const registry = new MemoryClosedSessionRegistry();
    await registry.markClosed(SESSION_ALPHA);
    const secondResult = await registry.markClosed(SESSION_ALPHA);
    expect(secondResult).toBe(true);
  });

  it("markClosed never throws for any session id", async () => {
    const registry = new MemoryClosedSessionRegistry();
    await expect(registry.markClosed("")).resolves.toBeDefined();
    await expect(registry.markClosed("session-with/special:chars")).resolves.toBeDefined();
    await expect(registry.markClosed("session-a")).resolves.toBeDefined();
  });

  it("isClosed never throws for any session id", async () => {
    const registry = new MemoryClosedSessionRegistry();
    await expect(registry.isClosed("")).resolves.toBeDefined();
    await expect(registry.isClosed("session-with/special:chars")).resolves.toBeDefined();
    await expect(registry.isClosed("session-a")).resolves.toBeDefined();
  });

  it("independent sessions do not interfere with each other", async () => {
    const registry = new MemoryClosedSessionRegistry();
    await registry.markClosed(SESSION_ALPHA);
    expect(await registry.isClosed(SESSION_ALPHA)).toBe(true);
    expect(await registry.isClosed(SESSION_BETA)).toBe(false);
  });

  it("closing many sessions all produce independent closed states", async () => {
    const registry = new MemoryClosedSessionRegistry();
    const sessions = ["s1", "s2", "s3", "s4", "s5"];
    for (const s of sessions) {
      await registry.markClosed(s);
    }
    for (const s of sessions) {
      expect(await registry.isClosed(s)).toBe(true);
    }
    expect(await registry.isClosed("s-not-closed")).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// MemoryClosedSessionRegistry — concurrent closure
// ---------------------------------------------------------------------------

describe("MemoryClosedSessionRegistry — concurrent closure attempts", () => {
  it("concurrent markClosed calls on the same session all resolve without error", async () => {
    const registry = new MemoryClosedSessionRegistry();
    const results = await Promise.all([
      registry.markClosed(SESSION_ALPHA),
      registry.markClosed(SESSION_ALPHA),
      registry.markClosed(SESSION_ALPHA),
    ]);
    // All must have resolved (not thrown); their return values may vary
    expect(results).toHaveLength(3);
    expect(results.every((r: unknown) => typeof r === "boolean")).toBe(true);
  });

  it("after concurrent markClosed calls, isClosed returns true", async () => {
    const registry = new MemoryClosedSessionRegistry();
    await Promise.all([
      registry.markClosed(SESSION_ALPHA),
      registry.markClosed(SESSION_ALPHA),
    ]);
    expect(await registry.isClosed(SESSION_ALPHA)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// FileClosedSessionRegistry — first-close path
// ---------------------------------------------------------------------------

describe("FileClosedSessionRegistry — first-close path", () => {
  it("isClosed returns false when no marker file exists yet", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    expect(await registry.isClosed(SESSION_ALPHA)).toBe(false);
  });

  it("markClosed creates a marker file and returns true", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    const result = await registry.markClosed(SESSION_ALPHA);
    expect(result).toBe(true);
  });

  it("isClosed returns true after markClosed has written the marker", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    await registry.markClosed(SESSION_ALPHA);
    expect(await registry.isClosed(SESSION_ALPHA)).toBe(true);
  });

  it("the marker file contains valid JSON with the required ClosedSessionMarker fields", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    await registry.markClosed(SESSION_ALPHA);

    // The marker lives at {root}/.session-ends/{sanitize(sessionId)}.json
    const markerPath = nodePath.join(
      paths.root,
      ".session-ends",
      sanitizeComponent(SESSION_ALPHA) + ".json",
    );
    expect(nodeFs.existsSync(markerPath)).toBe(true);

    const raw = nodeFs.readFileSync(markerPath, "utf-8");
    const marker = JSON.parse(raw) as ClosedSessionMarker;

    expect(typeof marker.session_id).toBe("string");
    expect(marker.session_id).toBe(SESSION_ALPHA);
    expect(typeof marker.closed_at).toBe("string");
    // ISO 8601 timestamp — must not be empty
    expect(marker.closed_at.length).toBeGreaterThan(0);
    expect(typeof marker.harness).toBe("string");
    // Must identify the opencode harness
    expect(marker.harness).toBe("opencode");
  });

  it("markClosed is idempotent: second call when marker already exists returns true and does not throw", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    await registry.markClosed(SESSION_ALPHA);

    let secondResult: boolean | undefined;
    await expect(async () => {
      secondResult = await registry.markClosed(SESSION_ALPHA);
    }).not.toThrow();
    // An already-present marker is success, not an error
    expect(secondResult).toBe(true);
  });

  it("markClosed creates parent directories automatically (no pre-existing .session-ends dir)", async () => {
    // Ensure the .session-ends dir does not exist before the call
    const sessionEndsDir = nodePath.join(paths.root, ".session-ends");
    expect(nodeFs.existsSync(sessionEndsDir)).toBe(false);

    const registry = new FileClosedSessionRegistry(paths);
    const result = await registry.markClosed(SESSION_ALPHA);

    expect(result).toBe(true);
    expect(nodeFs.existsSync(sessionEndsDir)).toBe(true);
  });

  it("markClosed and isClosed never throw for an empty-string session id", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    await expect(registry.markClosed("")).resolves.toBeDefined();
    await expect(registry.isClosed("")).resolves.toBeDefined();
  });

  it("session ids with characters invalid on some filesystems are sanitized in the marker path", async () => {
    const unsafeSessionId = "session/with:special*chars?";
    const registry = new FileClosedSessionRegistry(paths);
    await registry.markClosed(unsafeSessionId);

    const expectedPath = nodePath.join(
      paths.root,
      ".session-ends",
      sanitizeComponent(unsafeSessionId) + ".json",
    );
    expect(nodeFs.existsSync(expectedPath)).toBe(true);
    expect(await registry.isClosed(unsafeSessionId)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// FileClosedSessionRegistry — restart durability (simulated process restart)
// ---------------------------------------------------------------------------

describe("FileClosedSessionRegistry — durability across a simulated process restart", () => {
  it("a session closed by one registry instance is recognized as already closed by a fresh instance over the same root", async () => {
    // Simulate process 1: close the session
    const registry1 = new FileClosedSessionRegistry(paths);
    await registry1.markClosed(ORCH_SESSION);

    // Simulate process restart (process 2): a fresh registry over the same root.
    // There is no shared in-memory state between the two instances.
    const registry2 = new FileClosedSessionRegistry(paths);
    expect(await registry2.isClosed(ORCH_SESSION)).toBe(true);
  });

  it("a fresh registry does not recognize a session that was never closed", async () => {
    const registry1 = new FileClosedSessionRegistry(paths);
    // Close session-alpha but NOT session-beta
    await registry1.markClosed(SESSION_ALPHA);

    const registry2 = new FileClosedSessionRegistry(paths);
    expect(await registry2.isClosed(SESSION_ALPHA)).toBe(true);
    expect(await registry2.isClosed(SESSION_BETA)).toBe(false);
  });

  it("closing multiple sessions in one instance makes all of them visible to a fresh instance", async () => {
    const registry1 = new FileClosedSessionRegistry(paths);
    await registry1.markClosed(SESSION_ALPHA);
    await registry1.markClosed(SESSION_BETA);

    const registry2 = new FileClosedSessionRegistry(paths);
    expect(await registry2.isClosed(SESSION_ALPHA)).toBe(true);
    expect(await registry2.isClosed(SESSION_BETA)).toBe(true);
  });

  it("a marker written under one log root is invisible to a registry over a different log root", async () => {
    // This protects against the risk noted in the plan: a stale marker from an
    // unrelated workspace suppressing a legitimate close. Markers are scoped to
    // their log root by construction — a different root yields a different path.
    const tmpDirB = nodeFs.mkdtempSync(
      nodePath.join(nodeOs.tmpdir(), "mosaic-csr-test-rootB-"),
    );
    try {
      const pathsB = new LogPaths(tmpDirB);

      // Write a marker under log root A (paths)
      const registryA = new FileClosedSessionRegistry(paths);
      await registryA.markClosed(SESSION_ALPHA);
      expect(await registryA.isClosed(SESSION_ALPHA)).toBe(true);

      // A registry over a completely different root must not see that marker
      const registryB = new FileClosedSessionRegistry(pathsB);
      expect(await registryB.isClosed(SESSION_ALPHA)).toBe(false);
    } finally {
      nodeFs.rmSync(tmpDirB, { recursive: true, force: true });
    }
  });
});

// ---------------------------------------------------------------------------
// FileClosedSessionRegistry — degradation when I/O fails
// ---------------------------------------------------------------------------

describe("FileClosedSessionRegistry — degradation when I/O is unavailable", () => {
  it("isClosed returns false (not an error) when the marker file does not exist", async () => {
    // This is the normal case for a session that has not been closed, and is
    // also the read-failure degradation outcome: answer false, never throw.
    const registry = new FileClosedSessionRegistry(paths);
    const result = await registry.isClosed("session-no-marker");
    expect(result).toBe(false);
  });

  it("markClosed returns false when the write directory cannot be created, and never throws", async () => {
    // Block directory creation by placing a file where the .session-ends dir should be.
    nodeFs.mkdirSync(paths.root, { recursive: true });
    nodeFs.writeFileSync(nodePath.join(paths.root, ".session-ends"), "not-a-dir");

    const registry = new FileClosedSessionRegistry(paths);
    let result: boolean | undefined;
    await expect(async () => {
      result = await registry.markClosed(SESSION_ALPHA);
    }).not.toThrow();

    // Write failed, so result must be false
    expect(result).toBe(false);
  });

  it("isClosed degrades to false when the marker file content is invalid JSON", async () => {
    // Write a corrupt marker file
    const sessionEndsDir = nodePath.join(paths.root, ".session-ends");
    nodeFs.mkdirSync(sessionEndsDir, { recursive: true });
    nodeFs.writeFileSync(
      nodePath.join(sessionEndsDir, sanitizeComponent(SESSION_ALPHA) + ".json"),
      "not valid json {{{{",
    );

    const registry = new FileClosedSessionRegistry(paths);
    let result: boolean | undefined;
    await expect(async () => {
      result = await registry.isClosed(SESSION_ALPHA);
    }).not.toThrow();

    // Corrupt file = read failure = degrade to false
    expect(result).toBe(false);
  });

  it("a degraded registry (write fails) still lets the close path proceed rather than blocking it", async () => {
    // Block writes. The test verifies the behavior described in the close-path
    // contract: when markClosed returns false, the close is not blocked —
    // only the durability guarantee is lost, not the close itself.
    nodeFs.mkdirSync(paths.root, { recursive: true });
    nodeFs.writeFileSync(nodePath.join(paths.root, ".session-ends"), "not-a-dir");

    const registry = new FileClosedSessionRegistry(paths);
    // The registry must never throw — the close path swallows its own failures
    await expect(registry.markClosed(SESSION_ALPHA)).resolves.toBeDefined();
    // isClosed still returns false (the marker was not written)
    expect(await registry.isClosed(SESSION_ALPHA)).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// FileClosedSessionRegistry — concurrent closure attempts
// ---------------------------------------------------------------------------

describe("FileClosedSessionRegistry — concurrent closure attempts", () => {
  it("concurrent markClosed calls on the same session all resolve without error", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    const results = await Promise.all([
      registry.markClosed(SESSION_ALPHA),
      registry.markClosed(SESSION_ALPHA),
      registry.markClosed(SESSION_ALPHA),
    ]);
    // None may throw; all must resolve to a boolean
    expect(results).toHaveLength(3);
    expect(results.every((r: unknown) => typeof r === "boolean")).toBe(true);
  });

  it("after concurrent markClosed calls, isClosed returns true", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    await Promise.all([
      registry.markClosed(SESSION_ALPHA),
      registry.markClosed(SESSION_ALPHA),
      registry.markClosed(SESSION_ALPHA),
    ]);
    expect(await registry.isClosed(SESSION_ALPHA)).toBe(true);
  });

  it("a second registry instance recognizes the session as closed after concurrent first-instance marks", async () => {
    const registry1 = new FileClosedSessionRegistry(paths);
    await Promise.all([
      registry1.markClosed(SESSION_ALPHA),
      registry1.markClosed(SESSION_ALPHA),
    ]);

    // Fresh instance — no shared in-memory state
    const registry2 = new FileClosedSessionRegistry(paths);
    expect(await registry2.isClosed(SESSION_ALPHA)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Artifact placement — dot-prefixed directory, never inside a run folder
// ---------------------------------------------------------------------------

describe("ClosedSessionRegistry artifact placement", () => {
  it("the marker file resides inside a dot-prefixed directory under the log root", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    await registry.markClosed(SESSION_ALPHA);

    const sessionEndsDir = nodePath.join(paths.root, ".session-ends");
    const markerPath = nodePath.join(
      sessionEndsDir,
      sanitizeComponent(SESSION_ALPHA) + ".json",
    );

    expect(nodeFs.existsSync(markerPath)).toBe(true);
    // The directory name must start with "." so consumers skipping dot dirs miss it
    expect(nodePath.basename(sessionEndsDir).startsWith(".")).toBe(true);
  });

  it("the .session-ends directory is a direct child of the log root, not nested under any run folder", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    await registry.markClosed(SESSION_ALPHA);

    // Find the actual directory that was created by markClosed
    const sessionEndsDir = nodePath.join(paths.root, ".session-ends");
    expect(nodeFs.existsSync(sessionEndsDir)).toBe(true);

    // Its parent must be the root — the registry must not have created it inside a run folder
    expect(nodePath.dirname(sessionEndsDir)).toBe(paths.root);

    // It must NOT be inside any run folder (e.g. {root}/{runId}/.session-ends)
    const someRunDir = paths.runRoot(RUN_ID);
    expect(sessionEndsDir.startsWith(someRunDir)).toBe(false);
    expect(sessionEndsDir.startsWith(someRunDir + nodePath.sep)).toBe(false);
  });

  it("the .session-ends directory is not reachable by walking runRoot for any run id", async () => {
    const registry = new FileClosedSessionRegistry(paths);
    await registry.markClosed(SESSION_ALPHA);

    // Find the actual directory that was created by markClosed
    const sessionEndsDir = nodePath.join(paths.root, ".session-ends");
    expect(nodeFs.existsSync(sessionEndsDir)).toBe(true);

    // A run root is {root}/{runId}. The marker dir must not be inside any run root.
    const runRootA = paths.runRoot(RUN_ID);
    const runRootB = paths.runRoot("unknown-run");
    const runRootC = paths.runRoot("20260101T120000Z-bbbb");

    for (const runRoot of [runRootA, runRootB, runRootC]) {
      expect(sessionEndsDir === runRoot).toBe(false);
      expect(sessionEndsDir.startsWith(runRoot + nodePath.sep)).toBe(false);
    }
  });

  it("a consumer walking the log root's immediate run folders does not encounter the marker directory as run data", async () => {
    // After writing a marker, the .session-ends dir exists at paths.root.
    // Enumerate immediate children of paths.root and verify any dot-prefixed
    // entry is treated as non-run by standard log-consumer logic (skip "." dirs).
    const registry = new FileClosedSessionRegistry(paths);
    await registry.markClosed(SESSION_ALPHA);

    nodeFs.mkdirSync(paths.runRoot(RUN_ID), { recursive: true });

    const entries = await nodeFsPromises.readdir(paths.root, { withFileTypes: true });
    const dotEntries = entries.filter((e) => e.name.startsWith("."));
    const nonDotEntries = entries.filter((e) => !e.name.startsWith("."));

    // The .session-ends dir is a dot entry and would be skipped by any consumer
    // that filters hidden/dot directories
    expect(dotEntries.some((e) => e.name === ".session-ends")).toBe(true);
    // Non-dot entries are run folders only
    expect(nonDotEntries.every((e) => !e.name.startsWith("."))).toBe(true);
  });

  it("the marker path segment for a session id with reserved characters is sanitized", async () => {
    const unsafeId = "session<with>reserved:chars";
    const registry = new FileClosedSessionRegistry(paths);
    await registry.markClosed(unsafeId);

    // The registry must have written a marker — isClosed confirms the write succeeded
    expect(await registry.isClosed(unsafeId)).toBe(true);

    // Find the actual file created under .session-ends and verify its name has no reserved chars
    const sessionEndsDir = nodePath.join(paths.root, ".session-ends");
    expect(nodeFs.existsSync(sessionEndsDir)).toBe(true);
    const files = nodeFs.readdirSync(sessionEndsDir);
    expect(files).toHaveLength(1);

    const markerFileName = files[0];
    const reservedChars = ['<', '>', ':', '"', '/', '\\', '|', '?', '*'];
    for (const ch of reservedChars) {
      expect(markerFileName.includes(ch)).toBe(false);
    }

    // Marker path is deterministic — same id written again produces the same filename
    await registry.markClosed(unsafeId);
    const filesAfterSecondWrite = nodeFs.readdirSync(sessionEndsDir);
    expect(filesAfterSecondWrite).toHaveLength(1);
    expect(filesAfterSecondWrite[0]).toBe(markerFileName);
  });
});

// ---------------------------------------------------------------------------
// Session handler close-path integration — first-close emits exactly one pair
// ---------------------------------------------------------------------------

describe("Session handler close-path — first-close emits exactly one session_end + run_end", () => {
  it("a session never closed before emits exactly one session_end", async () => {
    const store = makeStore();
    store.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const collected: CollectedEvent[] = [];
    const registry = new MemoryClosedSessionRegistry();
    const deps = makeDepsWithRegistry(store, paths, makeNullSdk(), collected, registry);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(sessionIdleEvent(ORCH_SESSION));

    const sessionEnds = collected.filter((e) => e.event.event === "session_end");
    expect(sessionEnds).toHaveLength(1);
  });

  it("a session never closed before emits exactly one run_end", async () => {
    const store = makeStore();
    store.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const collected: CollectedEvent[] = [];
    const registry = new MemoryClosedSessionRegistry();
    const deps = makeDepsWithRegistry(store, paths, makeNullSdk(), collected, registry);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(sessionIdleEvent(ORCH_SESSION));

    const runEnds = collected.filter((e) => e.event.event === "run_end");
    expect(runEnds).toHaveLength(1);
  });

  it("session_end is emitted before run_end", async () => {
    const store = makeStore();
    store.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const collected: CollectedEvent[] = [];
    const registry = new MemoryClosedSessionRegistry();
    const deps = makeDepsWithRegistry(store, paths, makeNullSdk(), collected, registry);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(sessionIdleEvent(ORCH_SESSION));

    const eventNames = collected.map((e) => e.event.event);
    const sessionEndIdx = eventNames.indexOf("session_end");
    const runEndIdx = eventNames.indexOf("run_end");
    expect(sessionEndIdx).toBeGreaterThanOrEqual(0);
    expect(runEndIdx).toBeGreaterThanOrEqual(0);
    expect(sessionEndIdx).toBeLessThan(runEndIdx);
  });

  it("the close pair is emitted even when closedSessions.isClosed returns false initially", async () => {
    // This verifies the close path does not accidentally short-circuit on a fresh registry.
    const store = makeStore();
    store.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const collected: CollectedEvent[] = [];
    const freshRegistry = new MemoryClosedSessionRegistry();
    // freshRegistry.isClosed(ORCH_SESSION) returns false — first-ever close
    expect(await freshRegistry.isClosed(ORCH_SESSION)).toBe(false);

    const deps = makeDepsWithRegistry(store, paths, makeNullSdk(), collected, freshRegistry);
    const { handleEvent } = createSessionHandlers(deps);

    await handleEvent(sessionIdleEvent(ORCH_SESSION));

    expect(collected.some((e) => e.event.event === "session_end")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_end")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Session handler close-path integration — durable guard prevents second pair
// ---------------------------------------------------------------------------

describe("Session handler close-path — durable guard prevents second close pair", () => {
  it("a second session.idle for an already-closed session emits no additional close pair", async () => {
    const store = makeStore();
    store.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const collected: CollectedEvent[] = [];
    const registry = new MemoryClosedSessionRegistry();
    const deps = makeDepsWithRegistry(store, paths, makeNullSdk(), collected, registry);
    const { handleEvent } = createSessionHandlers(deps);

    // First close
    await handleEvent(sessionIdleEvent(ORCH_SESSION));
    const countAfterFirst = collected.length;

    // Second close attempt on the same session
    await handleEvent(sessionIdleEvent(ORCH_SESSION));
    const countAfterSecond = collected.length;

    // No additional events should have been emitted for the second attempt
    expect(countAfterSecond).toBe(countAfterFirst);
    // Exactly one session_end pair total
    expect(collected.filter((e) => e.event.event === "session_end")).toHaveLength(1);
    expect(collected.filter((e) => e.event.event === "run_end")).toHaveLength(1);
  });

  it("session.deleted for an already-closed session emits no additional close pair", async () => {
    const store = makeStore();
    store.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const collected: CollectedEvent[] = [];
    const registry = new MemoryClosedSessionRegistry();
    const deps = makeDepsWithRegistry(store, paths, makeNullSdk(), collected, registry);
    const { handleEvent } = createSessionHandlers(deps);

    // Close via idle
    await handleEvent(sessionIdleEvent(ORCH_SESSION));
    const eventsAfterIdle = collected.length;

    // Second close attempt via deleted — must be suppressed
    await handleEvent(sessionDeletedEvent(ORCH_SESSION));
    expect(collected.length).toBe(eventsAfterIdle);
    expect(collected.filter((e) => e.event.event === "session_end")).toHaveLength(1);
  });

  it("concurrent session.idle events for the same session yield at most one session_end and one run_end", async () => {
    // This tests a true race at the handler level: two handleEvent calls that are
    // both in-flight simultaneously — not two sequential awaited calls. This is the
    // scenario where both callers could pass the isClosed guard before either
    // finishes markClosed, causing a double-emit if the implementation does not
    // serialize per-session.
    const store = makeStore();
    store.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const collected: CollectedEvent[] = [];
    const registry = new MemoryClosedSessionRegistry();
    const deps = makeDepsWithRegistry(store, paths, makeNullSdk(), collected, registry);
    const { handleEvent } = createSessionHandlers(deps);

    // Both events in-flight simultaneously — neither awaits the other
    await Promise.all([
      handleEvent(sessionIdleEvent(ORCH_SESSION)),
      handleEvent(sessionIdleEvent(ORCH_SESSION)),
    ]);

    // Exactly one close pair must result regardless of which call won the race
    expect(collected.filter((e) => e.event.event === "session_end")).toHaveLength(1);
    expect(collected.filter((e) => e.event.event === "run_end")).toHaveLength(1);
  });

  it("restart simulation: a session closed before a process restart does not emit a second close pair", async () => {
    // Process 1: close the session via the durable (file-backed) registry
    const registry1 = new FileClosedSessionRegistry(paths);
    await registry1.markClosed(ORCH_SESSION);

    // Process 2: fresh store (no in-memory ended state), same log root
    const store2 = makeStore();
    store2.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      // ended is false — the fresh store has no memory of the previous close
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const collected: CollectedEvent[] = [];
    // Same root → fresh registry reads the marker written by process 1
    const registry2 = new FileClosedSessionRegistry(paths);
    const deps2 = makeDepsWithRegistry(store2, paths, makeNullSdk(), collected, registry2);
    const { handleEvent } = createSessionHandlers(deps2);

    // session.idle arrives after the restart — the durable guard must catch it
    await handleEvent(sessionIdleEvent(ORCH_SESSION));

    // No close pair should be emitted in process 2
    expect(collected.filter((e) => e.event.event === "session_end")).toHaveLength(0);
    expect(collected.filter((e) => e.event.event === "run_end")).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// Session handler close-path — degradation (unavailable registry still emits)
// ---------------------------------------------------------------------------

describe("Session handler close-path — degradation when registry is unavailable", () => {
  it("closure proceeds and emits even when the durable registry cannot persist the marker", async () => {
    // Block the .session-ends directory so writes fail
    nodeFs.mkdirSync(paths.root, { recursive: true });
    nodeFs.writeFileSync(nodePath.join(paths.root, ".session-ends"), "not-a-dir");

    const store = makeStore();
    store.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const collected: CollectedEvent[] = [];
    const registry = new FileClosedSessionRegistry(paths);
    const deps = makeDepsWithRegistry(store, paths, makeNullSdk(), collected, registry);
    const { handleEvent } = createSessionHandlers(deps);

    // Must not throw — degradation rule: swallow errors
    await expect(handleEvent(sessionIdleEvent(ORCH_SESSION))).resolves.toBeUndefined();

    // The close pair must still be emitted (closure must not be blocked)
    expect(collected.some((e) => e.event.event === "session_end")).toBe(true);
    expect(collected.some((e) => e.event.event === "run_end")).toBe(true);
  });

  it("degraded registry falls back to in-memory guard: a second idle in the same process is suppressed", async () => {
    // Block the .session-ends directory so writes fail
    nodeFs.mkdirSync(paths.root, { recursive: true });
    nodeFs.writeFileSync(nodePath.join(paths.root, ".session-ends"), "not-a-dir");

    const store = makeStore();
    store.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const collected: CollectedEvent[] = [];
    const registry = new FileClosedSessionRegistry(paths);
    const deps = makeDepsWithRegistry(store, paths, makeNullSdk(), collected, registry);
    const { handleEvent } = createSessionHandlers(deps);

    // First close — emits despite degraded registry
    await handleEvent(sessionIdleEvent(ORCH_SESSION));
    const firstCloseCount = collected.length;

    // Second idle — in-memory markEnded on the store should catch it
    await handleEvent(sessionIdleEvent(ORCH_SESSION));
    expect(collected.length).toBe(firstCloseCount);
    expect(collected.filter((e) => e.event.event === "session_end")).toHaveLength(1);
  });

  it("the session handler never throws when both the registry write and read fail", async () => {
    nodeFs.mkdirSync(paths.root, { recursive: true });
    nodeFs.writeFileSync(nodePath.join(paths.root, ".session-ends"), "not-a-dir");

    const store = makeStore();
    store.register(ORCH_SESSION, {
      sessionId: ORCH_SESSION,
      runId: RUN_ID,
      ended: false,
      createdAt: new Date().toISOString(),
    });
    const registry = new FileClosedSessionRegistry(paths);
    const deps = makeDepsWithRegistry(store, paths, makeNullSdk(), [], registry);
    const { handleEvent } = createSessionHandlers(deps);

    await expect(handleEvent(sessionIdleEvent(ORCH_SESSION))).resolves.toBeUndefined();
  });
});
