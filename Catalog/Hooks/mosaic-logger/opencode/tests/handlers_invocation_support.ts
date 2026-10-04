/**
 * handlers_invocation_support.ts - Shared helpers for the invocation handler tests.
 *
 * Test-only module (not part of the plugin runtime surface). Provides SDK client
 * stubs, a HandlerDependencies factory that collects appended events, and the
 * collected-event type.
 */

import type { HandlerDependencies, SdkClient } from "../lib/handlers_session";
import type { SessionCorrelationStore } from "../lib/correlation";
import type { LogPaths } from "../lib/core";

/** One appendEvent call captured by makeDeps. */
export type CollectedEvent = { filePath: string; event: Record<string, unknown> };

/** SDK client whose session.messages() resolves to [] and session.get() to undefined. */
export function makeNullSdk(): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async () => [],
    },
    app: { log: async () => {} },
  };
}

/**
 * SDK client that returns a controlled messages response for specific sessions.
 * `getMessagesForSession` maps session IDs to their message arrays.
 */
export function makeSdk(getMessagesForSession: (sessionId: string) => unknown): SdkClient {
  return {
    session: {
      get: async () => undefined,
      messages: async ({ path }) => getMessagesForSession(path.id),
    },
    app: { log: async () => {} },
  };
}

/**
 * HandlerDependencies wired to the given store, paths and SDK. appendEvent pushes
 * { filePath, event } onto `collected`.
 */
export function makeDeps(
  store: SessionCorrelationStore,
  paths: LogPaths,
  sdkClient: SdkClient,
  collected: CollectedEvent[],
): HandlerDependencies {
  return {
    store,
    paths,
    sdkClient,
    adapterVersion: "0.1.0",
    buildEvent: (event, envelope, fields) => ({
      schema_version: "1.0.0",
      event,
      timestamp: envelope.timestamp,
      harness: "opencode",
      ...(envelope.sessionId ? { session_id: envelope.sessionId } : {}),
      ...(envelope.runId ? { run_id: envelope.runId } : {}),
      ...Object.fromEntries(
        Object.entries(fields).filter(([, v]) => v !== undefined && v !== null && v !== ""),
      ),
    }),
    appendEvent: async (filePath, event) => {
      collected.push({ filePath, event });
    },
    fallbackCallId: (toolName) => `${toolName ?? "tool"}_fallback`,
  };
}
