/**
 * runstate.test.ts — Tests for identity extraction and fallback ID generators.
 *
 * Covers:
 *   - extractRunId: structured-first, bare-fallback, no match, never-throw
 *   - extractInstanceId: structured-first, bare-fallback, name boundary conditions
 *   - extractStatusCode: closed enum, last-match-wins, structured key only
 *   - newRunId / fallbackInstanceId / fallbackCallId: format compliance
 *
 * Ports the behavioral contract established by the Python adapter's test suite
 * to verify behavioral parity across the TypeScript and Python implementations.
 * All tests fail until the implementation is complete (TDD RED phase).
 */

import { describe, it, expect } from "vitest";

import {
  extractRunId,
  extractInstanceId,
  extractStatusCode,
  VALID_STATUS_CODES,
  newRunId,
  fallbackInstanceId,
  fallbackCallId,
} from "../lib/runstate";

// ---------------------------------------------------------------------------
// Shared constants
// ---------------------------------------------------------------------------

const RUN_ID_FORMAT = /^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$/;
const FALLBACK_INSTANCE_FORMAT = /^[A-Za-z0-9_.-]+_[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$/;
const FALLBACK_CALL_FORMAT = /^[A-Za-z0-9_.-]+_[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$/;

// Realistic dispatch prompts (same content the Python adapter handles)
const DISPATCH_WITH_BOTH = JSON.stringify({
  agent_instance_id: "planner-tdd-soft#9",
  run_id: "20260101T110000Z-a1b2",
  task_description: "Write the plan.",
});

const DISPATCH_WITH_AGENT_ID_ONLY = JSON.stringify({
  agent_instance_id: "test-writer-tdd#3",
  task_description: "Write the tests.",
});

const DISPATCH_NO_EXTRACTABLE_ID =
  "Here are the instructions for the task. Please proceed.";

// ---------------------------------------------------------------------------
// extractRunId
// ---------------------------------------------------------------------------

describe("extractRunId", () => {
  describe("structured key-value matching (priority 1)", () => {
    it("extracts run_id from a JSON dispatch prompt with quoted key and quoted value", () => {
      expect(extractRunId(DISPATCH_WITH_BOTH)).toBe("20260101T110000Z-a1b2");
    });

    it("extracts run_id from explicit JSON key syntax", () => {
      const text = '{"run_id": "20260727T170000Z-a3f9", "task": "work"}';
      expect(extractRunId(text)).toBe("20260727T170000Z-a3f9");
    });

    it("extracts run_id from YAML-style key-colon-value syntax without surrounding quotes", () => {
      const text = "run_id: 20260101T170000Z-a3f9\nsome other content";
      expect(extractRunId(text)).toBe("20260101T170000Z-a3f9");
    });

    it("extracts run_id from YAML-style key with quoted value", () => {
      const text = 'run_id: "20260101T170000Z-a3f9"\nsome other content';
      expect(extractRunId(text)).toBe("20260101T170000Z-a3f9");
    });

    it("structured match takes priority over an earlier bare occurrence", () => {
      // The bare pattern "20260101T170000Z-aaaa" appears first, but structured wins.
      const text =
        "Earlier bare: 20260101T170000Z-aaaa\n" +
        '{"run_id": "20260101T170000Z-bbbb"}';
      expect(extractRunId(text)).toBe("20260101T170000Z-bbbb");
    });

    it("extracts run_id when key has optional whitespace around separator", () => {
      const text = 'run_id = "20260201T080000Z-ff99"';
      expect(extractRunId(text)).toBe("20260201T080000Z-ff99");
    });
  });

  describe("bare pattern matching (fallback)", () => {
    it("extracts run_id by bare pattern when no structured key is present", () => {
      const text = "The run ID is 20260101T170000Z-a3f9 and it was successful.";
      expect(extractRunId(text)).toBe("20260101T170000Z-a3f9");
    });

    it("returns earliest bare occurrence when multiple bare matches exist", () => {
      const text =
        "first: 20260101T090000Z-1111 and later: 20260101T100000Z-2222";
      expect(extractRunId(text)).toBe("20260101T090000Z-1111");
    });

    it("extracts bare run_id at the start of text (no preceding character)", () => {
      const text = "20260101T170000Z-a3f9 is the run identifier.";
      expect(extractRunId(text)).toBe("20260101T170000Z-a3f9");
    });

    it("does not extract a bare match when immediately preceded by an alnum character", () => {
      // "xrun20260101T170000Z-a3f9" — the pattern is immediately preceded by 'n' (alnum)
      const text = "The xrun20260101T170000Z-a3f9 is not a valid run_id here.";
      // The embedded occurrence is not a confident bare match
      expect(extractRunId(text)).toBeUndefined();
    });
  });

  describe("no match cases", () => {
    it("returns undefined when text contains no run_id", () => {
      expect(extractRunId("The agent ran the task successfully.")).toBeUndefined();
    });

    it("returns undefined when text has a partial format (wrong digit count)", () => {
      const text = "Partial: 2026010T170000Z-a3f9 is not a match.";
      expect(extractRunId(text)).toBeUndefined();
    });

    it("returns undefined when the hex suffix has uppercase letters", () => {
      // Format requires lowercase hex (a-f), not uppercase (A-F)
      const text = "Not valid: 20260101T170000Z-A3F9";
      expect(extractRunId(text)).toBeUndefined();
    });

    it("returns undefined for a dispatch prompt with no run_id", () => {
      expect(extractRunId(DISPATCH_WITH_AGENT_ID_ONLY)).toBeUndefined();
    });

    it("returns undefined for plain prose with no ID pattern", () => {
      expect(extractRunId(DISPATCH_NO_EXTRACTABLE_ID)).toBeUndefined();
    });
  });

  describe("null / undefined / empty input", () => {
    it("returns undefined for null input", () => {
      expect(extractRunId(null)).toBeUndefined();
    });

    it("returns undefined for undefined input", () => {
      expect(extractRunId(undefined)).toBeUndefined();
    });

    it("returns undefined for empty string", () => {
      expect(extractRunId("")).toBeUndefined();
    });
  });

  describe("never-throw contract", () => {
    it("does not throw on null", () => {
      expect(() => extractRunId(null)).not.toThrow();
    });

    it("does not throw on undefined", () => {
      expect(() => extractRunId(undefined)).not.toThrow();
    });

    it("does not throw on very long input", () => {
      const big = "content ".repeat(10_000) + '{"run_id": "20260101T120000Z-abcd"}';
      expect(() => extractRunId(big)).not.toThrow();
    });

    it("does not throw on control characters", () => {
      expect(() => extractRunId("\x00\x01run_id 20260101T120000Z-abcd\xff")).not.toThrow();
    });
  });
});

// ---------------------------------------------------------------------------
// extractInstanceId
// ---------------------------------------------------------------------------

describe("extractInstanceId", () => {
  describe("structured key-value matching (priority 1)", () => {
    it("extracts agent_instance_id from a JSON dispatch prompt", () => {
      const text = DISPATCH_WITH_BOTH;
      expect(extractInstanceId(text)).toBe("planner-tdd-soft#9");
    });

    it("extracts agent_instance_id from explicit JSON key syntax", () => {
      const text = '{"agent_instance_id": "TestWriter#28", "task": "do something"}';
      expect(extractInstanceId(text)).toBe("TestWriter#28");
    });

    it("extracts agent_instance_id from YAML-style key without quotes", () => {
      const text = "agent_instance_id: ReviewAgent#5\nsome context";
      expect(extractInstanceId(text)).toBe("ReviewAgent#5");
    });

    it("extracts agent_instance_id from structured key using equals separator", () => {
      const text = "agent_instance_id = PlanWriter#3";
      expect(extractInstanceId(text)).toBe("PlanWriter#3");
    });

    it("structured match takes priority over an earlier bare occurrence", () => {
      // DataAnalyst#1 appears as bare text first, but structured key wins.
      const text =
        "Context mentions DataAnalyst#1 in passing.\n" +
        '{"agent_instance_id": "Researcher#7"}';
      expect(extractInstanceId(text)).toBe("Researcher#7");
    });

    it("handles agent name with hyphens in structured key", () => {
      const text = '{"agent_instance_id": "planner-tdd-soft#9"}';
      expect(extractInstanceId(text)).toBe("planner-tdd-soft#9");
    });

    it("handles agent name with dots in structured key", () => {
      const text = '{"agent_instance_id": "Research.Agent.v2#3"}';
      expect(extractInstanceId(text)).toBe("Research.Agent.v2#3");
    });

    it("handles agent name with underscores in structured key", () => {
      const text = '{"agent_instance_id": "test_writer_tdd#6"}';
      expect(extractInstanceId(text)).toBe("test_writer_tdd#6");
    });
  });

  describe("bare pattern matching (fallback)", () => {
    it("extracts AgentName#Number from plain dispatch text when no structured key", () => {
      const text =
        "Execute the task for DataProcessor#5. Here is the context...";
      expect(extractInstanceId(text)).toBe("DataProcessor#5");
    });

    it("extracts earliest bare occurrence when multiple bare matches exist", () => {
      const text = "PlanWriter#1 handed off to Reviewer#2 for checking.";
      expect(extractInstanceId(text)).toBe("PlanWriter#1");
    });

    it("extracts bare match at start of text (no preceding character)", () => {
      const text = "Researcher#3: please complete the analysis task.";
      expect(extractInstanceId(text)).toBe("Researcher#3");
    });

    it("extracts bare match with hyphens in agent name", () => {
      const text = "The contracts-designer#4 will work on this.";
      expect(extractInstanceId(text)).toBe("contracts-designer#4");
    });

    it("extracts bare match with multi-digit number", () => {
      const text = "Assigned to TestWriter#28 for the TDD phase.";
      expect(extractInstanceId(text)).toBe("TestWriter#28");
    });
  });

  describe("no match cases", () => {
    it("returns undefined when text contains no agent instance ID pattern", () => {
      expect(extractInstanceId("The agent ran the task successfully.")).toBeUndefined();
    });

    it("returns undefined for plain prose with no extractable ID", () => {
      expect(extractInstanceId(DISPATCH_NO_EXTRACTABLE_ID)).toBeUndefined();
    });

    it("returns undefined for a dispatch prompt containing only run_id", () => {
      const text = '{"run_id": "20260101T170000Z-a3f9", "task": "work"}';
      expect(extractInstanceId(text)).toBeUndefined();
    });
  });

  describe("null / undefined / empty input", () => {
    it("returns undefined for null input", () => {
      expect(extractInstanceId(null)).toBeUndefined();
    });

    it("returns undefined for undefined input", () => {
      expect(extractInstanceId(undefined)).toBeUndefined();
    });

    it("returns undefined for empty string", () => {
      expect(extractInstanceId("")).toBeUndefined();
    });
  });

  describe("never-throw contract", () => {
    it("does not throw on null", () => {
      expect(() => extractInstanceId(null)).not.toThrow();
    });

    it("does not throw on undefined", () => {
      expect(() => extractInstanceId(undefined)).not.toThrow();
    });

    it("does not throw on very long input", () => {
      const big = "context ".repeat(10_000) + '{"agent_instance_id": "Agent#1"}';
      expect(() => extractInstanceId(big)).not.toThrow();
    });

    it("does not throw on control characters", () => {
      expect(
        () => extractInstanceId("\x00\x01agent_instance_id Agent#1\xff"),
      ).not.toThrow();
    });
  });
});

// ---------------------------------------------------------------------------
// VALID_STATUS_CODES
// ---------------------------------------------------------------------------

describe("VALID_STATUS_CODES", () => {
  it("contains all six protocol status codes", () => {
    expect(VALID_STATUS_CODES.has("SUCCESS")).toBe(true);
    expect(VALID_STATUS_CODES.has("COMPLETED_NEEDS_ACTION")).toBe(true);
    expect(VALID_STATUS_CODES.has("PARTIALLY_DONE")).toBe(true);
    expect(VALID_STATUS_CODES.has("NEEDS_CLARIFICATION")).toBe(true);
    expect(VALID_STATUS_CODES.has("CAPABILITY_EXCEEDED")).toBe(true);
    expect(VALID_STATUS_CODES.has("BLOCKED")).toBe(true);
  });

  it("contains exactly six entries", () => {
    expect(VALID_STATUS_CODES.size).toBe(6);
  });
});

// ---------------------------------------------------------------------------
// extractStatusCode
// ---------------------------------------------------------------------------

describe("extractStatusCode", () => {
  describe("extracts each valid status code", () => {
    it("extracts SUCCESS", () => {
      const msg =
        '{"agent_instance_id": "A#1", "status_code": "SUCCESS", "status_message": "ok"}';
      expect(extractStatusCode(msg)).toBe("SUCCESS");
    });

    it("extracts COMPLETED_NEEDS_ACTION", () => {
      const msg =
        '{"status_code": "COMPLETED_NEEDS_ACTION", "status_message": "found issues"}';
      expect(extractStatusCode(msg)).toBe("COMPLETED_NEEDS_ACTION");
    });

    it("extracts PARTIALLY_DONE", () => {
      const msg =
        '{"status_code": "PARTIALLY_DONE", "status_message": "stopping early"}';
      expect(extractStatusCode(msg)).toBe("PARTIALLY_DONE");
    });

    it("extracts NEEDS_CLARIFICATION", () => {
      const msg =
        '{"status_code": "NEEDS_CLARIFICATION", "status_message": "unclear"}';
      expect(extractStatusCode(msg)).toBe("NEEDS_CLARIFICATION");
    });

    it("extracts CAPABILITY_EXCEEDED", () => {
      const msg =
        '{"status_code": "CAPABILITY_EXCEEDED", "status_message": "too hard"}';
      expect(extractStatusCode(msg)).toBe("CAPABILITY_EXCEEDED");
    });

    it("extracts BLOCKED", () => {
      const msg = '{"status_code": "BLOCKED", "error_code": "E101"}';
      expect(extractStatusCode(msg)).toBe("BLOCKED");
    });
  });

  describe("last-match-wins behavior", () => {
    it("returns the last structured occurrence when multiple exist", () => {
      const msg =
        'First block: {"status_code": "SUCCESS", "status_message": "early"}\n' +
        "Some intervening text.\n" +
        'Final block: {"status_code": "BLOCKED", "error_code": "E501"}';
      expect(extractStatusCode(msg)).toBe("BLOCKED");
    });

    it("returns the last occurrence in a multi-line response", () => {
      const msg =
        '{"status_code": "PARTIALLY_DONE"}\n' +
        "Additional analysis...\n" +
        '{"status_code": "SUCCESS"}';
      expect(extractStatusCode(msg)).toBe("SUCCESS");
    });
  });

  describe("rejects non-structured occurrences", () => {
    it("returns undefined when status name appears in free prose without key", () => {
      const msg = "The operation completed with SUCCESS in every aspect.";
      expect(extractStatusCode(msg)).toBeUndefined();
    });

    it("returns undefined when BLOCKED appears without structured key", () => {
      const msg = "This request is BLOCKED by the policy.";
      expect(extractStatusCode(msg)).toBeUndefined();
    });

    it("returns undefined when status name is unquoted in the value position", () => {
      // Key is present but value is not quoted — must not match
      const msg = "status_code: SUCCESS without quotes";
      expect(extractStatusCode(msg)).toBeUndefined();
    });
  });

  describe("rejects values outside the closed enum", () => {
    it("returns undefined for an unrecognized status value", () => {
      const msg =
        '{"status_code": "CUSTOM_STATUS", "status_message": "something"}';
      expect(extractStatusCode(msg)).toBeUndefined();
    });

    it("returns undefined for a lowercase valid-looking value", () => {
      // Status codes are uppercase only
      const msg = '{"status_code": "success"}';
      expect(extractStatusCode(msg)).toBeUndefined();
    });

    it("returns undefined for an empty quoted value", () => {
      const msg = '{"status_code": "", "status_message": "empty"}';
      expect(extractStatusCode(msg)).toBeUndefined();
    });
  });

  describe("absent status block", () => {
    it("returns undefined when no status_code key is present at all", () => {
      const msg =
        "The agent ran the task and produced an output with no status block.";
      expect(extractStatusCode(msg)).toBeUndefined();
    });

    it("never defaults to SUCCESS when no status block is found", () => {
      const result = extractStatusCode("Task completed successfully.");
      expect(result).not.toBe("SUCCESS");
    });

    it("returns undefined not an empty string when status is absent", () => {
      const result = extractStatusCode("No structured block here.");
      expect(result).toBeUndefined();
    });
  });

  describe("null / undefined / empty input", () => {
    it("returns undefined for null input", () => {
      expect(extractStatusCode(null)).toBeUndefined();
    });

    it("returns undefined for undefined input", () => {
      expect(extractStatusCode(undefined)).toBeUndefined();
    });

    it("returns undefined for empty string", () => {
      expect(extractStatusCode("")).toBeUndefined();
    });
  });

  describe("never-throw contract", () => {
    it("does not throw on null", () => {
      expect(() => extractStatusCode(null)).not.toThrow();
    });

    it("does not throw on undefined", () => {
      expect(() => extractStatusCode(undefined)).not.toThrow();
    });

    it("does not throw on malformed JSON surrounding the status block", () => {
      expect(() =>
        extractStatusCode('{"status_code": broken json {{{'),
      ).not.toThrow();
    });

    it("does not throw on very long input", () => {
      const big =
        "content ".repeat(10_000) + '{"status_code": "SUCCESS"}';
      expect(() => extractStatusCode(big)).not.toThrow();
    });

    it("does not throw on control characters", () => {
      expect(() =>
        extractStatusCode("\x00\x01\x02status_code SUCCESS\xff"),
      ).not.toThrow();
    });
  });
});

// ---------------------------------------------------------------------------
// Fallback ID generators — format compliance
// ---------------------------------------------------------------------------

describe("newRunId", () => {
  it("returns a string matching the {YYYYMMDD}T{HHMMSS}Z-{4hex} format", () => {
    expect(newRunId()).toMatch(RUN_ID_FORMAT);
  });

  it("returns a string of exactly 21 characters", () => {
    // 8 digits + T + 6 digits + Z + - + 4 hex = 21 chars
    expect(newRunId()).toHaveLength(21);
  });

  it("returns different values on successive calls (randomness)", () => {
    const a = newRunId();
    const b = newRunId();
    // Very unlikely to be equal; hex suffix is random
    expect(a).not.toBe(b);
  });

  it("produces a timestamp portion that reflects approximately the current time", () => {
    const before = new Date();
    const id = newRunId();
    const after = new Date();

    // Extract date portion: YYYYMMDD
    const year = parseInt(id.slice(0, 4), 10);
    const month = parseInt(id.slice(4, 6), 10);
    const day = parseInt(id.slice(6, 8), 10);

    expect(year).toBeGreaterThanOrEqual(before.getUTCFullYear());
    expect(year).toBeLessThanOrEqual(after.getUTCFullYear() + 1);
    expect(month).toBeGreaterThanOrEqual(1);
    expect(month).toBeLessThanOrEqual(12);
    expect(day).toBeGreaterThanOrEqual(1);
    expect(day).toBeLessThanOrEqual(31);
  });

  it("suffix portion contains only lowercase hex characters", () => {
    const id = newRunId();
    const suffix = id.slice(-4);
    expect(suffix).toMatch(/^[0-9a-f]{4}$/);
  });
});

describe("fallbackInstanceId", () => {
  it("returns a string matching the {prefix}_{YYYYMMDD}T{HHMMSS}Z-{4hex} format", () => {
    expect(fallbackInstanceId("agent")).toMatch(FALLBACK_INSTANCE_FORMAT);
  });

  it("uses the provided prefix before the underscore separator", () => {
    const id = fallbackInstanceId("Researcher");
    expect(id.startsWith("Researcher_")).toBe(true);
  });

  it("uses 'agent' as the prefix when called with no argument", () => {
    const id = fallbackInstanceId();
    expect(id.startsWith("agent_")).toBe(true);
  });

  it("uses 'agent' as the prefix when called with empty string", () => {
    const id = fallbackInstanceId("");
    expect(id.startsWith("agent_")).toBe(true);
  });

  it("uses 'agent' as the prefix when called with undefined", () => {
    const id = fallbackInstanceId(undefined);
    expect(id.startsWith("agent_")).toBe(true);
  });

  it("suffix after prefix contains the compact timestamp format", () => {
    const id = fallbackInstanceId("test");
    const rest = id.slice("test_".length);
    expect(rest).toMatch(RUN_ID_FORMAT);
  });

  it("returns different values on successive calls (randomness)", () => {
    const a = fallbackInstanceId("agent");
    const b = fallbackInstanceId("agent");
    expect(a).not.toBe(b);
  });

  it("preserves hyphens and dots in the provided prefix", () => {
    const id = fallbackInstanceId("planner-tdd-soft");
    expect(id.startsWith("planner-tdd-soft_")).toBe(true);
  });
});

describe("fallbackCallId", () => {
  it("returns a string matching the {toolName}_{YYYYMMDD}T{HHMMSS}Z-{4hex} format", () => {
    expect(fallbackCallId("bash")).toMatch(FALLBACK_CALL_FORMAT);
  });

  it("uses the provided tool name before the underscore separator", () => {
    const id = fallbackCallId("read");
    expect(id.startsWith("read_")).toBe(true);
  });

  it("uses 'tool' as the prefix when called with no argument", () => {
    const id = fallbackCallId();
    expect(id.startsWith("tool_")).toBe(true);
  });

  it("uses 'tool' as the prefix when called with empty string", () => {
    const id = fallbackCallId("");
    expect(id.startsWith("tool_")).toBe(true);
  });

  it("uses 'tool' as the prefix when called with undefined", () => {
    const id = fallbackCallId(undefined);
    expect(id.startsWith("tool_")).toBe(true);
  });

  it("suffix after tool name contains the compact timestamp format", () => {
    const id = fallbackCallId("edit");
    const rest = id.slice("edit_".length);
    expect(rest).toMatch(RUN_ID_FORMAT);
  });

  it("returns different values on successive calls (randomness)", () => {
    const a = fallbackCallId("bash");
    const b = fallbackCallId("bash");
    expect(a).not.toBe(b);
  });
});
