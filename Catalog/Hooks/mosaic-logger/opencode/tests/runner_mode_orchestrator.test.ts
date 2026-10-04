/**
 * runner_mode_orchestrator.test.ts — Orchestrator-role sessions in Runner mode.
 *
 * A top-level session run with MOSAIC_ROLE=orchestrator writes run-level
 * events as a native top-level session does, except that the Runner owns
 * run_start/run_end, so the plugin never writes them. session_end is written
 * exactly once and synchronously, before the first await of the idle path.
 */

import { describe, it, expect } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";

import {
  sessionCreatedEvent,
  sessionIdleEvent,
  sessionDeletedEvent,
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
  neverResolves,
  RUN_ID,
  SESSION,
  FINAL_RESPONSE,
} from "./runner_mode_support";

const testEnv = installRunnerTestEnv();

function orchestratorEvents(): Record<string, unknown>[] {
  return readJsonl(nodePath.join(runDir(testEnv.dir, RUN_ID), "00_orchestrator_events.jsonl"));
}

async function startOrchestratorPlugin(messages: () => unknown = () => []) {
  setRunnerEnv({ role: "orchestrator", runId: RUN_ID });
  return startPlugin(testEnv.dir, makeSdk(messages));
}

describe("orchestrator role - run-level events without run_start/run_end", () => {
  it("writes session_start, turns and session_end, but no run_start or run_end", async () => {
    const hooks = await startOrchestratorPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "Plan the work") });
    await hooks.event({ event: assistantMessageUpdated(SESSION, FINAL_RESPONSE) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const types = eventTypes(orchestratorEvents());
    expect(types[0]).toBe("session_start");
    expect(types).toContain("turn");
    expect(types).toContain("usage_record");
    expect(types[types.length - 1]).toBe("session_end");
    expect(types).not.toContain("run_start");
    expect(types).not.toContain("run_end");
  });

  it("puts session_id and run_id in every envelope", async () => {
    const hooks = await startOrchestratorPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "Plan the work") });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    expect(orchestratorEvents().length).toBeGreaterThan(0);
    for (const e of orchestratorEvents()) {
      expect(e["session_id"]).toBe(SESSION);
      expect(e["run_id"]).toBe(RUN_ID);
    }
  });

  it("writes no invocation_start or invocation_end", async () => {
    const hooks = await startOrchestratorPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const types = eventTypes(orchestratorEvents());
    expect(types).toContain("session_end");
    expect(types).not.toContain("invocation_start");
    expect(types).not.toContain("invocation_end");
  });

  it("writes exactly one session_end for idle followed by deleted", async () => {
    const hooks = await startOrchestratorPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });
    await hooks.event({ event: sessionIdleEvent(SESSION) });
    await hooks.event({ event: sessionDeletedEvent(SESSION) });

    const ends = eventTypes(orchestratorEvents()).filter((t) => t === "session_end");
    expect(ends).toHaveLength(1);
  });

  it("writes exactly one session_end when deleted arrives without idle", async () => {
    const hooks = await startOrchestratorPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: sessionDeletedEvent(SESSION) });

    const types = eventTypes(orchestratorEvents());
    expect(types.filter((t) => t === "session_end")).toHaveLength(1);
    expect(types).not.toContain("run_end");
  });

  it("has session_end on disk synchronously, before any await, with a never-resolving SDK", async () => {
    const hooks = await startOrchestratorPlugin(() => neverResolves());

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    void hooks.event({ event: sessionIdleEvent(SESSION) });

    const types = eventTypes(orchestratorEvents());
    expect(types).toContain("session_end");
    expect(types).not.toContain("run_end");
  });

  it("creates no invocation folder", async () => {
    const hooks = await startOrchestratorPlugin();

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "Plan the work") });
    await hooks.event({ event: sessionIdleEvent(SESSION) });

    const dirs = nodeFs
      .readdirSync(runDir(testEnv.dir, RUN_ID), { withFileTypes: true })
      .filter((d) => d.isDirectory());
    expect(dirs).toHaveLength(0);
  });
});
