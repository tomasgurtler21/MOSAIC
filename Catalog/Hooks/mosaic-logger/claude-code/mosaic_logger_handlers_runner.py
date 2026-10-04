"""mosaic_logger_handlers_runner.py — Handlers for Runner-mode sessions.

In Runner mode the primary Claude Code session is the whole unit of work, so the
run lifecycle (run_start/run_end) is owned by the Runner and never written here.

Subagent role (MOSAIC_ROLE=subagent): the whole session is ONE invocation.
  UserPromptSubmit  invocation_start, initial user turn, 01_input.md
  Stop              remembers the response (last wins); the transcript is still
                    being written, so nothing transcript-dependent happens here
  SessionEnd        invocation_end, final assistant turn, 02_output.md, usage
                    records and 04_session.raw (+ sidecar): the transcript is
                    final by now
Tool events are routed to the invocation stream by the tools handler. State that
must survive between the separate hook processes lives in the dot-prefixed
'.runner-session' directory of the run (LogAnalyzer skips dot-prefixed entries),
keyed by instance id AND session id so a resumed invocation is never blocked by
a previous session's claims. invocation_start and invocation_end are guarded by
atomic exclusive-create claims, so repeated or out-of-order firings write each
exactly once.

Orchestrator role (MOSAIC_ROLE=orchestrator): native primary-session handlers
write to 00_orchestrator_events.jsonl; SessionEnd additionally exports the final
transcript and its usage records.

Known Runner-mode field gaps (what native mode produces that Runner mode cannot):
  - invocation_start/invocation_end carry no harness agent_id, and no
    .agent-map entry is written (the primary session payload has none).
  - agent_type is derived from MOSAIC_AGENT_INSTANCE_ID (text before the last
    '#'), omitted when that is empty; the harness agent_type is not available.
  - invocation_end completion_source is 'session_end' (native: 'handback' or
    absent); attribution is never set.
  - No session_start/session_end events are written in the invocation stream.
  - Notification, PreCompact and PostCompact firings write nothing in the
    subagent role (no native invocation-stream counterpart).
  - The orchestrator-transcript usage_record stream and the
    00_orchestrator_session.raw export do not exist for a subagent-role run;
    04_session.raw holds the primary session transcript.
  - A resumed invocation (same instance id, new session id) appends to the same
    03_events.jsonl but overwrites 01_input.md, 02_output.md and 04_session.raw.
"""

import json
import os

import mosaic_logger_core as core
import mosaic_logger_runner_mode as runner_mode
import mosaic_logger_handlers_invocation as invocation
import mosaic_logger_handlers_session as session
import mosaic_logger_artifacts as artifacts
import mosaic_logger_export as export
import mosaic_logger_transcript as transcript
import mosaic_logger_usage as usage


COMPLETION_SOURCE_SESSION_END = "session_end"
STATE_DIRNAME = ".runner-session"


# ---------------------------------------------------------------------------
# Per-session state (dot-prefixed, shared between hook processes)
# ---------------------------------------------------------------------------

def _state_path(ctx: "core.HookContext", mode, suffix: str):
    key = (f"{core.sanitize_component(mode.agent_instance_id)}__"
           f"{core.sanitize_component(ctx.session_id or 'no-session')}")
    run_root = ctx.paths.run_root(core.effective_run_id(ctx))
    return run_root / STATE_DIRNAME / f"{key}.{suffix}"


def _claim(ctx: "core.HookContext", mode, suffix: str) -> bool:
    """Atomically create the marker; True when this firing owns it.

    When the marker cannot be attempted the firing proceeds (a possible
    duplicate is preferred to a lost event), matching the completion claim.
    """
    try:
        path = _state_path(ctx, mode, suffix)
        path.parent.mkdir(parents=True, exist_ok=True)
        os.close(os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY))
        return True
    except FileExistsError:
        return False
    except Exception as exc:
        core.debug_log(f"runner-session: claim {suffix!r} failed", exc)
        return True


def _is_claimed(ctx: "core.HookContext", mode, suffix: str) -> bool:
    try:
        return _state_path(ctx, mode, suffix).exists()
    except Exception:
        return False


def _remember_response(ctx: "core.HookContext", mode, response: str) -> None:
    core.atomic_replace_text(
        _state_path(ctx, mode, "response"),
        json.dumps({"response": response}, ensure_ascii=False),
    )


def _recall_response(ctx: "core.HookContext", mode) -> "str | None":
    try:
        data = json.loads(_state_path(ctx, mode, "response").read_text("utf-8"))
        value = data.get("response")
        return value if isinstance(value, str) else None
    except Exception:
        return None


# ---------------------------------------------------------------------------
# Subagent role
# ---------------------------------------------------------------------------

def _ensure_invocation_start(ctx: "core.HookContext", mode,
                             prompt: "str | None") -> None:
    """Write invocation_start, the initial user turn and 01_input.md once."""
    if not _claim(ctx, mode, "started"):
        return
    run_id = core.effective_run_id(ctx)
    instance_id = mode.agent_instance_id
    sink = ctx.paths.invocation_events(run_id, instance_id)
    core.append_event(sink, core.build_event(
        "invocation_start", ctx,
        agent_instance_id=instance_id,
        agent_type=mode.agent_type,
        prompt=prompt,
    ))
    if prompt is not None:
        core.append_event(sink, core.build_event(
            "turn", ctx, role="user", content=prompt))
    artifacts.write_artifact(
        ctx.paths.invocation_input(run_id, instance_id),
        artifacts.render_input(ctx, instance_id, prompt),
    )


