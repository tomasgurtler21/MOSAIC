"""mosaic_logger_handlers_tools.py — Tool-use event handlers.

Handles: PreToolUse, PostToolUse. Routes tool events to the orchestrator stream
or the owning subagent's stream based on agent_id.

Key vscode-ghcp divergences:
  D4: Tool output is nested snake_case: tool_result.text_result_for_llm
      (claude-code reads a flat field; ghcp-cli reads nested camelCase).
      read_tool_output is the single seam isolating this difference.
  AC2.11: VS Code matchers parse but do not filter — ALL filtering on
      subagent-dispatch tool names happens inside the handler body via
      SUBAGENT_DISPATCH_TOOL_NAMES. The registration fragment declares no matcher.
  AC2.2: No PostToolUseFailure handler — the event is unconfirmed for VS Code;
      every completed tool call is recorded with status='success'.

High-frequency path: performs no transcript reads and no directory scanning.
"""

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate


# Single module-top constant for the tool names treated as a subagent dispatch.
# VS Code's matchers do not filter, so the handler body must test tool_name here.
# Extending the set once real payloads confirm the correct names is a one-line edit.
SUBAGENT_DISPATCH_TOOL_NAMES = ("Agent", "Task", "runSubagent")


def resolve_destination(ctx: "core.HookContext"):
    """Route tool event to orchestrator or subagent stream based on agent_id presence.

    VS Code's documented tool payloads carry no agent_id, so all tool calls land
    in the orchestrator stream today. The agent_id branch is retained as dead-but-correct
    code that activates automatically if VS Code adds the field. No session_id-based
    heuristic is used because it would misattribute the orchestrator's own tool calls.
    """
    run_id = core.effective_run_id(ctx)
    if not ctx.agent_id:
        return ctx.paths.orchestrator_events(run_id)
    agent_instance_id = runstate.resolve_invocation_id(
        ctx.paths, run_id, ctx.agent_id
    )
    return ctx.paths.invocation_events(run_id, agent_instance_id)


def resolve_call_id(ctx: "core.HookContext") -> str:
    """Return tool_use_id when present, else a deterministic fallback."""
    native = ctx.field("tool_use_id")
    if native:
        return native
    return runstate.fallback_call_id(ctx.field("tool_name"))


def read_tool_output(ctx: "core.HookContext") -> "str | None":
    """Read tool output from the nested tool_result.text_result_for_llm path.

    D4: VS Code uses nested snake_case (tool_result.text_result_for_llm).
    This function deliberately bypasses ctx.field() (which is flat-only) and
    applies the same absent/empty-string -> None normalization.

    Returns None when tool_result is absent, not a dict, or when
    text_result_for_llm is absent or empty. Never raises.
    """
    try:
        tool_result = ctx.payload.get("tool_result")
        if not isinstance(tool_result, dict):
            return None
        text = tool_result.get("text_result_for_llm")
        if text is None or text == "":
            return None
        return text
    except Exception:
        return None


def handle_pre_tool_use(ctx: "core.HookContext") -> None:
    """Emit tool_call_start. For tool names in SUBAGENT_DISPATCH_TOOL_NAMES,
    also capture a pending dispatch when agent_instance_id is extractable.

    The dispatch capture is best-effort; its failure never suppresses the
    tool_call_start event. Filtering on tool name is done here, not via a
    matcher pattern in the registration fragment (VS Code matchers do not filter).

    Never raises.
    """
    tool_name = ctx.field("tool_name")

    # Capture pending dispatch for subagent-dispatch tool invocations.
    if tool_name in SUBAGENT_DISPATCH_TOOL_NAMES:
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
    """Emit tool_call_end with status='success'.

    Every completed PostToolUse is recorded as success because PostToolUseFailure
    is unconfirmed for VS Code. tool_output comes from the nested path
    tool_result.text_result_for_llm via read_tool_output.

    Never raises.
    """
    event = core.build_event(
        "tool_call_end", ctx,
        call_id=resolve_call_id(ctx),
        status="success",
        tool_output=read_tool_output(ctx),
    )
    core.append_event(resolve_destination(ctx), event)


# ---------------------------------------------------------------------------
# Handler registry
# ---------------------------------------------------------------------------

HANDLERS = {
    "PreToolUse":  handle_pre_tool_use,
    "PostToolUse": handle_post_tool_use,
}
