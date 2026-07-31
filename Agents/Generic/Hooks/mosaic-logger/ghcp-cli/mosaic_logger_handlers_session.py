"""mosaic_logger_handlers_session.py -- Session-scoped event handlers.

Handles 9 events: sessionStart, sessionEnd, userPromptSubmitted, agentStop,
notification, preCompact, userPromptTransformed, permissionRequest, errorOccurred.
All writes go to 00_orchestrator_events.jsonl.

Also exposes emit_run_start and emit_run_end, called by the dispatcher.
"""

import mosaic_logger_core as core
import mosaic_logger_export as export
import mosaic_logger_transcript as transcript


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
    """Emit session_start. Reads 'resumed' from payload (boolean field on GHCP CLI)."""
    run_id = core.effective_run_id(ctx)
    # GHCP CLI sends 'resumed' as a direct boolean field (not derived from 'source')
    resumed = ctx.field("resumed")
    event = core.build_event(
        "session_start", ctx,
        session_id=ctx.session_id,
        resumed=resumed,
        cwd=ctx.field("cwd"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


def handle_session_end(ctx: "core.HookContext") -> None:
    """Emit session_end with reason."""
    run_id = core.effective_run_id(ctx)
    event = core.build_event(
        "session_end", ctx,
        session_id=ctx.session_id,
        reason=ctx.field("reason"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


def handle_user_prompt_submitted(ctx: "core.HookContext") -> None:
    """Emit turn with role 'user'. Reads from camelCase 'prompt' field."""
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


def handle_agent_stop(ctx: "core.HookContext") -> None:
    """Emit turn with role 'assistant'. Reads transcriptPath, stopReason.

    Unlike claude-code's handle_stop, this handler does NOT read a
    last_assistant_message field from the payload because GHCP CLI's agentStop
    payload does not carry one. The assistant turn event is emitted without
    content (content field omitted). Model and token_usage are still read from
    the transcript via read_last_assistant_facts when transcriptPath is available.

    Never raises.
    """
    run_id = core.effective_run_id(ctx)

    facts = transcript.read_last_assistant_facts(ctx.transcript_path)

    # Emit turn without content (no last_assistant_message in GHCP CLI agentStop)
    event = core.build_event(
        "turn", ctx,
        role="assistant",
        model=facts.model,
        token_usage=facts.token_usage,
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)

    # Export orchestrator transcript (best-effort)
    export.export_transcript(
        ctx.transcript_path,
        ctx.paths.orchestrator_raw(run_id),
        "transcript_path",
    )


def handle_notification(ctx: "core.HookContext") -> None:
    """Emit notification with notification_type and message.
    GHCP CLI uses 'notificationType' (camelCase) in payload."""
    run_id = core.effective_run_id(ctx)
    notification_type = ctx.field("notificationType")
    if notification_type is None:
        return
    event = core.build_event(
        "notification", ctx,
        notification_type=notification_type,
        message=ctx.field("message"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


def handle_pre_compact(ctx: "core.HookContext") -> None:
    """Emit compaction with trigger and token counts."""
    run_id = core.effective_run_id(ctx)
    event = core.build_event(
        "compaction", ctx,
        trigger=ctx.field("trigger"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


def handle_user_prompt_transformed(ctx: "core.HookContext") -> None:
    """Emit observational prompt_transformed event.
    GHCP-CLI-specific. No decision output, no stdout."""
    run_id = core.effective_run_id(ctx)
    event = core.build_event(
        "prompt_transformed", ctx,
        transformed_prompt=ctx.field("transformedPrompt"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


def handle_permission_request(ctx: "core.HookContext") -> None:
    """Emit observational permission_request event.
    GHCP-CLI-specific. MUST NEVER emit any permissionDecision or write stdout.
    Logs toolName (camelCase) and any associated context."""
    run_id = core.effective_run_id(ctx)
    event = core.build_event(
        "permission_request", ctx,
        tool_name=ctx.field("toolName"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


def handle_error_occurred(ctx: "core.HookContext") -> None:
    """Emit observational error_occurred event.
    GHCP-CLI-specific. Logs error message, errorContext, recoverable flag."""
    run_id = core.effective_run_id(ctx)
    # error is a dict: {message, name, stack?}
    error_dict = ctx.payload.get("error")
    error_message = None
    if isinstance(error_dict, dict):
        error_message = error_dict.get("message") or None
    event = core.build_event(
        "error_occurred", ctx,
        error_message=error_message,
        error_context=ctx.field("errorContext"),
        recoverable=ctx.field("recoverable"),
    )
    core.append_event(ctx.paths.orchestrator_events(run_id), event)


# ---------------------------------------------------------------------------
# Handler registry
# ---------------------------------------------------------------------------

HANDLERS: dict = {
    "sessionStart": handle_session_start,
    "sessionEnd": handle_session_end,
    "userPromptSubmitted": handle_user_prompt_submitted,
    "agentStop": handle_agent_stop,
    "notification": handle_notification,
    "preCompact": handle_pre_compact,
    "userPromptTransformed": handle_user_prompt_transformed,
    "permissionRequest": handle_permission_request,
    "errorOccurred": handle_error_occurred,
}
