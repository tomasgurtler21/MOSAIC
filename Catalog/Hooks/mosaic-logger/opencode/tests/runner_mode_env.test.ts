/**
 * runner_mode_env.test.ts — Mode and identity resolution from process.env.
 *
 * Covers: env parsing (role, run id validity, instance id, agent type),
 * native mode staying unchanged when no role is set, role-based
 * classification of a top-level session, and MOSAIC_RUN_ID precedence.
 */

import { describe, it, expect } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";

import { readRunnerMode } from "../lib/runner_mode";
import { sessionCreatedEvent } from "./fixtures/opencode_api";
import {
  installRunnerTestEnv,
  setRunnerEnv,
  makeSdk,
  startPlugin,
  userMessageUpdated,
  readJsonl,
  eventTypes,
  runDir,
  RUN_ID,
  INSTANCE_ID,
  SESSION,
} from "./runner_mode_support";

const testEnv = installRunnerTestEnv();

describe("readRunnerMode", () => {
  it("returns undefined (native) when MOSAIC_ROLE is absent", () => {
    expect(readRunnerMode({ MOSAIC_RUN_ID: RUN_ID })).toBeUndefined();
  });

  it("returns undefined (native) when MOSAIC_ROLE is empty", () => {
    expect(readRunnerMode({ MOSAIC_ROLE: "", MOSAIC_RUN_ID: RUN_ID })).toBeUndefined();
  });

  it("resolves the subagent role with run id, instance id and agent type", () => {
    const mode = readRunnerMode({
      MOSAIC_ROLE: "subagent",
      MOSAIC_RUN_ID: RUN_ID,
      MOSAIC_AGENT_INSTANCE_ID: "Research#3",
    });
    expect(mode).toEqual({
      role: "subagent",
      runId: RUN_ID,
      agentInstanceId: "Research#3",
      agentType: "Research",
    });
  });

  it("uses the part before the LAST hash as agent type", () => {
    const mode = readRunnerMode({
      MOSAIC_ROLE: "subagent",
      MOSAIC_AGENT_INSTANCE_ID: "Team#Sub#4",
    });
    expect(mode?.agentType).toBe("Team#Sub");
  });

  it("omits agent type when nothing precedes the hash", () => {
    const mode = readRunnerMode({
      MOSAIC_ROLE: "subagent",
      MOSAIC_AGENT_INSTANCE_ID: "#4",
    });
    expect(mode?.role).toBe("subagent");
    expect(mode?.agentType).toBeUndefined();
  });

  it("resolves the orchestrator role and never carries an instance id", () => {
    const mode = readRunnerMode({
      MOSAIC_ROLE: "orchestrator",
      MOSAIC_RUN_ID: RUN_ID,
      MOSAIC_AGENT_INSTANCE_ID: "Research#3",
    });
    expect(mode?.role).toBe("orchestrator");
    expect(mode?.runId).toBe(RUN_ID);
    expect(mode?.agentInstanceId).toBeUndefined();
  });

  it("treats an unrecognised role as orchestrator", () => {
    const mode = readRunnerMode({ MOSAIC_ROLE: "banana", MOSAIC_RUN_ID: RUN_ID });
    expect(mode?.role).toBe("orchestrator");
  });

  it("treats subagent without an instance id as orchestrator", () => {
    expect(readRunnerMode({ MOSAIC_ROLE: "subagent" })?.role).toBe("orchestrator");
    expect(
      readRunnerMode({ MOSAIC_ROLE: "subagent", MOSAIC_AGENT_INSTANCE_ID: "" })?.role,
    ).toBe("orchestrator");
  });

  it.each([
    ["empty", ""],
    ["unknown-run", "unknown-run"],
    ["path traversal", "../x"],
    ["uppercase hex", "20261003T185044Z-07E9"],
    ["trailing path segment", "20261003T185044Z-07e9/x"],
    ["surrounding whitespace", " 20261003T185044Z-07e9"],
  ])("rejects an invalid MOSAIC_RUN_ID (%s) but keeps the role", (_name, value) => {
    const mode = readRunnerMode({
      MOSAIC_ROLE: "subagent",
      MOSAIC_RUN_ID: value,
      MOSAIC_AGENT_INSTANCE_ID: INSTANCE_ID,
    });
    expect(mode?.role).toBe("subagent");
    expect(mode?.runId).toBeUndefined();
  });

  it("reads process.env when no env argument is given", () => {
    setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
    expect(readRunnerMode()?.agentInstanceId).toBe(INSTANCE_ID);
  });
});

