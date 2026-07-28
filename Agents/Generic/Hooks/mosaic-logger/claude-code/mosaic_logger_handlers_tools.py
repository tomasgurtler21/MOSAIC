"""mosaic_logger_handlers_tools.py — Tool-use event handlers.

Handles: PreToolUse, PostToolUse, PostToolUseFailure. Routes tool events to
the orchestrator stream or the owning subagent's stream based on agent_id.

High-frequency path: performs no transcript reads and no directory scanning.
"""

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate


def resolve_destination(ctx: "core.HookContext"):
    """Route tool event to orchestrator or subagent stream based on agent_id presence."""
    if not ctx.agent_id:
        return ctx.paths.orchestrator_events(ctx.run_id)
    agent_instance_id = runstate.resolve_invocation_id(
        ctx.paths, ctx.run_id, ctx.agent_id
    )
    return ctx.paths.invocation_events(ctx.run_id, agent_instance_id)


def resolve_call_id(ctx: "core.HookContext") -> str:
    """Return harness-native tool_use_id when present, else a suffixed fallback."""
    native = ctx.field("tool_use_id")
    if native:
        return native
    return runstate.fallback_call_id(ctx.field("tool_name"))


def handle_pre_tool_use(ctx: "core.HookContext") -> None:
    """Emit tool_call_start."""
    if not ctx.run_id:
        return
    event = core.build_event(
        "tool_call_start", ctx,
        call_id=resolve_call_id(ctx),
        tool_name=ctx.field("tool_name"),
        tool_input=ctx.field("tool_input"),
    )
    core.append_event(resolve_destination(ctx), event)


def handle_post_tool_use(ctx: "core.HookContext") -> None:
    """Emit tool_call_end with status 'success'."""
    if not ctx.run_id:
        return
    event = core.build_event(
        "tool_call_end", ctx,
        call_id=resolve_call_id(ctx),
        status="success",
        tool_output=ctx.field("tool_output"),
    )
    core.append_event(resolve_destination(ctx), event)


def handle_post_tool_use_failure(ctx: "core.HookContext") -> None:
    """Emit tool_call_end with status 'error'."""
    if not ctx.run_id:
        return
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
