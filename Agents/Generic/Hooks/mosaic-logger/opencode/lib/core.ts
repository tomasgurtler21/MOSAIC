/**
 * core.ts — Core utilities for the mosaic-logger OpenCode hook adapter.
 *
 * Owns: constants, timestamp helpers, event envelope builder (with
 * degrade-never-fabricate prune logic), path layout, filename sanitization,
 * atomic file I/O, effective run_id resolution, debug logging, and
 * adapter version reading.
 *
 * No runtime dependencies beyond Node/Bun built-in modules.
 */

import * as nodePath from "node:path";
import * as nodeFsPromises from "node:fs/promises";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

export const SCHEMA_VERSION: string = "1.0.0";
export const HARNESS: string = "opencode";
export const LOGS_DIRNAME: string = "OrchestrationLogs";

/** Characters reserved by Windows/cross-platform filesystem conventions. */
const RESERVED_CHARS = new Set([...'<>:"/\\|?*']);

// ---------------------------------------------------------------------------
// Timestamp helpers
// ---------------------------------------------------------------------------

/** Return current UTC time as ISO 8601 with ms precision and Z suffix. */
export function currentTimestamp(): string {
  const now = new Date();
  const year = now.getUTCFullYear();
  const month = String(now.getUTCMonth() + 1).padStart(2, "0");
  const day = String(now.getUTCDate()).padStart(2, "0");
  const hours = String(now.getUTCHours()).padStart(2, "0");
  const minutes = String(now.getUTCMinutes()).padStart(2, "0");
  const seconds = String(now.getUTCSeconds()).padStart(2, "0");
  const ms = String(now.getUTCMilliseconds()).padStart(3, "0");
  return `${year}-${month}-${day}T${hours}:${minutes}:${seconds}.${ms}Z`;
}

// ---------------------------------------------------------------------------
// Prune logic
// ---------------------------------------------------------------------------

/**
 * Recursive prune logic. Returns [prunedValue, keep].
 *
 * Dropped (keep=false): null, undefined, empty string, empty object,
 * empty array, and any object whose entries are all dropped after recursion.
 *
 * Kept (keep=true): false, 0, true, non-empty string, any number,
 * non-empty array (arrays are kept as-is — elements are not individually
 * pruned), non-empty object (after recursive pruning of its entries).
 */
export function prune(value: unknown): [unknown, boolean] {
  if (value === null || value === undefined) {
    return [undefined, false];
  }

  if (typeof value === "boolean") {
    // false is a genuine resolved value — must be kept
    return [value, true];
  }

  if (typeof value === "number") {
    // 0 is a genuine resolved value — must be kept
    return [value, true];
  }

  if (typeof value === "string") {
    if (value === "") return [undefined, false];
    return [value, true];
  }

  if (Array.isArray(value)) {
    if (value.length === 0) return [undefined, false];
    // Non-empty arrays are kept as-is; individual elements are NOT pruned
    return [value, true];
  }

  if (typeof value === "object") {
    const pruned: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      const [pv, keep] = prune(v);
      if (keep) {
        pruned[k] = pv;
      }
    }
    if (Object.keys(pruned).length === 0) {
      return [undefined, false];
    }
    return [pruned, true];
  }

  // Fallback: keep any other value type
  return [value, true];
}

// ---------------------------------------------------------------------------
// Event envelope
// ---------------------------------------------------------------------------

/**
 * Build a complete event object: common envelope plus caller-supplied fields.
 *
 * Envelope: schema_version, event, timestamp, harness.
 * Optional envelope fields: session_id, run_id — included only when non-empty.
 *
 * Every value in fields that is undefined, null, empty string, empty object,
 * or empty array is DROPPED. false and 0 are genuine values and are KEPT.
 * Nested objects are pruned recursively; an object empty after pruning is
 * itself dropped.
 */
export function buildEvent(
  event: string,
  envelope: { sessionId?: string; runId?: string; timestamp: string },
  fields: Record<string, unknown>,
): Record<string, unknown> {
  const result: Record<string, unknown> = {
    schema_version: SCHEMA_VERSION,
    event,
    timestamp: envelope.timestamp,
    harness: HARNESS,
  };

  if (envelope.sessionId && envelope.sessionId !== "") {
    result["session_id"] = envelope.sessionId;
  }
  if (envelope.runId && envelope.runId !== "") {
    result["run_id"] = envelope.runId;
  }

  for (const [k, v] of Object.entries(fields)) {
    const [pv, keep] = prune(v);
    if (keep) {
      result[k] = pv;
    }
  }

  return result;
}

