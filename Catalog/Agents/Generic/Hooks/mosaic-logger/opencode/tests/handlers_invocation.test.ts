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
import { userMessageEntry, assistantMessageEntry } from "./fixtures/opencode_api";
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

/**
 * Build a messages array with one user message in the REAL OpenCode shape.
 * Role lives at entry.info.role; text lives in a text-typed part.
 * There is NO `content` field. Tests using this will fail (RED) until the
 * implementation (I1.2) reads from the real shape instead of the old one.
 */
function userMessages(text: string) {
  return [userMessageEntry(text)];
}

/**
 * Build a conversation messages array (user + assistant) in the REAL OpenCode shape.
 * Both messages use `{ info, parts }` format. Tests that depend on text extraction
 * from this shape will fail (RED) until I1.2 corrects the extraction logic.
 */
function conversationMessages(userText: string, assistantText: string) {
  return [userMessageEntry(userText), assistantMessageEntry(assistantText)];
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

  // NOTE: The test that asserted the OLD suppression behavior
  // ("returns early without emitting events when dispatch prompt has no agent_instance_id")
  // was removed as part of Stage 3 implementation. The new behavior (per MosaicLogFormat.md
  // §3.2 and §4.2) is to emit invocation_start unconditionally, routing to the degradation
  // bucket when identity is unresolved. The new behavior is covered by the Stage 3 describe
  // blocks below ("invocation_start emitted even when identity is unresolvable").

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

// ===========================================================================
// Stage 3: Identity Resolution from Task Dispatch
//
// All describe blocks below are RED until the Stage 3 implementation tasks
// are complete. They will produce TypeScript compilation errors because:
//   - SessionCorrelationStore lacks pushPendingDispatch / claimPendingDispatch /
//     adoptRunId (new methods added by Stage 3 implementation)
//   - handleInvocationStart currently has an early-return guard that suppresses
//     invocation_start when identity is unresolved (removed in Stage 3)
//   - SessionRecord lacks the invocationStarted field (added in Stage 3)
//
// Note: one existing test above ("returns early without emitting events when
// dispatch prompt has no agent_instance_id") asserts the OLD suppression
// behavior. That test will become incorrect after Stage 3 removes the guard
// and must be updated by the implementation agent as part of Stage 3 work.
// ===========================================================================

// ---------------------------------------------------------------------------
// handleInvocationStart — identity from pending dispatch (primary source)
// ---------------------------------------------------------------------------

describe("handleInvocationStart — identity from pending dispatch (primary source)", () => {
  it("claims the pending dispatch from the parent session and uses its agentInstanceId", async () => {
    const store = makeStore();
    const paths = makePaths();
    // SDK returns empty — identity must come from the pending dispatch, not messages
    const sdk = makeNullSdk();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    store.pushPendingDispatch(ORCH_SESSION, {
      callId: "call-dispatch-01",
      parentSessionId: ORCH_SESSION,
      agentInstanceId: "Research#4",
      agentType: "Research",
      runId: RUN_ID,
      prompt: DISPATCH_PROMPT,
      dispatchedAt: new Date().toISOString(),
    });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    expect(store.get(SUB_SESSION)?.agentInstanceId).toBe("Research#4");
  });

  it("uses runId from the pending dispatch without falling back to session.messages()", async () => {
    const store = makeStore();
    const paths = makePaths();
    let messagesCalled = false;
    const sdk: SdkClient = {
      session: {
        get: async () => undefined,
        messages: async () => {
          messagesCalled = true;
          return [];
        },
      },
      app: { log: async () => {} },
    };
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    store.pushPendingDispatch(ORCH_SESSION, {
      callId: "call-dispatch-01",
      parentSessionId: ORCH_SESSION,
      agentInstanceId: "Planner#2",
      runId: RUN_ID,
      dispatchedAt: new Date().toISOString(),
    });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    expect(store.get(SUB_SESSION)?.runId).toBe(RUN_ID);
    expect(messagesCalled).toBe(false);
  });

  it("uses agentType from the pending dispatch", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeNullSdk();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    store.pushPendingDispatch(ORCH_SESSION, {
      callId: "call-dispatch-01",
      parentSessionId: ORCH_SESSION,
      agentInstanceId: "codebase-research#1",
      agentType: "codebase-research",
      runId: RUN_ID,
      dispatchedAt: new Date().toISOString(),
    });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    expect(store.get(SUB_SESSION)?.agentType).toBe("codebase-research");
  });

  it("does not call session.messages() when a pending dispatch is available", async () => {
    const store = makeStore();
    const paths = makePaths();
    let messagesCalled = false;
    const sdk: SdkClient = {
      session: {
        get: async () => undefined,
        messages: async () => {
          messagesCalled = true;
          return [];
        },
      },
      app: { log: async () => {} },
    };
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    store.pushPendingDispatch(ORCH_SESSION, {
      callId: "call-dispatch-01",
      parentSessionId: ORCH_SESSION,
      agentInstanceId: "TestWriter#3",
      runId: RUN_ID,
      dispatchedAt: new Date().toISOString(),
    });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    expect(messagesCalled).toBe(false);
  });

  it("emits invocation_start with dispatch identity routed to the correct invocation events file", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeNullSdk();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    store.pushPendingDispatch(ORCH_SESSION, {
      callId: "call-dispatch-01",
      parentSessionId: ORCH_SESSION,
      agentInstanceId: "Research#1",
      runId: RUN_ID,
      dispatchedAt: new Date().toISOString(),
    });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    const invStart = collected.find((e) => e.event.event === "invocation_start");
    expect(invStart).toBeDefined();
    expect(invStart!.event.agent_instance_id).toBe("Research#1");
    expect(invStart!.filePath).toBe(paths.invocationEvents(RUN_ID, "Research#1"));
  });

  it("consumes the pending dispatch so the next subagent session claims the next dispatch in the queue", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeNullSdk();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});

    store.pushPendingDispatch(ORCH_SESSION, {
      callId: "call-first",
      parentSessionId: ORCH_SESSION,
      agentInstanceId: "Writer#1",
      runId: RUN_ID,
      dispatchedAt: new Date().toISOString(),
    });
    store.pushPendingDispatch(ORCH_SESSION, {
      callId: "call-second",
      parentSessionId: ORCH_SESSION,
      agentInstanceId: "Reviewer#2",
      runId: RUN_ID,
      dispatchedAt: new Date().toISOString(),
    });

    const SUB_A = "sub-session-a";
    const SUB_B = "sub-session-b";
    store.register(SUB_A, { parentId: ORCH_SESSION });
    store.register(SUB_B, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_A, ORCH_SESSION);
    await handleInvocationStart(SUB_B, ORCH_SESSION);

    expect(store.get(SUB_A)?.agentInstanceId).toBe("Writer#1");
    expect(store.get(SUB_B)?.agentInstanceId).toBe("Reviewer#2");
  });
});

