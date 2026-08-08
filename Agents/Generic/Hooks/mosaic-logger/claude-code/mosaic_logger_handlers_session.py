"""mosaic_logger_handlers_session.py — Session-scoped event handlers.

Handles: SessionStart, SessionEnd, UserPromptSubmit, Stop, Notification,
PreCompact, PostCompact. All writes go to 00_orchestrator_events.jsonl.

Also exposes emit_run_start and emit_run_end, called by the dispatcher's
run-identity resolution.
"""

import mosaic_logger_core as core
import mosaic_logger_export as export
import mosaic_logger_transcript as transcript
import mosaic_logger_usage as usage


# ---------------------------------------------------------------------------
# Run lifecycle events (called by dispatcher, not by HANDLERS registry)
# ---------------------------------------------------------------------------

def emit_run_start(ctx: "core.HookContext") -> None:
    """Emit run_start to the orchestrator stream when a session begins."""
    run_id = core.effective_run_id(ctx)
    event = core.build_event(
        "run_start", ctx,
        run_id=ctx.run_id,
        cwd=ctx.field("cwd"),
        model=ctx.field("model"),
        adapter_version=core.adapter_version(ctx),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


def emit_run_end(ctx: "core.HookContext", outcome: "str | None") -> None:
    """Emit run_end to the orchestrator stream when a session ends."""
    run_id = core.effective_run_id(ctx)
    event = core.build_event(
        "run_end", ctx,
        run_id=ctx.run_id,
        outcome=outcome,
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


# ---------------------------------------------------------------------------
# Session-scoped handlers
# ---------------------------------------------------------------------------

def handle_session_start(ctx: "core.HookContext") -> None:
    """Emit session_start."""
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


def handle_session_end(ctx: "core.HookContext") -> None:
    """Emit session_end."""
    run_id = core.effective_run_id(ctx)
    event = core.build_event(
        "session_end", ctx,
        session_id=ctx.session_id,
        reason=ctx.field("reason"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


def handle_user_prompt_submit(ctx: "core.HookContext") -> None:
    """Emit turn with role 'user'."""
    run_id = core.effective_run_id(ctx)
    content = ctx.field("user_prompt")
    if content is None:
        return
    event = core.build_event(
        "turn", ctx,
        role="user",
        content=content,
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


def handle_stop(ctx: "core.HookContext") -> None:
    """Emit turn with role 'assistant' for the orchestrator.

    This handler is registered for hook_event_name == 'Stop' only;
    SubagentStop events are dispatched to handle_subagent_stop instead.
    The event-name-based dispatch in HANDLERS already distinguishes
    orchestrator Stop from subagent SubagentStop, so no agent_id guard
    is needed.

    Processes Stop events regardless of whether agent_id is populated
    (which occurs when the orchestrator is launched with --agent).

    model and token_usage are read from the transcript JSONL at
    ctx.transcript_path. Both are independently optional; a missing or
    unreadable transcript degrades to omitted fields, never an exception.

    After writing the turn event, the orchestrator transcript is exported to
    00_orchestrator_session.raw plus its .meta.json sidecar. An export failure
    is swallowed and never prevents the turn event from being written.

    Never raises.
    """
    run_id = core.effective_run_id(ctx)
    content = ctx.field("last_assistant_message")
    if content is None:
        return

    # Read transcript-derived facts once; degrade silently on any failure.
    facts = transcript.read_last_assistant_facts(ctx.transcript_path)

    event = core.build_event(
        "turn", ctx,
        role="assistant",
        content=content,
        model=facts.model,
        token_usage=facts.token_usage,
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)

    # Emit one raw usage_record per assistant record observed in the
    # orchestrator transcript. Runs after the turn event so an emission
    # failure can never suppress it (never raises; see mosaic_logger_usage).
    usage.emit_usage_records(
        ctx, ctx.transcript_path, None, "orchestrator_transcript"
    )

    # Refresh the orchestrator raw transcript export (best-effort).
    export.export_transcript(
        ctx.transcript_path,
        ctx.paths.orchestrator_raw(run_id),
        "transcript_path",
    )


def handle_notification(ctx: "core.HookContext") -> None:
    """Emit notification."""
    run_id = core.effective_run_id(ctx)
    notification_type = ctx.field("notification_type")
    if notification_type is None:
        return
    event = core.build_event(
        "notification", ctx,
        notification_type=notification_type,
        message=ctx.field("message"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


def handle_pre_compact(ctx: "core.HookContext") -> None:
    """Emit partial compaction with trigger and tokens_before.

    Also emits raw usage_record events from the orchestrator transcript:
    PreCompact fires just before compaction may remove records, so capturing
    here minimises the window in which a record is lost before any firing
    observes it.
    """
    run_id = core.effective_run_id(ctx)
    event = core.build_event(
        "compaction", ctx,
        trigger=ctx.field("trigger"),
        tokens_before=ctx.field("tokens_before"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)

    usage.emit_usage_records(
        ctx, ctx.transcript_path, None, "orchestrator_transcript"
    )


def handle_post_compact(ctx: "core.HookContext") -> None:
    """Emit partial compaction with tokens_after.

    Also emits raw usage_record events from the (post-compaction) orchestrator
    transcript, per the same every-transcript-bearing-firing requirement as
    handle_pre_compact.
    """
    run_id = core.effective_run_id(ctx)
    event = core.build_event(
        "compaction", ctx,
        tokens_after=ctx.field("tokens_after"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)

    usage.emit_usage_records(
        ctx, ctx.transcript_path, None, "orchestrator_transcript"
    )


# ---------------------------------------------------------------------------
# Handler registry
# ---------------------------------------------------------------------------

HANDLERS = {
    "SessionStart": handle_session_start,
    "SessionEnd": handle_session_end,
    "UserPromptSubmit": handle_user_prompt_submit,
    "Stop": handle_stop,
    "Notification": handle_notification,
    "PreCompact": handle_pre_compact,
    "PostCompact": handle_post_compact,
}
