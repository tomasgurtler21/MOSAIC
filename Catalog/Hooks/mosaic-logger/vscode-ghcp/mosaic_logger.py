"""mosaic_logger.py — Dispatcher entrypoint for the mosaic-logger VS Code GHCP hook adapter.

This module is the process entrypoint spawned by the VS Code GHCP harness on every hook
firing. It reads one JSON object from stdin, dispatches to the appropriate handler by
reading hook_event_name from the payload, and exits 0 unconditionally. Nothing is written
to stdout.

Key vscode-ghcp divergences:
- Event name is read from payload['hook_event_name'], NOT from sys.argv[1]
  (that is the ghcp-cli pattern).
- HANDLERS contains exactly 8 events; SessionEnd, Notification, PostToolUseFailure,
  and PostCompact are absent (those events are unconfirmed for VS Code).
- run_start is emitted on SessionStart; run_end is emitted on Stop.
- RUN_ID_PROMPT_FIELDS maps UserPromptSubmit to 'prompt' (not 'user_prompt'),
  and SubagentStart is absent (its run_id comes from the popped pending dispatch).
"""

import json
import sys

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_session as _session
import mosaic_logger_handlers_invocation as _invocation
import mosaic_logger_handlers_tools as _tools


# ---------------------------------------------------------------------------
# Handler registry — exactly 8 VS Code event names
# ---------------------------------------------------------------------------

HANDLERS: dict = {}
for _mod in [_session, _invocation, _tools]:
    if hasattr(_mod, "HANDLERS"):
        HANDLERS.update(_mod.HANDLERS)


# ---------------------------------------------------------------------------
# Dispatcher functions
# ---------------------------------------------------------------------------

# Events with prompt-bearing fields from which run_id may be extracted.
# SubagentStart is deliberately absent: it carries no prompt text; its
# run_id comes from the popped pending dispatch.
# UserPromptSubmit maps to 'prompt' — VS Code's field name (not 'user_prompt').
RUN_ID_PROMPT_FIELDS: dict = {
    "UserPromptSubmit": "prompt",
    "SubagentStop":     "last_assistant_message",
    "Stop":             "last_assistant_message",
}


def build_context(payload: dict) -> core.HookContext:
    """Construct the immutable per-firing context from a parsed hook payload.

    Resolves the workspace root and LogPaths tree from the payload.
    Event name is read from payload['hook_event_name']; no separate event
    argument is accepted. Does not create directories.
    """
    workspace_root = core.resolve_workspace_root(payload)
    paths = core.build_paths(workspace_root)
    timestamp = core.current_timestamp()
    return core.HookContext(payload, workspace_root, paths, timestamp)


def resolve_run_identity(ctx: core.HookContext) -> None:
    """Set ctx.run_id by extracting from the event-specific prompt field.

    For events with no prompt-bearing field in RUN_ID_PROMPT_FIELDS
    (SessionStart, tool events, compaction, SubagentStart, etc.), ctx.run_id
    remains None. Routing to the appropriate bucket (run_id folder or
    unknown-run/) is handled by effective_run_id() in the core module.

    Never raises.
    """
    try:
        field_name = RUN_ID_PROMPT_FIELDS.get(ctx.event)
        if field_name is None:
            return
        prompt = ctx.field(field_name)
        ctx.run_id = runstate.extract_run_id(prompt)
    except Exception as exc:
        core.debug_log("resolve_run_identity failed", exc)


def dispatch(raw_input: str) -> None:
    """Testable core of main. Reads raw JSON, dispatches, swallows all exceptions.

    Steps (in fixed order):
      1. Parse raw_input as JSON; abort silently if not a dict.
      2. Build HookContext from the payload.
      3. resolve_run_identity — extracts run_id from the prompt field when present.
      4. Emit run_start when ctx.event == 'SessionStart' (D5).
      5. Invoke registered handler for ctx.event when registered; unknown events
         are silent no-ops.
      6. Emit run_end when ctx.event == 'Stop' (D5) — this step always runs,
         even when step 5 raised, so a failing Stop handler cannot swallow run_end.

    Every step is individually wrapped; the entire body is wrapped again.
    Nothing is written to stdout on any path.
    """
    try:
        # Step 1: parse
        try:
            payload = json.loads(raw_input)
        except Exception:
            return
        if not isinstance(payload, dict):
            return

        # Step 2: context
        ctx = build_context(payload)

        # Step 3: resolve run identity via extraction
        try:
            resolve_run_identity(ctx)
        except Exception as exc:
            core.debug_log("resolve_run_identity failed", exc)

        # Step 4: emit run_start on SessionStart (D5)
        try:
            if ctx.event == "SessionStart":
                _session.emit_run_start(ctx)
        except Exception as exc:
            core.debug_log("emit_run_start failed", exc)

        # Step 5: invoke handler when registered; unknown event is a silent no-op
        try:
            handler = HANDLERS.get(ctx.event)
            if handler is not None:
                handler(ctx)
        except Exception as exc:
            core.debug_log(f"handler for {ctx.event!r} raised", exc)

        # Step 6: emit run_end on Stop (D5) — always runs, even when step 5 raised
        try:
            if ctx.event == "Stop":
                _session.emit_run_end(ctx, None)
        except Exception as exc:
            core.debug_log("emit_run_end failed", exc)

    except Exception as exc:
        core.debug_log("dispatch outer exception", exc)


def main() -> int:
    """Process entrypoint. Reads one JSON object from stdin, dispatches, exits 0.

    ALWAYS exits 0. Writes NOTHING to stdout. Never raises.
    """
    try:
        raw = sys.stdin.read()
        dispatch(raw)
    except Exception as exc:
        core.debug_log("main() uncaught exception", exc)
    return 0


if __name__ == "__main__":
    sys.exit(main())
