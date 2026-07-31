"""mosaic_logger_handlers_tools.py — Tool-use event handlers.

Handles: PreToolUse, PostToolUse, PostToolUseFailure. Routes tool events to
the orchestrator stream or the owning subagent's stream based on agent_id.

High-frequency path: performs no transcript reads and no directory scanning.
"""

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate


def resolve_destination(ctx: "core.HookContext"):
    """Route tool event to orchestrator or subagent stream based on agent_id presence."""
    run_id = core.effective_run_id(ctx)
    if not ctx.agent_id:
        return ctx.paths.orchestrator_events(run_id)
    agent_instance_id = runstate.resolve_invocation_id(
        ctx.paths, run_id, ctx.agent_id
    )
    return ctx.paths.invocation_events(run_id, agent_instance_id)


def resolve_call_id(ctx: "core.HookContext") -> str:
    """Return harness-native tool_use_id when present, else a suffixed fallback."""
    native = ctx.field("tool_use_id")
    if native:
        return native
    return runstate.fallback_call_id(ctx.field("tool_name"))


def handle_pre_tool_use(ctx: "core.HookContext") -> None:
    """Emit tool_call_start. Additionally, when tool_name is 'Agent' or 'Task',
    extract agent_instance_id and run_id from tool_input.prompt and
    persist a pending dispatch for the current session_id.

    Both 'Agent' and 'Task' tool names are accepted because Claude Code may
    dispatch subagents under either name depending on the harness version.

    The pending-dispatch write is best-effort; failure does not prevent
    the tool_call_start event from being emitted. Never raises.
    """
    # Capture pending dispatch for Agent/Task tool invocations.
    tool_name = ctx.field("tool_name")
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
                    runstate.put_pending_dispatch(
                        ctx.paths,
                        core.effective_run_id(ctx),
                        ctx.session_id,
                        agent_instance_id,
                        extracted_run_id,
                        prompt=prompt,
                    )
        except Exception:
            pass  # Never let dispatch capture suppress the tool_call_start event.

    event = core.build_event(
        "tool_call_start", ctx,
        call_id=resolve_call_id(ctx),
        tool_name=tool_name,
        tool_input=ctx.field("tool_input"),
    )
    core.append_event(resolve_destination(ctx), event)


def handle_post_tool_use(ctx: "core.HookContext") -> None:
    """Emit tool_call_end with status 'success'."""
    event = core.build_event(
        "tool_call_end", ctx,
        call_id=resolve_call_id(ctx),
        status="success",
        tool_output=ctx.field("tool_output"),
    )
    core.append_event(resolve_destination(ctx), event)


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


HANDLERS = {
    "PreToolUse": handle_pre_tool_use,
    "PostToolUse": handle_post_tool_use,
    "PostToolUseFailure": handle_post_tool_use_failure,
}
