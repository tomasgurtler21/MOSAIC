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
import { SessionCorrelationStore } from "./lib/correlation.js";
import { fallbackCallId } from "./lib/runstate.js";
import {
  createSessionHandlers,
  extractSessionId,
  extractParentId,
  type HandlerDependencies,
  type OpenCodeEvent,
  type StopInput,
  type ToolBeforeInput,
  type ToolBeforeOutput,
  type ToolAfterInput,
  type SdkClient,
} from "./lib/handlers_session.js";
import { createInvocationHandlers } from "./lib/handlers_invocation.js";
import { createToolHandlers } from "./lib/handlers_tools.js";

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
    };

    // Create all handler sets
    const sessionHandlers = createSessionHandlers(handlerDeps);
    const invHandlers = createInvocationHandlers(handlerDeps);
    const toolHandlers = createToolHandlers(handlerDeps);

    // --- Return hook registration map ---
    return {
      /**
       * Generic event bus hook. Receives all session.* and other bus events.
       * Routes to session handler, then to invocation handler for subagent creations.
       */
      event: safeHandler(
        "event",
        async (input: { event: OpenCodeEvent }) => {
          const event = input.event;
          if (!event) return;

          // Let session handler process the event first (registers sessions,
          // emits orchestrator lifecycle events)
          await sessionHandlers.handleEvent(event);

          // For subagent session creation, also start the invocation
          if (event.type === "session.created") {
            const sessionId = extractSessionId(event);
            const parentId = extractParentId(event);
            if (sessionId && parentId) {
              await invHandlers.handleInvocationStart(sessionId, parentId);
            }
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
       */
      "tool.execute.after": safeHandler(
        "tool.execute.after",
        async (input: ToolAfterInput) => {
          await toolHandlers.handleToolAfter(input);
        },
      ),

      /**
       * Agent loop stopped hook. Routes to session end or invocation end
       * based on whether the session is a subagent or orchestrator session.
       */
      stop: safeHandler(
        "stop",
        async (input: StopInput) => {
          if (!input?.sessionID) return;

          if (store.isSubagentSession(input.sessionID)) {
            // Subagent finished — emit invocation_end + write artifacts + exports
            await invHandlers.handleInvocationEnd(input.sessionID);
          } else {
            // Orchestrator session ended — emit session_end + run_end
            await sessionHandlers.handleStop(input);
          }
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
