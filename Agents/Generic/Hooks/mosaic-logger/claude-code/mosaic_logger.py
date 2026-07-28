"""mosaic_logger.py — Dispatcher entrypoint for the mosaic-logger Claude Code hook adapter.

This module is the process entrypoint spawned by the Claude Code harness on every hook
firing. It reads one JSON object from stdin, dispatches it to the appropriate handler,
and exits 0 unconditionally. Nothing is written to stdout.
"""

import json
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

# Events permitted to mint a new run_id
MINTING_EVENTS: frozenset = frozenset({"SessionStart", "SubagentStart", "UserPromptSubmit"})

# Events that close the run marker
CLOSING_EVENTS: frozenset = frozenset({"SessionEnd"})


# ---------------------------------------------------------------------------
# Dispatcher functions
# ---------------------------------------------------------------------------

def build_context(payload: dict) -> core.HookContext:
    """Construct the immutable per-firing context from a parsed hook payload.
    Resolves the workspace root and LogPaths tree. Does not touch the run marker
    and does not create directories.
    """
    workspace_root = core.resolve_workspace_root(payload)
    paths = core.build_paths(workspace_root)
    timestamp = core.current_timestamp()
    return core.HookContext(payload, workspace_root, paths, timestamp)


def run_identity_prelude(ctx: core.HookContext) -> runstate.RunResolution:
    """Resolve (and when permitted, mint or close) the session's run_id.

    Sets ctx.run_id. Emits run_start on a MINTED outcome.
    Runs BEFORE the event handler so every handler sees a resolved ctx.run_id.
    Returns the RunResolution so the postlude can act on it.
    """
    if ctx.event in CLOSING_EVENTS:
        resolution = runstate.close_run(ctx.paths, ctx.session_id)
    else:
        allow_mint = ctx.event in MINTING_EVENTS
        resolution = runstate.resolve_run(ctx.paths, ctx.session_id, allow_mint)

    ctx.run_id = resolution.run_id

    if resolution.outcome == "minted":
        _session.emit_run_start(ctx)

    return resolution


def run_identity_postlude(ctx: core.HookContext,
                          resolution: runstate.RunResolution) -> None:
    """Emit run_end when, and only when, resolution.outcome == 'closed'.

    Called AFTER the event handler so run_end is the last line of the
    orchestrator stream — run is the outer scope and closes last.
    """
    if resolution.outcome == "closed":
        # Derive outcome from SessionEnd reason when available
        outcome = ctx.field("reason") if ctx.event in CLOSING_EVENTS else None
        _session.emit_run_end(ctx, outcome)


def dispatch(raw_input: str) -> None:
    """Testable core of main. Reads raw JSON, dispatches, swallows all exceptions.

    Steps:
      1. Parse raw_input as JSON; abort silently if not a dict.
      2. Build HookContext.
      3. run_identity_prelude — sets ctx.run_id, mints/closes marker, emits run_start.
      4. Invoke registered handler for ctx.event, if any.
      5. run_identity_postlude — emits run_end on CLOSED outcome.
    Step 5 runs even when step 4 raised, so a handler failure on SessionEnd
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

        # Step 3: prelude (marker mint/close, run_start emission)
        try:
            resolution = run_identity_prelude(ctx)
        except Exception as exc:
            core.debug_log("run_identity_prelude failed", exc)
            resolution = runstate.RunResolution(run_id=None, outcome="unresolved")

        # Step 4: handler
        try:
            handler = HANDLERS.get(ctx.event)
            if handler is not None:
                handler(ctx)
        except Exception as exc:
            core.debug_log(f"handler for {ctx.event!r} raised", exc)

        # Step 5: postlude (run_end emission) — always runs
        try:
            run_identity_postlude(ctx, resolution)
        except Exception as exc:
            core.debug_log("run_identity_postlude failed", exc)

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
