/**
 * plugin.ts — OpenCode mosaic-logger hook adapter entry point.
 *
 * This is the file OpenCode auto-discovers as the plugin entry point.
 * Called once per OpenCode process start; persists in-memory for the
 * lifetime of that process.
 *
 * Responsibilities:
 *   - Resolve workspace root from ctx.directory
 *   - Initialize LogPaths, SessionCorrelationStore
 *   - Capture the SDK client reference
 *   - Read adapter version from hook.yaml
 *   - Wire all handlers into the hook registration map
 *   - Wrap every hook handler in safeHandler
 */

import * as nodePath from "node:path";
import {
  LogPaths,
  buildEvent,
  appendEvent,
  setDebugLogger,
  debugLog,
  safeHandler,
  readAdapterVersion,
} from "./lib/core.js";
import { SessionCorrelationStore, FileClosedSessionRegistry } from "./lib/correlation.js";
import { fallbackCallId } from "./lib/runstate.js";
import {
  createSessionHandlers,
  type HandlerDependencies,
  type OpenCodeEvent,
  type PermissionAskInput,
  type SessionCompactingInput,
  type ToolBeforeInput,
  type ToolBeforeOutput,
  type ToolAfterInput,
  type ToolAfterOutput,
  type SdkClient,
} from "./lib/handlers_session.js";
import { createInvocationHandlers } from "./lib/handlers_invocation.js";
import { createToolHandlers } from "./lib/handlers_tools.js";
import { createNotificationHandlers } from "./lib/handlers_notifications.js";
import { createMessageHandlers } from "./lib/handlers_messages.js";

// ---------------------------------------------------------------------------
// Plugin factory
// ---------------------------------------------------------------------------

/**
 * The OpenCode plugin factory. Called once per OpenCode process start.
 * Returns a hook registration map with all handlers wrapped in safeHandler.
 * Never throws — initialization failures are logged and result in a no-op plugin.
 */
export const MosaicLogger = async (ctx: {
  directory: string;
  worktree?: string;
  client: SdkClient;
  project?: unknown;
  $?: unknown;
}) => {
  try {
    // --- Initialize adapter components ---
    const workspaceRoot = ctx.directory;
    const paths = new LogPaths(workspaceRoot);
    const store = new SessionCorrelationStore();
    const closedSessions = new FileClosedSessionRegistry(paths);
    const sdkClient = ctx.client;

    // Set up debug logging via the SDK client
    setDebugLogger((message, error) => {
      const extra: Record<string, unknown> = {};
      if (error !== undefined) {
        extra.error = error instanceof Error ? error.message : String(error);
      }
      sdkClient.app
        .log({
          body: {
            service: "mosaic-logger",
            level: "debug",
            message,
            extra: Object.keys(extra).length > 0 ? extra : undefined,
          },
        })
        .catch(() => {
          // Swallow SDK logging errors — debugLog must never propagate
        });
    });

    // Read adapter version from hook.yaml (best-effort)
    const hookYamlPath = nodePath.join(workspaceRoot, ".opencode", "plugins", "hook.yaml");
    const adapterVersion = await readAdapterVersion(hookYamlPath);

    // Build shared dependency bundle
    const handlerDeps: HandlerDependencies = {
      store,
      paths,
      sdkClient,
      adapterVersion,
      buildEvent,
      appendEvent,
      fallbackCallId,
      closedSessions,
    };

    // Create all handler sets
    const sessionHandlers = createSessionHandlers(handlerDeps);
    const invHandlers = createInvocationHandlers(handlerDeps);
    const toolHandlers = createToolHandlers(handlerDeps);
    const notificationHandlers = createNotificationHandlers(handlerDeps);
    const msgHandlers = createMessageHandlers(handlerDeps);

    // Late-bind invocation callbacks into the shared deps object.
    // The session handler reads invocationCallbacks via the deps reference (not a
    // destructured copy), so this assignment is visible inside session handler closures
    // for the session.created (subagent) and session.idle (subagent) paths.
    handlerDeps.invocationCallbacks = {
      handleInvocationStart: invHandlers.handleInvocationStart,
      handleInvocationEnd: invHandlers.handleInvocationEnd,
    };

    // Late-bind closeDispatchCall into the shared deps object.
    // handleInvocationEnd reads it via the deps reference so this assignment
    // is visible inside the invocation handler closure even though toolHandlers
    // is constructed after createInvocationHandlers returns.
    handlerDeps.closeDispatchCall = toolHandlers.closeDispatchCall;

    // --- Return hook registration map ---
    return {
      /**
       * Generic event bus hook. Receives all session.* and other bus events.
       *
       * Routing:
       *   - message.updated → msgHandlers.handleMessageEvent (turn + usage_record emission)
       *   - all other event types → sessionHandlers.handleEvent (session lifecycle)
       *
       * The session handler internally routes subagent session.created events to
       * invocationCallbacks.handleInvocationStart and subagent session.idle events
       * to invocationCallbacks.handleInvocationEnd via the late-bound callbacks.
       */
      event: safeHandler(
        "event",
        async (input: { event: OpenCodeEvent }) => {
          const event = input.event;
          if (!event) return;
          if (event.type === "message.updated") {
            await msgHandlers.handleMessageEvent(event);
          } else {
            await sessionHandlers.handleEvent(event);
          }
        },
      ),

      /**
       * Tool execution start hook. Maps to tool_call_start.
       */
      "tool.execute.before": safeHandler(
        "tool.execute.before",
        async (input: ToolBeforeInput, output: ToolBeforeOutput) => {
          await toolHandlers.handleToolBefore(input, output);
        },
      ),

      /**
       * Tool execution end hook. Maps to tool_call_end.
       * The after-hook's second argument carries the tool output payload; forwarding
       * it to handleToolAfter allows deriveToolOutcome to determine the real outcome
       * rather than defaulting to the hardcoded success fallback.
       */
      "tool.execute.after": safeHandler(
        "tool.execute.after",
        async (input: ToolAfterInput, output: ToolAfterOutput) => {
          await toolHandlers.handleToolAfter(input, output);
        },
      ),

      /**
       * Permission prompt hook. Emits a notification event to the appropriate stream.
       * Observe-only — never alters the permission decision.
       */
      "permission.ask": safeHandler(
        "permission.ask",
        async (input: PermissionAskInput, output?: unknown) => {
          await notificationHandlers.handlePermissionAsk(input, output);
        },
      ),

      /**
       * Session compaction hook (experimental). Emits a compaction event.
       * Observe-only — never alters compaction behaviour.
       */
      "experimental.session.compacting": safeHandler(
        "experimental.session.compacting",
        async (input: SessionCompactingInput, output?: unknown) => {
          await notificationHandlers.handleSessionCompacting(input, output);
        },
      ),
    };
  } catch (err) {
    // Initialization failure — log if possible, then return empty (no-op) plugin
    try {
      await ctx.client.app.log({
        body: {
          service: "mosaic-logger",
          level: "error",
          message: "mosaic-logger plugin failed to initialize",
          extra: {
            error: err instanceof Error ? err.message : String(err),
          },
        },
      });
    } catch {
      // Cannot log — silently return no-op
    }
    // Return empty hook map — OpenCode receives no handlers, adapter is a no-op
    return {};
  }
};