// ---------------------------------------------------------------------------
// Effective run_id resolution
// ---------------------------------------------------------------------------

/**
 * Return runId when present and non-empty, or "unknown-run" as the
 * fallback bucket name for path construction.
 */
export function effectiveRunId(runId: string | undefined): string {
  if (runId && runId !== "") {
    return runId;
  }
  return "unknown-run";
}

// ---------------------------------------------------------------------------
// Filename sanitization
// ---------------------------------------------------------------------------

/**
 * Sanitize a string for use as a single filesystem path component.
 * - Replaces each of <>:"/\|?* with _
 * - Replaces control characters (< 0x20) with _
 * - Strips trailing dots and spaces
 * - Returns "_" for empty or fully-stripped result
 * Deterministic, total, not reversible.
 */
export function sanitizeComponent(name: string): string {
  if (!name) return "_";

  const result: string[] = [];
  for (const ch of name) {
    const code = ch.charCodeAt(0);
    if (RESERVED_CHARS.has(ch) || code < 0x20) {
      result.push("_");
    } else {
      result.push(ch);
    }
  }

  const sanitized = result.join("").replace(/[. ]+$/, "");
  return sanitized || "_";
}

// ---------------------------------------------------------------------------
// Path layout
// ---------------------------------------------------------------------------

/**
 * The complete on-disk layout expressed as one object.
 * No path is ever concatenated at a call site; a layout change touches
 * only this class. Mirrors the Python adapter's LogPaths.
 */
export class LogPaths {
  /** Root: {workspaceRoot}/OrchestrationLogs */
  readonly root: string;

  constructor(workspaceRoot: string) {
    this.root = nodePath.join(workspaceRoot, LOGS_DIRNAME);
  }

  /** {root}/{runId} */
  runRoot(runId: string): string {
    return nodePath.join(this.root, runId);
  }

  /** {root}/{runId}/00_orchestrator_events.jsonl */
  orchestratorEvents(runId: string): string {
    return nodePath.join(this.runRoot(runId), "00_orchestrator_events.jsonl");
  }

  /** {root}/{runId}/00_orchestrator_session.raw */
  orchestratorRaw(runId: string): string {
    return nodePath.join(this.runRoot(runId), "00_orchestrator_session.raw");
  }

  /** {root}/{runId}/00_orchestrator_session.meta.json */
  orchestratorRawMeta(runId: string): string {
    return nodePath.join(this.runRoot(runId), "00_orchestrator_session.meta.json");
  }

  /** {root}/{runId}/{sanitize(agentInstanceId)}/ */
  invocationDir(runId: string, agentInstanceId: string): string {
    return nodePath.join(this.runRoot(runId), sanitizeComponent(agentInstanceId));
  }

  /** {root}/{runId}/{sanitize(agentInstanceId)}/03_events.jsonl */
  invocationEvents(runId: string, agentInstanceId: string): string {
    return nodePath.join(this.invocationDir(runId, agentInstanceId), "03_events.jsonl");
  }

  /** {root}/{runId}/{sanitize(agentInstanceId)}/01_input.md */
  invocationInput(runId: string, agentInstanceId: string): string {
    return nodePath.join(this.invocationDir(runId, agentInstanceId), "01_input.md");
  }

  /** {root}/{runId}/{sanitize(agentInstanceId)}/02_output.md */
  invocationOutput(runId: string, agentInstanceId: string): string {
    return nodePath.join(this.invocationDir(runId, agentInstanceId), "02_output.md");
  }

  /** {root}/{runId}/{sanitize(agentInstanceId)}/04_session.raw */
  invocationRaw(runId: string, agentInstanceId: string): string {
    return nodePath.join(this.invocationDir(runId, agentInstanceId), "04_session.raw");
  }

  /** {root}/{runId}/{sanitize(agentInstanceId)}/04_session.meta.json */
  invocationRawMeta(runId: string, agentInstanceId: string): string {
    return nodePath.join(this.invocationDir(runId, agentInstanceId), "04_session.meta.json");
  }
}

// ---------------------------------------------------------------------------
// Write primitives
// ---------------------------------------------------------------------------

/**
 * Append one JSON object as one line to a JSONL file, creating parent dirs.
 * Swallows all errors. Never throws.
 */
export async function appendEvent(
  filePath: string,
  event: Record<string, unknown>,
): Promise<void> {
  try {
    const dir = nodePath.dirname(filePath);
    await nodeFsPromises.mkdir(dir, { recursive: true });
    const line = JSON.stringify(event) + "\n";
    await nodeFsPromises.appendFile(filePath, line, "utf-8");
  } catch (err) {
    debugLog(`appendEvent failed for ${filePath}`, err);
  }
}

