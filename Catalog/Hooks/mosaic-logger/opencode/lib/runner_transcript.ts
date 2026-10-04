/**
 * runner_transcript.ts — in-session transcript refresh for Runner mode.
 *
 * Under `opencode run` the session.idle handler is not awaited at exit, so the
 * transcript is re-exported while the session is running. Each refresh writes the
 * raw file and then the sidecar synchronously, with no await, once the SDK
 * returns the messages, so the exit cannot interrupt it. The final refresh is
 * lost only if the SDK call itself has not returned when the process exits.
 */

import * as nodePath from "node:path";
import { transcriptScopeSegment, debugLog } from "./core.js";
import type { LogPaths } from "./core.js";
import { exportSession } from "./export.js";
import type { SdkClient } from "./handlers_session.js";

/** {runRoot}/00_orchestrator_session__{transcriptScopeSegment("opencode", sessionId)}.raw */
export function runnerOrchestratorRawPath(
  paths: LogPaths,
  runId: string,
  sessionId: string,
): string {
  const scope = transcriptScopeSegment("opencode", sessionId);
  return nodePath.join(paths.runRoot(runId), `00_orchestrator_session__${scope}.raw`);
}

export interface TranscriptRefresher {
  /** Request a refresh; coalesced: at most one export in flight and one pending. Never throws. */
  request(): void;
  /** Resolves when no export is in flight or pending. Never rejects. */
  settled(): Promise<void>;
}

/**
 * Refreshes targetPath (+ sidecar) via exportSession. Writes nothing when the
 * SDK yields no data (throws, null/undefined, or no client): availability is
 * decided at runtime by the SDK result, and a transcript is never fabricated.
 */
export function createTranscriptRefresher(
  sdkClient: SdkClient | undefined,
  sessionId: string,
  targetPath: string,
): TranscriptRefresher {
  let running: Promise<void> | undefined;
  let pending = false;

  async function drain(): Promise<void> {
    // Yield first so `running` is assigned before any synchronous completion.
    await Promise.resolve();
    try {
      do {
        pending = false;
        if (sdkClient) await exportSession(sdkClient, sessionId, targetPath);
      } while (pending);
    } catch (err) {
      debugLog(`transcript refresh failed for ${sessionId}`, err);
    } finally {
      running = undefined;
    }
  }

  return {
    request(): void {
      try {
        if (running) {
          pending = true;
          return;
        }
        running = drain();
      } catch (err) {
        debugLog(`transcript refresh request failed for ${sessionId}`, err);
      }
    },
    settled(): Promise<void> {
      return running ?? Promise.resolve();
    },
  };
}
