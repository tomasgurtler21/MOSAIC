"""mosaic_logger_handlers_invocation.py — Invocation-scoped event handlers.

Handles: SubagentStart, SubagentStop. Writes invocation_start, invocation_end,
boundary turn events, artifact files, and transcript exports for each subagent.

Key vscode-ghcp divergence (D3):
- Subagent correlation is keyed on agent_name, not agent_id.
- SubagentStart carries no agent_id in VS Code — the reference adapters'
  `if not ctx.agent_id: return` guard would suppress every invocation and
  must NOT be used here.
- resolve_agent_key is the single seam: both handlers route through it so
  start and end cannot disagree about which key was used.
- put_agent_mapping and get_agent_mapping use agent_name as the key.
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

# Match a structured status_code key followed by an enum value (quoted or unquoted).
# Matches both JSON style ("status_code": "SUCCESS") and YAML style (status_code: SUCCESS).
# Free prose mentioning a status name without the key prefix still won't match.
_STATUS_CODE_RE = re.compile(
    r'(?<![A-Za-z0-9_])status_code\s*["\']?\s*[:=]\s*["\']?([A-Z_]+)["\']?'
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

        last_value = matches[-1]
        if last_value in _VALID_STATUS_CODES:
            return last_value
        return None

    except Exception:
        return None


def resolve_agent_key(ctx: "core.HookContext") -> "str | None":
    """Return the correlation key for this firing.

    D3: returns agent_name when present (via ctx.field which normalizes empty
    string to None), falls back to agent_id, and returns None when both are
    absent. Both SubagentStart and SubagentStop route through this function so
    start and end cannot disagree about which key was used. A future VS Code
    build that adds agent_id to SubagentStart needs one edit here to switch
    precedence.
    """
    agent_name = ctx.field("agent_name")
    if agent_name is not None:
        return agent_name
    return ctx.agent_id  # may be None


def resolve_agent_type(ctx: "core.HookContext") -> "str | None":
    """Return the agent type, falling back to agent_name when agent_type is absent.

    The schema defines agent_type as the bare agent name; VS Code's agent_name
    carries exactly that value. Without this fallback, invocation_start would
    systematically omit a field that invocation_end populates for the same
    invocation. This is a semantic mapping between equivalent fields, not a
    fabricated value.
    """
    return ctx.agent_type or ctx.field("agent_name")


# ---------------------------------------------------------------------------
# Handlers
# ---------------------------------------------------------------------------

def handle_subagent_start(ctx: "core.HookContext") -> None:
    """Handle SubagentStart: persist mapping, emit invocation_start and user turn,
    write 01_input.md, and refresh 00_orchestrator_session.raw.

    D3: there is NO agent_id guard. The reference adapters' `if not ctx.agent_id:
    return` would suppress every VS Code SubagentStart (which carries no agent_id)
    and must not be used here. Correlation key is agent_name.

    The agent map entry is persisted BEFORE any event write to minimize the
    correlation window between SubagentStart and the first PreToolUse that could
    follow in a concurrent process.

    Never raises.
    """
    try:
        # Step 1: resolve correlation key; return early only when truly uncorrelatable.
        agent_key = resolve_agent_key(ctx)
        if agent_key is None:
            return

        # Step 2: pop pending dispatch.
        dispatch = runstate.pop_pending_dispatch(
            ctx.paths, core.effective_run_id(ctx), ctx.session_id
        )

        # Step 3: optionally set ctx.run_id from the dispatch when not yet set.
        if dispatch and dispatch.get("run_id") and not ctx.run_id:
            ctx.run_id = dispatch["run_id"]

        # Step 4: compute effective run_id for all downstream routing.
        run_id = core.effective_run_id(ctx)

        # Step 5: resolve agent_prompt and agent_instance_id.
        dispatch_prompt = dispatch.get("prompt") if dispatch else None
        agent_prompt = dispatch_prompt

        if dispatch:
            agent_instance_id = dispatch["agent_instance_id"]
        else:
            extracted = runstate.extract_instance_id(agent_prompt)
            agent_instance_id = extracted if extracted else "unknown-agent"

        # Step 6: persist the agent map entry FIRST — before any event write.
        runstate.put_agent_mapping(
            ctx.paths, run_id, agent_key, agent_instance_id, resolve_agent_type(ctx)
        )

        sink = ctx.paths.invocation_events(run_id, agent_instance_id)

        # Step 7: emit invocation_start.
        event = core.build_event(
            "invocation_start", ctx,
            agent_instance_id=agent_instance_id,
            agent_type=resolve_agent_type(ctx),
            prompt=agent_prompt,
        )
        core.append_event(sink, event)

        # Step 8: emit initial user turn when prompt is available.
        if agent_prompt is not None:
            turn = core.build_event("turn", ctx, role="user", content=agent_prompt)
            core.append_event(sink, turn)

        # Step 9: write 01_input.md.
        input_text = artifacts.render_input(ctx, agent_instance_id, agent_prompt)
        artifacts.write_artifact(
            ctx.paths.invocation_input(run_id, agent_instance_id), input_text
        )

        # Step 10: refresh 00_orchestrator_session.raw (best-effort).
        export.export_transcript(
            ctx.transcript_path,
            ctx.paths.orchestrator_raw(run_id, ctx.session_id),
            "transcript_path",
        )

    except Exception as exc:
        core.debug_log("handle_subagent_start failed", exc)


def handle_subagent_stop(ctx: "core.HookContext") -> None:
    """Handle SubagentStop: emit invocation_end and final assistant turn,
    write 02_output.md, export agent transcript, and refresh
    00_orchestrator_session.raw.

    D3: resolves the invocation by agent_name; falls back to agent_id only when
    agent_name is absent. Skips firings whose resolved agent_instance_id starts
    with 'unmapped_', which indicates no corresponding SubagentStart mapping
    was found — such firings are quarantined rather than written to any stream.

    Never raises.
    """
    try:
        # Step 1: resolve correlation key.
        agent_key = resolve_agent_key(ctx)
        if agent_key is None:
            return

        run_id = core.effective_run_id(ctx)

        # Step 2: resolve agent_instance_id; skip unmapped firings.
        agent_instance_id = runstate.resolve_invocation_id(
            ctx.paths, run_id, agent_key
        )
        if agent_instance_id.startswith("unmapped_"):
            return

        sink = ctx.paths.invocation_events(run_id, agent_instance_id)

        # Step 3: read transcript facts once (best-effort; no per-subagent transcript
        # field is confirmed for VS Code, so ctx.transcript_path is the session path).
        facts = transcript.read_last_assistant_facts(ctx.transcript_path)

        last_msg = ctx.field("last_assistant_message")
        status_code = extract_status_code(last_msg)

        # Step 4: emit invocation_end.
        event = core.build_event(
            "invocation_end", ctx,
            agent_instance_id=agent_instance_id,
            status_code=status_code,
            response=last_msg,
            model=facts.model,
            token_usage=facts.token_usage,
        )
        core.append_event(sink, event)

        # Step 5: emit final assistant turn when last_assistant_message is present.
        if last_msg is not None:
            turn = core.build_event(
                "turn", ctx,
                role="assistant",
                content=last_msg,
                model=facts.model,
                token_usage=facts.token_usage,
            )
            core.append_event(sink, turn)

        # Step 6: write 02_output.md.
        output_text = artifacts.render_output(
            ctx, agent_instance_id, last_msg, status_code, facts
        )
        artifacts.write_artifact(
            ctx.paths.invocation_output(run_id, agent_instance_id), output_text
        )

        # Step 7: export 04_session.raw (no-op; VS Code has no per-subagent transcript).
        export.export_transcript(
            ctx.transcript_path,
            ctx.paths.invocation_raw(run_id, agent_instance_id),
            "transcript_path",
        )

        # Step 8: refresh 00_orchestrator_session.raw.
        export.export_transcript(
            ctx.transcript_path,
            ctx.paths.orchestrator_raw(run_id, ctx.session_id),
            "transcript_path",
        )

    except Exception as exc:
        core.debug_log("handle_subagent_stop failed", exc)


# ---------------------------------------------------------------------------
# Handler registry
# ---------------------------------------------------------------------------

HANDLERS = {
    "SubagentStart": handle_subagent_start,
    "SubagentStop":  handle_subagent_stop,
}
