/**
 * export.ts — SDK-based session export and .meta.json sidecar.
 *
 * Retrieves session message history via the OpenCode SDK client, writes raw
 * JSON to disk, and writes a .meta.json sidecar. Never throws -- all failures
 * are swallowed and returned as ExportResult with ok: false.
 */

import * as nodeFsPromises from "node:fs/promises";
import * as nodePath from "node:path";
import { atomicReplace, currentTimestamp, debugLog, LogPaths } from "./core";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface SdkClient {
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
}

interface ExportResult {
  ok: boolean;
  rawPath?: string;
  metaPath?: string;
  byteCount?: number;
  reason?: string;
}

interface SidecarDocument {
  harness: string;
  native_format: string;
  source_mechanism: string;
  captured_at: string;
  byte_count: number;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Derive the .meta.json sidecar path from a .raw target path.
 * Replaces the trailing .raw extension with .meta.json.
 */
function metaPathFor(rawPath: string): string {
  return rawPath.replace(/\.raw$/, ".meta.json");
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Build the sidecar metadata document. Exposed separately for testability.
 */
export function buildSidecar(params: {
  sourceMechanism: string;
  byteCount: number;
  capturedAt: string;
}): SidecarDocument {
  return {
    harness: "opencode",
    native_format: "opencode-sdk-session-messages-json",
    source_mechanism: params.sourceMechanism,
    captured_at: params.capturedAt,
    byte_count: params.byteCount,
  };
}

/**
 * Export a session's messages via the OpenCode SDK client.
 *
 * Calls client.session.messages() to retrieve the full message history,
 * writes the JSON result to the target path via atomicReplace, and writes
 * a .meta.json sidecar. Never throws -- failures are swallowed and logged.
 *
 * @param sdkClient - The OpenCode SDK client from the plugin context
 * @param sessionId - The OpenCode session ID to export
 * @param targetPath - The file path for the raw export (e.g. 04_session.raw)
 * @returns ExportResult indicating success/failure
 */
export async function exportSession(
  sdkClient: SdkClient,
  sessionId: string,
  targetPath: string,
): Promise<ExportResult> {
  try {
    // Fetch messages from the SDK
    let messages: unknown;
    try {
      messages = await sdkClient.session.messages({ path: { id: sessionId } });
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      debugLog(`exportSession: SDK call failed for session ${sessionId}`, err);
      return { ok: false, reason };
    }

    // Degrade gracefully when SDK returns no data
    if (messages === null || messages === undefined) {
      return { ok: false, reason: "SDK returned no data" };
    }

    // Serialize to JSON bytes
    const jsonText = JSON.stringify(messages);
    const data = Buffer.from(jsonText, "utf-8");
    const byteCount = data.byteLength;

    // Ensure parent directory exists
    const dir = nodePath.dirname(targetPath);
    await nodeFsPromises.mkdir(dir, { recursive: true });

    // Write raw file atomically
    const rawOk = await atomicReplace(targetPath, data);
    if (!rawOk) {
      return { ok: false, reason: "Failed to write raw export file" };
    }

    // Write sidecar
    const capturedAt = currentTimestamp();
    const sidecar = buildSidecar({
      sourceMechanism: "sdk_client_session_messages",
      byteCount,
      capturedAt,
    });
    const metaPath = metaPathFor(targetPath);
    const metaData = Buffer.from(JSON.stringify(sidecar), "utf-8");
    const metaOk = await atomicReplace(metaPath, metaData);
    if (!metaOk) {
      return { ok: false, reason: "Failed to write sidecar file" };
    }

    return { ok: true, rawPath: targetPath, metaPath, byteCount };
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err);
    debugLog(`exportSession: unexpected error for session ${sessionId}`, err);
    return { ok: false, reason };
  }
}

/**
 * Export the orchestrator's own session transcript. Convenience wrapper
 * that targets 00_orchestrator_session.raw. Called on subagent lifecycle
 * events to keep the orchestrator transcript current.
 *
 * Never throws.
 */
export async function exportOrchestratorTranscript(
  sdkClient: SdkClient,
  orchestratorSessionId: string,
  paths: LogPaths,
  runId: string,
): Promise<ExportResult> {
  const targetPath = paths.orchestratorRaw(runId);
  return exportSession(sdkClient, orchestratorSessionId, targetPath);
}
