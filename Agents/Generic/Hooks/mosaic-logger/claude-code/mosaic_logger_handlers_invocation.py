"""mosaic_logger_handlers_invocation.py — Invocation-scoped event handlers.

Handles: SubagentStart, SubagentStop. Writes invocation_start, invocation_end,
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
# Uses a quoted value only — free prose like "SUCCESS" in plain text won't match.
_STATUS_CODE_RE = re.compile(
    r'(?<![A-Za-z0-9_])status_code\s*["\']?\s*[:=]\s*["\']([A-Z_]+)["\']'
)


# ---------------------------------------------------------------------------
# Public helpers
# ---------------------------------------------------------------------------

def extract_status_code(message: "str | None") -> "str | None":
    """Extract the MOSAIC status code from a subagent's final response.

    Matching is conservative: only a structured key-value occurrence (status_code
    key followed by a quoted value from the closed enum) is accepted. Free prose
    mentioning a status name is not a match. When multiple structured occurrences
    exist, the LAST one wins. Returns None on any ambiguity, any value outside the
    closed enum, or any missing/empty/None input. Never raises.
    """
    try:
        if not message:
            return None

        matches = _STATUS_CODE_RE.findall(message)
        if not matches:
            return None

        # Last occurrence wins (status block is at the end of a protocol response)
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
    """Handle SubagentStart: register mapping, emit invocation_start and input turn,
    write 01_input.md, and refresh 00_orchestrator_session.raw."""
    if not ctx.run_id or not ctx.agent_id:
        return

    agent_prompt = ctx.field("agent_prompt")
    agent_type = ctx.agent_type

    # 1. Resolve agent_instance_id
    extracted = runstate.extract_instance_id(agent_prompt)
    agent_instance_id = extracted or runstate.fallback_instance_id(agent_type)

    # 2. Persist mapping FIRST — before any event write
    runstate.put_agent_mapping(
        ctx.paths, ctx.run_id, ctx.agent_id, agent_instance_id, agent_type
    )

    sink = ctx.paths.invocation_events(ctx.run_id, agent_instance_id)

    # 3. Emit invocation_start
    event = core.build_event(
        "invocation_start", ctx,
        agent_instance_id=agent_instance_id,
        agent_type=agent_type,
        prompt=agent_prompt,
    )
    core.append_event(sink, event)

    # 4. Emit initial user turn (only when agent_prompt is present)
    if agent_prompt is not None:
        turn = core.build_event("turn", ctx, role="user", content=agent_prompt)
        core.append_event(sink, turn)

    # 5. Write 01_input.md
    input_text = artifacts.render_input(ctx, agent_instance_id, agent_prompt)
    artifacts.write_artifact(ctx.paths.invocation_input(ctx.run_id, agent_instance_id), input_text)

    # 6. Refresh 00_orchestrator_session.raw from the orchestrator transcript
    export.export_transcript(
        ctx.transcript_path,
        ctx.paths.orchestrator_raw(ctx.run_id),
        "transcript_path",
    )


def handle_subagent_stop(ctx: "core.HookContext") -> None:
    """Handle SubagentStop: emit invocation_end and final assistant turn, write
    02_output.md, export agent transcript, and refresh 00_orchestrator_session.raw."""
    if not ctx.run_id or not ctx.agent_id:
        return

    # 1. Resolve agent_instance_id via mapping (so start and end agree on folder)
    agent_instance_id = runstate.resolve_invocation_id(
        ctx.paths, ctx.run_id, ctx.agent_id
    )
    sink = ctx.paths.invocation_events(ctx.run_id, agent_instance_id)

    # 2. Read transcript facts once; reuse for both invocation_end and final turn
    agent_transcript_path = ctx.field("agent_transcript_path")
    facts = transcript.read_last_assistant_facts(agent_transcript_path)

    last_msg = ctx.field("last_assistant_message")
    status_code = extract_status_code(last_msg)

    # 3. Emit invocation_end
    event = core.build_event(
        "invocation_end", ctx,
        agent_instance_id=agent_instance_id,
        status_code=status_code,
        response=last_msg,
        model=facts.model,
        token_usage=facts.token_usage,
    )
    core.append_event(sink, event)

    # 4. Emit final assistant turn (only when last_assistant_message is present)
    if last_msg is not None:
        turn = core.build_event(
            "turn", ctx,
            role="assistant",
            content=last_msg,
            model=facts.model,
            token_usage=facts.token_usage,
        )
        core.append_event(sink, turn)

    # 5. Write 02_output.md
    output_text = artifacts.render_output(ctx, agent_instance_id, last_msg, status_code, facts)
    artifacts.write_artifact(ctx.paths.invocation_output(ctx.run_id, agent_instance_id), output_text)

    # 6. Export agent transcript to 04_session.raw + 04_session.meta.json
    export.export_transcript(
        agent_transcript_path,
        ctx.paths.invocation_raw(ctx.run_id, agent_instance_id),
        "agent_transcript_path",
    )

    # 7. Refresh 00_orchestrator_session.raw from the orchestrator's transcript
    export.export_transcript(
        ctx.transcript_path,
        ctx.paths.orchestrator_raw(ctx.run_id),
        "transcript_path",
    )


HANDLERS = {
    "SubagentStart": handle_subagent_start,
    "SubagentStop": handle_subagent_stop,
}
