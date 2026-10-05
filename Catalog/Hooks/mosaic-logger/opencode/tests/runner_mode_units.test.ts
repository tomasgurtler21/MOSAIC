/**
 * runner_mode_units.test.ts — Unit tests for the Runner-mode support modules:
 * the synchronous JSONL append and the coalescing transcript refresher.
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import { appendEventSync } from "../lib/sync_append";
import {
  createTranscriptRefresher,
  runnerOrchestratorRawPath,
} from "../lib/runner_transcript";
import { LogPaths, transcriptScopeSegment, setDebugLogger } from "../lib/core";
import type { SdkClient } from "../lib/handlers_session";
import { RUN_ID } from "./runner_mode_support";

let tmpDir: string;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "mosaic-runner-unit-"));
  setDebugLogger(() => {});
});

afterEach(() => {
  nodeFs.rmSync(tmpDir, { recursive: true, force: true });
});

function sdkReturning(messages: () => unknown): SdkClient {
  return {
    session: { get: async () => undefined, messages: async () => messages() as never },
    app: { log: async () => {} },
  };
}

describe("appendEventSync", () => {
  it("creates missing parent directories and appends one JSON line", () => {
    const file = nodePath.join(tmpDir, "a", "b", "03_events.jsonl");

    const ok = appendEventSync(file, { event: "invocation_end", agent_instance_id: "X#1" });

    expect(ok).toBe(true);
    const lines = nodeFs.readFileSync(file, "utf-8").split("\n").filter(Boolean);
    expect(lines).toHaveLength(1);
    expect(JSON.parse(lines[0])).toEqual({ event: "invocation_end", agent_instance_id: "X#1" });
  });

  it("appends to an existing file, one line per call, preserving earlier lines", () => {
    const file = nodePath.join(tmpDir, "events.jsonl");

    appendEventSync(file, { n: 1 });
    appendEventSync(file, { n: 2 });

    const parsed = nodeFs.readFileSync(file, "utf-8").split("\n").filter(Boolean).map((l) => JSON.parse(l));
    expect(parsed).toEqual([{ n: 1 }, { n: 2 }]);
  });

  it("terminates every line with a newline", () => {
    const file = nodePath.join(tmpDir, "events.jsonl");

    appendEventSync(file, { n: 1 });

    expect(nodeFs.readFileSync(file, "utf-8").endsWith("\n")).toBe(true);
  });

  it("returns false without throwing when the path cannot be written", () => {
    const blocker = nodePath.join(tmpDir, "a-file");
    nodeFs.writeFileSync(blocker, "x");

    const ok = appendEventSync(nodePath.join(blocker, "sub", "events.jsonl"), { n: 1 });

    expect(ok).toBe(false);
  });
});

describe("runnerOrchestratorRawPath", () => {
  it("is the session-scoped transcript name inside the run folder", () => {
    const paths = new LogPaths(tmpDir);

    const p = runnerOrchestratorRawPath(paths, RUN_ID, "sess-1");

    const scope = transcriptScopeSegment("opencode", "sess-1");
    expect(p).toBe(nodePath.join(paths.runRoot(RUN_ID), `00_orchestrator_session__${scope}.raw`));
  });

  it("differs per session", () => {
    const paths = new LogPaths(tmpDir);

    expect(runnerOrchestratorRawPath(paths, RUN_ID, "sess-1")).not.toBe(
      runnerOrchestratorRawPath(paths, RUN_ID, "sess-2"),
    );
  });
});

describe("createTranscriptRefresher", () => {
  const messagesV1 = [{ info: { id: "m1" } }];
  const messagesV2 = [{ info: { id: "m1" } }, { info: { id: "m2" } }];

  it("writes the raw file and its sidecar once settled", async () => {
    const target = nodePath.join(tmpDir, "inv", "04_session.raw");
    const refresher = createTranscriptRefresher(sdkReturning(() => messagesV1), "s1", target);

    refresher.request();
    await refresher.settled();

    expect(JSON.parse(nodeFs.readFileSync(target, "utf-8"))).toEqual(messagesV1);
    expect(nodeFs.existsSync(nodePath.join(tmpDir, "inv", "04_session.meta.json"))).toBe(true);
  });

  it("replaces the file on a later request", async () => {
    const target = nodePath.join(tmpDir, "inv", "04_session.raw");
    let current: unknown = messagesV1;
    const refresher = createTranscriptRefresher(sdkReturning(() => current), "s1", target);

    refresher.request();
    await refresher.settled();
    current = messagesV2;
    refresher.request();
    await refresher.settled();

    expect(JSON.parse(nodeFs.readFileSync(target, "utf-8"))).toEqual(messagesV2);
  });

  it("coalesces requests: never two exports in flight, one pending export captures the latest state", async () => {
    const target = nodePath.join(tmpDir, "inv", "04_session.raw");
    let calls = 0;
    let inFlight = 0;
    let maxInFlight = 0;
    let releaseFirst: () => void = () => {};
    const gate = new Promise<void>((resolve) => {
      releaseFirst = resolve;
    });
    let current: unknown = messagesV1;
    const sdk: SdkClient = {
      session: {
        get: async () => undefined,
        messages: async () => {
          calls += 1;
          inFlight += 1;
          maxInFlight = Math.max(maxInFlight, inFlight);
          const snapshot = current;
          if (calls === 1) await gate;
          inFlight -= 1;
          return snapshot as never;
        },
      },
      app: { log: async () => {} },
    };
    const refresher = createTranscriptRefresher(sdk, "s1", target);

    refresher.request();
    await new Promise((r) => setTimeout(r, 20));
    current = messagesV2;
    for (let i = 0; i < 5; i++) refresher.request();
    releaseFirst();
    await refresher.settled();

    expect(maxInFlight).toBe(1);
    expect(calls).toBe(2);
    expect(JSON.parse(nodeFs.readFileSync(target, "utf-8"))).toEqual(messagesV2);
  });

  it("settled resolves immediately when nothing was requested", async () => {
    const target = nodePath.join(tmpDir, "inv", "04_session.raw");
    const refresher = createTranscriptRefresher(sdkReturning(() => messagesV1), "s1", target);

    await expect(refresher.settled()).resolves.toBeUndefined();
    expect(nodeFs.existsSync(target)).toBe(false);
  });

  const unavailable: Array<[string, SdkClient | undefined]> = [
    [
      "messages() throws",
      sdkReturning(() => {
        throw new Error("boom");
      }),
    ],
    ["messages() returns null", sdkReturning(() => null)],
    ["messages() returns undefined", sdkReturning(() => undefined)],
    ["no SDK client", undefined],
  ];

  it.each(unavailable)("%s: writes nothing and never throws or rejects", async (_name, sdk) => {
    const target = nodePath.join(tmpDir, "inv", "04_session.raw");
    const refresher = createTranscriptRefresher(sdk, "s1", target);

    expect(() => refresher.request()).not.toThrow();
    await expect(refresher.settled()).resolves.toBeUndefined();

    expect(nodeFs.existsSync(target)).toBe(false);
    expect(nodeFs.existsSync(nodePath.join(tmpDir, "inv", "04_session.meta.json"))).toBe(false);
  });

  it("keeps the previous transcript when a later refresh finds no data", async () => {
    const target = nodePath.join(tmpDir, "inv", "04_session.raw");
    let current: unknown = messagesV1;
    const refresher = createTranscriptRefresher(sdkReturning(() => current), "s1", target);

    refresher.request();
    await refresher.settled();
    current = null;
    refresher.request();
    await refresher.settled();

    expect(JSON.parse(nodeFs.readFileSync(target, "utf-8"))).toEqual(messagesV1);
  });
});
