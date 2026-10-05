/**
 * handlers_invocation_end_identity.test.ts - handleInvocationEnd for a started
 * invocation whose session record carries no agentInstanceId.
 *
 * Such a record resolves one fallback instance id, and every output of the call
 * (event sink, invocation_end field, 02_output.md, 04_session.raw) uses it.
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import { createInvocationHandlers } from "../lib/handlers_invocation";
import { SessionCorrelationStore } from "../lib/correlation";
import { LogPaths, setDebugLogger } from "../lib/core";
import { userMessageEntry, assistantMessageEntry } from "./fixtures/opencode_api";
import { makeSdk, makeDeps, type CollectedEvent } from "./handlers_invocation_support";

const RUN_ID = "20260101T170000Z-a3f9";
const SESSION = "sess-noid-001";
const FALLBACK_ID_FORMAT = /^agent_[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$/;

let tmpDir: string;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "mosaic-hi-identity-"));
  setDebugLogger(() => {});
});

afterEach(() => {
  nodeFs.rmSync(tmpDir, { recursive: true, force: true });
  setDebugLogger(() => {});
});

/** Run handleInvocationEnd for a started session with no agentInstanceId. */
async function endSessionWithoutId(): Promise<CollectedEvent[]> {
  const store = new SessionCorrelationStore();
  const collected: CollectedEvent[] = [];
  const sdk = makeSdk(() => [
    userMessageEntry("Do the task."),
    assistantMessageEntry('Done.\n\n{"status_code": "SUCCESS", "status_message": "ok"}'),
  ]);
  const { handleInvocationEnd } = createInvocationHandlers(
    makeDeps(store, new LogPaths(tmpDir), sdk, collected),
  );
  store.register(SESSION, { invocationStarted: true, runId: RUN_ID });

  await handleInvocationEnd(SESSION);
  return collected;
}

function invocationEnd(collected: CollectedEvent[]): CollectedEvent {
  const found = collected.find((c) => c.event.event === "invocation_end");
  expect(found).toBeDefined();
  return found as CollectedEvent;
}

describe("handleInvocationEnd - started session without agentInstanceId", () => {
  it("emits invocation_end into a fallback-id folder whose name equals agent_instance_id", async () => {
    const collected = await endSessionWithoutId();

    const { filePath, event } = invocationEnd(collected);
    const folder = nodePath.basename(nodePath.dirname(filePath));

    expect(folder).not.toBe("_");
    expect(event.agent_instance_id).toMatch(FALLBACK_ID_FORMAT);
    expect(event.agent_instance_id).toBe(folder);
  });

  it("writes 02_output.md and 04_session.raw into that same instance folder, not _", async () => {
    const collected = await endSessionWithoutId();

    const { filePath } = invocationEnd(collected);
    const instanceDir = nodePath.dirname(filePath);
    const runDir = nodePath.dirname(instanceDir);

    expect(nodeFs.existsSync(nodePath.join(instanceDir, "02_output.md"))).toBe(true);
    expect(nodeFs.existsSync(nodePath.join(instanceDir, "04_session.raw"))).toBe(true);
    expect(nodeFs.existsSync(nodePath.join(runDir, "_"))).toBe(false);
    const entries = nodeFs.readdirSync(runDir);
    expect(entries).toContain(nodePath.basename(instanceDir));
    expect(entries).not.toContain("_");
  });
});
