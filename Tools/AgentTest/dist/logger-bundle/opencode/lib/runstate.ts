/**
 * runstate.ts — Identity extraction and fallback ID generators.
 *
 * Owns: run_id / agent_instance_id / status_code extraction from dispatch
 * content, and fallback ID generators for when extraction fails.
 *
 * All functions are pure (no side effects, no I/O). Never throw.
 * Matches the Python adapter's behavioral contract (structured-first,
 * bare-fallback, never-throw).
 */

// ---------------------------------------------------------------------------
// run_id extraction
// ---------------------------------------------------------------------------

// Structured match: run_id key (with optional quotes/colon/equals) followed
// by a value matching the {YYYYMMDD}T{HHMMSS}Z-{4hex} format.
const RUN_ID_STRUCTURED_RE =
  /(?<![A-Za-z0-9_])run_id\s*['"]?\s*[:=]\s*['"]?\s*([0-9]{8}T[0-9]{6}Z-[0-9a-f]{4})/;

// Bare match: {YYYYMMDD}T{HHMMSS}Z-{4hex} not immediately preceded by
// an alnum or ._- character.
const RUN_ID_BARE_RE =
  /(?<![A-Za-z0-9_.-])([0-9]{8}T[0-9]{6}Z-[0-9a-f]{4})/;

/**
 * Extract a MOSAIC run_id from text content.
 *
 * Matching order (stops at first hit):
 *   1. Structured: run_id key followed by {YYYYMMDD}T{HHMMSS}Z-{4hex} value
 *   2. Bare: earliest occurrence of the format pattern in text
 * Returns undefined when no confident match exists. Never throws.
 */
export function extractRunId(text: string | undefined | null): string | undefined {
  try {
    if (!text) return undefined;
    const structured = RUN_ID_STRUCTURED_RE.exec(text);
    if (structured) return structured[1];
    const bare = RUN_ID_BARE_RE.exec(text);
    if (bare) return bare[1];
    return undefined;
  } catch {
    return undefined;
  }
}

// ---------------------------------------------------------------------------
// agent_instance_id extraction
// ---------------------------------------------------------------------------

// Structured match: agent_instance_id key (with optional quotes/colon/equals)
// followed by a quoted or bare {Name}#{Number} value.
const INSTANCE_STRUCTURED_RE =
  /(?<![A-Za-z0-9_])agent_instance_id\s*['"]?\s*[:=]\s*['"]?\s*([A-Za-z][A-Za-z0-9_.-]*#[0-9]+)/;

// Bare match: {Name}#{Number} not immediately preceded by an alnum/.-_ character.
const INSTANCE_BARE_RE =
  /(?<![A-Za-z0-9_.-])([A-Za-z][A-Za-z0-9_.-]*#[0-9]+)/;

/**
 * Extract a MOSAIC {AgentName}#{Number} instance ID from text.
 *
 * Matching order (stops at first hit):
 *   1. Structured: agent_instance_id key followed by value
 *   2. Bare: earliest {Name}#{Number} occurrence
 * Returns undefined when no confident match exists. Never throws.
 */
export function extractInstanceId(text: string | undefined | null): string | undefined {
  try {
    if (!text) return undefined;
    const structured = INSTANCE_STRUCTURED_RE.exec(text);
    if (structured) return structured[1];
    const bare = INSTANCE_BARE_RE.exec(text);
    if (bare) return bare[1];
    return undefined;
  } catch {
    return undefined;
  }
}

// ---------------------------------------------------------------------------
// status_code extraction
// ---------------------------------------------------------------------------

/** The closed set of valid MOSAIC communication protocol status codes. */
export const VALID_STATUS_CODES: ReadonlySet<string> = new Set([
  "SUCCESS",
  "COMPLETED_NEEDS_ACTION",
  "PARTIALLY_DONE",
  "NEEDS_CLARIFICATION",
  "CAPABILITY_EXCEEDED",
  "BLOCKED",
]);

// Match a structured status_code key followed by a quoted enum value.
// Only quoted values are accepted — free prose won't match.
const STATUS_CODE_RE =
  /(?<![A-Za-z0-9_])status_code\s*['"]?\s*[:=]\s*['"]([A-Z_]+)['"]/g;

/**
 * Extract the MOSAIC status code from a subagent's final response.
 *
 * Matching: structured key-value only (status_code key + quoted value from
 * closed enum). Last match wins. Returns undefined on any ambiguity,
 * values outside the enum, or missing input. Never throws.
 */
export function extractStatusCode(text: string | undefined | null): string | undefined {
  try {
    if (!text) return undefined;
    const matches: string[] = [];
    // Use exec loop to collect all matches (global regex)
    const re = new RegExp(STATUS_CODE_RE.source, "g");
    let m: RegExpExecArray | null;
    while ((m = re.exec(text)) !== null) {
      matches.push(m[1]);
    }
    if (matches.length === 0) return undefined;
    const last = matches[matches.length - 1];
    if (VALID_STATUS_CODES.has(last)) return last;
    return undefined;
  } catch {
    return undefined;
  }
}

// ---------------------------------------------------------------------------
// Fallback ID generators
// ---------------------------------------------------------------------------

function randomHex4(): string {
  return Math.floor(Math.random() * 0x10000)
    .toString(16)
    .padStart(4, "0");
}

function compactTimestamp(): string {
  const now = new Date();
  const y = now.getUTCFullYear();
  const mo = String(now.getUTCMonth() + 1).padStart(2, "0");
  const d = String(now.getUTCDate()).padStart(2, "0");
  const h = String(now.getUTCHours()).padStart(2, "0");
  const mi = String(now.getUTCMinutes()).padStart(2, "0");
  const s = String(now.getUTCSeconds()).padStart(2, "0");
  return `${y}${mo}${d}T${h}${mi}${s}Z`;
}

/** Return {YYYYMMDD}T{HHMMSS}Z-{4hex}. */
export function newRunId(): string {
  return `${compactTimestamp()}-${randomHex4()}`;
}

/** Return {prefix}_{YYYYMMDD}T{HHMMSS}Z-{4hex}. Uses "agent" when prefix is empty. */
export function fallbackInstanceId(prefix?: string): string {
  const p = prefix || "agent";
  return `${p}_${compactTimestamp()}-${randomHex4()}`;
}

/** Return {toolName}_{YYYYMMDD}T{HHMMSS}Z-{4hex}. Uses "tool" when toolName is empty. */
export function fallbackCallId(toolName?: string): string {
  const p = toolName || "tool";
  return `${p}_${compactTimestamp()}-${randomHex4()}`;
}
