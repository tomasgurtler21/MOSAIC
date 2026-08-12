/**
 * message_extraction.test.ts — Tests for message-text extraction helpers.
 *
 * These helpers are the critical correctness fix for Stage 1. OpenCode returns
 * messages as `{ info: MessageInfo, parts: MessagePart[] }[]`, where:
 *   - Role lives at `entry.info.role`, NOT at `entry.role`
 *   - Text lives ONLY in parts where `part.type === "text"`, in `part.text`
 *   - There is no `content` field at any nesting level
 *
 * Tests marked RED will fail until the implementation (I1.2) corrects the
 * extraction logic to read from the real shape instead of the imagined one.
 *
 * Tests that verify returning `undefined` for bad input will pass from the start —
 * that is correct TDD behaviour (guards are already safe even before the fix).
 */

import { describe, it, expect } from "vitest";
import {
  extractTextFromParts,
  extractFirstUserText,
  extractLastAssistantText,
} from "../lib/handlers_invocation";
import {
  userMessageEntry,
  assistantMessageEntry,
  nonTextMessageEntry,
} from "./fixtures/opencode_api";

// ---------------------------------------------------------------------------
// extractTextFromParts
// ---------------------------------------------------------------------------

describe("extractTextFromParts — concatenates text-typed parts", () => {
  it("returns the text of a single text part", () => {
    const parts = [
      { id: "p1", messageID: "m1", sessionID: "s1", type: "text", text: "Hello world" },
    ];
    // RED: stub returns undefined; implementation must return "Hello world"
    expect(extractTextFromParts(parts)).toBe("Hello world");
  });

  it("joins multiple text parts with a newline", () => {
    const parts = [
      { id: "p1", type: "text", text: "First line" },
      { id: "p2", type: "text", text: "Second line" },
    ];
    // RED: stub returns undefined; implementation must return "First line\nSecond line"
    expect(extractTextFromParts(parts)).toBe("First line\nSecond line");
  });

  it("ignores non-text parts and concatenates only text parts", () => {
    const parts = [
      { id: "p1", type: "reasoning" },
      { id: "p2", type: "text", text: "Keep this" },
      { id: "p3", type: "step-start" },
      { id: "p4", type: "text", text: "And this" },
      { id: "p5", type: "tool" },
    ];
    // RED: stub returns undefined; implementation must return "Keep this\nAnd this"
    expect(extractTextFromParts(parts)).toBe("Keep this\nAnd this");
  });

  it("returns undefined when no parts are text-typed", () => {
    const parts = [
      { id: "p1", type: "reasoning" },
      { id: "p2", type: "step-start" },
      { id: "p3", type: "tool" },
    ];
    // GREEN: no text parts → undefined is the correct answer
    expect(extractTextFromParts(parts)).toBeUndefined();
  });

  it("returns undefined for an empty parts array", () => {
    // GREEN: empty → undefined
    expect(extractTextFromParts([])).toBeUndefined();
  });

  it("returns undefined for null input", () => {
    expect(extractTextFromParts(null)).toBeUndefined();
  });

  it("returns undefined for undefined input", () => {
    expect(extractTextFromParts(undefined)).toBeUndefined();
  });

  it("returns undefined for a non-array input (string)", () => {
    expect(extractTextFromParts("not an array")).toBeUndefined();
  });

  it("returns undefined for a non-array input (object)", () => {
    expect(extractTextFromParts({ type: "text", text: "foo" })).toBeUndefined();
  });

  it("returns undefined when a text part carries a non-string text field", () => {
    const parts = [{ id: "p1", type: "text", text: 42 }];
    // A non-string `text` field must be skipped, not coerced
    expect(extractTextFromParts(parts)).toBeUndefined();
  });

  it("does not throw for deeply malformed part entries", () => {
    const parts = [null, undefined, 42, "string", { type: "text" }];
    expect(() => extractTextFromParts(parts)).not.toThrow();
  });

  it("handles a text part with an empty string — returns undefined (no content)", () => {
    const parts = [{ id: "p1", type: "text", text: "" }];
    // An empty text string contributes nothing; the join result is empty, so undefined
    // Implementation must decide: empty string → undefined (per "undefined if none")
    // Accept either undefined or "" — both are defensible; test for no throw
    expect(() => extractTextFromParts(parts)).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// extractFirstUserText — reads real { info, parts } shape
// ---------------------------------------------------------------------------

describe("extractFirstUserText — real { info, parts } message shape", () => {
  it("returns the text of the first user message", () => {
    const messages = [userMessageEntry("Dispatch prompt text")];
    // RED: implementation reads m.role (undefined on real shape) → returns undefined
    // Fixed implementation must read m.info.role → returns "Dispatch prompt text"
    expect(extractFirstUserText(messages)).toBe("Dispatch prompt text");
  });

  it("returns text from user message even when an assistant message comes first", () => {
    const messages = [
      assistantMessageEntry("Some assistant text"),
      userMessageEntry("User prompt here"),
    ];
    // RED: same reason
    expect(extractFirstUserText(messages)).toBe("User prompt here");
  });

  it("returns only the FIRST user message text when multiple user messages exist", () => {
    const messages = [
      userMessageEntry("First user message"),
      assistantMessageEntry("Response"),
      userMessageEntry("Second user message"),
    ];
    // RED: must return "First user message"
    expect(extractFirstUserText(messages)).toBe("First user message");
  });

  it("returns concatenated text when the user message has multiple text parts", () => {
    const messages = [userMessageEntry(["Part one", "Part two"])];
    // RED: must return "Part one\nPart two"
    expect(extractFirstUserText(messages)).toBe("Part one\nPart two");
  });

  it("returns undefined when the only messages are assistant messages", () => {
    const messages = [assistantMessageEntry("No user here")];
    // GREEN: no user messages → undefined (returns undefined even today via wrong path)
    expect(extractFirstUserText(messages)).toBeUndefined();
  });

  it("returns undefined for a user message that has only non-text parts", () => {
    const entry = nonTextMessageEntry();
    // Override role to user so the extraction path reaches it
    const userNonText = {
      ...entry,
      info: { ...entry.info, role: "user" },
    };
    const messages = [userNonText];
    // RED: after role fix, must return undefined (no text parts)
    expect(extractFirstUserText(messages)).toBeUndefined();
  });

  it("returns undefined for an empty messages array", () => {
    expect(extractFirstUserText([])).toBeUndefined();
  });

  it("returns undefined for null input", () => {
    expect(extractFirstUserText(null)).toBeUndefined();
  });

  it("returns undefined for undefined input", () => {
    expect(extractFirstUserText(undefined)).toBeUndefined();
  });

  it("returns undefined for a non-array input", () => {
    expect(extractFirstUserText("not an array")).toBeUndefined();
  });

  it("returns undefined when messages array contains non-object entries", () => {
    const messages = [null, undefined, 42, "string"];
    expect(extractFirstUserText(messages)).toBeUndefined();
  });

  it("does not throw for deeply malformed input", () => {
    expect(() => extractFirstUserText([{ info: null, parts: null }])).not.toThrow();
  });

  it("does not throw when info is absent from a message entry", () => {
    const messages = [{ parts: [{ type: "text", text: "orphan" }] }];
    expect(() => extractFirstUserText(messages)).not.toThrow();
  });

  it("does not throw when parts is absent from a message entry", () => {
    const messages = [{ info: { role: "user" } }];
    expect(() => extractFirstUserText(messages)).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// extractLastAssistantText — reads real { info, parts } shape
// ---------------------------------------------------------------------------

describe("extractLastAssistantText — real { info, parts } message shape", () => {
  it("returns the text of a single assistant message", () => {
    const messages = [assistantMessageEntry("Final response here.")];
    // RED: implementation reads m.role (undefined on real shape) → returns undefined
    expect(extractLastAssistantText(messages)).toBe("Final response here.");
  });

  it("returns the text of the LAST assistant message when multiple exist", () => {
    const messages = [
      userMessageEntry("User prompt"),
      assistantMessageEntry("First assistant response"),
      userMessageEntry("Follow-up"),
      assistantMessageEntry("Final assistant response"),
    ];
    // RED: must return "Final assistant response"
    expect(extractLastAssistantText(messages)).toBe("Final assistant response");
  });

  it("returns concatenated text when the last assistant message has multiple text parts", () => {
    const messages = [assistantMessageEntry(["Part A", "Part B"])];
    // RED: must return "Part A\nPart B"
    expect(extractLastAssistantText(messages)).toBe("Part A\nPart B");
  });

  it("returns undefined when there are only user messages", () => {
    const messages = [userMessageEntry("Only user")];
    // GREEN: no assistant messages → undefined
    expect(extractLastAssistantText(messages)).toBeUndefined();
  });

  it("returns undefined when the only assistant message has no text parts", () => {
    const messages = [nonTextMessageEntry()];
    // RED: after role fix, must return undefined (no text parts)
    expect(extractLastAssistantText(messages)).toBeUndefined();
  });

  it("returns text from an earlier assistant message when the last has no text parts", () => {
    const messages = [
      assistantMessageEntry("Earlier text"),
      nonTextMessageEntry(), // last, but no text parts
    ];
    // RED: must return "Earlier text" (skip the last non-text entry)
    expect(extractLastAssistantText(messages)).toBe("Earlier text");
  });

  it("returns undefined for an empty messages array", () => {
    expect(extractLastAssistantText([])).toBeUndefined();
  });

  it("returns undefined for null input", () => {
    expect(extractLastAssistantText(null)).toBeUndefined();
  });

  it("returns undefined for undefined input", () => {
    expect(extractLastAssistantText(undefined)).toBeUndefined();
  });

  it("returns undefined for a non-array input", () => {
    expect(extractLastAssistantText("not an array")).toBeUndefined();
  });

  it("returns undefined when messages array contains non-object entries", () => {
    const messages = [null, undefined, 42, "string"];
    expect(extractLastAssistantText(messages)).toBeUndefined();
  });

  it("does not throw for deeply malformed input", () => {
    expect(() => extractLastAssistantText([{ info: null, parts: null }])).not.toThrow();
  });

  it("does not throw when info is absent from a message entry", () => {
    const messages = [{ parts: [{ type: "text", text: "orphan" }] }];
    expect(() => extractLastAssistantText(messages)).not.toThrow();
  });

  it("does not throw when parts is absent from a message entry", () => {
    const messages = [{ info: { role: "assistant" } }];
    expect(() => extractLastAssistantText(messages)).not.toThrow();
  });

  it("extracts status code carrier text correctly (dispatch prompt format)", () => {
    const statusText = `Here are the results.\n\n{"status_code": "SUCCESS", "status_message": "Done."}`;
    const messages = [
      userMessageEntry("Task description"),
      assistantMessageEntry(statusText),
    ];
    // RED: must return the full status text so extractStatusCode can parse it
    expect(extractLastAssistantText(messages)).toBe(statusText);
  });
});
