"""mosaic_logger_handlers_invocation.py -- Invocation-scoped event handlers.

Handles: subagentStart, subagentStop. Writes invocation_start, invocation_end,
boundary turn events, artifact files, and transcript exports for each subagent.
"""

import re

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_export as export
import mosaic_logger_transcript as transcript
import mosaic_logger_artifacts as artifacts


# ---------------------------------------------------------------------------
# Closed enum for status_code
# ---------------------------------------------------------------------------

_VALID_STATUS_CODES = frozenset({
    "SUCCESS",
    "COMPLETED_NEEDS_ACTION",
    "PARTIALLY_DONE",
    "NEEDS_CLARIFICATION",
    "CAPABILITY_EXCEEDED",
    "BLOCKED",
})

# Match a structured status_code key followed by a quoted enum value.
_STATUS_CODE_RE = re.compile(
    r'(?<![A-Za-z0-9_])status_code\s*["\']?\s*[:=]\s*["\']([A-Z_]+)["\']'
)


# ---------------------------------------------------------------------------
# Public helpers
# ---------------------------------------------------------------------------

def extract_status_code(message: "str | None") -> "str | None":
    """Extract MOSAIC status code from subagent's final response.
    Conservative: only structured key-value with quoted value from closed enum.
    Last occurrence wins. Returns None on ambiguity. Never raises."""
    try:
        if not message:
            return None

        matches = _STATUS_CODE_RE.findall(message)
        if not matches:
            return None

        last_value = matches[-1]
        if last_value in _VALID_STATUS_CODES:
            return last_value
        return None

    except Exception:
        return None


# ---------------------------------------------------------------------------
# Handlers
# ---------------------------------------------------------------------------

def handle_subagent_start(ctx: "core.HookContext") -> None:
    """Handle subagentStart: pop pending dispatch, register agent mapping,
    emit invocation_start and input turn, write 01_input.md.
    Reads agentName (camelCase), agentId (camelCase) from payload.
    Uses ctx.agent_id (mapped from payload 'agentId' by HookContext).
    Never raises."""
    if not ctx.agent_id:
        return

    # 1. Pop pending dispatch (before path resolution)
    dispatch = runstate.pop_pending_dispatch(
        ctx.paths, core.effective_run_id(ctx), ctx.session_id
    )

    # 2. Optionally set ctx.run_id from the dispatch's run_id when not yet set
    if dispatch and dispatch.get("run_id") and not ctx.run_id:
        ctx.run_id = dispatch["run_id"]

    # 3. Compute effective run_id for all downstream path routing
    run_id = core.effective_run_id(ctx)

    # 4. Resolve agent_prompt: prefer dispatch prompt; fall back to payload agentPrompt
    dispatch_prompt = dispatch.get("prompt") if dispatch else None
    agent_prompt = dispatch_prompt if dispatch_prompt is not None else ctx.field("agentPrompt")

    # 5. Resolve agent_instance_id: pending dispatch takes priority; fall back to
    #    extracting from agentPrompt; final fallback is 'unknown-agent'
    if dispatch:
        agent_instance_id = dispatch["agent_instance_id"]
    else:
        extracted = runstate.extract_instance_id(agent_prompt)
        agent_instance_id = extracted if extracted else "unknown-agent"
    agent_type = ctx.agent_type

    # 6. Persist mapping FIRST -- before any event write
    runstate.put_agent_mapping(
        ctx.paths, run_id, ctx.agent_id, agent_instance_id, agent_type
    )
    runstate.put_agent_run(ctx.paths, ctx.agent_id, run_id)

    sink = ctx.paths.invocation_events(run_id, agent_instance_id)

    # 7. Emit invocation_start
    event = core.build_event(
        "invocation_start", ctx,
        agent_instance_id=agent_instance_id,
        agent_type=agent_type,
        prompt=agent_prompt,
    )
    core.append_event(sink, event)

    # 8. Emit initial user turn (only when agent_prompt is present)
    if agent_prompt is not None:
        turn = core.build_event("turn", ctx, role="user", content=agent_prompt)
        core.append_event(sink, turn)

    # 9. Write 01_input.md
    input_text = artifacts.render_input(ctx, agent_instance_id, agent_prompt)
    artifacts.write_artifact(ctx.paths.invocation_input(run_id, agent_instance_id), input_text)

    # 10. Refresh orchestrator raw transcript (best-effort)
    export.export_transcript(
        ctx.transcript_path,
        ctx.paths.orchestrator_raw(run_id, ctx.session_id),
        "transcript_path",
    )


def handle_subagent_stop(ctx: "core.HookContext") -> None:
    """Handle subagentStop: emit invocation_end with status_code extraction,
    write 02_output.md, export agent transcript.
    Reads response (camelCase), agentId (camelCase), transcriptPath from payload.
    Guards against unmapped SubagentStop (no-op for unmapped_* invocation IDs).
    Never raises."""
    if not ctx.agent_id:
        return

    run_id = core.effective_run_id(ctx)

    # 1. Resolve agent_instance_id via mapping
    agent_instance_id = runstate.resolve_invocation_id(
        ctx.paths, run_id, ctx.agent_id
    )

    # Guard: skip unmapped subagentStop firings
    if agent_instance_id.startswith("unmapped_"):
        return

    sink = ctx.paths.invocation_events(run_id, agent_instance_id)

    # 2. Read subagent transcript facts
    subagent_transcript_path = ctx.field("transcriptPath")
    facts = transcript.read_last_assistant_facts(subagent_transcript_path)

    # GHCP CLI uses 'response' field (not 'last_assistant_message')
    response = ctx.field("response")
    status_code = extract_status_code(response)

    # 3. Emit invocation_end
    event = core.build_event(
        "invocation_end", ctx,
        agent_instance_id=agent_instance_id,
        status_code=status_code,
        response=response,
        model=facts.model,
        token_usage=facts.token_usage,
    )
    core.append_event(sink, event)

    # 4. Emit final assistant turn (only when response is present)
    if response is not None:
        turn = core.build_event(
            "turn", ctx,
            role="assistant",
            content=response,
            model=facts.model,
            token_usage=facts.token_usage,
        )
        core.append_event(sink, turn)

    # 5. Write 02_output.md
    output_text = artifacts.render_output(ctx, agent_instance_id, response, status_code, facts)
    artifacts.write_artifact(ctx.paths.invocation_output(run_id, agent_instance_id), output_text)

    # 6. Export subagent transcript
    export.export_transcript(
        subagent_transcript_path,
        ctx.paths.invocation_raw(run_id, agent_instance_id),
        "transcriptPath",
    )

    # 7. Refresh orchestrator raw transcript
    export.export_transcript(
        ctx.transcript_path,
        ctx.paths.orchestrator_raw(run_id, ctx.session_id),
        "transcript_path",
    )


HANDLERS: dict = {
    "subagentStart": handle_subagent_start,
    "subagentStop": handle_subagent_stop,
}