/**
 * Write data to a temp file in the target's directory, then rename over target.
 * Returns true on success, false on any failure. Never throws.
 * The target directory must already exist; this function does not create directories.
 */
export async function atomicReplace(
  filePath: string,
  data: Uint8Array | Buffer,
): Promise<boolean> {
  const dir = nodePath.dirname(filePath);
  const tmpName = nodePath.join(
    dir,
    `.tmp_${Date.now()}_${Math.random().toString(36).slice(2)}`,
  );
  try {
    await nodeFsPromises.writeFile(tmpName, data);
    await nodeFsPromises.rename(tmpName, filePath);
    return true;
  } catch {
    try {
      await nodeFsPromises.unlink(tmpName);
    } catch {
      // Ignore cleanup errors
    }
    return false;
  }
}

/**
 * UTF-8 convenience wrapper over atomicReplace. Never throws.
 */
export async function atomicReplaceText(
  filePath: string,
  text: string,
): Promise<boolean> {
  return atomicReplace(filePath, Buffer.from(text, "utf-8"));
}

// ---------------------------------------------------------------------------
// Debug logging
// ---------------------------------------------------------------------------

/** Logging function type — set once at plugin init from client.app.log() */
export type DebugLogFn = (message: string, error?: unknown) => void;

/** Module-level singleton. Null until setDebugLogger is called. */
let _debugLogger: DebugLogFn | null = null;

/**
 * Set the debug logging implementation. Called once during plugin factory init.
 * When client.app.log() is available, wraps it. Otherwise falls back to
 * file-based debug log.
 */
export function setDebugLogger(logFn: DebugLogFn): void {
  _debugLogger = logFn;
}

/**
 * Log a debug/diagnostic message. No-op until setDebugLogger is called.
 * Never throws — errors from the installed logger are swallowed.
 */
export function debugLog(message: string, error?: unknown): void {
  if (_debugLogger === null) return;
  try {
    _debugLogger(message, error);
  } catch {
    // Swallow logger errors — debugLog must never propagate exceptions
  }
}

// ---------------------------------------------------------------------------
// Safe handler wrapper
// ---------------------------------------------------------------------------

/**
 * Wrap any async handler function to catch and log all errors.
 * The wrapped handler never throws, satisfying the "never raise, never block"
 * discipline. Errors are logged via debugLog().
 *
 * @param name  Human-readable handler name for error log messages
 * @param handler  The async handler to wrap
 * @returns  A wrapped version with the same signature that swallows errors
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function safeHandler<T extends (...args: any[]) => Promise<void>>(
  name: string,
  handler: T,
): T {
  return (async (...args: Parameters<T>) => {
    try {
      await handler(...args);
    } catch (err) {
      debugLog(`safeHandler: error in "${name}" handler`, err);
    }
  }) as T;
}

// ---------------------------------------------------------------------------
// Adapter version
// ---------------------------------------------------------------------------

const VERSION_RE = /^version:\s*(.+?)\s*$/;

/**
 * Extract the top-level version value from hook.yaml without a YAML parser.
 * Matches only zero-indented lines. Returns undefined on any ambiguity.
 * Never throws.
 */
export async function readAdapterVersion(
  hookYamlPath: string,
): Promise<string | undefined> {
  try {
    const content = await nodeFsPromises.readFile(hookYamlPath, "utf-8");
    const matches: string[] = [];

    for (const rawLine of content.split("\n")) {
      const line = rawLine.replace(/\r$/, "");

      // Skip blank lines, comments, and indented lines
      if (!line || line[0] === "#" || line[0] === " " || line[0] === "\t") {
        continue;
      }

      const m = VERSION_RE.exec(line);
      if (!m) continue;

      let value = m[1];

      // Strip one layer of surrounding single or double quotes
      if (
        value.length >= 2 &&
        ((value[0] === '"' && value[value.length - 1] === '"') ||
          (value[0] === "'" && value[value.length - 1] === "'"))
      ) {
        value = value.slice(1, -1);
      }

      // Reject complex YAML types (objects, sequences) — ambiguous without a
      // real YAML parser
      if (!value || value.includes("{") || value.includes("[")) {
        continue;
      }

      matches.push(value);
    }

    // Ambiguous if more than one match
    if (matches.length === 1) {
      return matches[0];
    }
    return undefined;
  } catch {
    return undefined;
  }
}
