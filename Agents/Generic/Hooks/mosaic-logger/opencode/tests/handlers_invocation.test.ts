/**
 * handlers_invocation.test.ts — Tests for invocation (subagent) lifecycle handlers.
 *
 * Covers:
 *   - handleInvocationStart: registers agent_instance_id and run_id from dispatch prompt,
 *     emits invocation_start to the correct invocation events file
 *   - handleInvocationStart: writes 01_input.md with metadata
 *   - handleInvocationStart: propagates run_id to orchestrator session record
 *   - handleInvocationStart: gracefully handles SDK messages() failure
 *   - handleInvocationEnd: emits invocation_end with status_code from last message
 *   - handleInvocationEnd: writes 02_output.md
 *   - handleInvocationEnd: exports subagent session transcript
 *   - handleInvocationEnd: skips unmapped sessions (agentInstanceId starts with "unmapped_")
 *   - handleInvocationEnd: skips sessions already marked as ended (idempotent)
 *   - handleInvocationEnd: graceful degradation when SDK throws
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import { createInvocationHandlers } from "../lib/handlers_invocation";
import {
  type HandlerDependencies,
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
const AGENT_INSTANCE_ID = "Research#1";
const DISPATCH_PROMPT = `{
  "agent_instance_id": "Research#1",
  "run_id": "20260101T170000Z-a3f9",
  "task_description": "Do some research"
}`;
const FINAL_RESPONSE = `Here are the findings.

{"status_code": "SUCCESS", "status_message": "Done."}`;

let tmpDir: string;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "mosaic-hi-test-"));
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

/**
 * SDK client that returns a controlled messages response for specific sessions.
 * `getMessagesForSession` maps session IDs to their message arrays.
 */
function makeSdk(getMessagesForSession: (sessionId: string) => unknown): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async ({ path }) => getMessagesForSession(path.id),
    },
    app: { log: async () => {} },
  };
}

function makeThrowingSdk(): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async () => {
        throw new Error("SDK failure");
      },
    },
    app: { log: async () => {} },
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

/** Build a messages array with one user message containing the given text. */
function userMessages(text: string) {
  return [{ role: "user", content: [{ type: "text", text }] }];
}

/** Build a messages array with user + assistant messages. */
function conversationMessages(userText: string, assistantText: string) {
  return [
    { role: "user", content: [{ type: "text", text: userText }] },
    { role: "assistant", content: [{ type: "text", text: assistantText }] },
  ];
}

// ---------------------------------------------------------------------------
// handleInvocationStart
// ---------------------------------------------------------------------------

describe("handleInvocationStart — agent_instance_id and run_id extraction", () => {
  it("extracts agent_instance_id from the dispatch prompt and stores it", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => userMessages(DISPATCH_PROMPT));
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    const record = store.get(SUB_SESSION);
    expect(record?.agentInstanceId).toBe(AGENT_INSTANCE_ID);
  });

  it("extracts run_id from the dispatch prompt and stores it on the subagent record", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => userMessages(DISPATCH_PROMPT));
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    const record = store.get(SUB_SESSION);
    expect(record?.runId).toBe(RUN_ID);
  });

  it("propagates run_id to the orchestrator session record", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => userMessages(DISPATCH_PROMPT));
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    // Orchestrator session should now carry the run_id
    const orchRecord = store.get(ORCH_SESSION);
    expect(orchRecord?.runId).toBe(RUN_ID);
  });

  it("returns early without emitting events when dispatch prompt has no agent_instance_id", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => userMessages("No identity here at all."));
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    // When extraction fails, handleInvocationStart must return early without
    // emitting invocation_start (symmetric with handleInvocationEnd which also
    // skips unmapped sessions — no orphaned event pairs).
    const record = store.get(SUB_SESSION);
    expect(record?.agentInstanceId).toBeUndefined();
    expect(collected).toHaveLength(0);
  });
});

