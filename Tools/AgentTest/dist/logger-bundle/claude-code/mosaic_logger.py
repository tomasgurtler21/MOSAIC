"""mosaic_logger.py — Dispatcher entrypoint for the mosaic-logger Claude Code hook adapter.

This module is the process entrypoint spawned by the Claude Code harness on every hook
firing. It reads one JSON object from stdin, dispatches it to the appropriate handler,
and exits 0 unconditionally. Nothing is written to stdout.
"""

import json
import os
import sys

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_session as _session
import mosaic_logger_handlers_invocation as _invocation
import mosaic_logger_handlers_tools as _tools


# ---------------------------------------------------------------------------
# Handler registry — populated at import time from handler modules
# ---------------------------------------------------------------------------

HANDLERS: dict = {}
for _mod in [_session, _invocation, _tools]:
    if hasattr(_mod, "HANDLERS"):
        HANDLERS.update(_mod.HANDLERS)


# ---------------------------------------------------------------------------
# Dispatcher functions
# ---------------------------------------------------------------------------

# Events with prompt-bearing fields from which run_id may be extracted.
_RUN_ID_PROMPT_FIELDS: dict = {
    "SubagentStart": "agent_prompt",
    "SubagentStop": "last_assistant_message",
    "UserPromptSubmit": "user_prompt",
    "Stop": "last_assistant_message",
    "Notification": "message",
}


def build_context(payload: dict) -> core.HookContext:
    """Construct the immutable per-firing context from a parsed hook payload.
    Resolves the workspace root and LogPaths tree. Does not create directories.
    """
    workspace_root = core.resolve_workspace_root(payload)
    paths = core.build_paths(workspace_root)
    timestamp = core.current_timestamp()
    return core.HookContext(payload, workspace_root, paths, timestamp)


def resolve_run_identity(ctx: core.HookContext) -> None:
    """Populate ctx.run_id before any handler runs.

    Resolution order:
      1. Text extraction. When ctx.event has a prompt-bearing field and
         extract_run_id yields a value, that value is adopted via
         runstate.adopt_run_id -- it takes effect for this firing AND
         extends the session timeline. A value differing from the currently
         bound run therefore wins for this firing and every later firing.
      2. Binding lookup. Otherwise the run active at ctx.timestamp is read
         from the session timeline and assigned to ctx.run_id. This is the
         path that resolves the event types carrying no prompt-bearing
         field.
      3. Unresolved. ctx.run_id stays None, core.effective_run_id returns
         'unknown-run', and a 'run-identity: unresolved' diagnostic is
         emitted.

    Never raises.
    """
    try:
        field_name = _RUN_ID_PROMPT_FIELDS.get(ctx.event)
        extracted = None
        if field_name is not None:
            prompt = ctx.field(field_name)
            extracted = runstate.extract_run_id(prompt)

        if extracted:
            runstate.adopt_run_id(ctx, extracted)
            return

        bound = runstate.get_session_run_binding(ctx.paths, ctx.session_id, at=ctx.timestamp)
        if bound:
            ctx.run_id = bound
            return

        env_run_id = os.environ.get("MOSAIC_RUN_ID")
        if env_run_id and runstate.is_valid_run_id(env_run_id):
            runstate.adopt_run_id(ctx, env_run_id)
            return

        core.debug_log(
            f"run-identity: unresolved for session_id={ctx.session_id!r} "
            f"event={ctx.event!r}"
        )
    except Exception as exc:
        core.debug_log("resolve_run_identity failed", exc)


def dispatch(raw_input: str) -> None:
    """Testable core of main. Reads raw JSON, dispatches, swallows all exceptions.

    Steps:
      1. Parse raw_input as JSON; abort silently if not a dict.
      2. Build HookContext.
      3. resolve_run_identity — extracts run_id from prompt content when present.
      4. Emit run_start on SessionStart (event-based, not mint-outcome-based).
      5. Invoke registered handler for ctx.event, if any.
      6. Emit run_end on SessionEnd (event-based, after the handler) — always runs.
    Step 6 runs even when step 5 raised, so a handler failure on SessionEnd
    cannot swallow run_end.
    Any exception at any step is caught and swallowed.
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

        # Step 4: emit run_start on SessionStart
        try:
            if ctx.event == "SessionStart":
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

        # Step 6: emit run_end on SessionEnd — always runs
        try:
            if ctx.event == "SessionEnd":
                outcome = ctx.field("reason")
                _session.emit_run_end(ctx, outcome)
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
