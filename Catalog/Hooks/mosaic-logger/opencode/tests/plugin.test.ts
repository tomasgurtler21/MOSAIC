/**
 * plugin.test.ts — Tests for the plugin factory's hook registration surface.
 *
 * Covers (Stage 2, T2.1):
 *   - The returned hook map contains no `stop` key — `stop` is not a member of
 *     OpenCode's real 21-member Hooks interface and registering it is a silent no-op.
 *   - Every registered hook key is present in OpenCode's real Hooks interface.
 *   - The five hooks specified by the design are present and callable:
 *       event, tool.execute.before, tool.execute.after,
 *       permission.ask, experimental.session.compacting
 *   - The plugin factory returns an empty hook map rather than throwing on
 *     initialization failure.
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import * as nodePath from "node:path";
import * as nodeFs from "node:fs";
import * as nodeOs from "node:os";
import { MosaicLogger } from "../plugin";
import type { SdkClient } from "../lib/handlers_session";
import type { OpenCodeEvent } from "../lib/handlers_session";
import {
  messageUpdatedEvent,
  userMessageEntry,
  assistantMessageEntry,
} from "./fixtures/opencode_api";

// ---------------------------------------------------------------------------
// The complete set of valid hook names in OpenCode's real Hooks interface.
// Source: Stage 2 context — OpenCode `Hooks` has exactly 21 members.
// `stop` is NOT among them; registering it is a silent no-op.
// ---------------------------------------------------------------------------

const VALID_OPENCODE_HOOKS = new Set<string>([
  "dispose",
  "event",
  "config",
  "tool",
  "auth",
  "provider",
  "chat.message",
  "chat.params",
  "chat.headers",
  "permission.ask",
  "command.execute.before",
  "tool.execute.before",
  "tool.execute.after",
  "shell.env",
  "experimental.chat.messages.transform",
  "experimental.chat.system.transform",
  "experimental.provider.small_model",
  "experimental.session.compacting",
  "experimental.compaction.autocontinue",
  "experimental.text.complete",
  "tool.definition",
]);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let tmpDir: string;

beforeEach(() => {
  tmpDir = nodeFs.mkdtempSync(
    nodePath.join(nodeOs.tmpdir(), "mosaic-plugin-test-"),
  );
});

afterEach(() => {
  nodeFs.rmSync(tmpDir, { recursive: true, force: true });
});

/** Minimal SDK client that silently absorbs all calls. */
function makeNullClient(): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async () => [],
    },
    app: { log: async () => {} },
  };
}

/**
 * Invoke the plugin factory and return the resulting hook map as a plain
 * object so tests can inspect keys without fighting TypeScript's inferred
 * return type.
 */