describe("handleInvocationStart — invocation_start event emission", () => {
  it("emits invocation_start event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => userMessages(DISPATCH_PROMPT));
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    const invStart = collected.find((e) => e.event.event === "invocation_start");
    expect(invStart).toBeDefined();
    expect(invStart!.event.agent_instance_id).toBe(AGENT_INSTANCE_ID);
  });

  it("routes invocation_start to the invocation events file (not orchestrator)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => userMessages(DISPATCH_PROMPT));
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    const invStart = collected.find((e) => e.event.event === "invocation_start");
    expect(invStart!.filePath).toBe(paths.invocationEvents(RUN_ID, AGENT_INSTANCE_ID));
    // Must NOT go to orchestrator events file
    expect(invStart!.filePath).not.toBe(paths.orchestratorEvents(RUN_ID));
  });

  it("writes 01_input.md to the invocation directory", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => userMessages(DISPATCH_PROMPT));
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    const inputPath = paths.invocationInput(RUN_ID, AGENT_INSTANCE_ID);
    expect(nodeFs.existsSync(inputPath)).toBe(true);

    const content = nodeFs.readFileSync(inputPath, "utf-8");
    expect(content).toContain(AGENT_INSTANCE_ID);
  });

  it("does not throw when SDK messages() fails", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeThrowingSdk(), []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await expect(
      handleInvocationStart(SUB_SESSION, ORCH_SESSION),
    ).resolves.toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// handleInvocationEnd
// ---------------------------------------------------------------------------

describe("handleInvocationEnd — invocation_end emission", () => {
  it("emits invocation_end with status_code extracted from last assistant message", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk((id) =>
      id === SUB_SESSION
        ? conversationMessages(DISPATCH_PROMPT, FINAL_RESPONSE)
        : [],
    );
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
      agentType: "Research",
    });

    await handleInvocationEnd(SUB_SESSION);

    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invEnd).toBeDefined();
    expect(invEnd!.event.agent_instance_id).toBe(AGENT_INSTANCE_ID);
    expect(invEnd!.event.status_code).toBe("SUCCESS");
  });

  it("routes invocation_end to the invocation events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => conversationMessages(DISPATCH_PROMPT, FINAL_RESPONSE));
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handleInvocationEnd(SUB_SESSION);

    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invEnd!.filePath).toBe(paths.invocationEvents(RUN_ID, AGENT_INSTANCE_ID));
  });

  it("writes 02_output.md to the invocation directory", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => conversationMessages(DISPATCH_PROMPT, FINAL_RESPONSE));
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handleInvocationEnd(SUB_SESSION);

    const outputPath = paths.invocationOutput(RUN_ID, AGENT_INSTANCE_ID);
    expect(nodeFs.existsSync(outputPath)).toBe(true);

    const content = nodeFs.readFileSync(outputPath, "utf-8");
    expect(content).toContain(AGENT_INSTANCE_ID);
    expect(content).toContain("SUCCESS");
  });

  it("marks the session as ended to prevent duplicate invocation_end events", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => conversationMessages(DISPATCH_PROMPT, FINAL_RESPONSE));
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handleInvocationEnd(SUB_SESSION);
    await handleInvocationEnd(SUB_SESSION);

    const endEvents = collected.filter((e) => e.event.event === "invocation_end");
    expect(endEvents).toHaveLength(1);
  });

  it("skips unmapped sessions (agentInstanceId starts with unmapped_)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: `unmapped_${SUB_SESSION}`,
    });

    await handleInvocationEnd(SUB_SESSION);

    expect(collected).toHaveLength(0);
  });

  it("skips sessions not present in the store", async () => {
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    await handleInvocationEnd("completely-unknown-session");

    expect(collected).toHaveLength(0);
  });

  it("does not throw when SDK messages() fails during invocation end", async () => {
    const store = makeStore();
    const paths = makePaths();
    const deps = makeDeps(store, paths, makeThrowingSdk(), []);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await expect(handleInvocationEnd(SUB_SESSION)).resolves.toBeUndefined();
  });

  it("still emits invocation_end even when status_code cannot be extracted", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() =>
      conversationMessages(DISPATCH_PROMPT, "Some response without a status code."),
    );
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handleInvocationEnd(SUB_SESSION);

    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invEnd).toBeDefined();
    // status_code should be absent (pruned out)
    expect(invEnd!.event.status_code).toBeUndefined();
  });

  it("includes response text in the invocation_end event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk((id) =>
      id === SUB_SESSION
        ? conversationMessages(DISPATCH_PROMPT, FINAL_RESPONSE)
        : [],
    );
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handleInvocationEnd(SUB_SESSION);

    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invEnd).toBeDefined();
    // response field carries the last assistant message text (per AC4.3)
    expect(typeof invEnd!.event.response).toBe("string");
    expect(invEnd!.event.response as string).toContain("Here are the findings");
  });

  it("exports the subagent session transcript (04_session.raw) after invocation end", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk((id) =>
      id === SUB_SESSION
        ? conversationMessages(DISPATCH_PROMPT, FINAL_RESPONSE)
        : [],
    );
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handleInvocationEnd(SUB_SESSION);

    // 04_session.raw must exist after a completed invocation (AC4.7)
    const rawPath = paths.invocationRaw(RUN_ID, AGENT_INSTANCE_ID);
    expect(nodeFs.existsSync(rawPath)).toBe(true);
  });

  it("refreshes the orchestrator transcript (00_orchestrator_session.raw) after invocation end", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => conversationMessages(DISPATCH_PROMPT, FINAL_RESPONSE));
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      agentInstanceId: AGENT_INSTANCE_ID,
    });

    await handleInvocationEnd(SUB_SESSION);

    // The orchestrator transcript is refreshed on every invocation end (AC4.7)
    const orchRawPath = paths.orchestratorRaw(RUN_ID);
    expect(nodeFs.existsSync(orchRawPath)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// handleInvocationStart — orchestrator transcript refresh
// ---------------------------------------------------------------------------

describe("handleInvocationStart — orchestrator transcript refresh", () => {
  it("refreshes the orchestrator transcript (00_orchestrator_session.raw) after invocation start", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => userMessages(DISPATCH_PROMPT));
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    // Orchestrator transcript is refreshed on invocation start so the log
    // captures the dispatch message that was sent to the subagent
    const orchRawPath = paths.orchestratorRaw(RUN_ID);
    expect(nodeFs.existsSync(orchRawPath)).toBe(true);
  });
});
