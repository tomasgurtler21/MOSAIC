/**
 * export.test.ts — Tests for SDK-based session export and .meta.json sidecar.
 *
 * Covers:
 *   - buildSidecar: produces the correct SidecarDocument structure
 *   - exportSession: calls client.session.messages(), writes JSON + .meta.json
 *   - exportSession: graceful degradation when SDK throws (no files written, no error)
 *   - exportSession: graceful degradation when SDK returns no data (no files written)
 *   - exportOrchestratorTranscript: convenience wrapper targeting 00_orchestrator_session.raw
 *
 * All tests fail until the implementation is complete (TDD RED phase).
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import {
  exportSession,
  exportOrchestratorTranscript,
  buildSidecar,
} from "../lib/export";
import { LogPaths } from "../lib/core";
import { userMessageEntry, assistantMessageEntry } from "./fixtures/opencode_api";

// ---------------------------------------------------------------------------
// Test helpers and stub SDK client
// ---------------------------------------------------------------------------

const SESSION_ID = "sess-orch-001";
const SUBAGENT_SESSION_ID = "sess-sub-001";
const RUN_ID = "20260101T170000Z-a3f9";

/**
 * Minimal SdkClient stub that returns controlled responses.
 * All fields are required to match the SdkClient interface contract.
 */
function makeSdkClient(options: {
  messagesResult?: unknown;
  messagesThrows?: Error;
  getResult?: { parentID?: string } | undefined;
} = {}): {
  session: {
    get(params: { path: { id: string } }): Promise<{ parentID?: string } | undefined>;
    messages(params: { path: { id: string } }): Promise<unknown>;
  };
  app: {
    log(params: {
      body: {
        service: string;
        level: "debug" | "info" | "warn" | "error";
        message: string;
        extra?: Record<string, unknown>;
      };
    }): Promise<void>;
  };
} {
  return {
    session: {
      get: async (_params) => options.getResult,
      messages: async (_params) => {
        if (options.messagesThrows) {
          throw options.messagesThrows;
        }
        return options.messagesResult;
      },
    },
    app: {
      log: async (_params) => {},
    },
  };
}

/**
 * Sample message list as the SDK would return from client.session.messages().
 * Built via fixture builders so the real { info, parts } shape is used throughout
 * this file — satisfying the suite-wide constraint that no test constructs a
 * message as { role, content }.
 */
const SAMPLE_MESSAGES = [
  userMessageEntry("Do the task.", { id: "msg-001" }),
  assistantMessageEntry('{"status_code": "SUCCESS"}', { id: "msg-002" }),
];

// ---------------------------------------------------------------------------
// buildSidecar
// ---------------------------------------------------------------------------