// ---------------------------------------------------------------------------
// handleInvocationStart — run-id propagation to orchestrator via adoptRunId
// ---------------------------------------------------------------------------

describe("handleInvocationStart — run-id propagated to orchestrator session", () => {
  it("propagates run id from a resolved dispatch to the orchestrator session", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeNullSdk();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {}); // no run id initially
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    store.pushPendingDispatch(ORCH_SESSION, {
      callId: "call-01",
      parentSessionId: ORCH_SESSION,
      agentInstanceId: "Research#1",
      runId: RUN_ID,
      dispatchedAt: new Date().toISOString(),
    });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    // Orchestrator session now carries the run id learned from the dispatch
    expect(store.get(ORCH_SESSION)?.runId).toBe(RUN_ID);
  });

  it("does not overwrite an existing orchestrator run id when dispatch carries a different one", async () => {
    const existingRun = "20260101T000000Z-orig";
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeNullSdk();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: existingRun });
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    store.pushPendingDispatch(ORCH_SESSION, {
      callId: "call-01",
      parentSessionId: ORCH_SESSION,
      agentInstanceId: "Research#1",
      runId: RUN_ID, // different from existingRun
      dispatchedAt: new Date().toISOString(),
    });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    // Original run id on the orchestrator session must be preserved
    expect(store.get(ORCH_SESSION)?.runId).toBe(existingRun);
  });

  it("after run-id propagation all sessions in the chain resolve the same run", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeNullSdk();
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    store.pushPendingDispatch(ORCH_SESSION, {
      callId: "call-01",
      parentSessionId: ORCH_SESSION,
      agentInstanceId: "Research#1",
      runId: RUN_ID,
      dispatchedAt: new Date().toISOString(),
    });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    // Both sessions resolve the same run id after propagation
    expect(store.getRunId(ORCH_SESSION)).toBe(RUN_ID);
    expect(store.getRunId(SUB_SESSION)).toBe(RUN_ID);
  });
});

