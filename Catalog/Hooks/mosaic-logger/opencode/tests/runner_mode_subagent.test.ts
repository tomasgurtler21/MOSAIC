/**
 * runner_mode_subagent.test.ts — Subagent-role lifecycle in Runner mode.
 *
 * A top-level OpenCode session run with MOSAIC_ROLE=subagent is ONE invocation.
 * Verifies the invocation folder content, the native-equivalent invocation_end
 * field set, exactly-once close, nothing leaking into the orchestrator stream,
 * and that invocation_end is on disk before the first await of the idle path
 * (OpenCode does not await session.idle handlers on process exit).
 */

import { describe, it, expect } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";

import {
  sessionCreatedEvent,
  sessionIdleEvent,
  sessionDeletedEvent,
  toolBeforePayload,
  toolAfterPayload,
} from "./fixtures/opencode_api";
import {
  installRunnerTestEnv,
  setRunnerEnv,
  makeSdk,
  startPlugin,
  userMessageUpdated,
  assistantMessageUpdated,
  readJsonl,
  eventTypes,
  runDir,
  waitFor,
  neverResolves,
  RUN_ID,
  INSTANCE_ID,
  SESSION,
  FINAL_RESPONSE,
} from "./runner_mode_support";
import { messageUpdatedEvent, assistantMessageEntry } from "./fixtures/opencode_api";

const testEnv = installRunnerTestEnv();

const PROMPT = "Research the market landscape";

// Envelope fields plus the only fields native OpenCode invocation_end carries.
const NATIVE_INVOCATION_END_KEYS = new Set([
  "schema_version",
  "event",
  "timestamp",
  "harness",
  "session_id",
  "run_id",
  "agent_instance_id",
  "status_code",
  "response",
]);

function invocationEventsPath(): string {
  return nodePath.join(runDir(testEnv.dir, RUN_ID), INSTANCE_ID, "03_events.jsonl");
}

function invocationEvents(): Record<string, unknown>[] {
  return readJsonl(invocationEventsPath());
}

async function startSubagentPlugin(messages: () => unknown = () => []) {
  setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
  return startPlugin(testEnv.dir, makeSdk(messages));
}

describe("subagent role - invocation folder content", () => {
  it("produces invocation_start, turns, tool calls, usage and one invocation_end in order", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    const [bIn, bOut] = toolBeforePayload({ tool: "bash", sessionID: SESSION, callID: "call-1" });
    await hooks["tool.execute.before"](bIn, bOut);
    const [aIn, aOut] = toolAfterPayload({ tool: "bash", sessionID: SESSION, callID: "call-1" });
    await hooks["tool.execute.after"](aIn, aOut);
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const types = eventTypes(invocationEvents());
    expect(types[0]).toBe("invocation_start");
    expect(types).toContain("tool_call_start");
    expect(types).toContain("tool_call_end");
    expect(types).toContain("usage_record");
    expect(types.filter((t) => t === "turn").length).toBeGreaterThanOrEqual(2);
    expect(types.filter((t) => t === "invocation_start")).toHaveLength(1);
    expect(types.filter((t) => t === "invocation_end")).toHaveLength(1);
    expect(types[types.length - 1]).toBe("invocation_end");
  });

  it("invocation_start carries agent_instance_id, agent_type and the prompt", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });

    const start = invocationEvents()[0];
    expect(start["event"]).toBe("invocation_start");
    expect(start["agent_instance_id"]).toBe(INSTANCE_ID);
    expect(start["agent_type"]).toBe("Research");
    expect(String(start["prompt"])).toContain(PROMPT);
  });

  it("emits invocation_start before invocation_end even when idle is the first event seen", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const types = eventTypes(invocationEvents());
    expect(types[0]).toBe("invocation_start");
    expect(types).toContain("invocation_end");
  });

  it("puts session_id and run_id in every envelope", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const events = invocationEvents();
    expect(events.length).toBeGreaterThan(0);
    for (const e of events) {
      expect(e["session_id"]).toBe(SESSION);
      expect(e["run_id"]).toBe(RUN_ID);
    }
  });

  it("writes none of run_start, run_end, session_start, session_end anywhere", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });
    await hooks.event({ event: sessionDeletedEvent(SESSION) });

    const forbidden = ["run_start", "run_end", "session_start", "session_end"];
    const types = eventTypes(invocationEvents());
    expect(types).toContain("invocation_end");
    for (const f of forbidden) expect(types).not.toContain(f);
  });

  it("writes nothing to the orchestrator event stream", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });
    await hooks.event({ event: sessionDeletedEvent(SESSION) });

    const run = runDir(testEnv.dir, RUN_ID);
    expect(eventTypes(invocationEvents())).toContain("invocation_start");
    expect(nodeFs.existsSync(nodePath.join(run, "00_orchestrator_events.jsonl"))).toBe(false);
  });

  it("writes 01_input.md and 02_output.md best-effort after the mandatory events", async () => {
    const hooks = await startSubagentPlugin(() => []);

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const dir = nodePath.join(runDir(testEnv.dir, RUN_ID), INSTANCE_ID);
    const input = nodePath.join(dir, "01_input.md");
    const output = nodePath.join(dir, "02_output.md");
    expect(await waitFor(() => nodeFs.existsSync(input) && nodeFs.existsSync(output))).toBe(true);
    expect(nodeFs.readFileSync(input, "utf-8")).toContain(PROMPT);
    expect(nodeFs.readFileSync(output, "utf-8")).toContain("Here are the findings.");
  });
});