describe("buildSidecar", () => {
  const FIXED_TS = "2026-01-01T17:00:00.000Z";
  const BYTE_COUNT = 1234;

  it("returns an object with harness set to opencode", () => {
    const sidecar = buildSidecar({
      sourceMechanism: "sdk_client_session_messages",
      byteCount: BYTE_COUNT,
      capturedAt: FIXED_TS,
    });
    expect(sidecar.harness).toBe("opencode");
  });

  it("returns an object with native_format set to opencode-sdk-session-messages-json", () => {
    const sidecar = buildSidecar({
      sourceMechanism: "sdk_client_session_messages",
      byteCount: BYTE_COUNT,
      capturedAt: FIXED_TS,
    });
    expect(sidecar.native_format).toBe("opencode-sdk-session-messages-json");
  });

  it("returns an object with source_mechanism matching the passed value", () => {
    const sidecar = buildSidecar({
      sourceMechanism: "sdk_client_session_messages",
      byteCount: BYTE_COUNT,
      capturedAt: FIXED_TS,
    });
    expect(sidecar.source_mechanism).toBe("sdk_client_session_messages");
  });

  it("returns an object with captured_at matching the passed timestamp", () => {
    const sidecar = buildSidecar({
      sourceMechanism: "sdk_client_session_messages",
      byteCount: BYTE_COUNT,
      capturedAt: FIXED_TS,
    });
    expect(sidecar.captured_at).toBe(FIXED_TS);
  });

  it("returns an object with byte_count matching the passed value", () => {
    const sidecar = buildSidecar({
      sourceMechanism: "sdk_client_session_messages",
      byteCount: BYTE_COUNT,
      capturedAt: FIXED_TS,
    });
    expect(sidecar.byte_count).toBe(BYTE_COUNT);
  });

  it("returns the full expected sidecar document structure", () => {
    const sidecar = buildSidecar({
      sourceMechanism: "sdk_client_session_messages",
      byteCount: 42,
      capturedAt: FIXED_TS,
    });
    expect(sidecar).toEqual({
      harness: "opencode",
      native_format: "opencode-sdk-session-messages-json",
      source_mechanism: "sdk_client_session_messages",
      captured_at: FIXED_TS,
      byte_count: 42,
    });
  });

  it("accepts a zero byte count (valid for empty response)", () => {
    const sidecar = buildSidecar({
      sourceMechanism: "sdk_client_session_messages",
      byteCount: 0,
      capturedAt: FIXED_TS,
    });
    expect(sidecar.byte_count).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// exportSession — success path
// ---------------------------------------------------------------------------

describe("exportSession — success path", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = nodeFs.mkdtempSync(
      nodePath.join(nodeOs.tmpdir(), "mosaic-export-test-"),
    );
  });

  afterEach(() => {
    nodeFs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("returns an ExportResult with ok: true on success", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    const result = await exportSession(client, SESSION_ID, targetPath);

    expect(result.ok).toBe(true);
  });

  it("calls client.session.messages with the correct session ID", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const calledWith: string[] = [];
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });
    const originalMessages = client.session.messages.bind(client.session);
    client.session.messages = async (params) => {
      calledWith.push(params.path.id);
      return originalMessages(params);
    };

    await exportSession(client, SESSION_ID, targetPath);

    expect(calledWith).toContain(SESSION_ID);
  });

  it("writes the SDK messages as JSON to the target path", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    await exportSession(client, SESSION_ID, targetPath);

    expect(nodeFs.existsSync(targetPath)).toBe(true);
    const written = JSON.parse(nodeFs.readFileSync(targetPath, "utf-8"));
    expect(written).toEqual(SAMPLE_MESSAGES);
  });

  it("writes a .meta.json sidecar alongside the raw file", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const expectedMetaPath = nodePath.join(tempDir, "04_session.meta.json");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    await exportSession(client, SESSION_ID, targetPath);

    expect(nodeFs.existsSync(expectedMetaPath)).toBe(true);
  });

  it("sidecar contains harness: opencode", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const metaPath = nodePath.join(tempDir, "04_session.meta.json");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    await exportSession(client, SESSION_ID, targetPath);

    const meta = JSON.parse(nodeFs.readFileSync(metaPath, "utf-8"));
    expect(meta.harness).toBe("opencode");
  });

  it("sidecar contains native_format: opencode-sdk-session-messages-json", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const metaPath = nodePath.join(tempDir, "04_session.meta.json");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    await exportSession(client, SESSION_ID, targetPath);

    const meta = JSON.parse(nodeFs.readFileSync(metaPath, "utf-8"));
    expect(meta.native_format).toBe("opencode-sdk-session-messages-json");
  });

  it("sidecar contains source_mechanism: sdk_client_session_messages", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const metaPath = nodePath.join(tempDir, "04_session.meta.json");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    await exportSession(client, SESSION_ID, targetPath);

    const meta = JSON.parse(nodeFs.readFileSync(metaPath, "utf-8"));
    expect(meta.source_mechanism).toBe("sdk_client_session_messages");
  });

  it("sidecar contains a captured_at ISO 8601 timestamp", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const metaPath = nodePath.join(tempDir, "04_session.meta.json");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    await exportSession(client, SESSION_ID, targetPath);

    const meta = JSON.parse(nodeFs.readFileSync(metaPath, "utf-8"));
    expect(typeof meta.captured_at).toBe("string");
    expect(meta.captured_at).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
  });

  it("sidecar contains byte_count matching the written raw file size", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const metaPath = nodePath.join(tempDir, "04_session.meta.json");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    await exportSession(client, SESSION_ID, targetPath);

    const rawSize = nodeFs.statSync(targetPath).size;
    const meta = JSON.parse(nodeFs.readFileSync(metaPath, "utf-8"));
    expect(meta.byte_count).toBe(rawSize);
  });

  it("ExportResult includes rawPath pointing to the written file", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    const result = await exportSession(client, SESSION_ID, targetPath);

    expect(result.rawPath).toBe(targetPath);
  });

  it("ExportResult includes metaPath pointing to the sidecar file", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const expectedMetaPath = nodePath.join(tempDir, "04_session.meta.json");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    const result = await exportSession(client, SESSION_ID, targetPath);

    expect(result.metaPath).toBe(expectedMetaPath);
  });

  it("ExportResult includes byteCount matching the raw file size", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    const result = await exportSession(client, SESSION_ID, targetPath);

    const rawSize = nodeFs.statSync(targetPath).size;
    expect(result.byteCount).toBe(rawSize);
  });

  it("creates parent directories for the target path when they do not exist", async () => {
    const nestedPath = nodePath.join(tempDir, "run-abc", "Agent#1", "04_session.raw");
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    const result = await exportSession(client, SESSION_ID, nestedPath);

    expect(result.ok).toBe(true);
    expect(nodeFs.existsSync(nestedPath)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// exportSession — graceful degradation
// ---------------------------------------------------------------------------

describe("exportSession — graceful degradation on SDK failure", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = nodeFs.mkdtempSync(
      nodePath.join(nodeOs.tmpdir(), "mosaic-export-degrade-test-"),
    );
  });

  afterEach(() => {
    nodeFs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("does not throw when SDK client.session.messages() throws", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesThrows: new Error("SDK unavailable") });

    await expect(exportSession(client, SESSION_ID, targetPath)).resolves.not.toThrow();
  });

  it("returns ok: false when SDK throws", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesThrows: new Error("SDK unavailable") });

    const result = await exportSession(client, SESSION_ID, targetPath);

    expect(result.ok).toBe(false);
  });

  it("does not write the raw file when SDK throws", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesThrows: new Error("SDK unavailable") });

    await exportSession(client, SESSION_ID, targetPath);

    expect(nodeFs.existsSync(targetPath)).toBe(false);
  });

  it("does not write the .meta.json sidecar when SDK throws", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const metaPath = nodePath.join(tempDir, "04_session.meta.json");
    const client = makeSdkClient({ messagesThrows: new Error("SDK unavailable") });

    await exportSession(client, SESSION_ID, targetPath);

    expect(nodeFs.existsSync(metaPath)).toBe(false);
  });

  it("does not throw when SDK returns null", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesResult: null });

    await expect(exportSession(client, SESSION_ID, targetPath)).resolves.not.toThrow();
  });

  it("returns ok: false when SDK returns null", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesResult: null });

    const result = await exportSession(client, SESSION_ID, targetPath);

    expect(result.ok).toBe(false);
  });

  it("does not write any files when SDK returns null", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const metaPath = nodePath.join(tempDir, "04_session.meta.json");
    const client = makeSdkClient({ messagesResult: null });

    await exportSession(client, SESSION_ID, targetPath);

    expect(nodeFs.existsSync(targetPath)).toBe(false);
    expect(nodeFs.existsSync(metaPath)).toBe(false);
  });

  it("does not throw when SDK returns undefined", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesResult: undefined });

    await expect(exportSession(client, SESSION_ID, targetPath)).resolves.not.toThrow();
  });

  it("returns ok: false when SDK returns undefined", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesResult: undefined });

    const result = await exportSession(client, SESSION_ID, targetPath);

    expect(result.ok).toBe(false);
  });

  it("does not write any files when SDK returns undefined", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const metaPath = nodePath.join(tempDir, "04_session.meta.json");
    const client = makeSdkClient({ messagesResult: undefined });

    await exportSession(client, SESSION_ID, targetPath);

    expect(nodeFs.existsSync(targetPath)).toBe(false);
    expect(nodeFs.existsSync(metaPath)).toBe(false);
  });

  it("returns a reason string when failing", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesThrows: new Error("network error") });

    const result = await exportSession(client, SESSION_ID, targetPath);

    expect(result.ok).toBe(false);
    expect(typeof result.reason).toBe("string");
    expect(result.reason!.length).toBeGreaterThan(0);
  });

  it("never rejects — always resolves to an ExportResult", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const client = makeSdkClient({ messagesThrows: new Error("catastrophic failure") });

    // Must resolve (not reject) even on catastrophic failure
    const result = await exportSession(client, SESSION_ID, targetPath);
    expect(result).toBeDefined();
    expect(typeof result.ok).toBe("boolean");
  });
});

