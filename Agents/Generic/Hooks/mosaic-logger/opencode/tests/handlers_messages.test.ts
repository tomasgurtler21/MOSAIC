/**
 * handlers_messages.test.ts — Tests for message-derived turn and usage_record handlers.
 *
 * Covers the behavior of `handleMessageEvent` (the single entry point for
 * `message.updated` bus events) and the `buildTokenUsage` pure function.
 *
 * Turn emission:
 *   - User and assistant turns emitted with the correct role field
 *   - Text assembled from text-typed parts, multiple parts joined with "\n"
 *   - Non-text parts (reasoning, tool, step-start, ...) are ignored during assembly
 *   - Turn emitted without a text field when the message has no text parts
 *   - No throw for any message shape, including completely empty entries
 *   - Orchestrator-scoped messages go to the orchestrator events stream
 *   - Subagent-scoped messages go to the invocation events stream
 *   - Same message id observed twice produces exactly one turn (de-duplication)
 *
 * Usage record emission:
 *   - record_id, record_index, and source present on every assistant usage_record
 *   - source is "orchestrator_transcript" on the orchestrator stream
 *   - source is "agent_transcript" on an invocation stream
 *   - agent_instance_id present on invocation streams and absent on orchestrator streams
 *   - Same assistant message observed twice yields the same record_id (supersede rule)
 *   - All observations of a given record_id go to the same file path (single-stream invariant)
 *   - Orchestrator and subagent usage records land on separate streams
 *   - Second distinct assistant record on the same stream receives record_index 1
 *   - record_index is unchanged across supersede re-observations of the same record
 *   - token_usage present on the emitted usage_record when tokens are resolvable
 *   - token_usage absent from the emitted usage_record when nothing is resolvable
 *   - model present when info.modelID is resolvable, absent when not
 *   - service_tier present when info.mode is resolvable, absent when not
 *
 * buildTokenUsage — token sub-field presence rules:
 *   - Each sub-field (input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens)
 *     is present only when its source value is a genuine number
 *   - Zero counts as a genuine number and is included
 *   - Undefined, null, and non-number values cause the sub-field to be omitted
 *   - Returns undefined when no sub-field is resolvable (so the object is omitted entirely)
 *
 * High-frequency path:
 *   - handleMessageEvent makes no SDK call (no client.session.get or client.session.messages)
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import { createMessageHandlers, buildTokenUsage } from "../lib/handlers_messages";
import type {
  HandlerDependencies,
  SdkClient,
  MessageInfo,
} from "../lib/handlers_session";
import { SessionCorrelationStore } from "../lib/correlation";
import { LogPaths, setDebugLogger } from "../lib/core";
import {
  userMessageEntry,
  assistantMessageEntry,
  nonTextMessageEntry,
  emptyPartsMessageEntry,
  messageUpdatedEvent,
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
  tmpDir = nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "mosaic-msg-test-"));
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

type CollectedEvent = { filePath: string; event: Record<string, unknown> };

function makeSdkWithTracking(): {
  sdk: SdkClient;
  getCalled: () => boolean;
  messagesCalled: () => boolean;
} {
  const tracker = { getCalled: false, messagesCalled: false };
  return {
    sdk: {
      session: {
        get: async () => {
          tracker.getCalled = true;
          return undefined;
        },
        messages: async () => {
          tracker.messagesCalled = true;
          return [];
        },
      },
      app: { log: async () => {} },
    },
    getCalled: () => tracker.getCalled,
    messagesCalled: () => tracker.messagesCalled,
  };
}

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
    fallbackCallId: () => "fallback-call-id",
  };
}

// ---------------------------------------------------------------------------
// Turn emission — user turns
// ---------------------------------------------------------------------------

describe("handleMessageEvent — user turn emission", () => {
  it("emits a turn event when a user message is observed", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = userMessageEntry("Hello", { id: "msg-user-1", sessionID: ORCH_SESSION });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turns = collected.filter((e) => e.event.event === "turn");
    expect(turns).toHaveLength(1);
  });

  it("sets role to 'user' on the emitted turn event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = userMessageEntry("Hi there", { id: "msg-user-2", sessionID: ORCH_SESSION });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turn = collected.find((e) => e.event.event === "turn");
    expect(turn?.event.role).toBe("user");
  });

  it("includes the message text in the user turn event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = userMessageEntry("What is the weather?", {
      id: "msg-user-3",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turn = collected.find((e) => e.event.event === "turn");
    expect(turn?.event.text).toBe("What is the weather?");
  });
});

// ---------------------------------------------------------------------------
// Turn emission — assistant turns
// ---------------------------------------------------------------------------

describe("handleMessageEvent — assistant turn emission", () => {
  it("emits a turn event when an assistant message is observed", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("The weather is sunny.", {
      id: "msg-asst-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turns = collected.filter((e) => e.event.event === "turn");
    expect(turns).toHaveLength(1);
  });

  it("sets role to 'assistant' on the emitted turn event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Here is my answer.", {
      id: "msg-asst-2",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turn = collected.find((e) => e.event.event === "turn");
    expect(turn?.event.role).toBe("assistant");
  });

  it("includes the message text in the assistant turn event", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Here is my detailed answer.", {
      id: "msg-asst-3",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turn = collected.find((e) => e.event.event === "turn");
    expect(turn?.event.text).toBe("Here is my detailed answer.");
  });
});

// ---------------------------------------------------------------------------
// Turn routing — orchestrator session
// ---------------------------------------------------------------------------

describe("handleMessageEvent — orchestrator stream routing", () => {
  it("routes an orchestrator-session turn to the orchestrator events stream", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = userMessageEntry("Hello", {
      id: "msg-orch-route-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turn = collected.find((e) => e.event.event === "turn");
    expect(turn?.filePath).toBe(paths.orchestratorEvents(RUN_ID));
  });

  it("never routes an orchestrator-session turn to an invocation stream", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, { parentId: ORCH_SESSION, agentInstanceId: AGENT_INSTANCE_ID });
    const msg = userMessageEntry("Hello", {
      id: "msg-orch-route-2",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turn = collected.find((e) => e.event.event === "turn");
    expect(turn?.filePath).not.toBe(paths.invocationEvents(RUN_ID, AGENT_INSTANCE_ID));
  });
});

// ---------------------------------------------------------------------------
// Turn routing — subagent session
// ---------------------------------------------------------------------------

describe("handleMessageEvent — subagent (invocation) stream routing", () => {
  it("routes a subagent-session turn to the invocation events stream", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, { parentId: ORCH_SESSION, agentInstanceId: AGENT_INSTANCE_ID });
    const msg = assistantMessageEntry("Sub response.", {
      id: "msg-sub-route-1",
      sessionID: SUB_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turn = collected.find((e) => e.event.event === "turn");
    expect(turn?.filePath).toBe(paths.invocationEvents(RUN_ID, AGENT_INSTANCE_ID));
  });

  it("never routes a subagent-session turn to the orchestrator stream", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, { parentId: ORCH_SESSION, agentInstanceId: AGENT_INSTANCE_ID });
    const msg = assistantMessageEntry("Sub response.", {
      id: "msg-sub-route-2",
      sessionID: SUB_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turn = collected.find((e) => e.event.event === "turn");
    expect(turn?.filePath).not.toBe(paths.orchestratorEvents(RUN_ID));
  });
});

// ---------------------------------------------------------------------------
// Multi-part text assembly
// ---------------------------------------------------------------------------

describe("handleMessageEvent — multi-part text assembly", () => {
  it("joins multiple text parts with a newline to produce the turn text", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = userMessageEntry(["First part", "Second part"], {
      id: "msg-multipart-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const turn = collected.find((e) => e.event.event === "turn");
    expect(turn?.event.text).toBe("First part\nSecond part");
  });

  it("ignores non-text parts when assembling the turn text", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    // Build a message with interleaved reasoning and text parts
    const textMsg = userMessageEntry("Visible text", {
      id: "msg-mixed-1",
      sessionID: ORCH_SESSION,
    });
    const mixedMsg = {
      ...textMsg,
      parts: [
        { id: "p0", type: "reasoning", messageID: "msg-mixed-1", sessionID: ORCH_SESSION },
        ...textMsg.parts!,
        { id: "p2", type: "step-finish", messageID: "msg-mixed-1", sessionID: ORCH_SESSION },
      ],
    };
    await handleMessageEvent(messageUpdatedEvent(mixedMsg));

    const turn = collected.find((e) => e.event.event === "turn");
    // Only text parts contribute; non-text parts are silently ignored
    expect(turn?.event.text).toBe("Visible text");
  });
});

// ---------------------------------------------------------------------------
// Turn with no extractable text — graceful degradation
// ---------------------------------------------------------------------------

describe("handleMessageEvent — graceful handling when no text is extractable", () => {
  it("emits a turn event even when the message has no text parts", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const noTextMsg = nonTextMessageEntry();
    const event = messageUpdatedEvent({
      ...noTextMsg,
      info: { ...noTextMsg.info, sessionID: ORCH_SESSION, id: "msg-notext-1" },
    });
    await handleMessageEvent(event);

    const turns = collected.filter((e) => e.event.event === "turn");
    expect(turns).toHaveLength(1);
  });

  it("omits the text field when no text is extractable", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const noTextMsg = nonTextMessageEntry();
    const event = messageUpdatedEvent({
      ...noTextMsg,
      info: { ...noTextMsg.info, sessionID: ORCH_SESSION, id: "msg-notext-2" },
    });
    await handleMessageEvent(event);

    const turns = collected.filter((e) => e.event.event === "turn");
    // Exactly one turn must be emitted even when there is no text
    expect(turns).toHaveLength(1);
    // The text field must be absent — a no-text message is not a malformed one
    expect(turns[0]!.event).not.toHaveProperty("text");
  });

  it("does not throw when the message has only non-text parts", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const noTextMsg = nonTextMessageEntry();
    const event = messageUpdatedEvent({
      ...noTextMsg,
      info: { ...noTextMsg.info, sessionID: ORCH_SESSION, id: "msg-notext-3" },
    });

    await expect(handleMessageEvent(event)).resolves.toBeUndefined();
  });

  it("does not throw for a completely empty parts array", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const entry = emptyPartsMessageEntry({ id: "msg-empty-1", sessionID: ORCH_SESSION });
    const event = messageUpdatedEvent(entry);

    await expect(handleMessageEvent(event)).resolves.toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Turn de-duplication
// ---------------------------------------------------------------------------

describe("handleMessageEvent — turn de-duplication", () => {
  it("emits exactly one turn when the same message id is observed twice", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = userMessageEntry("Hello", {
      id: "msg-dedup-1",
      sessionID: ORCH_SESSION,
    });
    const event = messageUpdatedEvent(msg);

    // Process the same message event twice — simulates two message.updated bus firings
    // for the same message as its content or metadata settles.
    await handleMessageEvent(event);
    await handleMessageEvent(event);

    const turns = collected.filter((e) => e.event.event === "turn");
    expect(turns).toHaveLength(1);
  });
});

// ---------------------------------------------------------------------------
// Usage record — required fields
// ---------------------------------------------------------------------------

describe("handleMessageEvent — usage_record required fields", () => {
  it("emits a usage_record event for an assistant message", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Done.", {
      id: "msg-usage-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecords = collected.filter((e) => e.event.event === "usage_record");
    expect(usageRecords).toHaveLength(1);
  });

  it("includes a non-empty string record_id in the usage_record", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Done.", {
      id: "msg-usage-2",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(typeof usageRecord?.event.record_id).toBe("string");
    expect((usageRecord?.event.record_id as string).length).toBeGreaterThan(0);
  });

  it("includes a numeric record_index in the usage_record", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Done.", {
      id: "msg-usage-3",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(typeof usageRecord?.event.record_index).toBe("number");
    expect(usageRecord?.event.record_index as number).toBeGreaterThanOrEqual(0);
  });

  it("includes a source field in the usage_record", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Done.", {
      id: "msg-usage-4",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord?.event.source).toBeDefined();
  });

  it("assigns a 0-based ordinal record_index to the first assistant record on a stream", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("First.", {
      id: "msg-index-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord?.event.record_index).toBe(0);
  });

  it("does not emit a usage_record for a user message", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = userMessageEntry("A user prompt.", {
      id: "msg-no-usage-user-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecords = collected.filter((e) => e.event.event === "usage_record");
    expect(usageRecords).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// Source field — per-stream provenance
// ---------------------------------------------------------------------------

describe("handleMessageEvent — source field per stream", () => {
  it("sets source to 'orchestrator_transcript' for an orchestrator session", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Orch answer.", {
      id: "msg-src-orch-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord?.event.source).toBe("orchestrator_transcript");
  });

  it("sets source to 'agent_transcript' for a subagent session", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, { parentId: ORCH_SESSION, agentInstanceId: AGENT_INSTANCE_ID });
    const msg = assistantMessageEntry("Agent answer.", {
      id: "msg-src-sub-1",
      sessionID: SUB_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord?.event.source).toBe("agent_transcript");
  });
});

// ---------------------------------------------------------------------------
// agent_instance_id — present on invocation streams, absent on orchestrator
// ---------------------------------------------------------------------------

describe("handleMessageEvent — agent_instance_id field presence", () => {
  it("includes agent_instance_id on invocation stream usage records", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, { parentId: ORCH_SESSION, agentInstanceId: AGENT_INSTANCE_ID });
    const msg = assistantMessageEntry("Agent answer.", {
      id: "msg-iid-sub-1",
      sessionID: SUB_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord?.event.agent_instance_id).toBe(AGENT_INSTANCE_ID);
  });

  it("omits agent_instance_id from orchestrator stream usage records", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Orch answer.", {
      id: "msg-iid-orch-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    // A usage_record must be emitted; then verify the field is absent on orchestrator events
    expect(usageRecord).toBeDefined();
    expect(usageRecord!.event).not.toHaveProperty("agent_instance_id");
  });
});

// ---------------------------------------------------------------------------
// Supersede rule — stable record_id across observations
// ---------------------------------------------------------------------------

describe("handleMessageEvent — supersede rule: stable record_id across observations", () => {
  it("produces the same record_id for two observations of the same assistant message", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Settling answer.", {
      id: "msg-supersede-1",
      sessionID: ORCH_SESSION,
    });
    const event = messageUpdatedEvent(msg);

    // Observe the same message twice as its metadata values settle
    await handleMessageEvent(event);
    await handleMessageEvent(event);

    const usageRecords = collected.filter((e) => e.event.event === "usage_record");
    expect(usageRecords.length).toBeGreaterThanOrEqual(2);

    const firstRecordId = usageRecords[0]!.event.record_id;
    const secondRecordId = usageRecords[1]!.event.record_id;
    // The record_id must be identical across observations so consumers can apply
    // the supersede rule (later value wins, keyed by record_id).
    expect(firstRecordId).toBe(secondRecordId);
  });
});

// ---------------------------------------------------------------------------
// Single-stream invariant — all observations on one stream
// ---------------------------------------------------------------------------

describe("handleMessageEvent — single-stream invariant", () => {
  it("routes all observations of the same record_id to the same file path", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Updating...", {
      id: "msg-invariant-1",
      sessionID: ORCH_SESSION,
    });
    const event = messageUpdatedEvent(msg);

    await handleMessageEvent(event);
    await handleMessageEvent(event);

    const usageRecords = collected.filter((e) => e.event.event === "usage_record");
    expect(usageRecords.length).toBeGreaterThanOrEqual(2);

    const firstFilePath = usageRecords[0]!.filePath;
    const secondFilePath = usageRecords[1]!.filePath;
    // All observations of the same record must land on exactly one stream.
    expect(firstFilePath).toBe(secondFilePath);
  });

  it("keeps orchestrator and subagent usage records on separate streams", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    store.register(SUB_SESSION, { parentId: ORCH_SESSION, agentInstanceId: AGENT_INSTANCE_ID });

    const orchMsg = assistantMessageEntry("Orch.", {
      id: "msg-sep-orch-1",
      sessionID: ORCH_SESSION,
    });
    const subMsg = assistantMessageEntry("Sub.", {
      id: "msg-sep-sub-1",
      sessionID: SUB_SESSION,
    });

    await handleMessageEvent(messageUpdatedEvent(orchMsg));
    await handleMessageEvent(messageUpdatedEvent(subMsg));

    const usageRecords = collected.filter((e) => e.event.event === "usage_record");
    const orchRecord = usageRecords.find(
      (e) => e.filePath === paths.orchestratorEvents(RUN_ID),
    );
    const subRecord = usageRecords.find(
      (e) => e.filePath === paths.invocationEvents(RUN_ID, AGENT_INSTANCE_ID),
    );

    // Each session's usage records must land on that session's stream
    expect(orchRecord).toBeDefined();
    expect(subRecord).toBeDefined();
    // Different messages → different record_ids
    expect(orchRecord?.event.record_id).not.toBe(subRecord?.event.record_id);
  });
});

// ---------------------------------------------------------------------------
// record_index — ordinal incrementing across distinct records and stability
// across supersede re-observations
// ---------------------------------------------------------------------------

describe("handleMessageEvent — record_index ordinal across distinct records", () => {
  it("assigns record_index 1 to the second distinct assistant record on the same stream", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg1 = assistantMessageEntry("First.", {
      id: "msg-ordinal-1",
      sessionID: ORCH_SESSION,
    });
    const msg2 = assistantMessageEntry("Second.", {
      id: "msg-ordinal-2",
      sessionID: ORCH_SESSION,
    });

    await handleMessageEvent(messageUpdatedEvent(msg1));
    await handleMessageEvent(messageUpdatedEvent(msg2));

    const usageRecords = collected.filter((e) => e.event.event === "usage_record");
    // Two distinct records must produce two usage_records in observation order
    expect(usageRecords).toHaveLength(2);
    // First distinct record is ordinal 0, second is ordinal 1
    expect(usageRecords[0]!.event.record_index).toBe(0);
    expect(usageRecords[1]!.event.record_index).toBe(1);
  });

  it("keeps the same record_index on a supersede re-observation of the same record", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Settling answer.", {
      id: "msg-stable-idx-1",
      sessionID: ORCH_SESSION,
    });
    const event = messageUpdatedEvent(msg);

    // Observe the same message twice — simulates values settling under the supersede rule
    await handleMessageEvent(event);
    await handleMessageEvent(event);

    const usageRecords = collected.filter((e) => e.event.event === "usage_record");
    // Both emissions must carry the same ordinal — the index is per record_id, not per emission
    expect(usageRecords.length).toBeGreaterThanOrEqual(2);
    expect(usageRecords[0]!.event.record_index).toBe(usageRecords[1]!.event.record_index);
  });
});

// ---------------------------------------------------------------------------
// token_usage — handler-level wiring into usage_record
// ---------------------------------------------------------------------------

describe("handleMessageEvent — token_usage wiring into emitted usage_record", () => {
  it("includes token_usage on the usage_record when the info carries resolvable token counts", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Answer.", {
      id: "msg-tokwire-1",
      sessionID: ORCH_SESSION,
      tokens: { input: 100, output: 50 },
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord).toBeDefined();
    // token_usage must be present because at least one sub-field resolved to a genuine number
    expect(usageRecord!.event.token_usage).toBeDefined();
  });

  it("omits token_usage from the usage_record when no token sub-field is resolvable", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    // No tokens field — nothing resolvable for buildTokenUsage to return
    const msg = assistantMessageEntry("Answer.", {
      id: "msg-tokwire-2",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord).toBeDefined();
    // token_usage must be absent when buildTokenUsage returns undefined
    expect(usageRecord!.event).not.toHaveProperty("token_usage");
  });
});

// ---------------------------------------------------------------------------
// model and service_tier — optional field presence on usage_record
// ---------------------------------------------------------------------------

describe("handleMessageEvent — model optional field on usage_record", () => {
  it("includes model on the usage_record when info.modelID is resolvable", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Answer.", {
      id: "msg-model-1",
      sessionID: ORCH_SESSION,
      modelID: "claude-3-5-sonnet",
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord).toBeDefined();
    expect(usageRecord!.event.model).toBeDefined();
  });

  it("omits model from the usage_record when info.modelID is absent", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    // No modelID provided — field should be absent from the emitted event
    const msg = assistantMessageEntry("Answer.", {
      id: "msg-model-2",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord).toBeDefined();
    expect(usageRecord!.event).not.toHaveProperty("model");
  });
});

describe("handleMessageEvent — service_tier optional field on usage_record", () => {
  it("includes service_tier on the usage_record when info.mode is resolvable", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Answer.", {
      id: "msg-svc-1",
      sessionID: ORCH_SESSION,
      mode: "auto",
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord).toBeDefined();
    expect(usageRecord!.event.service_tier).toBeDefined();
  });

  it("omits service_tier from the usage_record when info.mode is absent", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    // No mode provided — service_tier should be absent from the emitted event
    const msg = assistantMessageEntry("Answer.", {
      id: "msg-svc-2",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    const usageRecord = collected.find((e) => e.event.event === "usage_record");
    expect(usageRecord).toBeDefined();
    expect(usageRecord!.event).not.toHaveProperty("service_tier");
  });
});

// ---------------------------------------------------------------------------
// buildTokenUsage — token sub-field presence rules
// ---------------------------------------------------------------------------

describe("buildTokenUsage — sub-field inclusion when source value is a genuine number", () => {
  it("includes input_tokens when tokens.input is a positive number", () => {
    const info: MessageInfo = { tokens: { input: 100 } };
    const result = buildTokenUsage(info);
    expect(result?.input_tokens).toBe(100);
  });

  it("includes input_tokens when tokens.input is zero", () => {
    const info: MessageInfo = { tokens: { input: 0 } };
    const result = buildTokenUsage(info);
    // Zero is a genuine measurement and must be included, not treated as absent.
    expect(result?.input_tokens).toBe(0);
  });

  it("includes output_tokens when tokens.output is a positive number", () => {
    const info: MessageInfo = { tokens: { output: 50 } };
    const result = buildTokenUsage(info);
    expect(result?.output_tokens).toBe(50);
  });

  it("includes output_tokens when tokens.output is zero", () => {
    const info: MessageInfo = { tokens: { output: 0 } };
    const result = buildTokenUsage(info);
    expect(result?.output_tokens).toBe(0);
  });

  it("includes cache_read_tokens when tokens.cache.read is a positive number", () => {
    const info: MessageInfo = { tokens: { cache: { read: 200 } } };
    const result = buildTokenUsage(info);
    expect(result?.cache_read_tokens).toBe(200);
  });

  it("includes cache_read_tokens when tokens.cache.read is zero", () => {
    const info: MessageInfo = { tokens: { cache: { read: 0 } } };
    const result = buildTokenUsage(info);
    expect(result?.cache_read_tokens).toBe(0);
  });

  it("includes cache_creation_tokens when tokens.cache.write is a positive number", () => {
    const info: MessageInfo = { tokens: { cache: { write: 75 } } };
    const result = buildTokenUsage(info);
    expect(result?.cache_creation_tokens).toBe(75);
  });

  it("includes cache_creation_tokens when tokens.cache.write is zero", () => {
    const info: MessageInfo = { tokens: { cache: { write: 0 } } };
    const result = buildTokenUsage(info);
    expect(result?.cache_creation_tokens).toBe(0);
  });

  it("includes multiple sub-fields when multiple source values are genuine numbers", () => {
    const info: MessageInfo = { tokens: { input: 100, output: 50 } };
    const result = buildTokenUsage(info);
    expect(result?.input_tokens).toBe(100);
    expect(result?.output_tokens).toBe(50);
  });
});

describe("buildTokenUsage — sub-field omission when source value is not a genuine number", () => {
  it("omits input_tokens when tokens.input is undefined but other fields are resolvable", () => {
    const info: MessageInfo = { tokens: { output: 5 } };
    const result = buildTokenUsage(info);
    // output_tokens is resolvable, so the object is returned — but input_tokens must be absent
    expect(result).toBeDefined();
    expect(result!).not.toHaveProperty("input_tokens");
  });

  it("returns undefined when the only token field has a null value (null is not a number)", () => {
    const info: MessageInfo = { tokens: { input: null as unknown as number } };
    const result = buildTokenUsage(info);
    // Nothing resolvable — the entire object is omitted, which is stronger than "field absent"
    expect(result).toBeUndefined();
  });

  it("returns undefined when the only token field has a non-number string value", () => {
    const info: MessageInfo = { tokens: { input: "100" as unknown as number } };
    const result = buildTokenUsage(info);
    // Strings are not genuine numbers — the entire object is omitted
    expect(result).toBeUndefined();
  });

  it("omits output_tokens when tokens.output is undefined but other fields are resolvable", () => {
    const info: MessageInfo = { tokens: { input: 10 } };
    const result = buildTokenUsage(info);
    // input_tokens is resolvable, so the object is returned — but output_tokens must be absent
    expect(result).toBeDefined();
    expect(result!).not.toHaveProperty("output_tokens");
  });

  it("omits cache_read_tokens when tokens.cache.read is undefined but write is resolvable", () => {
    const info: MessageInfo = { tokens: { cache: { write: 5 } } };
    const result = buildTokenUsage(info);
    // cache_creation_tokens is resolvable, so the object is returned — but cache_read_tokens must be absent
    expect(result).toBeDefined();
    expect(result!).not.toHaveProperty("cache_read_tokens");
  });

  it("omits cache_creation_tokens when tokens.cache.write is undefined but read is resolvable", () => {
    const info: MessageInfo = { tokens: { cache: { read: 10 } } };
    const result = buildTokenUsage(info);
    // cache_read_tokens is resolvable, so the object is returned — but cache_creation_tokens must be absent
    expect(result).toBeDefined();
    expect(result!).not.toHaveProperty("cache_creation_tokens");
  });
});

describe("buildTokenUsage — returns undefined when nothing is resolvable", () => {
  it("returns undefined when the tokens field is absent", () => {
    const info: MessageInfo = {};
    const result = buildTokenUsage(info);
    expect(result).toBeUndefined();
  });

  it("returns undefined when info itself is undefined", () => {
    const result = buildTokenUsage(undefined);
    expect(result).toBeUndefined();
  });

  it("returns undefined when tokens exists but no sub-field holds a genuine number", () => {
    const info: MessageInfo = {
      tokens: {
        input: undefined,
        output: null as unknown as number,
        cache: {
          read: "not-a-number" as unknown as number,
          write: undefined,
        },
      },
    };
    const result = buildTokenUsage(info);
    expect(result).toBeUndefined();
  });

  it("returns undefined when tokens is an empty object", () => {
    const info: MessageInfo = { tokens: {} };
    const result = buildTokenUsage(info);
    expect(result).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// High-frequency path — no SDK calls
// ---------------------------------------------------------------------------

describe("handleMessageEvent — high-frequency path makes no SDK calls", () => {
  it("does not call client.session.get()", async () => {
    const store = makeStore();
    const paths = makePaths();
    const tracking = makeSdkWithTracking();
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, tracking.sdk, []),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = userMessageEntry("Hello", {
      id: "msg-sdk-get-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    expect(tracking.getCalled()).toBe(false);
  });

  it("does not call client.session.messages()", async () => {
    const store = makeStore();
    const paths = makePaths();
    const tracking = makeSdkWithTracking();
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, tracking.sdk, []),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    const msg = assistantMessageEntry("Answer.", {
      id: "msg-sdk-msgs-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(msg));

    expect(tracking.messagesCalled()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// record_index ordinal — shared stream when multiple session ids share one file
// ---------------------------------------------------------------------------

describe("handleMessageEvent — record_index ordinal across sessions sharing a stream file", () => {
  it("assigns sequential record_index values when two unregistered session ids share the degraded stream", async () => {
    // Neither session is registered in the store, so both fall through to the
    // same degradation-bucket fallback path in resolveEventSink. With the ordinal
    // keyed by sink path (not session id), the two records share one counter and
    // receive 0 and 1 in observation order — not both 0.
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    // No sessions registered — both will route to the same unknown-run degradation sink
    const msg1 = assistantMessageEntry("First from unknown session A.", {
      id: "msg-shared-sink-1",
      sessionID: "unknown-sess-A",
      role: "assistant",
    });
    const msg2 = assistantMessageEntry("Second from unknown session B.", {
      id: "msg-shared-sink-2",
      sessionID: "unknown-sess-B",
      role: "assistant",
    });

    await handleMessageEvent(messageUpdatedEvent(msg1));
    await handleMessageEvent(messageUpdatedEvent(msg2));

    const usageRecords = collected.filter((e) => e.event.event === "usage_record");
    expect(usageRecords).toHaveLength(2);

    // Both must land in the same file (they share the degradation sink)
    expect(usageRecords[0]!.filePath).toBe(usageRecords[1]!.filePath);

    // The ordinals must be sequential (0 then 1), not both 0
    expect(usageRecords[0]!.event.record_index).toBe(0);
    expect(usageRecords[1]!.event.record_index).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// Empty-string text part — unified behaviour with the shared extraction helper
// ---------------------------------------------------------------------------

describe("handleMessageEvent — empty-string text part excluded from turn text", () => {
  it("omits the text field when the only text part has an empty string value", async () => {
    // The shared extractTextFromParts helper (handlers_invocation.ts) only includes
    // text parts whose string has length > 0. A part with text: "" is therefore
    // treated identically to a non-text part: it yields no text, so the field is absent.
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    // Build a user message whose single text part has text: "" (empty string)
    const emptyTextMsg = userMessageEntry("", {
      id: "msg-empty-text-part-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(emptyTextMsg));

    const turns = collected.filter((e) => e.event.event === "turn");
    // A turn must still be emitted (graceful degradation, same as the no-text case)
    expect(turns).toHaveLength(1);
    // But the text field must be absent — an empty text part contributes nothing
    expect(turns[0]!.event).not.toHaveProperty("text");
  });

  it("omits the text field when all text parts have empty string values", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    // Build a user message with multiple text parts, all empty strings
    const allEmptyMsg = userMessageEntry(["", ""], {
      id: "msg-all-empty-text-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(allEmptyMsg));

    const turns = collected.filter((e) => e.event.event === "turn");
    expect(turns).toHaveLength(1);
    expect(turns[0]!.event).not.toHaveProperty("text");
  });

  it("includes only non-empty text parts when mixed empty and non-empty parts are present", async () => {
    const store = makeStore();
    const paths = makePaths();
    const { sdk } = makeSdkWithTracking();
    const collected: CollectedEvent[] = [];
    const { handleMessageEvent } = createMessageHandlers(
      makeDeps(store, paths, sdk, collected),
    );

    store.register(ORCH_SESSION, { runId: RUN_ID });
    // Build a user message where some parts are empty and some are non-empty
    const mixedMsg = userMessageEntry(["", "actual content", ""], {
      id: "msg-mixed-empty-1",
      sessionID: ORCH_SESSION,
    });
    await handleMessageEvent(messageUpdatedEvent(mixedMsg));

    const turns = collected.filter((e) => e.event.event === "turn");
    expect(turns).toHaveLength(1);
    // Only the non-empty part contributes; empty parts are excluded
    expect(turns[0]!.event.text).toBe("actual content");
  });
});
