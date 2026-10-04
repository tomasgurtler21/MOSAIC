/**
 * runner_messages.ts — message text accumulation for Runner mode.
 *
 * Runner mode cannot read the session back from the SDK before the process
 * exits, so the prompt and the final response are accumulated from the
 * message / message-part events observed during the session.
 */

interface MessageState {
  role?: string;
  parts: Map<string, string>;
}

export interface MessageAccumulator {
  /** Record a message.updated payload (role plus any text parts it carries). */
  recordMessageUpdate(sessionId: string, info: Record<string, unknown>, parts: unknown): void;
  /** Record a message.part.updated text part. */
  recordPartUpdate(sessionId: string, part: Record<string, unknown>): void;
  /** Text of the first (or last) message with the given role that has text. */
  findMessageText(sessionId: string, role: string, last: boolean): string | undefined;
}

export function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function textOf(state: MessageState): string | undefined {
  const texts = [...state.parts.values()].filter((t) => t.length > 0);
  return texts.length > 0 ? texts.join("\n") : undefined;
}

export function createMessageAccumulator(): MessageAccumulator {
  /** sessionId -> messageId -> accumulated message text. */
  const messages = new Map<string, Map<string, MessageState>>();

  function messageState(sessionId: string, messageId: string): MessageState {
    let session = messages.get(sessionId);
    if (!session) {
      session = new Map();
      messages.set(sessionId, session);
    }
    let state = session.get(messageId);
    if (!state) {
      state = { parts: new Map() };
      session.set(messageId, state);
    }
    return state;
  }

  function findMessageText(sessionId: string, role: string, last: boolean): string | undefined {
    let found: string | undefined;
    for (const state of messages.get(sessionId)?.values() ?? []) {
      if (state.role !== role) continue;
      const text = textOf(state);
      if (text === undefined) continue;
      found = text;
      if (!last) break;
    }
    return found;
  }

  function recordMessageUpdate(
    sessionId: string,
    info: Record<string, unknown>,
    parts: unknown,
  ): void {
    const messageId = typeof info["id"] === "string" ? info["id"] : "no-id";
    const state = messageState(sessionId, messageId);
    if (typeof info["role"] === "string") state.role = info["role"];
    if (Array.isArray(parts)) {
      const textParts = parts
        .map((p) => asRecord(p))
        .filter((p): p is Record<string, unknown> => p !== undefined && p["type"] === "text");
      if (textParts.length > 0) {
        state.parts.clear();
        textParts.forEach((p, i) => {
          const key = typeof p["id"] === "string" ? p["id"] : `index-${i}`;
          if (typeof p["text"] === "string") state.parts.set(key, p["text"]);
        });
      }
    }
  }

  function recordPartUpdate(sessionId: string, part: Record<string, unknown>): void {
    if (part["type"] !== "text" || typeof part["text"] !== "string") return;
    if (typeof part["messageID"] !== "string") return;
    const key = typeof part["id"] === "string" ? part["id"] : "part";
    messageState(sessionId, part["messageID"]).parts.set(key, part["text"]);
  }

  return { recordMessageUpdate, recordPartUpdate, findMessageText };
}
