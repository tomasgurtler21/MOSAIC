/**
 * runner_mode_support.ts — Shared helpers for the Runner-mode test files.
 *
 * Drives the real plugin factory with MOSAIC_* environment variables set, so
 * the tests specify observable on-disk behaviour rather than module internals.
 * Every test that uses installRunnerTestEnv gets a fresh temp workspace and a
 * process.env restored to its original state afterwards.
 */

import { beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import { MosaicLogger } from "../plugin";
import { setDebugLogger } from "../lib/core";
import type {
  OpenCodeEvent,
  SdkClient,
  ToolAfterInput,
  ToolAfterOutput,
  ToolBeforeInput,
  ToolBeforeOutput,
} from "../lib/handlers_session";
import {
  userMessageEntry,
  assistantMessageEntry,
  messageUpdatedEvent,
} from "./fixtures/opencode_api";

export const RUN_ID = "20261003T185044Z-07e9";
export const INSTANCE_ID = "Research#3";
export const SESSION = "sess-runner-001";

export const FINAL_RESPONSE = `Here are the findings.

{"status_code": "SUCCESS", "status_message": "Research complete."}`;

const ENV_KEYS = ["MOSAIC_ROLE", "MOSAIC_RUN_ID", "MOSAIC_AGENT_INSTANCE_ID"];

export interface RunnerTestEnv {
  /** Fresh temp workspace root for the current test. */
  dir: string;
}

/** Registers per-test temp dir creation and process.env snapshot/restore. */
export function installRunnerTestEnv(): RunnerTestEnv {
  const env: RunnerTestEnv = { dir: "" };
  let saved: Record<string, string | undefined> = {};

  beforeEach(() => {
    env.dir = nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "mosaic-runner-test-"));
    saved = {};
    for (const k of ENV_KEYS) {
      saved[k] = process.env[k];
      delete process.env[k];
    }
    setDebugLogger(() => {});
  });

  afterEach(() => {
    for (const k of ENV_KEYS) {
      if (saved[k] === undefined) delete process.env[k];
      else process.env[k] = saved[k];
    }
    nodeFs.rmSync(env.dir, { recursive: true, force: true });
    setDebugLogger(() => {});
  });

  return env;
}

/** Set MOSAIC_* variables for the current test (undefined values are left unset). */
export function setRunnerEnv(vars: {
  role?: string;
  runId?: string;
  instanceId?: string;
}): void {
  if (vars.role !== undefined) process.env["MOSAIC_ROLE"] = vars.role;
  if (vars.runId !== undefined) process.env["MOSAIC_RUN_ID"] = vars.runId;
  if (vars.instanceId !== undefined) process.env["MOSAIC_AGENT_INSTANCE_ID"] = vars.instanceId;
}

export type MessagesFn = (sessionId: string) => unknown | Promise<unknown>;

/** SDK client whose session.messages() delegates to the given function. */
export function makeSdk(messages: MessagesFn): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async ({ path }) => messages(path.id) as never,
    },
    app: { log: async () => {} },
  };
}

export interface PluginHooks {
  event(input: { event: OpenCodeEvent }): Promise<void>;
  "tool.execute.before"(i: ToolBeforeInput, o: ToolBeforeOutput): Promise<void>;
  "tool.execute.after"(i: ToolAfterInput, o: ToolAfterOutput): Promise<void>;
}

/** Create the plugin with the current process.env (read once at factory time). */
export async function startPlugin(dir: string, sdk: SdkClient): Promise<PluginHooks> {
  const hooks = await MosaicLogger({ directory: dir, client: sdk });
  return hooks as unknown as PluginHooks;
}

// --- Bus event builders with explicit session identity -----------------------

export function userMessageUpdated(sessionId: string, text: string, id = "msg-user-1"): OpenCodeEvent {
  return messageUpdatedEvent(userMessageEntry(text, { id, sessionID: sessionId }));
}

export function assistantMessageUpdated(
  sessionId: string,
  text: string,
  opts: { id?: string; completed?: boolean } = {},
): OpenCodeEvent {
  const time = opts.completed === false ? { created: 1000 } : { created: 1000, completed: 2000 };
  return messageUpdatedEvent(
    assistantMessageEntry(text, { id: opts.id ?? "msg-asst-1", sessionID: sessionId, time }),
  );
}

// --- Disk readers -------------------------------------------------------------

export function readJsonl(filePath: string): Record<string, unknown>[] {
  if (!nodeFs.existsSync(filePath)) return [];
  return nodeFs
    .readFileSync(filePath, "utf-8")
    .split("\n")
    .filter((l) => l.trim() !== "")
    .map((l) => JSON.parse(l) as Record<string, unknown>);
}

export function eventTypes(events: Record<string, unknown>[]): string[] {
  return events.map((e) => String(e["event"]));
}

export function runDir(dir: string, runId: string): string {
  return nodePath.join(dir, "OrchestrationLogs", runId);
}

/** Poll until the predicate holds; resolves false on timeout. */
export async function waitFor(pred: () => boolean, timeoutMs = 2000): Promise<boolean> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (pred()) return true;
    await new Promise((r) => setTimeout(r, 10));
  }
  return pred();
}

/** A promise that never settles, for simulating post-exit-lost SDK calls. */
export function neverResolves(): Promise<never> {
  return new Promise<never>(() => {});
}
