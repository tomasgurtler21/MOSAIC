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
import mosaic_logger_usage as usage


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
    """Handle SubagentStart: register mapping, emit invocation_start and
    input turn, write 01_input.md, and refresh 00_orchestrator_session.raw.

    Agent instance ID is sourced from the pending-dispatch queue (populated
    by handle_pre_tool_use when tool_name was 'Agent') rather than from
    ctx.field('agent_prompt') (which is never populated in practice).
    Falls back to 'unknown-agent' when no pending dispatch is available.

    If the pending dispatch also carried a run_id and ctx.run_id is not
    yet set, adopts it via runstate.adopt_run_id so ctx.run_id and the
    session-run binding cannot disagree.

    Never raises.
    """
    if not ctx.agent_id:
        return

    # 1. Pop pending dispatch (after guard, before path resolution). The
    #    queue path no longer carries a run_id component (Design D4), so
    #    this always addresses the same file the writer used regardless of
    #    what either firing resolved for run_id.
    dispatch = runstate.pop_pending_dispatch(ctx.paths, ctx.session_id)

    # 2. Optionally adopt ctx.run_id from the dispatch's run_id when not yet set.
    if dispatch and dispatch.get("run_id") and not ctx.run_id:
        runstate.adopt_run_id(ctx, dispatch.get("run_id"))

    # 3. Compute effective run_id for all downstream path routing.
    run_id = core.effective_run_id(ctx)

    # 4. Resolve agent_prompt: prefer the prompt from the pending dispatch;
    #    fall back to ctx.field("agent_prompt") for backward compatibility with
    #    callers that supply the prompt directly in the payload without a dispatch.
    dispatch_prompt = dispatch.get("prompt") if dispatch else None
    agent_prompt = dispatch_prompt if dispatch_prompt is not None else ctx.field("agent_prompt")

    # 5. Resolve agent_instance_id: pending dispatch takes priority; fall back
    #    to extracting from agent_prompt (backward-compatible path for callers
    #    that supply the prompt directly without a pending dispatch); final
    #    fallback is 'unknown-agent'.
    if dispatch:
        agent_instance_id = dispatch["agent_instance_id"]
    else:
        extracted = runstate.extract_instance_id(agent_prompt)
        agent_instance_id = extracted if extracted else "unknown-agent"
    agent_type = ctx.agent_type

    # 6. Persist mapping FIRST — before any event write
    runstate.put_agent_mapping(
        ctx.paths, run_id, ctx.agent_id, agent_instance_id, agent_type
    )

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

    # 10. Emit raw usage_record events from the orchestrator transcript
    #     (ctx.transcript_path, NOT the subagent's) to the orchestrator
    #     stream. SubagentStart has orchestrator-transcript access and is
    #     one of the every-transcript-bearing-firing capture points.
    usage.emit_usage_records(
        ctx, ctx.transcript_path, None, "orchestrator_transcript"
    )

    # 6. Refresh 00_orchestrator_session.raw from the orchestrator transcript
    export.export_transcript(
        ctx.transcript_path,
        ctx.paths.orchestrator_raw(run_id),
        "transcript_path",
    )


def classify_unmapped_stop(ctx: "core.HookContext") -> str:
    """Classify a SubagentStop firing whose agent mapping did not resolve.

    Returns 'spurious' ONLY when the firing carries neither an
    agent_transcript_path nor a last_assistant_message -- a shape consistent
    with Claude Code's internal activity-narration snapshots and with no
    genuine invocation behind it.

    Returns 'genuine' in every other case. The classification is
    deliberately biased toward recording: ambiguity resolves to 'genuine'
    and is quarantined rather than discarded, per the data-integrity
    requirement.

    Never raises.
    """
    try:
        if ctx.field("agent_transcript_path") or ctx.field("last_assistant_message"):
            return "genuine"
        return "spurious"
    except Exception:
        return "genuine"


