/**
 * artifacts.ts — Human-readable markdown artifact rendering.
 *
 * Owns: rendering 01_input.md and 02_output.md for each subagent invocation.
 * Returns markdown text and performs no I/O; callers write the result via core.
 *
 * Pure functions: renderInput and renderOutput have no side effects.
 */

import * as nodeFsPromises from "node:fs/promises";
import * as nodePath from "node:path";
import { HARNESS, atomicReplaceText } from "./core";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Return a markdown table row string, or empty string when value is absent.
 * Absent means: undefined or null.
 */
function row(label: string, value: string | undefined | null): string {
  if (value === undefined || value === null) return "";
  return `| ${label} | ${value} |\n`;
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Render the 01_input.md body: metadata table followed by raw prompt content.
 *
 * Returns markdown text. Performs no I/O. Pure function.
 * Rows with absent (undefined/null) values are omitted entirely.
 */
export function renderInput(params: {
  agentInstanceId: string;
  agentType?: string | null;
  sessionId?: string | null;
  runId?: string | null;
  timestamp: string;
  prompt?: string | null;
}): string {
  const lines: string[] = [];

  lines.push(`# Invocation Input: ${params.agentInstanceId}\n\n`);
  lines.push("| Field | Value |\n");
  lines.push("|---|---|\n");
  lines.push(row("Agent Instance ID", params.agentInstanceId));
  lines.push(row("Agent Type", params.agentType));
  lines.push(row("Session ID", params.sessionId));
  lines.push(row("Run ID", params.runId));
  lines.push(row("Harness", HARNESS));
  lines.push(row("Captured At", params.timestamp));

  if (params.prompt !== undefined && params.prompt !== null) {
    lines.push("\n## Prompt\n\n");
    lines.push(params.prompt);
    lines.push("\n");
  }

  return lines.join("");
}

/**
 * Render the 02_output.md body: metadata table followed by raw response content.
 *
 * Returns markdown text. Performs no I/O. Pure function.
 * Rows with absent (undefined/null) values are omitted entirely.
 * Token usage rows are each independently optional.
 */
export function renderOutput(params: {
  agentInstanceId: string;
  agentType?: string | null;
  sessionId?: string | null;
  runId?: string | null;
  timestamp: string;
  statusCode?: string | null;
  model?: string | null;
  tokenUsage?: {
    input_tokens?: number;
    output_tokens?: number;
    cache_read_tokens?: number;
    cache_creation_tokens?: number;
  } | null;
  response?: string | null;
}): string {
  const lines: string[] = [];

  lines.push(`# Invocation Output: ${params.agentInstanceId}\n\n`);
  lines.push("| Field | Value |\n");
  lines.push("|---|---|\n");
  lines.push(row("Agent Instance ID", params.agentInstanceId));
  lines.push(row("Agent Type", params.agentType));
  lines.push(row("Session ID", params.sessionId));
  lines.push(row("Run ID", params.runId));
  lines.push(row("Harness", HARNESS));
  lines.push(row("Captured At", params.timestamp));
  lines.push(row("Status Code", params.statusCode));
  lines.push(row("Model", params.model));

  // Token usage rows — each independently optional; omitted when value absent
  const tu = params.tokenUsage;
  if (tu) {
    if (tu.input_tokens !== undefined) {
      lines.push(row("Input Tokens", String(tu.input_tokens)));
    }
    if (tu.output_tokens !== undefined) {
      lines.push(row("Output Tokens", String(tu.output_tokens)));
    }
    if (tu.cache_read_tokens !== undefined) {
      lines.push(row("Cache Read Tokens", String(tu.cache_read_tokens)));
    }
    if (tu.cache_creation_tokens !== undefined) {
      lines.push(row("Cache Creation Tokens", String(tu.cache_creation_tokens)));
    }
  }

  if (params.response !== undefined && params.response !== null) {
    lines.push("\n## Response\n\n");
    lines.push(params.response);
    lines.push("\n");
  }

  return lines.join("");
}

/**
 * Write a markdown artifact via atomicReplaceText.
 * Creates parent directories as needed. Returns true on success. Never throws.
 */
export async function writeArtifact(filePath: string, text: string): Promise<boolean> {
  try {
    const dir = nodePath.dirname(filePath);
    await nodeFsPromises.mkdir(dir, { recursive: true });
    return atomicReplaceText(filePath, text);
  } catch {
    return false;
  }
}
