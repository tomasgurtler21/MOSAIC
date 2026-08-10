"""mosaic_logger_handlers_tools.py — Tool-use event handlers.

Handles: PreToolUse, PostToolUse, PostToolUseFailure. Routes tool events to
the orchestrator stream or the owning subagent's stream based on agent_id.

High-frequency path: performs no transcript reads and no directory scanning
for tool events. When tool_capture_enabled() allows it and the firing is in
orchestrator scope (no ctx.agent_id), a transcript read is deliberately
performed to emit usage_record events. Subagent-scoped firings suppress
usage emission entirely; the subagent-stop handler is the sole source of a
subagent's own usage records, reading the subagent's own transcript at stop.
"""

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_usage as usage


def resolve_destination(ctx: "core.HookContext"):
    """Route tool event to orchestrator or subagent stream based on agent_id presence."""
    run_id = core.effective_run_id(ctx)
    if not ctx.agent_id:
        return ctx.paths.orchestrator_events(run_id)
    agent_instance_id, _mapped = runstate.resolve_invocation(
        ctx.paths, run_id, ctx.agent_id
    )
    return ctx.paths.invocation_events(run_id, agent_instance_id)


def _resolve_usage_scope(ctx: "core.HookContext"):
    """Return (agent_instance_id, source) for orchestrator-scoped firings,
    matching resolve_destination's routing decision.

    Only called for orchestrator-scoped firings (ctx.agent_id unset).
    Subagent-scoped firings are suppressed before this function is reached;
    _emit_tool_usage_records returns early when ctx.agent_id is set, so
    this function never runs in subagent scope."""
    if not ctx.agent_id:
        return None, "orchestrator_transcript"
    run_id = core.effective_run_id(ctx)
    agent_instance_id, _mapped = runstate.resolve_invocation(
        ctx.paths, run_id, ctx.agent_id
    )
    return agent_instance_id, "agent_transcript"


def _emit_tool_usage_records(ctx: "core.HookContext") -> None:
    """Emit usage_record events from ctx.transcript_path when in orchestrator
    scope and tool_capture_enabled() allows it. Never suppresses the
    surrounding tool event.

    When ctx.agent_id is set (subagent scope), returns immediately without
    emitting any usage records to any stream. The subagent-stop handler in
    mosaic_logger_handlers_invocation.py is the sole source of a subagent's
    own usage records; it reads the subagent's own transcript at stop time.

    When ctx.agent_id is unset (orchestrator scope) and tool_capture_enabled()
    allows it, reads ctx.transcript_path and emits one usage_record per
    assistant record to the orchestrator stream, unchanged from prior behaviour."""
    if ctx.agent_id:
        return
    if not usage.tool_capture_enabled():
        return
    agent_instance_id, source = _resolve_usage_scope(ctx)
    usage.emit_usage_records(ctx, ctx.transcript_path, agent_instance_id, source)


def resolve_call_id(ctx: "core.HookContext") -> str:
    """Return harness-native tool_use_id when present, else a suffixed fallback."""
    native = ctx.field("tool_use_id")
    if native:
        return native
    return runstate.fallback_call_id(ctx.field("tool_name"))


def handle_pre_tool_use(ctx: "core.HookContext") -> None:
    """Emit tool_call_start. Additionally, when tool_name is 'Agent' or 'Task',
    extract agent_instance_id and run_id from tool_input.prompt, adopt any
    extracted run_id into the session-run binding, and persist a pending
    dispatch for the current session_id.

    Both 'Agent' and 'Task' tool names are accepted because Claude Code may
    dispatch subagents under either name depending on the harness version.

    tool_call_start's own destination is resolved before the Agent/Task
    capture runs, so an extraction that adopts a new run_id for this session
    extends the binding for LATER firings without changing where THIS
    firing's own event lands -- that decision was already made by
    resolve_run_identity ahead of this handler.

    The pending-dispatch write is best-effort; failure does not prevent
    the tool_call_start event from being emitted. Never raises.
    """
    tool_name = ctx.field("tool_name")

    event = core.build_event(
        "tool_call_start", ctx,
        call_id=resolve_call_id(ctx),
        tool_name=tool_name,
        tool_input=ctx.field("tool_input"),
    )
    core.append_event(resolve_destination(ctx), event)

    # Capture pending dispatch for Agent/Task tool invocations.
    if tool_name in ("Agent", "Task"):
        try:
            tool_input = ctx.field("tool_input")
            prompt = None
            if isinstance(tool_input, dict):
                prompt = tool_input.get("prompt") or None
            if prompt:
                agent_instance_id = runstate.extract_instance_id(prompt)
                if agent_instance_id:
                    extracted_run_id = runstate.extract_run_id(prompt)
                    runstate.adopt_run_id(ctx, extracted_run_id)
                    runstate.put_pending_dispatch(
                        ctx.paths,
                        ctx.session_id,
                        agent_instance_id,
                        extracted_run_id,
                        prompt=prompt,
                    )
        except Exception:
            pass  # Never let dispatch capture suppress the tool_call_start event.

    # Deliberate reversal of the "no transcript reads" property on this
    # high-frequency path, gated by tool_capture_enabled() (design decision,
    # not incidental). Runs after tool_call_start so an emission failure
    # can never suppress it.
    _emit_tool_usage_records(ctx)


def handle_post_tool_use(ctx: "core.HookContext") -> None:
    """Emit tool_call_end with status 'success'."""
    event = core.build_event(
        "tool_call_end", ctx,
        call_id=resolve_call_id(ctx),
        status="success",
        tool_output=ctx.field("tool_output"),
    )
    core.append_event(resolve_destination(ctx), event)
    _emit_tool_usage_records(ctx)


def handle_post_tool_use_failure(ctx: "core.HookContext") -> None:
    """Emit tool_call_end with status 'error'."""
    tool_output = ctx.field("tool_output")
    error = ctx.field("error") or (str(tool_output) if tool_output is not None else None)
    event = core.build_event(
        "tool_call_end", ctx,
        call_id=resolve_call_id(ctx),
        status="error",
        tool_output=tool_output,
        error=error,
    )
    core.append_event(resolve_destination(ctx), event)
    _emit_tool_usage_records(ctx)


HANDLERS = {
    "PreToolUse": handle_pre_tool_use,
    "PostToolUse": handle_post_tool_use,
    "PostToolUseFailure": handle_post_tool_use_failure,
}
