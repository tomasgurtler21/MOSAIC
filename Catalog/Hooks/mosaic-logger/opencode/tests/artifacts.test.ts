/**
 * artifacts.test.ts — Tests for markdown artifact rendering functions.
 *
 * Covers:
 *   - renderInput: metadata table + optional Prompt section
 *   - renderOutput: metadata table + optional Response section + per-field token usage rows
 *   - Omit-absent-row behavior: rows with undefined/null values are not rendered
 *   - Harness field always "opencode" (constant, never omitted)
 *   - writeArtifact: creates parent directories, delegates to atomicReplaceText
 *
 * All tests fail until the implementation is complete (TDD RED phase).
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import { renderInput, renderOutput, writeArtifact } from "../lib/artifacts";

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

const FIXED_TS = "2026-01-01T17:00:00.000Z";
const AGENT_ID = "Research#1";
const RUN_ID = "20260101T170000Z-a3f9";
const SESSION_ID = "sess-abc-123";

// ---------------------------------------------------------------------------
// renderInput
// ---------------------------------------------------------------------------

describe("renderInput", () => {
  describe("returns a string (pure function, no I/O)", () => {
    it("returns a string for minimal required params", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
      });
      expect(typeof result).toBe("string");
    });

    it("does not return null or undefined", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
      });
      expect(result).not.toBeNull();
      expect(result).not.toBeUndefined();
    });
  });

  describe("document heading", () => {
    it("opens with a level-1 heading containing the agentInstanceId", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toMatch(/^# Invocation Input: Research#1\n/);
    });

    it("blank line follows the heading", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toMatch(/^# Invocation Input: Research#1\n\n/);
    });

    it("heading uses the actual agentInstanceId passed in", () => {
      const result = renderInput({ agentInstanceId: "PlanWriter#3", timestamp: FIXED_TS });
      expect(result).toMatch(/^# Invocation Input: PlanWriter#3\n/);
    });
  });

  describe("metadata table structure", () => {
    it("renders the table header row", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toContain("| Field | Value |");
    });

    it("renders the table separator row", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toContain("|---|---|");
    });

    it("header row appears before separator row", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      const headerPos = result.indexOf("| Field | Value |");
      const sepPos = result.indexOf("|---|---|");
      expect(headerPos).toBeLessThan(sepPos);
    });
  });

  describe("Agent Instance ID row — always present", () => {
    it("renders Agent Instance ID row with the provided value", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toContain("| Agent Instance ID | Research#1 |");
    });

    it("Agent Instance ID row appears in the table body", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      const sepPos = result.indexOf("|---|---|");
      const rowPos = result.indexOf("| Agent Instance ID |");
      expect(rowPos).toBeGreaterThan(sepPos);
    });
  });

  describe("Harness row — always opencode", () => {
    it("renders the Harness row with value opencode", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toContain("| Harness | opencode |");
    });

    it("renders Harness row even when all optional fields are absent", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        // no agentType, sessionId, runId, prompt
      });
      expect(result).toContain("| Harness | opencode |");
    });
  });

  describe("Captured At row — always present (timestamp is required)", () => {
    it("renders Captured At row with the timestamp", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toContain(`| Captured At | ${FIXED_TS} |`);
    });

    it("Captured At reflects the exact timestamp value passed in", () => {
      const ts = "2025-06-15T09:30:00.999Z";
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: ts });
      expect(result).toContain(`| Captured At | ${ts} |`);
    });
  });

  describe("optional Agent Type row", () => {
    it("renders Agent Type row when agentType is provided", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        agentType: "Research",
        timestamp: FIXED_TS,
      });
      expect(result).toContain("| Agent Type | Research |");
    });

    it("omits Agent Type row when agentType is undefined", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("| Agent Type |");
    });

    it("omits Agent Type row when agentType is null", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        agentType: null as unknown as string,
        timestamp: FIXED_TS,
      });
      expect(result).not.toContain("| Agent Type |");
    });
  });

  describe("optional Session ID row", () => {
    it("renders Session ID row when sessionId is provided", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        sessionId: SESSION_ID,
        timestamp: FIXED_TS,
      });
      expect(result).toContain(`| Session ID | ${SESSION_ID} |`);
    });

    it("omits Session ID row when sessionId is undefined", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("| Session ID |");
    });
  });

  describe("optional Run ID row", () => {
    it("renders Run ID row when runId is provided", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        runId: RUN_ID,
        timestamp: FIXED_TS,
      });
      expect(result).toContain(`| Run ID | ${RUN_ID} |`);
    });

    it("omits Run ID row when runId is undefined", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("| Run ID |");
    });
  });

  describe("row ordering in metadata table", () => {
    it("renders Agent Instance ID before Agent Type", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        agentType: "Research",
        timestamp: FIXED_TS,
      });
      const idPos = result.indexOf("| Agent Instance ID |");
      const typePos = result.indexOf("| Agent Type |");
      expect(idPos).toBeLessThan(typePos);
    });

    it("renders Session ID before Run ID", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        sessionId: SESSION_ID,
        runId: RUN_ID,
        timestamp: FIXED_TS,
      });
      const sessPos = result.indexOf("| Session ID |");
      const runPos = result.indexOf("| Run ID |");
      expect(sessPos).toBeLessThan(runPos);
    });

    it("renders Run ID before Harness", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        runId: RUN_ID,
        timestamp: FIXED_TS,
      });
      const runPos = result.indexOf("| Run ID |");
      const harnessPos = result.indexOf("| Harness |");
      expect(runPos).toBeLessThan(harnessPos);
    });

    it("renders Harness before Captured At", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      const harnessPos = result.indexOf("| Harness |");
      const capturedPos = result.indexOf("| Captured At |");
      expect(harnessPos).toBeLessThan(capturedPos);
    });
  });

  describe("Prompt section", () => {
    it("renders a level-2 Prompt heading when prompt is provided", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        prompt: "Do the task.",
      });
      expect(result).toContain("## Prompt");
    });

    it("renders the prompt content after the Prompt heading", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        prompt: "Do the task.",
      });
      const promptHeadingPos = result.indexOf("## Prompt");
      const promptContentPos = result.indexOf("Do the task.", promptHeadingPos);
      expect(promptContentPos).toBeGreaterThan(promptHeadingPos);
    });

    it("omits the Prompt section when prompt is undefined", () => {
      const result = renderInput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("## Prompt");
    });

    it("omits the Prompt section when prompt is null", () => {
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        prompt: null as unknown as string,
      });
      expect(result).not.toContain("## Prompt");
    });

    it("preserves multi-line prompt text verbatim", () => {
      const multiLinePrompt = "Line one.\nLine two.\nLine three.";
      const result = renderInput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        prompt: multiLinePrompt,
      });
      expect(result).toContain(multiLinePrompt);
    });
  });

  describe("full document with all fields", () => {
    it("produces the complete expected markdown document", () => {
      const expected = [
        "# Invocation Input: Research#1\n",
        "\n",
        "| Field | Value |\n",
        "|---|---|\n",
        "| Agent Instance ID | Research#1 |\n",
        "| Agent Type | Research |\n",
        `| Session ID | ${SESSION_ID} |\n`,
        `| Run ID | ${RUN_ID} |\n`,
        "| Harness | opencode |\n",
        `| Captured At | ${FIXED_TS} |\n`,
        "\n## Prompt\n\n",
        "The dispatch prompt text.",
        "\n",
      ].join("");

      const result = renderInput({
        agentInstanceId: AGENT_ID,
        agentType: "Research",
        sessionId: SESSION_ID,
        runId: RUN_ID,
        timestamp: FIXED_TS,
        prompt: "The dispatch prompt text.",
      });

      expect(result).toBe(expected);
    });

    it("produces the expected document with all optional fields absent", () => {
      const expected = [
        "# Invocation Input: Research#1\n",
        "\n",
        "| Field | Value |\n",
        "|---|---|\n",
        "| Agent Instance ID | Research#1 |\n",
        "| Harness | opencode |\n",
        `| Captured At | ${FIXED_TS} |\n`,
      ].join("");

      const result = renderInput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
      });

      expect(result).toBe(expected);
    });
  });
});

// ---------------------------------------------------------------------------
// renderOutput
// ---------------------------------------------------------------------------

describe("renderOutput", () => {
  describe("returns a string (pure function, no I/O)", () => {
    it("returns a string for minimal required params", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
      });
      expect(typeof result).toBe("string");
    });
  });

  describe("document heading", () => {
    it("opens with a level-1 Invocation Output heading containing the agentInstanceId", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toMatch(/^# Invocation Output: Research#1\n/);
    });

    it("blank line follows the heading", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toMatch(/^# Invocation Output: Research#1\n\n/);
    });
  });

  describe("metadata table structure", () => {
    it("renders the table header and separator rows", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toContain("| Field | Value |");
      expect(result).toContain("|---|---|");
    });
  });

  describe("shared optional fields — same behavior as renderInput", () => {
    it("renders Agent Type row when agentType is provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        agentType: "Research",
        timestamp: FIXED_TS,
      });
      expect(result).toContain("| Agent Type | Research |");
    });

    it("omits Agent Type row when agentType is undefined", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("| Agent Type |");
    });

    it("renders Session ID row when sessionId is provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        sessionId: SESSION_ID,
        timestamp: FIXED_TS,
      });
      expect(result).toContain(`| Session ID | ${SESSION_ID} |`);
    });

    it("omits Session ID row when sessionId is undefined", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("| Session ID |");
    });

    it("renders Run ID row when runId is provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        runId: RUN_ID,
        timestamp: FIXED_TS,
      });
      expect(result).toContain(`| Run ID | ${RUN_ID} |`);
    });

    it("omits Run ID row when runId is undefined", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("| Run ID |");
    });

    it("always renders Harness as opencode", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toContain("| Harness | opencode |");
    });

    it("always renders Captured At with the timestamp", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).toContain(`| Captured At | ${FIXED_TS} |`);
    });
  });

  describe("optional Status Code row", () => {
    it("renders Status Code row when statusCode is provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        statusCode: "SUCCESS",
      });
      expect(result).toContain("| Status Code | SUCCESS |");
    });

    it("omits Status Code row when statusCode is undefined", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("| Status Code |");
    });

    it("omits Status Code row when statusCode is null", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        statusCode: null as unknown as string,
      });
      expect(result).not.toContain("| Status Code |");
    });
  });

  describe("optional Model row", () => {
    it("renders Model row when model is provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        model: "claude-sonnet-4-5",
      });
      expect(result).toContain("| Model | claude-sonnet-4-5 |");
    });

    it("omits Model row when model is undefined", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("| Model |");
    });
  });

  describe("token usage rows — each independently optional", () => {
    it("renders Input Tokens row when input_tokens is provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { input_tokens: 1000 },
      });
      expect(result).toContain("| Input Tokens | 1000 |");
    });

    it("omits Input Tokens row when input_tokens is absent from tokenUsage", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { output_tokens: 500 },
      });
      expect(result).not.toContain("| Input Tokens |");
    });

    it("renders Output Tokens row when output_tokens is provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { output_tokens: 500 },
      });
      expect(result).toContain("| Output Tokens | 500 |");
    });

    it("omits Output Tokens row when output_tokens is absent from tokenUsage", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { input_tokens: 1000 },
      });
      expect(result).not.toContain("| Output Tokens |");
    });

    it("renders Cache Read Tokens row when cache_read_tokens is provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { cache_read_tokens: 200 },
      });
      expect(result).toContain("| Cache Read Tokens | 200 |");
    });

    it("omits Cache Read Tokens row when cache_read_tokens is absent", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { input_tokens: 100 },
      });
      expect(result).not.toContain("| Cache Read Tokens |");
    });

    it("renders Cache Creation Tokens row when cache_creation_tokens is provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { cache_creation_tokens: 100 },
      });
      expect(result).toContain("| Cache Creation Tokens | 100 |");
    });

    it("omits Cache Creation Tokens row when cache_creation_tokens is absent", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { input_tokens: 100 },
      });
      expect(result).not.toContain("| Cache Creation Tokens |");
    });

    it("renders all four token usage rows when all are provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: {
          input_tokens: 1000,
          output_tokens: 500,
          cache_read_tokens: 200,
          cache_creation_tokens: 100,
        },
      });
      expect(result).toContain("| Input Tokens | 1000 |");
      expect(result).toContain("| Output Tokens | 500 |");
      expect(result).toContain("| Cache Read Tokens | 200 |");
      expect(result).toContain("| Cache Creation Tokens | 100 |");
    });

    it("renders only the token rows that are present when tokenUsage is partial", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { input_tokens: 1000, cache_read_tokens: 200 },
      });
      expect(result).toContain("| Input Tokens | 1000 |");
      expect(result).toContain("| Cache Read Tokens | 200 |");
      expect(result).not.toContain("| Output Tokens |");
      expect(result).not.toContain("| Cache Creation Tokens |");
    });

    it("omits all token rows when tokenUsage is undefined", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("| Input Tokens |");
      expect(result).not.toContain("| Output Tokens |");
      expect(result).not.toContain("| Cache Read Tokens |");
      expect(result).not.toContain("| Cache Creation Tokens |");
    });

    it("omits all token rows when tokenUsage is an empty object", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: {},
      });
      expect(result).not.toContain("| Input Tokens |");
      expect(result).not.toContain("| Output Tokens |");
      expect(result).not.toContain("| Cache Read Tokens |");
      expect(result).not.toContain("| Cache Creation Tokens |");
    });

    it("renders token count of 0 as a row (0 is a legitimate value)", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { cache_read_tokens: 0 },
      });
      expect(result).toContain("| Cache Read Tokens | 0 |");
    });
  });

  describe("token usage row ordering", () => {
    it("renders Input Tokens before Output Tokens", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { input_tokens: 1000, output_tokens: 500 },
      });
      const inputPos = result.indexOf("| Input Tokens |");
      const outputPos = result.indexOf("| Output Tokens |");
      expect(inputPos).toBeLessThan(outputPos);
    });

    it("renders Output Tokens before Cache Read Tokens", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { output_tokens: 500, cache_read_tokens: 200 },
      });
      const outputPos = result.indexOf("| Output Tokens |");
      const cacheReadPos = result.indexOf("| Cache Read Tokens |");
      expect(outputPos).toBeLessThan(cacheReadPos);
    });

    it("renders Cache Read Tokens before Cache Creation Tokens", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        tokenUsage: { cache_read_tokens: 200, cache_creation_tokens: 100 },
      });
      const cacheReadPos = result.indexOf("| Cache Read Tokens |");
      const cacheCreatePos = result.indexOf("| Cache Creation Tokens |");
      expect(cacheReadPos).toBeLessThan(cacheCreatePos);
    });
  });

  describe("output row ordering relative to shared rows", () => {
    it("renders Status Code after Captured At", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        statusCode: "SUCCESS",
      });
      const capturedPos = result.indexOf("| Captured At |");
      const statusPos = result.indexOf("| Status Code |");
      expect(capturedPos).toBeLessThan(statusPos);
    });

    it("renders Model after Status Code", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        statusCode: "SUCCESS",
        model: "claude-sonnet-4-5",
      });
      const statusPos = result.indexOf("| Status Code |");
      const modelPos = result.indexOf("| Model |");
      expect(statusPos).toBeLessThan(modelPos);
    });

    it("renders token usage rows after Model", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        model: "claude-sonnet-4-5",
        tokenUsage: { input_tokens: 100 },
      });
      const modelPos = result.indexOf("| Model |");
      const tokenPos = result.indexOf("| Input Tokens |");
      expect(modelPos).toBeLessThan(tokenPos);
    });
  });

  describe("Response section", () => {
    it("renders a level-2 Response heading when response is provided", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        response: "Task complete.",
      });
      expect(result).toContain("## Response");
    });

    it("renders the response content after the Response heading", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        response: "Task complete.",
      });
      const headingPos = result.indexOf("## Response");
      const contentPos = result.indexOf("Task complete.", headingPos);
      expect(contentPos).toBeGreaterThan(headingPos);
    });

    it("omits the Response section when response is undefined", () => {
      const result = renderOutput({ agentInstanceId: AGENT_ID, timestamp: FIXED_TS });
      expect(result).not.toContain("## Response");
    });

    it("omits the Response section when response is null", () => {
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        response: null as unknown as string,
      });
      expect(result).not.toContain("## Response");
    });

    it("preserves multi-line response text verbatim", () => {
      const multiLineResponse =
        '{"status_code": "SUCCESS", "status_message": "Done."}\n\nWith extra context.';
      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
        response: multiLineResponse,
      });
      expect(result).toContain(multiLineResponse);
    });
  });

  describe("full document with all fields", () => {
    it("produces the complete expected markdown document", () => {
      const expected = [
        "# Invocation Output: Research#1\n",
        "\n",
        "| Field | Value |\n",
        "|---|---|\n",
        "| Agent Instance ID | Research#1 |\n",
        "| Agent Type | Research |\n",
        `| Session ID | ${SESSION_ID} |\n`,
        `| Run ID | ${RUN_ID} |\n`,
        "| Harness | opencode |\n",
        `| Captured At | ${FIXED_TS} |\n`,
        "| Status Code | SUCCESS |\n",
        "| Model | claude-sonnet-4-5 |\n",
        "| Input Tokens | 1000 |\n",
        "| Output Tokens | 500 |\n",
        "| Cache Read Tokens | 200 |\n",
        "| Cache Creation Tokens | 100 |\n",
        "\n## Response\n\n",
        "The final response text.",
        "\n",
      ].join("");

      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        agentType: "Research",
        sessionId: SESSION_ID,
        runId: RUN_ID,
        timestamp: FIXED_TS,
        statusCode: "SUCCESS",
        model: "claude-sonnet-4-5",
        tokenUsage: {
          input_tokens: 1000,
          output_tokens: 500,
          cache_read_tokens: 200,
          cache_creation_tokens: 100,
        },
        response: "The final response text.",
      });

      expect(result).toBe(expected);
    });

    it("produces the expected document with all optional fields absent", () => {
      const expected = [
        "# Invocation Output: Research#1\n",
        "\n",
        "| Field | Value |\n",
        "|---|---|\n",
        "| Agent Instance ID | Research#1 |\n",
        "| Harness | opencode |\n",
        `| Captured At | ${FIXED_TS} |\n`,
      ].join("");

      const result = renderOutput({
        agentInstanceId: AGENT_ID,
        timestamp: FIXED_TS,
      });

      expect(result).toBe(expected);
    });
  });
});

// ---------------------------------------------------------------------------
// writeArtifact
// ---------------------------------------------------------------------------

describe("writeArtifact", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = nodeFs.mkdtempSync(
      nodePath.join(nodeOs.tmpdir(), "mosaic-write-artifact-test-"),
    );
  });

  afterEach(() => {
    nodeFs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("writes text to the specified file and returns true", async () => {
    const filePath = nodePath.join(tempDir, "01_input.md");
    const text = "# Invocation Input: Agent#1\n\n| Field | Value |\n";
    const result = await writeArtifact(filePath, text);
    expect(result).toBe(true);
    expect(nodeFs.readFileSync(filePath, "utf-8")).toBe(text);
  });

  it("creates intermediate parent directories that do not exist", async () => {
    const nestedPath = nodePath.join(tempDir, "run-123", "Agent#1", "01_input.md");
    const text = "# Invocation Input: Agent#1\n";
    const result = await writeArtifact(nestedPath, text);
    expect(result).toBe(true);
    expect(nodeFs.existsSync(nestedPath)).toBe(true);
  });

  it("overwrites an existing artifact", async () => {
    const filePath = nodePath.join(tempDir, "01_input.md");
    nodeFs.writeFileSync(filePath, "old content");
    const newText = "# New content\n";
    const result = await writeArtifact(filePath, newText);
    expect(result).toBe(true);
    expect(nodeFs.readFileSync(filePath, "utf-8")).toBe(newText);
  });

  it("writes unicode text correctly", async () => {
    const filePath = nodePath.join(tempDir, "unicode.md");
    const text = "| Agent Instance ID | Résearch#1 — special chars |\n";
    await writeArtifact(filePath, text);
    expect(nodeFs.readFileSync(filePath, "utf-8")).toBe(text);
  });

  it("never throws — returns false when write fails for unrecoverable reasons", async () => {
    // Create a directory at the target path so the write will fail
    const dirAsFile = nodePath.join(tempDir, "conflicting-dir");
    nodeFs.mkdirSync(dirAsFile);
    await expect(writeArtifact(dirAsFile, "content")).resolves.toBe(false);
  });

  it("does not reject on failure", async () => {
    const dirAsFile = nodePath.join(tempDir, "another-dir");
    nodeFs.mkdirSync(dirAsFile);
    await expect(writeArtifact(dirAsFile, "content")).resolves.not.toThrow();
  });
});