def handle_subagent_stop(ctx: "core.HookContext") -> None:
    """Handle SubagentStop: emit invocation_end and final assistant turn,
    write 02_output.md, export agent transcript, and refresh
    00_orchestrator_session.raw.

    Three-way outcome based on agent-map resolution:
      - Mapped: unchanged behaviour, writing to the ordinary invocation
        directory.
      - Unmapped and classified 'genuine': the same event, artifact, and
        export are still written, but to the dot-prefixed quarantine
        destination, with attribution='quarantined' added to invocation_end.
        Emits a 'subagent-stop: quarantined' diagnostic.
      - Unmapped and classified 'spurious': nothing is written, no directory
        is created. Emits a 'subagent-stop: discarded' diagnostic.

    Never raises.
    """
    if not ctx.agent_id:
        return

    run_id = core.effective_run_id(ctx)

    # 1. Resolve agent_instance_id via mapping (so start and end agree on folder)
    agent_instance_id, mapped = runstate.resolve_invocation(
        ctx.paths, run_id, ctx.agent_id
    )

    quarantined = False
    if not mapped:
        outcome = classify_unmapped_stop(ctx)
        if outcome == "spurious":
            core.debug_log(
                f"subagent-stop: discarded for run_id={run_id!r} "
                f"agent_id={ctx.agent_id!r}"
            )
            return
        quarantined = True

    # 2. Resolve the events/output/raw destinations for this outcome.
    if quarantined:
        events_path = ctx.paths.quarantine_events(run_id, ctx.agent_id)
        output_path = ctx.paths.quarantine_output(run_id, ctx.agent_id)
        raw_path = ctx.paths.quarantine_raw(run_id, ctx.agent_id)
    else:
        events_path = ctx.paths.invocation_events(run_id, agent_instance_id)
        output_path = ctx.paths.invocation_output(run_id, agent_instance_id)
        raw_path = ctx.paths.invocation_raw(run_id, agent_instance_id)

    sink = events_path

    # 3. Read transcript facts once; reuse for both invocation_end and final turn
    agent_transcript_path = ctx.field("agent_transcript_path")
    facts = transcript.read_last_assistant_facts(agent_transcript_path)

    last_msg = ctx.field("last_assistant_message")
    status_code = extract_status_code(last_msg)

    # 4. Emit invocation_end
    event = core.build_event(
        "invocation_end", ctx,
        agent_instance_id=agent_instance_id,
        status_code=status_code,
        response=last_msg,
        model=facts.model,
        token_usage=facts.token_usage,
        attribution="quarantined" if quarantined else None,
    )
    core.append_event(sink, event)

    # 5. Emit final assistant turn (only when last_assistant_message is present)
    if last_msg is not None:
        turn = core.build_event(
            "turn", ctx,
            role="assistant",
            content=last_msg,
            model=facts.model,
            token_usage=facts.token_usage,
        )
        core.append_event(sink, turn)

    # 6. Write 02_output.md
    output_text = artifacts.render_output(ctx, agent_instance_id, last_msg, status_code, facts)
    artifacts.write_artifact(output_path, output_text)

    # 6b. Emit raw usage_record events from both transcripts this firing has
    #     access to, each routed to exactly one stream:
    #       - the agent transcript -> this invocation's own destination
    #         (events_path, which is the quarantine events file when
    #         quarantined, so the one-stream-per-record property holds even
    #         then)
    #       - the orchestrator transcript -> always the orchestrator stream,
    #         regardless of quarantine status
    usage.emit_usage_records(
        ctx, agent_transcript_path, agent_instance_id, "agent_transcript",
        sink_override=events_path,
    )
    usage.emit_usage_records(
        ctx, ctx.transcript_path, None, "orchestrator_transcript"
    )

    # 7. Export agent transcript to 04_session.raw + 04_session.meta.json
    export.export_transcript(
        agent_transcript_path,
        raw_path,
        "agent_transcript_path",
    )

    # 8. Refresh 00_orchestrator_session.raw from the orchestrator's transcript
    export.export_transcript(
        ctx.transcript_path,
        ctx.paths.orchestrator_raw(run_id),
        "transcript_path",
    )

    # 9. Emit the quarantine diagnostic last, once the write is complete.
    if quarantined:
        core.debug_log(
            f"subagent-stop: quarantined for run_id={run_id!r} "
            f"agent_id={ctx.agent_id!r}"
        )


HANDLERS = {
    "SubagentStart": handle_subagent_start,
    "SubagentStop": handle_subagent_stop,
}