describe("subagent role - invocation_end content", () => {
  it("carries only agent_instance_id, status_code and response beyond the envelope", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const end = invocationEvents().find((e) => e["event"] === "invocation_end");
    expect(end).toBeDefined();
    for (const key of Object.keys(end as object)) {
      expect(NATIVE_INVOCATION_END_KEYS.has(key)).toBe(true);
    }
    expect(end?.["agent_instance_id"]).toBe(INSTANCE_ID);
    expect(end?.["status_code"]).toBe("SUCCESS");
    expect(String(end?.["response"])).toContain("Here are the findings.");
  });

  it("uses the last assistant message as the response", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({
      event: assistantMessageUpdated(SESSION, "Interim thoughts", { id: "msg-asst-1" }),
    });
    await hooks.event({
      event: assistantMessageUpdated(SESSION, FINAL_RESPONSE, { id: "msg-asst-2" }),
    });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const end = invocationEvents().find((e) => e["event"] === "invocation_end");
    expect(String(end?.["response"])).toContain("Here are the findings.");
    expect(String(end?.["response"])).not.toContain("Interim thoughts");
  });

  it("uses the latest text of a message updated several times", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({
      event: assistantMessageUpdated(SESSION, "Partial", { completed: false }),
    });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const end = invocationEvents().find((e) => e["event"] === "invocation_end");
    expect(String(end?.["response"])).toContain("Here are the findings.");
  });

  it("omits status_code and response when the assistant produced no text", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const end = invocationEvents().find((e) => e["event"] === "invocation_end");
    expect(end).toBeDefined();
    expect(end?.["status_code"]).toBeUndefined();
    expect(end?.["response"]).toBeUndefined();
  });

  it("omits status_code when the response has no structured status", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({ event: assistantMessageUpdated(SESSION, "Plain answer, no status.") });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const end = invocationEvents().find((e) => e["event"] === "invocation_end");
    expect(end?.["status_code"]).toBeUndefined();
    expect(end?.["response"]).toBe("Plain answer, no status.");
  });
});

describe("subagent role - exactly-once close", () => {
  it("writes one invocation_end for repeated session.idle events", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const ends = eventTypes(invocationEvents()).filter((t) => t === "invocation_end");
    expect(ends).toHaveLength(1);
  });

  it("writes one invocation_end when idle events are fired without awaiting", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    void hooks.event({ event: sessionIdleEvent(SESSION) });
    void hooks.event({ event: sessionIdleEvent(SESSION) });

    const ends = eventTypes(invocationEvents()).filter((t) => t === "invocation_end");
    expect(ends).toHaveLength(1);
  });

  it("session.deleted after the close writes nothing further", async () => {
    const hooks = await startSubagentPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });
    const before = nodeFs.readFileSync(invocationEventsPath(), "utf-8");
    await hooks.event({ event: sessionDeletedEvent(SESSION) });

    expect(nodeFs.readFileSync(invocationEventsPath(), "utf-8")).toBe(before);
  });
});

describe("subagent role - exit safety of the idle path", () => {
  it("has invocation_end on disk synchronously, before any await, with a never-resolving SDK", async () => {
    const hooks = await startSubagentPlugin(() => neverResolves());

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });

    // Deliberately not awaited: nothing after the first await of the idle path survives exit.
    void hooks.event({ event: sessionIdleEvent(SESSION) });

    const end = invocationEvents().find((e) => e["event"] === "invocation_end");
    expect(end).toBeDefined();
    expect(end?.["agent_instance_id"]).toBe(INSTANCE_ID);
    expect(end?.["status_code"]).toBe("SUCCESS");
  });

  it("has invocation_start on disk synchronously when idle is the first event, with a never-resolving SDK", async () => {
    const hooks = await startSubagentPlugin(() => neverResolves());

    void hooks.event({ event: sessionIdleEvent(SESSION) });

    const types = eventTypes(invocationEvents());
    expect(types[0]).toBe("invocation_start");
    expect(types).toContain("invocation_end");
  });

  it("builds the response from events accumulated during the session, not from the SDK", async () => {
    // The SDK would report a different (stale) final message; the written one must come from events.
    const hooks = await startSubagentPlugin(() => [
      assistantMessageEntry("Stale SDK answer"),
    ]);

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT) });
    await hooks.event({
      event: messageUpdatedEvent(
        assistantMessageEntry("Event-sourced answer", {
          id: "msg-asst-9",
          sessionID: SESSION,
          time: { created: 1, completed: 2 },
        }),
      ),
    });
    void hooks.event({ event: sessionIdleEvent(SESSION) });

    const end = invocationEvents().find((e) => e["event"] === "invocation_end");
    expect(end?.["response"]).toBe("Event-sourced answer");
  });
});