// ---------------------------------------------------------------------------
// handleInvocationStart — degraded emission when identity is unresolvable
//
// The current implementation returns early and suppresses invocation_start
// when identity cannot be extracted (unmapped guard). The correct behavior
// per MosaicLogFormat.md §3.2 is to emit invocation_start unconditionally,
// routing to the degradation bucket when identity is unknown.
// These tests are RED because the early-return guard is still present.
// ---------------------------------------------------------------------------

describe("handleInvocationStart — invocation_start emitted even when identity is unresolvable", () => {
  it("emits invocation_start when there is no pending dispatch and session.messages() yields no identity", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => []); // no messages → no identity
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    // No pending dispatch pushed for ORCH_SESSION

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    const invStart = collected.find((e) => e.event.event === "invocation_start");
    expect(invStart).toBeDefined();
  });

  it("sets some agentInstanceId on the session record using fallback naming when identity is unknown", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => []);
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    const record = store.get(SUB_SESSION);
    expect(record?.agentInstanceId).toBeDefined();
    expect(typeof record?.agentInstanceId).toBe("string");
    expect(record!.agentInstanceId!.length).toBeGreaterThan(0);
  });

  it("routes the degraded invocation_start to the degradation bucket (unknown-run path)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => []);
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {}); // no run id → event goes to unknown-run
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    const invStart = collected.find((e) => e.event.event === "invocation_start");
    expect(invStart).toBeDefined();
    expect(invStart!.filePath).toContain("unknown-run");
  });

  it("sets invocationStarted on the session record so invocation_end can stay symmetric", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => []);
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    // invocationStarted must be truthy so handleInvocationEnd knows to emit its pair
    const record = store.get(SUB_SESSION) as unknown as Record<string, unknown>;
    expect(record?.["invocationStarted"]).toBe(true);
  });

  it("does not throw when identity is completely unresolvable", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => []);
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await expect(
      handleInvocationStart(SUB_SESSION, ORCH_SESSION),
    ).resolves.toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// handleInvocationEnd — symmetric emission for degraded sessions
//
// These tests are RED because handleInvocationEnd currently has a guard that
// skips emission when agentInstanceId is missing or starts with "unmapped_".
// The correct behavior is to emit invocation_end for any session that received
// an invocation_start (tracked by the invocationStarted flag on SessionRecord).
// ---------------------------------------------------------------------------