// ---------------------------------------------------------------------------
// exportOrchestratorTranscript
// ---------------------------------------------------------------------------

describe("exportOrchestratorTranscript", () => {
  let tempDir: string;
  let paths: LogPaths;

  beforeEach(() => {
    tempDir = nodeFs.mkdtempSync(
      nodePath.join(nodeOs.tmpdir(), "mosaic-orch-export-test-"),
    );
    paths = new LogPaths(tempDir);
  });

  afterEach(() => {
    nodeFs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("returns an ExportResult with ok: true on success", async () => {
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    const result = await exportOrchestratorTranscript(
      client,
      SESSION_ID,
      paths,
      RUN_ID,
    );

    expect(result.ok).toBe(true);
  });

  it("writes the session data to the orchestrator raw path", async () => {
    const expectedRawPath = paths.orchestratorRaw(RUN_ID);
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    await exportOrchestratorTranscript(client, SESSION_ID, paths, RUN_ID);

    expect(nodeFs.existsSync(expectedRawPath)).toBe(true);
  });

  it("targets the correct orchestrator raw path: 00_orchestrator_session.raw", async () => {
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    const result = await exportOrchestratorTranscript(
      client,
      SESSION_ID,
      paths,
      RUN_ID,
    );

    expect(result.rawPath).toBe(paths.orchestratorRaw(RUN_ID));
    expect(result.rawPath).toContain("00_orchestrator_session.raw");
  });

  it("writes a sidecar at 00_orchestrator_session.meta.json", async () => {
    const expectedMetaPath = paths.orchestratorRawMeta(RUN_ID);
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    await exportOrchestratorTranscript(client, SESSION_ID, paths, RUN_ID);

    expect(nodeFs.existsSync(expectedMetaPath)).toBe(true);
  });

  it("the sidecar contains harness: opencode", async () => {
    const expectedMetaPath = paths.orchestratorRawMeta(RUN_ID);
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    await exportOrchestratorTranscript(client, SESSION_ID, paths, RUN_ID);

    const meta = JSON.parse(nodeFs.readFileSync(expectedMetaPath, "utf-8"));
    expect(meta.harness).toBe("opencode");
  });

  it("uses the provided orchestratorSessionId when calling the SDK", async () => {
    const calledWith: string[] = [];
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });
    const originalMessages = client.session.messages.bind(client.session);
    client.session.messages = async (params) => {
      calledWith.push(params.path.id);
      return originalMessages(params);
    };

    await exportOrchestratorTranscript(client, SESSION_ID, paths, RUN_ID);

    expect(calledWith).toContain(SESSION_ID);
  });

  it("uses the runId to construct the correct path under OrchestrationLogs", async () => {
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    const result = await exportOrchestratorTranscript(
      client,
      SESSION_ID,
      paths,
      RUN_ID,
    );

    expect(result.rawPath).toContain(RUN_ID);
  });

  it("does not throw when SDK throws — returns ok: false", async () => {
    const client = makeSdkClient({ messagesThrows: new Error("SDK down") });

    await expect(
      exportOrchestratorTranscript(client, SESSION_ID, paths, RUN_ID),
    ).resolves.toBeDefined();

    const result = await exportOrchestratorTranscript(client, SESSION_ID, paths, RUN_ID);
    expect(result.ok).toBe(false);
  });

  it("does not write any files when SDK throws", async () => {
    const client = makeSdkClient({ messagesThrows: new Error("SDK down") });
    const expectedRawPath = paths.orchestratorRaw(RUN_ID);
    const expectedMetaPath = paths.orchestratorRawMeta(RUN_ID);

    await exportOrchestratorTranscript(client, SESSION_ID, paths, RUN_ID);

    expect(nodeFs.existsSync(expectedRawPath)).toBe(false);
    expect(nodeFs.existsSync(expectedMetaPath)).toBe(false);
  });

  it("exports a different session correctly by using a different orchestratorSessionId", async () => {
    const client = makeSdkClient({ messagesResult: [{ id: "different-session-msg" }] });

    const result = await exportOrchestratorTranscript(
      client,
      SUBAGENT_SESSION_ID,
      paths,
      RUN_ID,
    );

    expect(result.ok).toBe(true);
    const rawContent = JSON.parse(nodeFs.readFileSync(result.rawPath!, "utf-8"));
    expect(rawContent).toEqual([{ id: "different-session-msg" }]);
  });
});

