/**
 * export_sync_write.test.ts -- Completion guarantee of exportSession.
 *
 * Once the SDK session.messages() promise settles, exportSession must finish
 * its writes (directory, raw file, sidecar) without yielding to the event loop,
 * so a process that exits right after the SDK call cannot leave a half-written
 * export. The sidecar is written last and acts as the commit marker.
 *
 * Covers:
 *   - raw file and sidecar both exist at the first reaction after the SDK promise
 *   - no .tmp_* entry remains after success or after a failed rename
 *   - failed rename returns ok: false and never throws
 *   - the contract (sidecar fields, file names) still holds on this path
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import { exportSession } from "../lib/export";
import { makeSdkClient, SAMPLE_MESSAGES } from "./export_support";

const SESSION_ID = "sess-sync-001";

function tmpEntries(dir: string): string[] {
  return nodeFs.readdirSync(dir).filter((name) => name.startsWith(".tmp_"));
}

describe("exportSession synchronous write after SDK result", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "export-sync-"));
  });

  afterEach(() => {
    nodeFs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("has written raw file and sidecar at the first reaction after the SDK promise resolves", async () => {
    // Arrange: a reaction registered on the same promise after exportSession
    // was invoked runs after the export's own continuation. Only files written
    // synchronously within that continuation are visible to it.
    let resolveMessages!: (value: unknown) => void;
    const messagesPromise = new Promise<unknown>((resolve) => {
      resolveMessages = resolve;
    });
    const targetPath = nodePath.join(tempDir, "nested", "04_session.raw");
    const metaPath = nodePath.join(tempDir, "nested", "04_session.meta.json");
    const client = makeSdkClient({ messagesPromise });

    // Act
    const exportPromise = exportSession(client, SESSION_ID, targetPath);
    const observed = messagesPromise.then(() => ({
      rawExists: nodeFs.existsSync(targetPath),
      metaExists: nodeFs.existsSync(metaPath),
      rawSize: nodeFs.existsSync(targetPath) ? nodeFs.statSync(targetPath).size : -1,
      metaText: nodeFs.existsSync(metaPath) ? nodeFs.readFileSync(metaPath, "utf-8") : "",
    }));
    resolveMessages(SAMPLE_MESSAGES);
    const snapshot = await observed;
    const result = await exportPromise;

    // Assert
    expect(snapshot.rawExists).toBe(true);
    expect(snapshot.metaExists).toBe(true);
    expect(JSON.parse(snapshot.metaText).byte_count).toBe(snapshot.rawSize);
    expect(result.ok).toBe(true);
  });

  it("leaves no .tmp_* entry in the target directory after a successful export", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");

    const result = await exportSession(
      makeSdkClient({ messagesResult: SAMPLE_MESSAGES }),
      SESSION_ID,
      targetPath,
    );

    expect(result.ok).toBe(true);
    expect(tmpEntries(tempDir)).toEqual([]);
  });

  it("returns ok: false with a reason and leaves no .tmp_* entry when the raw rename fails", async () => {
    // Arrange: the target path is an existing non-empty directory, so renaming
    // a file over it fails on both Windows and POSIX.
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    nodeFs.mkdirSync(targetPath);
    nodeFs.writeFileSync(nodePath.join(targetPath, "occupant.txt"), "x");

    // Act
    const result = await exportSession(
      makeSdkClient({ messagesResult: SAMPLE_MESSAGES }),
      SESSION_ID,
      targetPath,
    );

    // Assert
    expect(result.ok).toBe(false);
    expect(result.reason).toBe("Failed to write raw export file");
    expect(nodeFs.existsSync(nodePath.join(tempDir, "04_session.meta.json"))).toBe(false);
    expect(tmpEntries(tempDir)).toEqual([]);
  });

  it("returns ok: false with the sidecar reason and no .tmp_* entry when the sidecar rename fails", async () => {
    // Arrange: occupy the sidecar path with a non-empty directory.
    const targetPath = nodePath.join(tempDir, "04_session.raw");
    const metaDir = nodePath.join(tempDir, "04_session.meta.json");
    nodeFs.mkdirSync(metaDir);
    nodeFs.writeFileSync(nodePath.join(metaDir, "occupant.txt"), "x");

    // Act
    const result = await exportSession(
      makeSdkClient({ messagesResult: SAMPLE_MESSAGES }),
      SESSION_ID,
      targetPath,
    );

    // Assert
    expect(result.ok).toBe(false);
    expect(result.reason).toBe("Failed to write sidecar file");
    expect(tmpEntries(tempDir)).toEqual([]);
  });

  it("writes a sidecar with the unchanged format fields and matching byte count on success", async () => {
    const targetPath = nodePath.join(tempDir, "04_session.raw");

    const result = await exportSession(
      makeSdkClient({ messagesResult: SAMPLE_MESSAGES }),
      SESSION_ID,
      targetPath,
    );

    expect(result.ok).toBe(true);
    expect(result.rawPath).toBe(targetPath);
    expect(result.metaPath).toBe(nodePath.join(tempDir, "04_session.meta.json"));
    const sidecar = JSON.parse(nodeFs.readFileSync(result.metaPath!, "utf-8"));
    expect(sidecar.native_format).toBe("opencode-sdk-session-messages-json");
    expect(sidecar.source_mechanism).toBe("sdk_client_session_messages");
    expect(sidecar.byte_count).toBe(nodeFs.statSync(targetPath).size);
    expect(result.byteCount).toBe(sidecar.byte_count);
  });
});