describe("native mode (no MOSAIC_ROLE) is unchanged", () => {
  it("classifies a top-level session as orchestrator: session_start and run_start, no invocation folder", async () => {
    // MOSAIC_RUN_ID alone (as set by AgentTest) must not switch to Runner mode
    setRunnerEnv({ runId: RUN_ID });
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => []));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });

    const logsRoot = nodePath.join(testEnv.dir, "OrchestrationLogs");
    const entries = nodeFs.readdirSync(logsRoot, { recursive: true }).map(String);
    const orchEvents = entries.find((e) => e.endsWith("00_orchestrator_events.jsonl"));
    expect(orchEvents).toBeDefined();
    const types = eventTypes(readJsonl(nodePath.join(logsRoot, orchEvents as string)));
    expect(types).toContain("session_start");
    expect(types).toContain("run_start");
    expect(entries.some((e) => e.endsWith("03_events.jsonl"))).toBe(false);
  });
});

describe("Runner-mode classification of a top-level session", () => {
  it("subagent role: the session is one invocation, not an orchestrator session", async () => {
    setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => []));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "Do the research") });

    const run = runDir(testEnv.dir, RUN_ID);
    const invEvents = readJsonl(nodePath.join(run, INSTANCE_ID, "03_events.jsonl"));
    expect(eventTypes(invEvents)[0]).toBe("invocation_start");
    expect(nodeFs.existsSync(nodePath.join(run, "00_orchestrator_events.jsonl"))).toBe(false);
  });

  it("orchestrator role: the session is a run-level session with no invocation folder", async () => {
    setRunnerEnv({ role: "orchestrator", runId: RUN_ID });
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => []));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });

    const run = runDir(testEnv.dir, RUN_ID);
    const types = eventTypes(readJsonl(nodePath.join(run, "00_orchestrator_events.jsonl")));
    expect(types).toContain("session_start");
    const dirs = nodeFs.readdirSync(run, { withFileTypes: true }).filter((d) => d.isDirectory());
    expect(dirs).toHaveLength(0);
  });
});

describe("MOSAIC_RUN_ID precedence in Runner mode", () => {
  const OTHER_RUN_ID = "20260101T170000Z-a3f9";
  const PROMPT_WITH_OTHER_RUN = JSON.stringify({
    agent_instance_id: "Other#9",
    run_id: OTHER_RUN_ID,
    task_description: "Research",
  });

  it("subagent role: logs go to the env run id even when the prompt names another run", async () => {
    setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => []));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT_WITH_OTHER_RUN) });

    expect(
      nodeFs.existsSync(nodePath.join(runDir(testEnv.dir, RUN_ID), INSTANCE_ID, "03_events.jsonl")),
    ).toBe(true);
    expect(nodeFs.existsSync(runDir(testEnv.dir, OTHER_RUN_ID))).toBe(false);
  });

  it("subagent role: the env instance id names the folder even when the prompt names another instance", async () => {
    setRunnerEnv({ role: "subagent", runId: RUN_ID, instanceId: INSTANCE_ID });
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => []));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT_WITH_OTHER_RUN) });

    expect(nodeFs.existsSync(nodePath.join(runDir(testEnv.dir, RUN_ID), INSTANCE_ID))).toBe(true);
    expect(nodeFs.existsSync(nodePath.join(runDir(testEnv.dir, RUN_ID), "Other#9"))).toBe(false);
  });

  it("orchestrator role: run-level events go to the env run id", async () => {
    setRunnerEnv({ role: "orchestrator", runId: RUN_ID });
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => []));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, PROMPT_WITH_OTHER_RUN) });

    expect(
      nodeFs.existsSync(nodePath.join(runDir(testEnv.dir, RUN_ID), "00_orchestrator_events.jsonl")),
    ).toBe(true);
    expect(nodeFs.existsSync(runDir(testEnv.dir, OTHER_RUN_ID))).toBe(false);
  });

  it("subagent role with an invalid MOSAIC_RUN_ID still honours the role and falls back to native run id resolution", async () => {
    setRunnerEnv({ role: "subagent", runId: "../x", instanceId: INSTANCE_ID });
    const hooks = await startPlugin(testEnv.dir, makeSdk(() => []));

    await hooks.event({ event: sessionCreatedEvent(SESSION) });
    await hooks.event({ event: userMessageUpdated(SESSION, "no run id in this prompt") });

    const bucket = nodePath.join(testEnv.dir, "OrchestrationLogs", "unknown-run");
    const invEvents = readJsonl(nodePath.join(bucket, INSTANCE_ID, "03_events.jsonl"));
    expect(eventTypes(invEvents)[0]).toBe("invocation_start");
    expect(nodeFs.existsSync(nodePath.join(bucket, "00_orchestrator_events.jsonl"))).toBe(false);
  });
});
