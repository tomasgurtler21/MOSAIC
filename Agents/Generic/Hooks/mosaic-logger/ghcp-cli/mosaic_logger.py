"""mosaic_logger.py -- Dispatcher entrypoint for the mosaic-logger GHCP CLI hook adapter.

This module is the process entrypoint spawned by the GHCP CLI harness on every hook
firing. It reads the event name from sys.argv[1] and one JSON object from stdin,
dispatches to the appropriate handler, and exits 0 unconditionally. Nothing is
written to stdout.
"""

import json
import sys

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_session as _session
import mosaic_logger_handlers_invocation as _invocation
import mosaic_logger_handlers_tools as _tools


# ---------------------------------------------------------------------------
# Handler registry -- populated at import time from handler modules
# ---------------------------------------------------------------------------

HANDLERS: dict = {}
for _mod in [_session, _invocation, _tools]:
    if hasattr(_mod, "HANDLERS"):
        HANDLERS.update(_mod.HANDLERS)


# ---------------------------------------------------------------------------
# Dispatcher functions
# ---------------------------------------------------------------------------

# Events with prompt-bearing fields from which run_id may be extracted.
# Keys are GHCP CLI camelCase event names; values are camelCase payload field names.
_RUN_ID_PROMPT_FIELDS: dict = {
    "subagentStart": "agentPrompt",
    "subagentStop": "response",
    "userPromptSubmitted": "prompt",
    "notification": "message",
}
# agentStop is intentionally absent: GHCP CLI's agentStop payload does not carry
# a last_assistant_message or equivalent text field for run_id extraction.


def build_context(event: str, payload: dict) -> core.HookContext:
    """Construct the immutable per-firing context from an event name and parsed payload.
    The event name comes from sys.argv[1] (passed by the hook registration command).
    Resolves the workspace root and LogPaths tree. Does not create directories."""
    workspace_root = core.resolve_workspace_root(payload)
    paths = core.build_paths(workspace_root)
    timestamp = core.current_timestamp()
    return core.HookContext(event, payload, workspace_root, paths, timestamp)


def resolve_run_identity(ctx: core.HookContext) -> None:
    """Set ctx.run_id by extracting from the event-specific prompt field.

    For events with no prompt-bearing field, ctx.run_id remains None.
    Uses ctx.field(field_name) with the standard normalization path for all
    prompt fields -- no special-casing is needed for any particular field name.
    Never raises.
    """
    try:
        field_name = _RUN_ID_PROMPT_FIELDS.get(ctx.event)
        if field_name is None:
            return
        prompt = ctx.field(field_name)
        ctx.run_id = runstate.extract_run_id(prompt)
    except Exception as exc:
        core.debug_log("resolve_run_identity failed", exc)


def dispatch(event: str, raw_input: str) -> None:
    """Testable core of main. Receives the event name and raw JSON from stdin.

    Steps:
      1. Parse raw_input as JSON; abort silently if not a dict.
      2. Build HookContext from event name and parsed payload.
      3. resolve_run_identity -- extracts run_id from prompt content when present.
      4. Emit run_start on sessionStart.
      5. Invoke registered handler for ctx.event, if any.
      6. Emit run_end on sessionEnd -- always runs even if step 5 raised.
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
        ctx = build_context(event, payload)

        # Step 3: resolve run identity via extraction
        try:
            resolve_run_identity(ctx)
        except Exception as exc:
            core.debug_log("resolve_run_identity failed", exc)

        # Step 4: emit run_start on sessionStart
        try:
            if ctx.event == "sessionStart":
                _session.emit_run_start(ctx)
        except Exception as exc:
            core.debug_log("emit_run_start failed", exc)

        # Step 5: handler
        try:
            handler = HANDLERS.get(ctx.event)
            if handler is not None:
                handler(ctx)
        except Exception as exc:
            core.debug_log(f"handler for {ctx.event!r} raised", exc)

        # Step 6: emit run_end on sessionEnd -- always runs
        try:
            if ctx.event == "sessionEnd":
                outcome = ctx.field("reason")
                _session.emit_run_end(ctx, outcome)
        except Exception as exc:
            core.debug_log("emit_run_end failed", exc)

    except Exception as exc:
        core.debug_log("dispatch outer exception", exc)


def main() -> int:
    """Process entrypoint. Reads event name from sys.argv[1] and one JSON object
    from stdin, dispatches via dispatch(event, raw_input), exits 0.

    ALWAYS exits 0. Writes NOTHING to stdout. Never raises.
    """
    try:
        event = sys.argv[1] if len(sys.argv) > 1 else ""
        raw = sys.stdin.read()
        dispatch(event, raw)
    except Exception as exc:
        core.debug_log("main() uncaught exception", exc)
    return 0


if __name__ == "__main__":
    sys.exit(main())
