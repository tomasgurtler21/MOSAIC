"""mosaic_logger_runner_mode.py — Runner mode selection from the environment.

The adapter is in Runner mode only when MOSAIC_ROLE is set (non-empty) in the
environment of the hook process; mode is never inferred from prompt content.
Without MOSAIC_ROLE the adapter is in native mode and behaves as it always has.

Environment contract (set by the Runner):
  MOSAIC_ROLE               'subagent' or 'orchestrator'
  MOSAIC_RUN_ID             the run identity
  MOSAIC_AGENT_INSTANCE_ID  the invocation identity (subagent role only)

A subagent role without an instance id cannot name an invocation folder, and an
unrecognised role value is unknown territory; both degrade to the orchestrator
role, which needs no instance identity.
"""

import collections
import os

import mosaic_logger_runstate as runstate


ROLE_SUBAGENT = "subagent"
ROLE_ORCHESTRATOR = "orchestrator"

RunnerMode = collections.namedtuple(
    "RunnerMode", ["role", "run_id", "agent_instance_id", "agent_type"]
)


def _agent_type_of(instance_id: str) -> "str | None":
    """The part of an instance id before its last '#', or None when empty."""
    head = instance_id.rpartition("#")[0]
    return head or None


def read_runner_mode(env: "dict | None" = None) -> "RunnerMode | None":
    """Return the RunnerMode described by env (default os.environ), or None
    for native mode. run_id is None when MOSAIC_RUN_ID is absent or not a
    valid run id. Never raises."""
    try:
        source = os.environ if env is None else env
        role = source.get("MOSAIC_ROLE")
        if not role:
            return None

        run_id = source.get("MOSAIC_RUN_ID")
        if not runstate.is_valid_run_id(run_id):
            run_id = None

        instance_id = source.get("MOSAIC_AGENT_INSTANCE_ID")
        if role == ROLE_SUBAGENT and instance_id:
            return RunnerMode(ROLE_SUBAGENT, run_id, instance_id,
                              _agent_type_of(instance_id))
        return RunnerMode(ROLE_ORCHESTRATOR, run_id, None, None)
    except Exception:
        return None
