/**
 * sync_append.ts — synchronous JSONL append for exit-safe writes.
 *
 * Under `opencode run` anything after the first await of the session.idle
 * handler is lost at process exit, so the closing event must hit the disk
 * synchronously.
 */

import * as nodeFs from "node:fs";
import * as nodePath from "node:path";
import { debugLog } from "./core.js";

/** Synchronously append one JSON line, creating parent dirs. Returns false on any failure. Never throws. */
export function appendEventSync(
  filePath: string,
  event: Record<string, unknown>,
): boolean {
  try {
    nodeFs.mkdirSync(nodePath.dirname(filePath), { recursive: true });
    nodeFs.appendFileSync(filePath, JSON.stringify(event) + "\n", "utf-8");
    return true;
  } catch (err) {
    debugLog(`appendEventSync failed for ${filePath}`, err);
    return false;
  }
}
