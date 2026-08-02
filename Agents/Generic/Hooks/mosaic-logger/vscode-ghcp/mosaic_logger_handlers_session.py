"""mosaic_logger_handlers_session.py — Session-scoped event handlers.

Handles: SessionStart, UserPromptSubmit, PreCompact, Stop.
All writes go to 00_orchestrator_events.jsonl.

Also exposes emit_run_start and emit_run_end, called by the dispatcher's
run-lifecycle hooks rather than through the HANDLERS registry.

Key vscode-ghcp divergences (D5):
- Session lifecycle is anchored to SessionStart and Stop (no SessionEnd).
- run_end is emitted on Stop by the dispatcher; this module does not register
  a SessionEnd handler.
- session_end.reason is never populated: VS Code's stop_reason is not a valid
  member of the complete|error|abort|timeout|user_exit enum.
- session_end is emitted unconditionally on Stop, even when last_assistant_message
  is absent.
- UserPromptSubmit reads the field named 'prompt' (not 'user_prompt').
- PreCompact emits only schema-defined fields; custom_instructions is excluded.
"""

import mosaic_logger_core as core
import mosaic_logger_export as export
import mosaic_logger_transcript as transcript


# ---------------------------------------------------------------------------
# Run lifecycle events (called by dispatcher, not by HANDLERS registry)
# ---------------------------------------------------------------------------

def emit_run_start(ctx: "core.HookContext") -> None:
    """Emit run_start to the orchestrator stream when a session begins."""
    try:
        run_id = core.effective_run_id(ctx)
        event = core.build_event(
            "run_start", ctx,
            run_id=ctx.run_id,
            cwd=ctx.field("cwd"),
            model=ctx.field("model"),
            adapter_version=core.adapter_version(ctx),
        )
        core.append_event(ctx.paths.orchestrator_events(run_id), event)
    except Exception as exc:
        core.debug_log("emit_run_start failed", exc)


def emit_run_end(ctx: "core.HookContext", outcome: "str | None" = None) -> None:
    """Emit run_end to the orchestrator stream.

    D5: outcome is always omitted regardless of the parameter value because
    VS Code's stop_reason is not a valid mapping to the outcome enum. The
    parameter is retained so a future mapping requires only a call-site change.
    """
    try:
        run_id = core.effective_run_id(ctx)
        event = core.build_event(
            "run_end", ctx,
            run_id=ctx.run_id,
            outcome=outcome,
        )
        core.append_event(ctx.paths.orchestrator_events(run_id), event)
    except Exception as exc:
        core.debug_log("emit_run_end failed", exc)


# ---------------------------------------------------------------------------
# Session-scoped handlers
# ---------------------------------------------------------------------------

def handle_session_start(ctx: "core.HookContext") -> None:
    """Emit session_start with session_id, resumed, cwd, and model.

    resumed: True when source=='resume', False for other documented values,
    absent (omitted) when source is absent. False is a genuine resolved value
    and is kept, not pruned.
    """
    try:
        run_id = core.effective_run_id(ctx)
        source = ctx.field("source")
        resumed = (source == "resume") if source is not None else None
        event = core.build_event(
            "session_start", ctx,
            session_id=ctx.session_id,
            resumed=resumed,
            cwd=ctx.field("cwd"),
            model=ctx.field("model"),
        )
        core.append_event(ctx.paths.orchestrator_events(run_id), event)
    except Exception as exc:
        core.debug_log("handle_session_start failed", exc)


def handle_user_prompt_submit(ctx: "core.HookContext") -> None:
    """Emit turn with role 'user'.

    D5: reads the field named 'prompt' (VS Code's field name), not 'user_prompt'
    (the claude-code field name). Emits nothing when the field is absent.
    """
    try:
        run_id = core.effective_run_id(ctx)
        content = ctx.field("prompt")
        if content is None:
            return
        event = core.build_event(
            "turn", ctx,
            role="user",
            content=content,
        )
        core.append_event(ctx.paths.orchestrator_events(run_id), event)
    except Exception as exc:
        core.debug_log("handle_user_prompt_submit failed", exc)


def handle_pre_compact(ctx: "core.HookContext") -> None:
    """Emit compaction with schema-defined fields only.

    VS Code's payload includes custom_instructions, but the compaction event
    schema defines only trigger, tokens_before, and tokens_after. Emitting
    custom_instructions under any key would invent a field with no schema
    definition, violating the degrade-never-fabricate rule.
    """
    try:
        run_id = core.effective_run_id(ctx)
        event = core.build_event(
            "compaction", ctx,
            trigger=ctx.field("trigger"),
            tokens_before=ctx.field("tokens_before"),
        )
        core.append_event(ctx.paths.orchestrator_events(run_id), event)
    except Exception as exc:
        core.debug_log("handle_pre_compact failed", exc)


def handle_stop(ctx: "core.HookContext") -> None:
    """Emit assistant turn (when last_assistant_message present) and session_end.

    D5 divergences:
    - session_end is emitted UNCONDITIONALLY — even when last_assistant_message
      is absent. The reference adapter returns early; that shape would make
      session termination invisible whenever the final message is missing.
    - session_end.reason is NEVER populated. VS Code's stop_reason values
      (end_turn, max_tokens, etc.) are not members of the
      complete|error|abort|timeout|user_exit enum; any mapping would fabricate
      a value.

    Step 1 (conditional turn) is wrapped independently so a failure there does
    not suppress step 2 (unconditional session_end).
    """
    run_id = core.effective_run_id(ctx)

    # Step 1: emit assistant turn when last_assistant_message is present.
    try:
        content = ctx.field("last_assistant_message")
        if content is not None:
            facts = transcript.read_last_assistant_facts(ctx.transcript_path)
            turn = core.build_event(
                "turn", ctx,
                role="assistant",
                content=content,
                model=facts.model,
                token_usage=facts.token_usage,
            )
            core.append_event(ctx.paths.orchestrator_events(run_id), turn)
    except Exception as exc:
        core.debug_log("handle_stop: turn emit failed", exc)

    # Step 2: emit session_end unconditionally — no reason field.
    try:
        session_end = core.build_event(
            "session_end", ctx,
            session_id=ctx.session_id,
        )
        core.append_event(ctx.paths.orchestrator_events(run_id), session_end)
    except Exception as exc:
        core.debug_log("handle_stop: session_end emit failed", exc)

    # Step 3: best-effort transcript export (no-op when transcript_path is absent).
    try:
        export.export_transcript(
            ctx.transcript_path,
            ctx.paths.orchestrator_raw(run_id),
            "transcript_path",
        )
    except Exception as exc:
        core.debug_log("handle_stop: export failed", exc)


# ---------------------------------------------------------------------------
# Handler registry
# ---------------------------------------------------------------------------

HANDLERS = {
    "SessionStart":     handle_session_start,
    "UserPromptSubmit": handle_user_prompt_submit,
    "PreCompact":       handle_pre_compact,
    "Stop":             handle_stop,
}