describe("handleInvocationEnd — invocation_end emitted to match a degraded invocation_start", () => {
  it("emits invocation_end after a degraded invocation_start for an unresolved session", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => []);
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationStart, handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });
    // No pending dispatch — identity cannot be resolved

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);
    // invocationStarted is now set; invocation_end must follow
    await handleInvocationEnd(SUB_SESSION);

    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invEnd).toBeDefined();
  });

  it("does not emit invocation_end for a session where invocation_start was never emitted", async () => {
    // A session that never went through handleInvocationStart must not produce
    // an orphaned invocation_end — the pair symmetry is maintained both ways.
    const store = makeStore();
    const paths = makePaths();
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, makeNullSdk(), collected);
    const { handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, {
      parentId: ORCH_SESSION,
      // No agentInstanceId, no invocationStarted flag
    });

    await handleInvocationEnd(SUB_SESSION);

    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invEnd).toBeUndefined();
  });

  it("invocation_start and invocation_end both route to the same event file (symmetric pair)", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => []);
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationStart, handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);
    await handleInvocationEnd(SUB_SESSION);

    const invStart = collected.find((e) => e.event.event === "invocation_start");
    const invEnd = collected.find((e) => e.event.event === "invocation_end");
    expect(invStart).toBeDefined();
    expect(invEnd).toBeDefined();
    expect(invStart!.filePath).toBe(invEnd!.filePath);
  });

  it("does not emit a second invocation_end when called twice for the same degraded session", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => []);
    const collected: CollectedEvent[] = [];
    const deps = makeDeps(store, paths, sdk, collected);
    const { handleInvocationStart, handleInvocationEnd } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);
    await handleInvocationEnd(SUB_SESSION);
    await handleInvocationEnd(SUB_SESSION); // second call must be a no-op

    const endEvents = collected.filter((e) => e.event.event === "invocation_end");
    expect(endEvents).toHaveLength(1);
  });
});

// ---------------------------------------------------------------------------
// handleInvocationStart — session-messages fallback when no pending dispatch
//
// When claimPendingDispatch(parentId) returns undefined (no dispatch was
// queued for this parent), handleInvocationStart falls back to the corrected
// session.messages() extraction. These tests assert the fallback path still
// resolves identity correctly.
//
// Most of these tests will pass with the current code (messages is always used
// now), but they are important regression tests ensuring the fallback still
// works after Stage 3 changes the primary source to pending dispatch.
// ---------------------------------------------------------------------------

describe("handleInvocationStart — session-messages fallback when no pending dispatch", () => {
  it("falls back to session.messages() when no pending dispatch exists for the parent", async () => {
    const store = makeStore();
    const paths = makePaths();
    // No pending dispatch pushed — fallback must resolve from messages
    const sdk = makeSdk(() => userMessages(DISPATCH_PROMPT));
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    expect(store.get(SUB_SESSION)?.agentInstanceId).toBe(AGENT_INSTANCE_ID);
    expect(store.get(SUB_SESSION)?.runId).toBe(RUN_ID);
  });

  it("calls session.messages() when there is no pending dispatch for the parent", async () => {
    const store = makeStore();
    const paths = makePaths();
    let messagesCalled = false;
    const sdk: SdkClient = {
      session: {
        get: async () => undefined,
        messages: async () => {
          messagesCalled = true;
          return userMessages(DISPATCH_PROMPT);
        },
      },
      app: { log: async () => {} },
    };
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    expect(messagesCalled).toBe(true);
  });

  it("emits invocation_start with identity from session.messages() when no dispatch is queued", async () => {
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

  it("propagates run id from session.messages() to the orchestrator session when no dispatch is queued", async () => {
    const store = makeStore();
    const paths = makePaths();
    const sdk = makeSdk(() => userMessages(DISPATCH_PROMPT));
    const deps = makeDeps(store, paths, sdk, []);
    const { handleInvocationStart } = createInvocationHandlers(deps);

    store.register(ORCH_SESSION, {});
    store.register(SUB_SESSION, { parentId: ORCH_SESSION });

    await handleInvocationStart(SUB_SESSION, ORCH_SESSION);

    // Run id from the session.messages() fallback must also propagate to orchestrator
    expect(store.get(ORCH_SESSION)?.runId).toBe(RUN_ID);
  });
});
