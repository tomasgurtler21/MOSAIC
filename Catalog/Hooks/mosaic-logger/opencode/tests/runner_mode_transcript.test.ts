/**
 * runner_mode_transcript.test.ts — In-session transcript export in Runner mode.
 *
 * Under `opencode run` work after the first await of the idle path is lost, so
 * the transcript is refreshed while the session is running. Verifies that:
 *   - the subagent transcript (04_session.raw + sidecar) exists before any idle
 *     and later refreshes replace it,
 *   - it does not depend on the idle path completing,
 *   - an unavailable transcript writes nothing and disturbs nothing else,
 *   - orchestrator-role transcripts use the session-scoped name in the real
 *     run folder, one file per session.
 */

import { describe, it, expect } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";

import { transcriptScopeSegment } from "../lib/core";
import {
  sessionCreatedEvent,
  sessionIdleEvent,
  toolBeforePayload,
  toolAfterPayload,
  userMessageEntry,
  assistantMessageEntry,
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

const testEnv = installRunnerTestEnv();

const TRANSCRIPT_V1 = [userMessageEntry("dispatch"), assistantMessageEntry("first answer")];
const TRANSCRIPT_V2 = [
  userMessageEntry("dispatch"),
  assistantMessageEntry("first answer"),
  assistantMessageEntry("second answer"),
];

function invDir(): string {
  return nodePath.join(runDir(testEnv.dir, RUN_ID), INSTANCE_ID);
}
function rawPath(): string {
  return nodePath.join(invDir(), "04_session.raw");
}
function metaPath(): string {
  return nodePath.join(invDir(), "04_session.meta.json");
}
function readRaw(): unknown {
  return JSON.parse(nodeFs.readFileSync(rawPath(), "utf-8"));
}

describe("subagent role - transcript refreshed during the session", () => {
  it("writes 04_session.raw and sidecar after an assistant message completes, before any idle", async () => {
    setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => TRANSCRIPT_V1));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "dispatch") });
    await hooks.event({ event: assistantMessageUpdated(SESSION, "first answer") });

    expect(await waitFor(() => nodeFs.existsSync(rawPath()) && nodeFs.existsSync(metaPath()))).toBe(true);
    expect(readRaw()).toEqual(JSON.parse(JSON.stringify(TRANSCRIPT_V1)));
    const types = eventTypes(readJsonl(nodePath.join(invDir(), "03_events.jsonl")));
    expect(types).not.toContain("invocation_end");
  });

  it("does not refresh for an assistant message that has not completed", async () => {
    setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
    let calls = 0;
    const hooks = await startPlugin(
      testEnv.dir,
      makeSdk(() => {
        calls += 1;
        return TRANSCRIPT_V1;
      }),
    );

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "dispatch") });
    await hooks.event({ event: assistantMessageUpdated(SESSION, "typing", { completed: false }) });
    await new Promise((r) => setTimeout(r, 100));

    expect(calls).toBe(0);
    expect(nodeFs.existsSync(rawPath())).toBe(false);
  });

  it("writes the transcript after a tool execution, before any idle", async () => {
    setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => TRANSCRIPT_V1));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "dispatch") });
    const [bIn, bOut] = toolBeforePayload({ tool: "bash", sessionID: SESSION, callID: "c1" });
    await hooks["tool.execute.before"](bIn, bOut);
    const [aIn, aOut] = toolAfterPayload({ tool: "bash", sessionID: SESSION, callID: "c1" });
    await hooks["tool.execute.after"](aIn, aOut);

    expect(await waitFor(() => nodeFs.existsSync(rawPath()))).toBe(true);
    expect(readRaw()).toEqual(JSON.parse(JSON.stringify(TRANSCRIPT_V1)));
  });

  it("replaces the previous transcript on a later refresh", async () => {
    setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
    let current: unknown = TRANSCRIPT_V1;
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => current));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "dispatch") });
    await hooks.event({ event: assistantMessageUpdated(SESSION, "first answer", { id: "m1" }) });
    expect(await waitFor(() => nodeFs.existsSync(rawPath()))).toBe(true);

    current = TRANSCRIPT_V2;
    await hooks.event({ event: assistantMessageUpdated(SESSION, "second answer", { id: "m2" }) });

    const expected = JSON.parse(JSON.stringify(TRANSCRIPT_V2));
    expect(await waitFor(() => JSON.stringify(readRaw()) === JSON.stringify(expected))).toBe(true);
  });
});

describe("subagent role - transcript does not depend on the idle path", () => {
  it("keeps the in-session transcript and invocation_end when every SDK call during idle never resolves", async () => {
    setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
    let hang = false;
    const hooks = await startPlugin(
      testEnv.dir,
      makeSdk(() => (hang ? neverResolves() : TRANSCRIPT_V1)),
    );

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "dispatch") });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    expect(await waitFor(() => nodeFs.existsSync(rawPath()) && nodeFs.existsSync(metaPath()))).toBe(true);

    hang = true;
    void hooks.event({ event: sessionIdleEvent(SESSION) });
    await new Promise((r) => setTimeout(r, 50));

    expect(readRaw()).toEqual(JSON.parse(JSON.stringify(TRANSCRIPT_V1)));
    expect(nodeFs.existsSync(metaPath())).toBe(true);
    const types = eventTypes(readJsonl(nodePath.join(invDir(), "03_events.jsonl")));
    expect(types.filter((t) => t === "invocation_end")).toHaveLength(1);
  });
});

