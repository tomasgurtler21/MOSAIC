"""mosaic_logger_usage.py — Raw per-record usage event emission.

This module's docstring is the single authoritative wire-shape contract
consumed by the Go LogAnalyzer decoder.

usage_record event
-------------------

    {
      "schema_version": "1.1.0",
      "event": "usage_record",
      "timestamp": "2026-08-08T06:27:35.412Z",
      "harness": "claude-code",
      "session_id": "abc-123",
      "run_id": "20260808T062735Z-770f",
      "agent_instance_id": "contracts-designer#13",
      "record_id": "msg_01ABCdef...",
      "record_index": 7,
      "source": "agent_transcript",
      "model": "claude-opus-4-6",
      "service_tier": "standard",
      "token_usage": { }
    }

Field           | Type    | Presence                | Meaning
--------------- | ------- | ------------------------ | -------
record_id       | string  | always                   | Deduplication key. Stable per API call.
record_index    | integer | always                   | 0-based ordinal among assistant records in file order. Diagnostic/ordering aid only.
source          | string  | always                   | 'orchestrator_transcript' or 'agent_transcript'. Forensic only; stream placement is authoritative.
agent_instance_id | string | invocation streams only | Omitted on the orchestrator stream.
model           | string  | when resolvable          | The model of this record.
service_tier    | string  | when present             | Descriptive detail only; drives no pricing decision.
token_usage     | object  | when at least one sub-field resolved | See below.

The envelope (schema_version, event, timestamp, harness, session_id, run_id)
is produced by the unchanged core.build_event, including its pruning rule:
None, empty string, empty dict and empty list are dropped; False and 0 are
kept.

Single-stream invariant. Exactly one usage_record line is written per
observed record per firing, to exactly one events file. This is what allows
the analyzer to attribute every record to exactly one of {orchestrator turn
total, subagent invocation total}.

token_usage block
------------------

Key                    | Source transcript field
----------------------- | ------------------------
input_tokens            | message.usage.input_tokens
output_tokens           | message.usage.output_tokens
cache_read_tokens       | message.usage.cache_read_input_tokens
cache_creation_tokens   | message.usage.cache_creation_input_tokens (flattened; the only cache-write representation on the wire)

These four keys are exactly the four the adapter maps today, unchanged. Each
appears only when its source field is a genuine number, including 0. No
ephemeral cache-write tier fields exist in this block.
"""

import os

import mosaic_logger_core as core
import mosaic_logger_transcript as transcript


USAGE_EVENT = "usage_record"

CAPTURE_MODE_ENV = "MOSAIC_LOGGER_USAGE_CAPTURE"
# Values: "all" (default, any unrecognised value included) -> emit from every
#         firing with transcript access, including tool firings.
#         "boundaries" -> emit only from Stop, SubagentStart, SubagentStop,
#         PreCompact and PostCompact. Widens the compaction window; use only
#         if transcript reads on the high-frequency tool path measurably harm
#         interactive latency.


def tool_capture_enabled() -> bool:
    """True unless CAPTURE_MODE_ENV is set to 'boundaries'. Never raises."""
    try:
        value = os.environ.get(CAPTURE_MODE_ENV, "")
        return value != "boundaries"
    except Exception:
        return True


def emit_usage_records(ctx: "core.HookContext",
                        transcript_path: "str | None",
                        agent_instance_id: "str | None",
                        source: str,
                        sink_override: "object | None" = None) -> int:
    """Emit one usage_record event per assistant record in transcript_path.

    source is 'orchestrator_transcript' or 'agent_transcript' and is carried
    on the event for forensics; STREAM ROUTING is authoritative, not this
    field.

    Every event is appended to exactly ONE sink: sink_override when supplied
    (used by the quarantine branch of handle_subagent_stop, whose events file
    is not the ordinary core.event_sink_for(ctx, agent_instance_id) path),
    otherwise core.event_sink_for(ctx, agent_instance_id). agent_instance_id
    None routes to the orchestrator events file; anything else routes to
    that invocation's events file. A record is never written to two streams.

    Returns the number of events appended. A falsy transcript_path, an
    unreadable transcript, or a transcript with no assistant records
    returns 0. Any failure degrades silently with a diagnostic and NEVER
    prevents the surrounding event from being written.

    Re-emission of an already-emitted record on a later firing is expected
    and permitted; deduplication is the analyzer's responsibility.

    Never raises.
    """
    try:
        if not transcript_path or not isinstance(transcript_path, str):
            return 0

        try:
            records = transcript.read_assistant_records(transcript_path)
        except Exception as exc:
            core.debug_log(
                f"usage-capture: failed transcript_path={transcript_path} "
                f"agent_instance_id={agent_instance_id}",
                exc,
            )
            return 0

        if not records:
            return 0

        sink = sink_override if sink_override is not None else core.event_sink_for(
            ctx, agent_instance_id
        )
        count = 0
        for rec in records:
            try:
                fields = {
                    "record_id": rec.record_id,
                    "record_index": rec.record_index,
                    "source": source,
                    "model": rec.model,
                    "service_tier": rec.service_tier,
                    "token_usage": rec.token_usage,
                }
                if agent_instance_id is not None:
                    fields["agent_instance_id"] = agent_instance_id
                event = core.build_event(USAGE_EVENT, ctx, **fields)
                core.append_event(sink, event)
                count += 1
            except Exception as exc:
                core.debug_log(
                    f"usage-capture: failed transcript_path={transcript_path} "
                    f"agent_instance_id={agent_instance_id}",
                    exc,
                )
                continue

        return count
    except Exception as exc:
        core.debug_log(
            f"usage-capture: failed transcript_path={transcript_path} "
            f"agent_instance_id={agent_instance_id}",
            exc,
        )
        return 0
