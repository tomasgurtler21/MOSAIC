/**
 * export_support.ts -- Shared helpers for the export test files.
 *
 * Provides the stub SDK client and the sample message list used by
 * export.test.ts and export_sync_write.test.ts.
 */

import { userMessageEntry, assistantMessageEntry } from "./fixtures/opencode_api";

/** Structural SdkClient type accepted by exportSession (kept in lockstep with lib/export.ts). */
export type ExportSdkClient = Parameters<typeof import("../lib/export").exportSession>[0];

export interface ExportSdkClientOptions {
  messagesResult?: unknown;
  messagesThrows?: Error;
  getResult?: { parentID?: string } | undefined;
  /** When set, session.messages returns this exact promise object (not re-wrapped). */
  messagesPromise?: Promise<unknown>;
}

/**
 * Minimal SdkClient stub that returns controlled responses.
 * When messagesPromise is set, messages() returns that same promise object so a
 * test can attach a reaction ordered after the export's own continuation.
 */
export function makeSdkClient(options: ExportSdkClientOptions = {}): ExportSdkClient {
  return {
    session: {
      get: async (_params) => options.getResult,
      messages: (_params) => {
        if (options.messagesPromise) {
          return options.messagesPromise;
        }
        return (async () => {
          if (options.messagesThrows) {
            throw options.messagesThrows;
          }
          return options.messagesResult;
        })();
      },
    },
    app: {
      log: async (_params) => {},
    },
  };
}

/**
 * Sample message list as the SDK would return from client.session.messages().
 * Built via fixture builders so the real { info, parts } shape is used.
 */
export const SAMPLE_MESSAGES: unknown[] = [
  userMessageEntry("Do the task.", { id: "msg-001" }),
  assistantMessageEntry('{"status_code": "SUCCESS"}', { id: "msg-002" }),
];