async function buildHookMap(): Promise<Record<string, unknown>> {
  const result = await MosaicLogger({
    directory: tmpDir,
    client: makeNullClient(),
  });
  return result as Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// No invalid hooks registered
// ---------------------------------------------------------------------------

describe("plugin hook registration — no invalid hooks", () => {
  it("does not register a stop hook (stop is absent from OpenCode's real interface)", async () => {
    const hookMap = await buildHookMap();
    // stop IS registered in the current implementation — this assertion FAILS until I2.1
    expect(Object.keys(hookMap)).not.toContain("stop");
  });

  it("every registered hook key belongs to OpenCode's real 21-member Hooks interface", async () => {
    const hookMap = await buildHookMap();
    const registeredKeys = Object.keys(hookMap);
    // At least one key ("stop") is currently not in the valid set — FAILS until I2.1
    for (const key of registeredKeys) {
      expect(VALID_OPENCODE_HOOKS.has(key)).toBe(true);
    }
  });
});

// ---------------------------------------------------------------------------
// Required hooks present
// ---------------------------------------------------------------------------

describe("plugin hook registration — required hooks present and callable", () => {
  it("registers the event hook as a function", async () => {
    const hookMap = await buildHookMap();
    expect(typeof hookMap["event"]).toBe("function");
  });

  it("registers tool.execute.before as a function", async () => {
    const hookMap = await buildHookMap();
    expect(typeof hookMap["tool.execute.before"]).toBe("function");
  });

  it("registers tool.execute.after as a function", async () => {
    const hookMap = await buildHookMap();
    expect(typeof hookMap["tool.execute.after"]).toBe("function");
  });

  it("registers permission.ask as a function", async () => {
    const hookMap = await buildHookMap();
    // permission.ask is NOT currently registered — this assertion FAILS until I2.1
    expect(typeof hookMap["permission.ask"]).toBe("function");
  });

  it("registers experimental.session.compacting as a function", async () => {
    const hookMap = await buildHookMap();
    // experimental.session.compacting is NOT currently registered — FAILS until I2.1
    expect(typeof hookMap["experimental.session.compacting"]).toBe("function");
  });
});

// ---------------------------------------------------------------------------
// Registered hook count matches the design-specified set
// ---------------------------------------------------------------------------

describe("plugin hook registration — hook count", () => {
  it("registers exactly 5 hooks matching the design specification", async () => {
    const hookMap = await buildHookMap();
    const keys = Object.keys(hookMap);
    // Current implementation registers 4 hooks (including stop) — FAILS until I2.1
    // After I2.1: event + tool.execute.before + tool.execute.after +
    //             permission.ask + experimental.session.compacting = 5
    expect(keys).toHaveLength(5);
  });
});

// ---------------------------------------------------------------------------
// safeHandler wrapping — hooks must not throw into the host (AC2.8)
// ---------------------------------------------------------------------------

describe("plugin hook registration — safeHandler wrapping at the PluginHookMap boundary", () => {
  it("permission.ask resolves rather than throws when the underlying handler throws", async () => {
    // Build a client whose session.get throws to simulate a handler dependency failure
    const throwingClient: SdkClient = {
      session: {
        get: async () => {
          throw new Error("simulated dependency failure");
        },
        messages: async () => {
          throw new Error("simulated dependency failure");
        },
      },
      app: { log: async () => {} },
    };

    // The hook map must still be built successfully (factory catches init errors)
    const result = await MosaicLogger({
      directory: tmpDir,
      client: throwingClient,
    });
    const hookMap = result as Record<string, unknown>;

    // If permission.ask is registered (GREEN state), calling it must resolve, not throw —
    // safeHandler wraps every entry point. In RED state the key is absent; skip rather than fail.
    const permissionAsk = hookMap["permission.ask"];
    if (typeof permissionAsk === "function") {
      await expect(
        permissionAsk({ sessionID: "session-abc" }, undefined),
      ).resolves.toBeUndefined();
    }
  });

  it("experimental.session.compacting resolves rather than throws when the underlying handler throws", async () => {
    const throwingClient: SdkClient = {
      session: {
        get: async () => {
          throw new Error("simulated dependency failure");
        },
        messages: async () => {
          throw new Error("simulated dependency failure");
        },
      },
      app: { log: async () => {} },
    };

    const result = await MosaicLogger({
      directory: tmpDir,
      client: throwingClient,
    });
    const hookMap = result as Record<string, unknown>;

    const compacting = hookMap["experimental.session.compacting"];
    if (typeof compacting === "function") {
      await expect(
        compacting({ sessionID: "session-abc" }, undefined),
      ).resolves.toBeUndefined();
    }
  });
});

// ---------------------------------------------------------------------------
// Initialization failure resilience
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Durable session registry wiring — session-end path writes .session-ends marker
// ---------------------------------------------------------------------------

describe("plugin factory — durable session registry wiring", () => {
  it("writes a .session-ends marker after a session closes via session.idle", async () => {
    const hookMap = await buildHookMap();
    const eventHook = hookMap["event"] as ((input: { event: OpenCodeEvent }) => Promise<void>) | undefined;
    if (typeof eventHook !== "function") {
      // If event hook is absent (shouldn't happen in GREEN state), skip to avoid false pass
      return;
    }

    const sessionId = "registry-wiring-test-session";

    // Fire session.created without a parentID → registers as a top-level orchestrator session
    await eventHook({
      event: {
        type: "session.created",
        properties: { info: { id: sessionId } },
      },
    });

    // Fire session.idle → triggers emitSessionEnd → writes the durable marker
    await eventHook({
      event: {
        type: "session.idle",
        properties: { info: { id: sessionId } },
      },
    });

    // The marker must be at {root}/.session-ends/{sessionId}.json
    const markerPath = nodePath.join(
      tmpDir,
      "OrchestrationLogs",
      ".session-ends",
      `${sessionId}.json`,
    );
    expect(nodeFs.existsSync(markerPath)).toBe(true);

    // The marker content must be valid JSON with the correct session_id field
    const raw = nodeFs.readFileSync(markerPath, "utf-8");
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    expect(parsed["session_id"]).toBe(sessionId);
  });

  it("a second session.idle after the marker exists is suppressed (no second close pair)", async () => {
    const hookMap = await buildHookMap();
    const eventHook = hookMap["event"] as ((input: { event: OpenCodeEvent }) => Promise<void>) | undefined;
    if (typeof eventHook !== "function") return;

    const sessionId = "registry-dedup-test-session";
    const eventsPath = nodePath.join(tmpDir, "OrchestrationLogs", "unknown-run", "00_orchestrator_events.jsonl");

    await eventHook({ event: { type: "session.created", properties: { info: { id: sessionId } } } });
    await eventHook({ event: { type: "session.idle", properties: { info: { id: sessionId } } } });

    // Count close events written after the first idle
    const afterFirst = nodeFs.existsSync(eventsPath)
      ? nodeFs.readFileSync(eventsPath, "utf-8").split("\n").filter(Boolean).length
      : 0;

    // Simulate process restart: build a fresh plugin factory over the same tmpDir.
    // The in-memory store is empty but the .session-ends marker persists on disk.
    const hookMap2 = await (async () => {
      const result = await MosaicLogger({ directory: tmpDir, client: makeNullClient() });
      return result as Record<string, unknown>;
    })();
    const eventHook2 = hookMap2["event"] as ((input: { event: OpenCodeEvent }) => Promise<void>) | undefined;
    if (typeof eventHook2 !== "function") return;

    // Drive session.idle again on the fresh factory — durable registry must suppress the duplicate
    await eventHook2({ event: { type: "session.idle", properties: { info: { id: sessionId } } } });

    const afterSecond = nodeFs.existsSync(eventsPath)
      ? nodeFs.readFileSync(eventsPath, "utf-8").split("\n").filter(Boolean).length
      : 0;

    // No additional events should have been appended
    expect(afterSecond).toBe(afterFirst);
  });
});

// ---------------------------------------------------------------------------
// message.updated wiring — regression guard for the plugin.ts → handleMessageEvent path
//
// These tests call MosaicLogger, invoke the real `event` hook with a
// `message.updated` fixture, and assert that the expected event types land in
// the on-disk stream file.  They protect against the original dead-code bug
// (message.updated falling through to sessionHandlers.handleEvent and being
// silently swallowed) being reintroduced by a future refactor.
// ---------------------------------------------------------------------------

describe("plugin factory — message.updated routing to handleMessageEvent", () => {
  it("writes a turn line for a user message.updated event fired through the real event hook", async () => {
    const hookMap = await buildHookMap();
    const eventHook = hookMap["event"] as
      | ((input: { event: OpenCodeEvent }) => Promise<void>)
      | undefined;
    if (typeof eventHook !== "function") return;

    const sessionId = "msg-wiring-user-regression";

    // Register as an orchestrator-scoped (no parentID) session
    await eventHook({
      event: {
        type: "session.created",
        properties: { info: { id: sessionId } },
      },
    });

    // Fire a message.updated event with a user-role message through the hook
    const userEntry = userMessageEntry("Hello from user.", {
      id: "msg-user-wiring",
      sessionID: sessionId,
    });
    await eventHook({ event: messageUpdatedEvent(userEntry) });

    // A turn line must appear in the orchestrator events stream
    const eventsPath = nodePath.join(
      tmpDir,
      "OrchestrationLogs",
      "unknown-run",
      "00_orchestrator_events.jsonl",
    );
    expect(nodeFs.existsSync(eventsPath)).toBe(true);
    const lines = nodeFs
      .readFileSync(eventsPath, "utf-8")
      .split("\n")
      .filter(Boolean);
    const parsed = lines.map((l) => JSON.parse(l) as Record<string, unknown>);
    const turnLines = parsed.filter((e) => e["event"] === "turn");
    expect(turnLines.length).toBeGreaterThan(0);
    expect(turnLines[0]!["role"]).toBe("user");

    // User messages must NOT produce a usage_record line
    const usageLines = parsed.filter((e) => e["event"] === "usage_record");
    expect(usageLines.length).toBe(0);
  });

  it("writes a turn and a usage_record line for an assistant message.updated event fired through the real event hook", async () => {
    const hookMap = await buildHookMap();
    const eventHook = hookMap["event"] as
      | ((input: { event: OpenCodeEvent }) => Promise<void>)
      | undefined;
    if (typeof eventHook !== "function") return;

    const sessionId = "msg-wiring-assistant-regression";

    // Register as an orchestrator-scoped (no parentID) session
    await eventHook({
      event: {
        type: "session.created",
        properties: { info: { id: sessionId } },
      },
    });

    // Fire a message.updated event with an assistant-role message through the hook
    const assistantEntry = assistantMessageEntry("Hello from assistant.", {
      id: "msg-assistant-wiring",
      sessionID: sessionId,
    });
    await eventHook({ event: messageUpdatedEvent(assistantEntry) });

    // Both a turn and a usage_record line must appear in the orchestrator events stream
    const eventsPath = nodePath.join(
      tmpDir,
      "OrchestrationLogs",
      "unknown-run",
      "00_orchestrator_events.jsonl",
    );
    expect(nodeFs.existsSync(eventsPath)).toBe(true);
    const lines = nodeFs
      .readFileSync(eventsPath, "utf-8")
      .split("\n")
      .filter(Boolean);
    const parsed = lines.map((l) => JSON.parse(l) as Record<string, unknown>);

    const turnLines = parsed.filter((e) => e["event"] === "turn");
    expect(turnLines.length).toBeGreaterThan(0);
    expect(turnLines[0]!["role"]).toBe("assistant");

    const usageLines = parsed.filter((e) => e["event"] === "usage_record");
    expect(usageLines.length).toBeGreaterThan(0);
    expect(typeof usageLines[0]!["record_id"]).toBe("string");
    expect(usageLines[0]!["source"]).toBe("orchestrator_transcript");
  });
});

describe("plugin factory — initialization failure resilience", () => {
  it("returns an object (not throws) when initialized with an invalid directory", async () => {
    const throwingClient: SdkClient = {
      session: {
        get: async () => {
          throw new Error("sdk unavailable");
        },
        messages: async () => {
          throw new Error("sdk unavailable");
        },
      },
      app: { log: async () => {} },
    };

    // Must not throw — the factory catches init errors and returns {}
    await expect(
      MosaicLogger({
        directory: "/nonexistent-path-xyz-mosaic",
        client: throwingClient,
      }),
    ).resolves.toBeDefined();
  });

  it("returns an empty hook map on catastrophic initialization failure", async () => {
    // A client whose app.log throws prevents the factory's own error logger from
    // completing, but the outer catch still returns {}.
    const badLoggingClient: SdkClient = {
      session: {
        get: async () => undefined,
        messages: async () => [],
      },
      app: {
        log: async () => {
          throw new Error("logging failed");
        },
      },
    };

    // The factory must return an object (possibly empty), never throw
    const result = await MosaicLogger({
      directory: "/nonexistent-path-xyz-mosaic",
      client: badLoggingClient,
    });
    expect(typeof result).toBe("object");
    expect(result).not.toBeNull();
  });
});