describe("subagent role - transcript unavailable", () => {
  const unavailable: Array<[string, () => unknown]> = [
    [
      "messages() throws",
      () => {
        throw new Error("transcript not provided");
      },
    ],
    ["messages() returns null", () => null],
    ["messages() returns undefined", () => undefined],
  ];

  it.each(unavailable)("%s: no transcript files, other output unaffected", async (_name, messages) => {
    setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
    let calls = 0;
    const hooks = await startPlugin(
      testEnv.dir,
      makeSdk(() => {
        calls += 1;
        return messages();
      }),
    );

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "dispatch") });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    await expect(hooks.event({ event: sessionIdleEvent(SESSION) })).resolves.toBeUndefined();

    // The SDK must have been consulted, and still nothing may be written.
    expect(await waitFor(() => calls > 0)).toBe(true);
    await new Promise((r) => setTimeout(r, 100));
    expect(nodeFs.existsSync(rawPath())).toBe(false);
    expect(nodeFs.existsSync(metaPath())).toBe(false);

    const events = readJsonl(nodePath.join(invDir(), "03_events.jsonl"));
    const types = eventTypes(events);
    expect(types[0]).toBe("invocation_start");
    expect(types.filter((t) => t === "invocation_end")).toHaveLength(1);
    const end = events.find((e) => e["event"] === "invocation_end");
    expect(end?.["status_code"]).toBe("SUCCESS");
  });
});

describe("orchestrator role - session-scoped transcript in the real run folder", () => {
  function scopedRaw(sessionId: string): string {
    const scope = transcriptScopeSegment("opencode", sessionId);
    return nodePath.join(runDir(testEnv.dir, RUN_ID), `00_orchestrator_session__${scope}.raw`);
  }
  function scopedMeta(sessionId: string): string {
    const scope = transcriptScopeSegment("opencode", sessionId);
    return nodePath.join(runDir(testEnv.dir, RUN_ID), `00_orchestrator_session__${scope}.meta.json`);
  }

  it("refreshes the transcript under the session-scoped name before any idle", async () => {
    setRunnerEnv({ role: "orchestrator", runId: RUN_ID });
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => TRANSCRIPT_V1));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "dispatch") });
    await hooks.event({ event: assistantMessageUpdated(SESSION, "first answer") });

    expect(await waitFor(() => nodeFs.existsSync(scopedRaw(SESSION)) && nodeFs.existsSync(scopedMeta(SESSION)))).toBe(true);
    expect(JSON.parse(nodeFs.readFileSync(scopedRaw(SESSION), "utf-8"))).toEqual(
      JSON.parse(JSON.stringify(TRANSCRIPT_V1)),
    );
    expect(nodeFs.existsSync(nodePath.join(runDir(testEnv.dir, RUN_ID), "00_orchestrator_session.raw"))).toBe(false);
  });

  it("keeps two files for two sessions in the same run", async () => {
    setRunnerEnv({ role: "orchestrator", runId: RUN_ID });
    const bySession: Record<string, unknown> = {
      "sess-a": [userMessageEntry("question A")],
      "sess-b": [userMessageEntry("question B")],
    };
    const hooks = await startPlugin(testEnv.dir, makeSdk((id) => bySession[id] ?? []));

    for (const id of ["sess-a", "sess-b"]) {
      await hooks.event({ event: sessionCreatedEvent(id) });
      await hooks.event({ event: userMessageUpdated(id, `question ${id}`, `u-${id}`) });
      await hooks.event({ event: assistantMessageUpdated(id, "answer", { id: `a-${id}` }) });
    }

    expect(await waitFor(() => nodeFs.existsSync(scopedRaw("sess-a")) && nodeFs.existsSync(scopedRaw("sess-b")))).toBe(true);
    expect(scopedRaw("sess-a")).not.toBe(scopedRaw("sess-b"));
    expect(nodeFs.readFileSync(scopedRaw("sess-a"), "utf-8")).toContain("question A");
    expect(nodeFs.readFileSync(scopedRaw("sess-b"), "utf-8")).toContain("question B");
  });

  it("writes no transcript file when the SDK provides none", async () => {
    setRunnerEnv({ role: "orchestrator", runId: RUN_ID });
    let calls = 0;
    const hooks = await startPlugin(
      testEnv.dir,
      makeSdk(() => {
        calls += 1;
        return undefined;
      }),
    );

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: assistantMessageUpdated(SESSION, "answer") });

    expect(await waitFor(() => calls > 0)).toBe(true);
    await new Promise((r) => setTimeout(r, 100));
    const names = nodeFs.readdirSync(runDir(testEnv.dir, RUN_ID));
    expect(names.filter((n) => n.includes(".raw") || n.includes(".meta.json"))).toEqual([]);
    const types = eventTypes(readJsonl(nodePath.join(runDir(testEnv.dir, RUN_ID), "00_orchestrator_events.jsonl")));
    expect(types).toContain("session_start");
  });
});