def handle_subagent_role_prompt(ctx: "core.HookContext") -> None:
    mode = runner_mode.read_runner_mode()
    if mode is None or _is_claimed(ctx, mode, "completed"):
        return
    _ensure_invocation_start(ctx, mode, ctx.field("user_prompt"))


def handle_subagent_role_stop(ctx: "core.HookContext") -> None:
    mode = runner_mode.read_runner_mode()
    message = ctx.field("last_assistant_message")
    if mode is None or message is None or _is_claimed(ctx, mode, "completed"):
        return
    _remember_response(ctx, mode, message)


def handle_subagent_role_session_end(ctx: "core.HookContext") -> None:
    mode = runner_mode.read_runner_mode()
    if mode is None or not _claim(ctx, mode, "completed"):
        return

    _ensure_invocation_start(ctx, mode, None)

    run_id = core.effective_run_id(ctx)
    instance_id = mode.agent_instance_id
    sink = ctx.paths.invocation_events(run_id, instance_id)

    response = _recall_response(ctx, mode)
    if response is None:
        response = ctx.field("last_assistant_message")
    facts = _session_facts(ctx.transcript_path)
    status_code = invocation.extract_status_code(response)
    response_format = (invocation.classify_handback_message(response)
                       if response is not None else None)

    core.append_event(sink, core.build_event(
        "invocation_end", ctx,
        agent_instance_id=instance_id,
        status_code=status_code,
        response=response,
        model=facts.model,
        token_usage=facts.token_usage,
        completion_source=COMPLETION_SOURCE_SESSION_END,
        response_format=response_format,
    ))
    if response is not None and response.strip():
        core.append_event(sink, core.build_event(
            "turn", ctx,
            role="assistant",
            content=response,
            model=facts.model,
            token_usage=facts.token_usage,
        ))

    artifacts.write_artifact(
        ctx.paths.invocation_output(run_id, instance_id),
        artifacts.render_output(
            ctx, instance_id, response, status_code, facts,
            completion_source=COMPLETION_SOURCE_SESSION_END,
            response_format=response_format,
        ),
    )

    _emit_session_usage(ctx, run_id, instance_id, sink)
    export.export_transcript(
        ctx.transcript_path,
        ctx.paths.invocation_raw(run_id, instance_id),
        "transcript_path",
    )


def _session_facts(transcript_path: "str | None"):
    """Model of the last assistant record and token usage summed over every
    assistant record: the session is one invocation, so its usage is the whole
    transcript's (a record seen twice counts once, latest values winning)."""
    facts = transcript.read_last_assistant_facts(transcript_path)
    latest = {}
    for record in transcript.read_assistant_records(transcript_path):
        latest[record.record_id] = record.token_usage or {}
    totals = {}
    for usage_block in latest.values():
        for key, value in usage_block.items():
            totals[key] = totals.get(key, 0) + value
    return transcript.TurnFacts(facts.model, totals or None)


def _emit_session_usage(ctx: "core.HookContext", run_id: str,
                        instance_id: str, sink) -> None:
    """Emit this session's usage records, even when a resumed session's
    transcript repeats records an earlier session already emitted."""
    try:
        ctx.paths.usage_state_entry(run_id, usage.stream_key_for(sink)).unlink()
    except FileNotFoundError:
        pass
    except Exception as exc:
        core.debug_log("runner-session: usage state reset failed", exc)
    usage.emit_usage_records(
        ctx, ctx.transcript_path, instance_id, "agent_transcript")


def ignore_event(ctx: "core.HookContext") -> None:
    """Primary-session events with no native invocation counterpart."""


SUBAGENT_ROLE_HANDLERS = {
    "SessionStart": ignore_event,
    "UserPromptSubmit": handle_subagent_role_prompt,
    "Stop": handle_subagent_role_stop,
    "SessionEnd": handle_subagent_role_session_end,
    "Notification": ignore_event,
    "PreCompact": ignore_event,
    "PostCompact": ignore_event,
}


# ---------------------------------------------------------------------------
# Orchestrator role
# ---------------------------------------------------------------------------

def handle_orchestrator_role_session_end(ctx: "core.HookContext") -> None:
    """Native session_end, then the final transcript export and usage records.

    The transcript is final at SessionEnd and may have grown after the last
    Stop, so the session-scoped export and the usage records are refreshed here.
    """
    try:
        session.handle_session_end(ctx)
    finally:
        run_id = core.effective_run_id(ctx)
        usage.emit_usage_records(
            ctx, ctx.transcript_path, None, "orchestrator_transcript")
        export.export_transcript(
            ctx.transcript_path,
            ctx.paths.orchestrator_raw(run_id, ctx.session_id),
            "transcript_path",
        )


ORCHESTRATOR_ROLE_HANDLERS = {
    "SessionEnd": handle_orchestrator_role_session_end,
}


def handlers_for(mode) -> dict:
    """Handler overrides for a Runner mode (empty for native mode)."""
    if mode is None:
        return {}
    if mode.role == runner_mode.ROLE_SUBAGENT:
        return SUBAGENT_ROLE_HANDLERS
    return ORCHESTRATOR_ROLE_HANDLERS
