/**
 * core.test.ts — Tests for core utilities of the mosaic-logger OpenCode adapter.
 *
 * Covers:
 *   - Event envelope building (buildEvent) and recursive prune logic
 *   - Timestamp helper (currentTimestamp)
 *   - Path layout (LogPaths)
 *   - Filename sanitization (sanitizeComponent)
 *   - File I/O primitives (appendEvent, atomicReplace, atomicReplaceText)
 *   - Effective run_id resolution (effectiveRunId)
 *   - Debug logger API (setDebugLogger / debugLog)
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";

import {
  SCHEMA_VERSION,
  HARNESS,
  LOGS_DIRNAME,
  currentTimestamp,
  buildEvent,
  prune,
  effectiveRunId,
  appendEvent,
  atomicReplace,
  atomicReplaceText,
  sanitizeComponent,
  LogPaths,
  setDebugLogger,
  debugLog,
  readAdapterVersion,
  type DebugLogFn,
} from "../lib/core";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

describe("constants", () => {
  it("SCHEMA_VERSION is 1.0.0", () => {
    expect(SCHEMA_VERSION).toBe("1.0.0");
  });

  it("HARNESS is opencode", () => {
    expect(HARNESS).toBe("opencode");
  });

  it("LOGS_DIRNAME is OrchestrationLogs", () => {
    expect(LOGS_DIRNAME).toBe("OrchestrationLogs");
  });
});

// ---------------------------------------------------------------------------
// Timestamp helpers
// ---------------------------------------------------------------------------

describe("currentTimestamp", () => {
  it("returns a string matching ISO 8601 with millisecond precision and Z suffix", () => {
    const ts = currentTimestamp();
    expect(typeof ts).toBe("string");
    expect(ts).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
  });

  it("returns a timestamp within a few seconds of now", () => {
    const before = Date.now();
    const ts = currentTimestamp();
    const after = Date.now();
    const parsed = new Date(ts).getTime();
    expect(parsed).toBeGreaterThanOrEqual(before);
    expect(parsed).toBeLessThanOrEqual(after + 1000);
  });

  it("millisecond component reflects real sub-second precision, not a hardcoded zero", () => {
    const nowMs = Date.now();
    const ts = currentTimestamp();

    // Extract the ms field directly from the string (e.g. "2024-01-15T10:30:00.123Z" -> 123)
    const msMatch = ts.match(/\.(\d{3})Z$/);
    expect(msMatch).not.toBeNull();
    const tsMs = parseInt(msMatch![1], 10);

    // Verify the ms field in the string equals what JavaScript's Date parsing produces
    // from the same string — an implementation that hardcodes ".000Z" would fail this
    // if Date.now() happens to have a non-zero ms component at call time.
    expect(tsMs).toBe(new Date(ts).getMilliseconds());

    // Verify the ms field is within 50ms of Date.now() captured at call time,
    // accounting for modular rollover at second boundaries.
    const expectedMs = nowMs % 1000;
    const delta = Math.min(
      Math.abs(tsMs - expectedMs),
      1000 - Math.abs(tsMs - expectedMs),
    );
    expect(delta).toBeLessThan(50);
  });
});

// ---------------------------------------------------------------------------
// Prune logic
// ---------------------------------------------------------------------------

describe("prune", () => {
  describe("values that are dropped (keep=false)", () => {
    it("drops null", () => {
      const [, keep] = prune(null);
      expect(keep).toBe(false);
    });

    it("drops undefined", () => {
      const [, keep] = prune(undefined);
      expect(keep).toBe(false);
    });

    it("drops empty string", () => {
      const [, keep] = prune("");
      expect(keep).toBe(false);
    });

    it("drops empty object", () => {
      const [, keep] = prune({});
      expect(keep).toBe(false);
    });

    it("drops empty array", () => {
      const [, keep] = prune([]);
      expect(keep).toBe(false);
    });

    it("drops an object that is entirely empty after recursive pruning", () => {
      const [, keep] = prune({ a: null, b: "", c: {} });
      expect(keep).toBe(false);
    });

    it("drops a deeply nested object that empties out recursively", () => {
      const [, keep] = prune({ outer: { inner: null } });
      expect(keep).toBe(false);
    });
  });

  describe("values that are kept (keep=true)", () => {
    it("keeps false — genuine resolved boolean", () => {
      const [val, keep] = prune(false);
      expect(keep).toBe(true);
      expect(val).toBe(false);
    });

    it("keeps 0 — genuine resolved number", () => {
      const [val, keep] = prune(0);
      expect(keep).toBe(true);
      expect(val).toBe(0);
    });

    it("keeps true", () => {
      const [val, keep] = prune(true);
      expect(keep).toBe(true);
      expect(val).toBe(true);
    });

    it("keeps non-empty string", () => {
      const [val, keep] = prune("hello");
      expect(keep).toBe(true);
      expect(val).toBe("hello");
    });

    it("keeps positive number", () => {
      const [val, keep] = prune(42);
      expect(keep).toBe(true);
      expect(val).toBe(42);
    });

    it("keeps non-empty array", () => {
      const [val, keep] = prune([1, 2, 3]);
      expect(keep).toBe(true);
      expect(val).toEqual([1, 2, 3]);
    });

    it("keeps non-empty object", () => {
      const [val, keep] = prune({ a: 1 });
      expect(keep).toBe(true);
      expect(val).toEqual({ a: 1 });
    });
  });

  describe("recursive pruning of nested objects", () => {
    it("strips null entries from a mixed object, keeps non-null", () => {
      const [val, keep] = prune({ a: null, b: "hello" });
      expect(keep).toBe(true);
      expect(val).toEqual({ b: "hello" });
    });

    it("keeps false inside a nested object", () => {
      const [val, keep] = prune({ a: false, b: null });
      expect(keep).toBe(true);
      expect(val).toEqual({ a: false });
    });

    it("keeps 0 inside a nested object", () => {
      const [val, keep] = prune({ count: 0, empty: "" });
      expect(keep).toBe(true);
      expect(val).toEqual({ count: 0 });
    });

    it("prunes multiple levels deep", () => {
      const input = {
        outer: {
          inner: "value",
          empty: null,
        },
        removed: "",
      };
      const [val, keep] = prune(input);
      expect(keep).toBe(true);
      expect(val).toEqual({ outer: { inner: "value" } });
    });

    it("an object with only empty-string values becomes empty and is dropped", () => {
      const [, keep] = prune({ a: "", b: "" });
      expect(keep).toBe(false);
    });
  });

  describe("array element pruning boundary", () => {
    it("keeps a non-empty array whose elements are pruneable — arrays are not element-pruned", () => {
      // The spec prunes nested objects recursively but does not prune array elements
      // individually. A non-empty array (even one containing only null/"") is kept as-is.
      // An implementation that element-prunes arrays would reduce [null, ""] to [],
      // then drop the outer array — this test pins the behavior as: non-empty array, keep=true.
      const [val, keep] = prune([null, ""]);
      expect(keep).toBe(true);
      expect(val).toEqual([null, ""]);
    });
  });
});

// ---------------------------------------------------------------------------
// buildEvent
// ---------------------------------------------------------------------------

describe("buildEvent", () => {
  const FIXED_TS = "2024-01-15T10:30:00.123Z";
  const SAMPLE_RUN_ID = "20240115T103000Z-abcd";

  describe("common envelope fields", () => {
    it("sets schema_version to SCHEMA_VERSION constant", () => {
      const result = buildEvent("test_event", { timestamp: FIXED_TS }, {});
      expect(result["schema_version"]).toBe("1.0.0");
    });

    it("sets event to the provided event name", () => {
      const result = buildEvent("session_start", { timestamp: FIXED_TS }, {});
      expect(result["event"]).toBe("session_start");
    });

    it("sets timestamp from the envelope parameter", () => {
      const result = buildEvent("test_event", { timestamp: FIXED_TS }, {});
      expect(result["timestamp"]).toBe(FIXED_TS);
    });

    it("sets harness to opencode", () => {
      const result = buildEvent("test_event", { timestamp: FIXED_TS }, {});
      expect(result["harness"]).toBe("opencode");
    });
  });

  describe("session_id and run_id in envelope", () => {
    it("includes session_id when provided", () => {
      const result = buildEvent(
        "test_event",
        { sessionId: "sess-abc-123", timestamp: FIXED_TS },
        {},
      );
      expect(result["session_id"]).toBe("sess-abc-123");
    });

    it("omits session_id when not provided", () => {
      const result = buildEvent("test_event", { timestamp: FIXED_TS }, {});
      expect("session_id" in result).toBe(false);
    });

    it("omits session_id when empty string", () => {
      const result = buildEvent(
        "test_event",
        { sessionId: "", timestamp: FIXED_TS },
        {},
      );
      expect("session_id" in result).toBe(false);
    });

    it("includes run_id when provided", () => {
      const result = buildEvent(
        "test_event",
        { runId: SAMPLE_RUN_ID, timestamp: FIXED_TS },
        {},
      );
      expect(result["run_id"]).toBe(SAMPLE_RUN_ID);
    });

    it("omits run_id when not provided", () => {
      const result = buildEvent("test_event", { timestamp: FIXED_TS }, {});
      expect("run_id" in result).toBe(false);
    });

    it("omits run_id when empty string", () => {
      const result = buildEvent(
        "test_event",
        { runId: "", timestamp: FIXED_TS },
        {},
      );
      expect("run_id" in result).toBe(false);
    });

    it("includes both session_id and run_id when both provided", () => {
      const result = buildEvent(
        "run_start",
        { sessionId: "sess-xyz", runId: SAMPLE_RUN_ID, timestamp: FIXED_TS },
        {},
      );
      expect(result["session_id"]).toBe("sess-xyz");
      expect(result["run_id"]).toBe(SAMPLE_RUN_ID);
    });
  });

  describe("extra fields merging", () => {
    it("includes extra fields from the fields parameter", () => {
      const result = buildEvent(
        "invocation_start",
        { timestamp: FIXED_TS },
        { agent_instance_id: "Research#1", agent_type: "Research" },
      );
      expect(result["agent_instance_id"]).toBe("Research#1");
      expect(result["agent_type"]).toBe("Research");
    });

    it("keeps false in extra fields", () => {
      const result = buildEvent(
        "test_event",
        { timestamp: FIXED_TS },
        { success: false },
      );
      expect(result["success"]).toBe(false);
    });

    it("keeps 0 in extra fields", () => {
      const result = buildEvent(
        "test_event",
        { timestamp: FIXED_TS },
        { token_count: 0 },
      );
      expect(result["token_count"]).toBe(0);
    });
  });

  describe("prune logic applied to extra fields", () => {
    it("drops null extra fields", () => {
      const result = buildEvent(
        "test_event",
        { timestamp: FIXED_TS },
        { missing: null },
      );
      expect("missing" in result).toBe(false);
    });

    it("drops undefined extra fields", () => {
      const result = buildEvent(
        "test_event",
        { timestamp: FIXED_TS },
        { missing: undefined },
      );
      expect("missing" in result).toBe(false);
    });

    it("drops empty string extra fields", () => {
      const result = buildEvent(
        "test_event",
        { timestamp: FIXED_TS },
        { agent_type: "" },
      );
      expect("agent_type" in result).toBe(false);
    });

    it("drops empty object extra fields", () => {
      const result = buildEvent(
        "test_event",
        { timestamp: FIXED_TS },
        { metadata: {} },
      );
      expect("metadata" in result).toBe(false);
    });

    it("drops empty array extra fields", () => {
      const result = buildEvent(
        "test_event",
        { timestamp: FIXED_TS },
        { items: [] },
      );
      expect("items" in result).toBe(false);
    });

    it("prunes nested objects in extra fields recursively", () => {
      const result = buildEvent(
        "test_event",
        { timestamp: FIXED_TS },
        {
          metadata: { empty: null, value: "present" },
        },
      );
      expect(result["metadata"]).toEqual({ value: "present" });
    });

    it("drops an extra field whose nested object empties out after pruning", () => {
      const result = buildEvent(
        "test_event",
        { timestamp: FIXED_TS },
        {
          metadata: { empty: null },
        },
      );
      expect("metadata" in result).toBe(false);
    });
  });
});

// ---------------------------------------------------------------------------
// LogPaths
// ---------------------------------------------------------------------------

describe("LogPaths", () => {
  const workspaceRoot = nodePath.join(nodeOs.tmpdir(), "mosaic-log-paths-test");
  const runId = "20240115T103000Z-abcd";
  let paths: LogPaths;

  beforeEach(() => {
    paths = new LogPaths(workspaceRoot);
  });

  describe("root", () => {
    it("root is workspaceRoot joined with OrchestrationLogs", () => {
      expect(paths.root).toBe(nodePath.join(workspaceRoot, "OrchestrationLogs"));
    });
  });

  describe("runRoot", () => {
    it("runRoot returns root joined with runId", () => {
      expect(paths.runRoot(runId)).toBe(nodePath.join(paths.root, runId));
    });
  });

  describe("orchestrator paths", () => {
    it("orchestratorEvents returns run root with 00_orchestrator_events.jsonl", () => {
      expect(paths.orchestratorEvents(runId)).toBe(
        nodePath.join(paths.root, runId, "00_orchestrator_events.jsonl"),
      );
    });

    it("orchestratorRaw returns run root with 00_orchestrator_session.raw", () => {
      expect(paths.orchestratorRaw(runId)).toBe(
        nodePath.join(paths.root, runId, "00_orchestrator_session.raw"),
      );
    });

    it("orchestratorRawMeta returns run root with 00_orchestrator_session.meta.json", () => {
      expect(paths.orchestratorRawMeta(runId)).toBe(
        nodePath.join(paths.root, runId, "00_orchestrator_session.meta.json"),
      );
    });
  });

  describe("invocation paths", () => {
    it("invocationDir returns run root joined with sanitized agentInstanceId", () => {
      // Research#1 — # is not a reserved char so it passes through unchanged
      expect(paths.invocationDir(runId, "Research#1")).toBe(
        nodePath.join(paths.root, runId, "Research#1"),
      );
    });

    it("invocationDir sanitizes reserved characters in agentInstanceId", () => {
      // colon is a reserved filesystem character, must become underscore
      expect(paths.invocationDir(runId, "Agent:Name")).toBe(
        nodePath.join(paths.root, runId, "Agent_Name"),
      );
    });

    it("invocationEvents returns invocationDir with 03_events.jsonl", () => {
      expect(paths.invocationEvents(runId, "Research#1")).toBe(
        nodePath.join(paths.root, runId, "Research#1", "03_events.jsonl"),
      );
    });

    it("invocationInput returns invocationDir with 01_input.md", () => {
      expect(paths.invocationInput(runId, "Research#1")).toBe(
        nodePath.join(paths.root, runId, "Research#1", "01_input.md"),
      );
    });

    it("invocationOutput returns invocationDir with 02_output.md", () => {
      expect(paths.invocationOutput(runId, "Research#1")).toBe(
        nodePath.join(paths.root, runId, "Research#1", "02_output.md"),
      );
    });

    it("invocationRaw returns invocationDir with 04_session.raw", () => {
      expect(paths.invocationRaw(runId, "Research#1")).toBe(
        nodePath.join(paths.root, runId, "Research#1", "04_session.raw"),
      );
    });

    it("invocationRawMeta returns invocationDir with 04_session.meta.json", () => {
      expect(paths.invocationRawMeta(runId, "Research#1")).toBe(
        nodePath.join(paths.root, runId, "Research#1", "04_session.meta.json"),
      );
    });
  });
});

// ---------------------------------------------------------------------------
// sanitizeComponent
// ---------------------------------------------------------------------------

describe("sanitizeComponent", () => {
  describe("passthrough for safe inputs", () => {
    it("returns normal alphanumeric identifier unchanged", () => {
      expect(sanitizeComponent("Research")).toBe("Research");
    });

    it("returns identifier with hash unchanged — # is not reserved", () => {
      expect(sanitizeComponent("Research#1")).toBe("Research#1");
    });

    it("returns identifier with hyphen unchanged", () => {
      expect(sanitizeComponent("my-agent")).toBe("my-agent");
    });

    it("returns identifier with underscore unchanged", () => {
      expect(sanitizeComponent("my_agent")).toBe("my_agent");
    });
  });

  describe("reserved filesystem characters replaced with underscore", () => {
    // Reserved set: <>:"/\|?*
    const cases: [string, string][] = [
      ["<test>", "_test_"],
      [">name", "_name"],
      ["file:name", "file_name"],
      ['say"hi"', "say_hi_"],
      ["path/file", "path_file"],
      ["back\\slash", "back_slash"],
      ["pipe|char", "pipe_char"],
      ["what?", "what_"],
      ["glob*", "glob_"],
    ];

    for (const [input, expected] of cases) {
      it(`sanitizes "${input}" to "${expected}"`, () => {
        expect(sanitizeComponent(input)).toBe(expected);
      });
    }
  });

  describe("control characters replaced with underscore", () => {
    it("replaces null byte (0x00) with underscore", () => {
      expect(sanitizeComponent("a\x00b")).toBe("a_b");
    });

    it("replaces control character 0x01 with underscore", () => {
      expect(sanitizeComponent("a\x01b")).toBe("a_b");
    });

    it("replaces tab (0x09) with underscore", () => {
      expect(sanitizeComponent("a\tb")).toBe("a_b");
    });

    it("replaces newline (0x0A) with underscore", () => {
      expect(sanitizeComponent("a\nb")).toBe("a_b");
    });

    it("replaces carriage return (0x0D) with underscore", () => {
      expect(sanitizeComponent("a\rb")).toBe("a_b");
    });

    it("replaces all control characters below 0x20", () => {
      // 0x1F is the last control character in the range
      expect(sanitizeComponent("a\x1Fb")).toBe("a_b");
    });
  });

  describe("trailing dots and spaces stripped", () => {
    it("strips a trailing dot", () => {
      expect(sanitizeComponent("name.")).toBe("name");
    });

    it("strips multiple trailing dots", () => {
      expect(sanitizeComponent("name...")).toBe("name");
    });

    it("strips trailing spaces", () => {
      expect(sanitizeComponent("name   ")).toBe("name");
    });

    it("strips mixed trailing dots and spaces", () => {
      expect(sanitizeComponent("name. .")).toBe("name");
    });

    it("does not strip dots from the middle of the name", () => {
      expect(sanitizeComponent("name.with.dots")).toBe("name.with.dots");
    });
  });

  describe("empty and fully-stripped inputs return underscore", () => {
    it("returns underscore for empty string", () => {
      expect(sanitizeComponent("")).toBe("_");
    });

    it("returns underscore for a string of only dots", () => {
      expect(sanitizeComponent("...")).toBe("_");
    });

    it("returns underscore for a string of only spaces", () => {
      expect(sanitizeComponent("   ")).toBe("_");
    });

    it("returns underscore for a string of only reserved chars that strip to empty", () => {
      // * is replaced with _ but then the result is non-empty, so verify stripping logic
      // A single dot becomes empty after strip -> underscore
      expect(sanitizeComponent(".")).toBe("_");
    });
  });
});

// ---------------------------------------------------------------------------
// effectiveRunId
// ---------------------------------------------------------------------------

describe("effectiveRunId", () => {
  it("returns the run_id when present", () => {
    expect(effectiveRunId("20240115T103000Z-abcd")).toBe(
      "20240115T103000Z-abcd",
    );
  });

  it("returns unknown-run when run_id is undefined", () => {
    expect(effectiveRunId(undefined)).toBe("unknown-run");
  });

  it("returns unknown-run when run_id is empty string", () => {
    expect(effectiveRunId("")).toBe("unknown-run");
  });
});

// ---------------------------------------------------------------------------
// appendEvent
// ---------------------------------------------------------------------------

describe("appendEvent", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = nodeFs.mkdtempSync(
      nodePath.join(nodeOs.tmpdir(), "mosaic-append-test-"),
    );
  });

  afterEach(() => {
    nodeFs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("creates the target file and writes one JSONL line", async () => {
    const filePath = nodePath.join(tempDir, "events.jsonl");
    const event = { event: "session_start", harness: "opencode" };
    await appendEvent(filePath, event);

    const content = nodeFs.readFileSync(filePath, "utf-8");
    expect(content).toBe(JSON.stringify(event) + "\n");
  });

  it("appends multiple events as separate JSONL lines", async () => {
    const filePath = nodePath.join(tempDir, "events.jsonl");
    const event1 = { event: "session_start" };
    const event2 = { event: "session_end" };
    await appendEvent(filePath, event1);
    await appendEvent(filePath, event2);

    const lines = nodeFs.readFileSync(filePath, "utf-8").trim().split("\n");
    expect(lines).toHaveLength(2);
    expect(JSON.parse(lines[0])).toEqual(event1);
    expect(JSON.parse(lines[1])).toEqual(event2);
  });

  it("creates parent directories that do not yet exist", async () => {
    const nestedPath = nodePath.join(tempDir, "subdir", "nested", "events.jsonl");
    await appendEvent(nestedPath, { event: "test" });
    expect(nodeFs.existsSync(nestedPath)).toBe(true);
  });

  it("does not throw when the path is not writable — swallows the error", async () => {
    // Pointing at a directory as a file path should fail silently
    const dirAsFile = tempDir; // a directory, not a file — write will fail
    await expect(appendEvent(dirAsFile, { event: "test" })).resolves.toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// atomicReplace
// ---------------------------------------------------------------------------

describe("atomicReplace", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = nodeFs.mkdtempSync(
      nodePath.join(nodeOs.tmpdir(), "mosaic-atomic-test-"),
    );
  });

  afterEach(() => {
    nodeFs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("writes data to file and returns true on success", async () => {
    const filePath = nodePath.join(tempDir, "output.raw");
    const data = Buffer.from("hello world", "utf-8");
    const result = await atomicReplace(filePath, data);
    expect(result).toBe(true);
    expect(nodeFs.readFileSync(filePath)).toEqual(data);
  });

  it("overwrites an existing file atomically", async () => {
    const filePath = nodePath.join(tempDir, "output.raw");
    nodeFs.writeFileSync(filePath, "old content");
    const newData = Buffer.from("new content", "utf-8");
    const result = await atomicReplace(filePath, newData);
    expect(result).toBe(true);
    expect(nodeFs.readFileSync(filePath, "utf-8")).toBe("new content");
  });

  it("returns false when target directory does not exist — does not create directories", async () => {
    const missingDir = nodePath.join(tempDir, "nonexistent-dir");
    const filePath = nodePath.join(missingDir, "output.raw");
    const result = await atomicReplace(filePath, Buffer.from("data"));
    expect(result).toBe(false);
  });

  it("does not throw on failure — never rejects", async () => {
    const missingDir = nodePath.join(tempDir, "nonexistent-dir");
    const filePath = nodePath.join(missingDir, "output.raw");
    await expect(
      atomicReplace(filePath, Buffer.from("data")),
    ).resolves.toBe(false);
  });

  it("writes binary data correctly", async () => {
    const filePath = nodePath.join(tempDir, "binary.raw");
    const data = new Uint8Array([0x00, 0x01, 0xff, 0xfe]);
    const result = await atomicReplace(filePath, data);
    expect(result).toBe(true);
    const read = nodeFs.readFileSync(filePath);
    expect(Array.from(read)).toEqual([0x00, 0x01, 0xff, 0xfe]);
  });
});

// ---------------------------------------------------------------------------
// atomicReplaceText
// ---------------------------------------------------------------------------

describe("atomicReplaceText", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = nodeFs.mkdtempSync(
      nodePath.join(nodeOs.tmpdir(), "mosaic-atomic-text-test-"),
    );
  });

  afterEach(() => {
    nodeFs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("writes text as UTF-8 and returns true on success", async () => {
    const filePath = nodePath.join(tempDir, "doc.md");
    const result = await atomicReplaceText(filePath, "# Hello\n");
    expect(result).toBe(true);
    expect(nodeFs.readFileSync(filePath, "utf-8")).toBe("# Hello\n");
  });

  it("encodes non-ASCII text as UTF-8 correctly", async () => {
    const filePath = nodePath.join(tempDir, "unicode.md");
    const text = "Agent — Research#1: résumé\n";
    const result = await atomicReplaceText(filePath, text);
    expect(result).toBe(true);
    expect(nodeFs.readFileSync(filePath, "utf-8")).toBe(text);
  });

  it("returns false when target directory does not exist", async () => {
    const missingDir = nodePath.join(tempDir, "nonexistent");
    const filePath = nodePath.join(missingDir, "doc.md");
    const result = await atomicReplaceText(filePath, "hello");
    expect(result).toBe(false);
  });

  it("does not throw on failure — never rejects", async () => {
    const missingDir = nodePath.join(tempDir, "nonexistent");
    const filePath = nodePath.join(missingDir, "doc.md");
    await expect(atomicReplaceText(filePath, "hello")).resolves.toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Debug logging API
// ---------------------------------------------------------------------------

describe("setDebugLogger / debugLog", () => {
  beforeEach(() => {
    // Reset the module-level singleton to a known no-op state before every test
    // in this group. Without this reset, a throwing logger installed by one test
    // would persist into subsequent tests (including the "does not throw when no
    // logger set" test), producing incorrect test semantics.
    setDebugLogger(() => {});
  });

  it("debugLog does not throw when no logger has been set", () => {
    // debugLog should be a no-op until setDebugLogger is called, never throwing
    expect(() => debugLog("test message")).not.toThrow();
  });

  it("debugLog routes to the installed logger function", () => {
    const captured: Array<{ message: string; error: unknown }> = [];
    const testLogger: DebugLogFn = (message, error) => {
      captured.push({ message, error });
    };
    setDebugLogger(testLogger);

    debugLog("hello from test");
    expect(captured).toHaveLength(1);
    expect(captured[0].message).toBe("hello from test");
  });

  it("debugLog passes error argument to the logger", () => {
    const captured: Array<{ message: string; error: unknown }> = [];
    setDebugLogger((message, error) => captured.push({ message, error }));

    const err = new Error("something broke");
    debugLog("failure context", err);
    expect(captured[0].error).toBe(err);
  });

  it("debugLog does not throw even if the installed logger throws", () => {
    setDebugLogger(() => {
      throw new Error("logger failed");
    });
    // debugLog must swallow logger errors — never propagate
    expect(() => debugLog("this should not throw")).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// readAdapterVersion
// ---------------------------------------------------------------------------

describe("readAdapterVersion", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = nodeFs.mkdtempSync(
      nodePath.join(nodeOs.tmpdir(), "mosaic-adapter-version-test-"),
    );
  });

  afterEach(() => {
    nodeFs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("returns the version string from a hook.yaml containing a version line", async () => {
    const hookYamlPath = nodePath.join(tempDir, "hook.yaml");
    nodeFs.writeFileSync(
      hookYamlPath,
      'version: "1.2.3"\nopencode:\n  supported: true\n',
    );
    const result = await readAdapterVersion(hookYamlPath);
    expect(result).toBe("1.2.3");
  });

  it("returns undefined when the hook.yaml contains no version key", async () => {
    const hookYamlPath = nodePath.join(tempDir, "hook.yaml");
    nodeFs.writeFileSync(hookYamlPath, "opencode:\n  supported: true\n");
    const result = await readAdapterVersion(hookYamlPath);
    expect(result).toBeUndefined();
  });

  it("returns undefined without throwing when the file does not exist", async () => {
    const missingPath = nodePath.join(tempDir, "nonexistent.yaml");
    await expect(readAdapterVersion(missingPath)).resolves.toBeUndefined();
  });

  it("returns undefined for an ambiguous or malformed version value", async () => {
    const hookYamlPath = nodePath.join(tempDir, "hook.yaml");
    // A nested-object value is ambiguous for a regex-based parser with no YAML library
    nodeFs.writeFileSync(hookYamlPath, "version: {nested: value}\n");
    const result = await readAdapterVersion(hookYamlPath);
    expect(result).toBeUndefined();
  });
});