// ---------------------------------------------------------------------------
// exportOrchestratorTranscript — bucket scoping wiring (integration)
// ---------------------------------------------------------------------------
// These tests verify that the session id is forwarded through
// exportOrchestratorTranscript to LogPaths.orchestratorRaw, so the degradation
// bucket gets session-scoped filenames on disk. This is the level of coverage
// that was missing from Stage 8: the LogPaths unit was correct, but the call
// site that connects session ids to filenames was untested end-to-end.

describe("exportOrchestratorTranscript — bucket scoping wiring", () => {
  let tempDir: string;
  let paths: LogPaths;

  beforeEach(() => {
    tempDir = nodeFs.mkdtempSync(
      nodePath.join(nodeOs.tmpdir(), "mosaic-scope-wiring-test-"),
    );
    paths = new LogPaths(tempDir);
  });

  afterEach(() => {
    nodeFs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("writes a session-scoped file to the bucket when sessionId is provided", async () => {
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });
    const sessId = "orch-sess-integration-A";

    const result = await exportOrchestratorTranscript(
      client,
      sessId,
      paths,
      "unknown-run",
      sessId,
    );

    expect(result.ok).toBe(true);
    const expectedPath = paths.orchestratorRaw("unknown-run", sessId);
    expect(nodeFs.existsSync(expectedPath)).toBe(true);
    expect(nodePath.basename(expectedPath)).toContain(sessId);
  });

  it("two calls with different session ids each land a distinct file on disk", async () => {
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });
    const sessA = "orch-sess-bucket-A";
    const sessB = "orch-sess-bucket-B";

    await exportOrchestratorTranscript(client, sessA, paths, "unknown-run", sessA);
    await exportOrchestratorTranscript(client, sessB, paths, "unknown-run", sessB);

    const pathA = paths.orchestratorRaw("unknown-run", sessA);
    const pathB = paths.orchestratorRaw("unknown-run", sessB);
    expect(pathA).not.toBe(pathB);
    expect(nodeFs.existsSync(pathA)).toBe(true);
    expect(nodeFs.existsSync(pathB)).toBe(true);
  });

  it("session B's write does not overwrite session A's file", async () => {
    const clientA = makeSdkClient({ messagesResult: [{ sessionLabel: "A" }] });
    const clientB = makeSdkClient({ messagesResult: [{ sessionLabel: "B" }] });
    const sessA = "orch-sess-nooverwrite-A";
    const sessB = "orch-sess-nooverwrite-B";

    await exportOrchestratorTranscript(clientA, sessA, paths, "unknown-run", sessA);
    await exportOrchestratorTranscript(clientB, sessB, paths, "unknown-run", sessB);

    const pathA = paths.orchestratorRaw("unknown-run", sessA);
    const contentsA = JSON.parse(nodeFs.readFileSync(pathA, "utf-8"));
    expect(contentsA).toEqual([{ sessionLabel: "A" }]);
  });

  it("falls back to the unscoped path when no sessionId is provided", async () => {
    const client = makeSdkClient({ messagesResult: SAMPLE_MESSAGES });

    const result = await exportOrchestratorTranscript(
      client,
      SESSION_ID,
      paths,
      "unknown-run",
    );

    expect(result.ok).toBe(true);
    const expectedPath = paths.orchestratorRaw("unknown-run");
    expect(nodeFs.existsSync(expectedPath)).toBe(true);
    expect(nodePath.basename(expectedPath)).toBe("00_orchestrator_session.raw");
  });
});
