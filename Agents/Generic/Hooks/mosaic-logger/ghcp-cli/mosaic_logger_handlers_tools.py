"""mosaic_logger_handlers_tools.py -- Tool-use event handlers.

Handles: preToolUse, postToolUse, postToolUseFailure. Routes tool events to
the orchestrator stream or the owning subagent's stream based on agentId.

High-frequency path: performs no transcript reads and no directory scanning.
"""

import pathlib

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate


def resolve_destination(ctx: "core.HookContext") -> pathlib.Path:
    """Route tool event to orchestrator or subagent stream based on agent_id."""
    run_id = core.effective_run_id(ctx)
    if not ctx.agent_id:
        return ctx.paths.orchestrator_events(run_id)
    agent_instance_id = runstate.resolve_invocation_id(
        ctx.paths, run_id, ctx.agent_id
    )
    return ctx.paths.invocation_events(run_id, agent_instance_id)


def resolve_call_id(ctx: "core.HookContext") -> str:
    """Return harness-native toolUseId when present, else a suffixed fallback."""
    native = ctx.field("toolUseId")
    if native:
        return native
    return runstate.fallback_call_id(ctx.field("toolName"))


def handle_pre_tool_use(ctx: "core.HookContext") -> None:
    """Emit tool_call_start. When toolName is 'agent' or 'task' (GHCP CLI native names),
    extract agent_instance_id from toolArgs.prompt and persist a pending dispatch.
    NEVER writes to stdout. NEVER emits permissionDecision. Never raises."""
    tool_name = ctx.field("toolName")

    # Capture pending dispatch for agent/task tool invocations (GHCP CLI uses lowercase)
    if tool_name in ("agent", "task"):
        try:
            tool_args = ctx.field("toolArgs")
            prompt = None
            if isinstance(tool_args, dict):
                prompt = tool_args.get("prompt") or None
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
            pass

    event = core.build_event(
        "tool_call_start", ctx,
        call_id=resolve_call_id(ctx),
        tool_name=tool_name,
        tool_input=ctx.field("toolArgs"),
    )
    core.append_event(resolve_destination(ctx), event)


def handle_post_tool_use(ctx: "core.HookContext") -> None:
    """Emit tool_call_end with status 'success'.
    Reads tool output from nested payload path: ctx.payload["toolResult"]["textResultForLlm"].
    This nested access bypasses ctx.field() and applies its own None-normalization.
    Never raises."""
    # Nested access for toolResult.textResultForLlm (only event requiring nested access)
    tool_result = ctx.payload.get("toolResult")
    if isinstance(tool_result, dict):
        text = tool_result.get("textResultForLlm")
        # normalize: None, empty string -> omit from event
        if text == "" or text is None:
            text = None
    else:
        text = None

    event = core.build_event(
        "tool_call_end", ctx,
        call_id=resolve_call_id(ctx),
        status="success",
        tool_output=text,
    )
    core.append_event(resolve_destination(ctx), event)


def handle_post_tool_use_failure(ctx: "core.HookContext") -> None:
    """Emit tool_call_end with status 'error'. Never raises."""
    error = ctx.field("error")
    event = core.build_event(
        "tool_call_end", ctx,
        call_id=resolve_call_id(ctx),
        status="error",
        error=error,
    )
    core.append_event(resolve_destination(ctx), event)


HANDLERS: dict = {
    "preToolUse": handle_pre_tool_use,
    "postToolUse": handle_post_tool_use,
    "postToolUseFailure": handle_post_tool_use_failure,
}
