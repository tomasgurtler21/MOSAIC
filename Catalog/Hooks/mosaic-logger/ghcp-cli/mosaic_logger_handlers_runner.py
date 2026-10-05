"""mosaic_logger_handlers_runner.py -- Handlers for Runner-mode sessions.

In Runner mode the primary GHCP CLI session is the whole unit of work, so the
run lifecycle (run_start/run_end) is owned by the Runner and never written here.

Subagent role (MOSAIC_ROLE=subagent): the whole session is ONE invocation.
  userPromptSubmitted  invocation_start, initial user turn, 01_input.md
  agentStop            completes the invocation (see below)
  sessionEnd           completes the invocation when agentStop did not
  sessionStart         ignored: GHCP CLI fires userPromptSubmitted first, and
                       the prompt belongs on invocation_start
Completion writes invocation_end, the final assistant turn, 02_output.md and
04_session.raw (+ sidecar). agentStop carries no assistant text, so response
and status_code are derived from the session transcript (the last assistant
text); model and token_usage come from the last assistant record. Whatever the
transcript cannot honestly provide is omitted.
Tool events are routed to the invocation stream by the tools handler. State
that must survive between the separate hook processes lives in the
dot-prefixed '.runner-session' directory of the run, keyed by instance id AND
session id so a resumed invocation is never blocked by a previous session's
claims. invocation_start and invocation_end are guarded by atomic
exclusive-create claims, so repeated or out-of-order firings write each once.

Orchestrator role (MOSAIC_ROLE=orchestrator): the native primary-session
handlers apply unchanged except that run_start/run_end are suppressed by the
dispatcher and the orchestrator transcript uses the session-scoped name.

Known Runner-mode field gaps (what native mode produces that Runner mode cannot):
  - invocation_start/invocation_end carry no harness agent_id, and no
    .agent-map entry is written (the primary session payload has none).
  - agent_type is derived from MOSAIC_AGENT_INSTANCE_ID (text before the last
    '#'), omitted when that is empty; the harness agentType is not available.
  - response and status_code come from the transcript, not a hook payload;
    they are omitted when the transcript is missing, unreadable or holds no
    assistant text. model and token_usage are omitted likewise.
  - 04_session.raw is not written when no transcript file can be read.
  - No session_start/session_end events are written in the invocation stream;
    sessionStart writes nothing, so a session without any prompt gets its
    invocation_start (without prompt) at completion.
  - Notification, preCompact, errorOccurred, permissionRequest,
    userPromptTransformed, subagentStart and subagentStop firings write nothing
    in the subagent role (no native invocation-stream counterpart).
  - No usage_record events (native GHCP CLI emits none) and no
    00_orchestrator_session.raw export for a subagent-role run; 04_session.raw
    holds the primary session transcript.
  - A resumed invocation (same instance id, new session id) appends to the same
    03_events.jsonl but overwrites 01_input.md, 02_output.md and 04_session.raw.
"""

import os

import mosaic_logger_core as core
import mosaic_logger_runner_mode as runner_mode
import mosaic_logger_handlers_invocation as invocation
import mosaic_logger_artifacts as artifacts
import mosaic_logger_export as export
import mosaic_logger_transcript as transcript


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
    duplicate is preferred to a lost event).
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
    mode = ctx.runner_mode
    if mode is None or _is_claimed(ctx, mode, "completed"):
        return
    _ensure_invocation_start(ctx, mode, ctx.field("prompt"))


def complete_invocation(ctx: "core.HookContext") -> None:
    """Write invocation_end and the output artifacts exactly once."""
    mode = ctx.runner_mode
    if mode is None or not _claim(ctx, mode, "completed"):
        return

    _ensure_invocation_start(ctx, mode, None)

    run_id = core.effective_run_id(ctx)
    instance_id = mode.agent_instance_id
    sink = ctx.paths.invocation_events(run_id, instance_id)
    path = ctx.transcript_path if isinstance(ctx.transcript_path, str) else None

    response = transcript.read_last_assistant_text(path)
    facts = transcript.read_last_assistant_facts(path)
    status_code = invocation.extract_status_code(response)

    core.append_event(sink, core.build_event(
        "invocation_end", ctx,
        agent_instance_id=instance_id,
        status_code=status_code,
        response=response,
        model=facts.model,
        token_usage=facts.token_usage,
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
        artifacts.render_output(ctx, instance_id, response, status_code, facts),
    )
    export.export_transcript(
        path, ctx.paths.invocation_raw(run_id, instance_id), "transcriptPath")


def ignore_event(ctx: "core.HookContext") -> None:
    """Primary-session events with no native invocation counterpart."""


SUBAGENT_HANDLERS = {
    "sessionStart": ignore_event,
    "userPromptSubmitted": handle_subagent_role_prompt,
    "agentStop": complete_invocation,
    "sessionEnd": complete_invocation,
}

_IGNORED_SUBAGENT_EVENTS = (
    "notification", "preCompact", "userPromptTransformed", "permissionRequest",
    "errorOccurred", "subagentStart", "subagentStop",
)


def handlers_for(mode) -> dict:
    """Handler overrides for a Runner mode (empty for native and orchestrator)."""
    if mode is not None and mode.role == runner_mode.ROLE_SUBAGENT:
        overrides = {event: ignore_event for event in _IGNORED_SUBAGENT_EVENTS}
        overrides.update(SUBAGENT_HANDLERS)
        return overrides
    return {}
