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

Single-stream invariant (format spec). A given record_id belongs to exactly
one stream within a run, so a record contributes to exactly one actor's
total. Producers must not emit the same record_id on more than one stream.
This module enforces the invariant on the tool-firing path by suppressing
usage emission entirely in subagent scope (when ctx.agent_id is set); the
subagent-stop handler in mosaic_logger_handlers_invocation.py is the sole
producer of a subagent's own usage records, reading the subagent's own
transcript. Orchestrator-scoped tool firings continue to emit to the
orchestrator stream as before. A consumer that observes a record_id on two
streams treats it as a data-quality finding, not a double-count.

Emit-on-change semantics. A record is written to a stream only when its
record_id is new to that stream, or when its analyzer-significant values
(token_usage, model) have changed since it was last written. Per-stream
state is maintained in a dot-prefixed sidecar directory so the analyzer's
scanner skips it. The analyzer's supersede rule remains authoritative and
last-wins: re-emission delivers the settled values, not the first
observation.

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

import hashlib
import json
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


_USAGE_STATE_VERSION = 1


def record_fingerprint(record) -> str:
    """Return a short, stable fingerprint of the record content whose change
    must reach the analyzer.

    Covers exactly model and token_usage — the two analyzer-significant
    fields. record_id, record_index and service_tier are excluded.
    Envelope fields are not inputs to this function.

    Returns "" when the record cannot be fingerprinted for any reason —
    including when the object has no model attribute. "" is the 'unknown'
    sentinel: the caller must emit the record and must not store an entry.

    Never raises.
    """
    try:
        if not hasattr(record, "model") or not hasattr(record, "token_usage"):
            return ""
        model = record.model or ""
        token_usage = record.token_usage or {}
        # Serialize with sorted keys for order-independence
        content = json.dumps({"model": model, "token_usage": token_usage},
                             sort_keys=True, ensure_ascii=False)
        return hashlib.sha256(content.encode("utf-8")).hexdigest()
    except Exception:
        return ""


def load_usage_state(paths: "core.LogPaths",
                     run_id: str,
                     stream_key: str) -> dict:
    """Return the record_id -> fingerprint mapping last persisted for this stream.

    Returns {} when the file is absent, unreadable, not valid JSON, not the
    expected shape, or of an unrecognised version. Entries whose key or
    value is not a non-empty string are skipped.

    Never raises.
    """
    try:
        entry = paths.usage_state_entry(run_id, stream_key)
        try:
            text = entry.read_text(encoding="utf-8")
        except Exception:
            return {}
        try:
            data = json.loads(text)
        except Exception:
            return {}
        if not isinstance(data, dict):
            return {}
        if data.get("version") != _USAGE_STATE_VERSION:
            return {}
        records = data.get("records")
        if not isinstance(records, dict):
            return {}
        result = {}
        for k, v in records.items():
            if isinstance(k, str) and k and isinstance(v, str) and v:
                result[k] = v
        return result
    except Exception:
        return {}


def save_usage_state(paths: "core.LogPaths",
                     run_id: str,
                     stream_key: str,
                     state: dict,
                     stream: "str | None" = None) -> bool:
    """Persist the record_id -> fingerprint mapping for this stream via
    core.atomic_replace, creating the parent directory if needed.

    stream, when supplied, is the resolved sink path as a human-readable
    string (a forensic/debugging aid — never read back for a decision).
    Defaults to stream_key when not provided.

    Returns True on success, False on any failure. Never raises.
    """
    try:
        entry = paths.usage_state_entry(run_id, stream_key)
        try:
            entry.parent.mkdir(parents=True, exist_ok=True)
        except Exception as exc:
            core.debug_log(
                f"usage-state: failed to create state directory {entry.parent}",
                exc,
            )
            return False
        data = {
            "version": _USAGE_STATE_VERSION,
            "stream": stream if stream is not None else stream_key,
            "records": state,
        }
        text = json.dumps(data, ensure_ascii=False)
        ok = core.atomic_replace_text(entry, text)
        if not ok:
            core.debug_log(
                f"usage-state: atomic_replace failed for {entry}"
            )
        return ok
    except Exception as exc:
        core.debug_log(
            f"usage-state: save failed stream_key={stream_key}",
            exc,
        )
        return False


def emit_usage_records(ctx: "core.HookContext",
                        transcript_path: "str | None",
                        agent_instance_id: "str | None",
                        source: str,
                        sink_override: "object | None" = None) -> int:
    """Emit one usage_record event per assistant record in transcript_path,
    only when the record is new or its analyzer-significant values changed.

    source is 'orchestrator_transcript' or 'agent_transcript' and is carried
    on the event for forensics; STREAM ROUTING is authoritative, not this
    field.

    Every event is appended to exactly ONE sink: sink_override when supplied
    (used by the quarantine branch of handle_subagent_stop, whose events file
    is not the ordinary core.event_sink_for(ctx, agent_instance_id) path),
    otherwise core.event_sink_for(ctx, agent_instance_id). agent_instance_id
    None routes to the orchestrator events file; anything else routes to
    that invocation's events file. A record is never written to two streams.

    Returns the number of events actually appended. A falsy transcript_path,
    an unreadable transcript, or a transcript with no assistant records
    returns 0. Any failure degrades silently with a diagnostic and NEVER
    prevents the surrounding event from being written.

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

        # Derive a stable, fixed-length stream key from the resolved sink.
        stream_key = hashlib.sha256(str(sink).encode("utf-8")).hexdigest()[:16]

        # Load per-stream state; treat any failure as empty (emit everything).
        try:
            state = load_usage_state(ctx.paths, core.effective_run_id(ctx), stream_key)
        except Exception as exc:
            core.debug_log(
                f"usage-capture: state load failed transcript_path={transcript_path} "
                f"agent_instance_id={agent_instance_id}",
                exc,
            )
            state = {}

        updated_state = dict(state)  # copy; we accumulate changes here
        count = 0
        for rec in records:
            try:
                record_id = getattr(rec, "record_id", None) or ""
                fp = record_fingerprint(rec)

                # Decide whether to emit this record.
                if record_id and fp:
                    stored_fp = state.get(record_id)
                    if stored_fp == fp:
                        # Unchanged and already emitted — skip.
                        continue
                # else: falsy record_id or unfingerprintable → always emit,
                # do not store state entry.

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
                # Update state only after the write is confirmed, so state
                # can never mark a record as written unless the append succeeded.
                if record_id and fp:
                    updated_state[record_id] = fp
            except Exception as exc:
                core.debug_log(
                    f"usage-capture: failed transcript_path={transcript_path} "
                    f"agent_instance_id={agent_instance_id}",
                    exc,
                )
                continue

        # Persist updated state only when something changed.
        if updated_state != state:
            try:
                save_usage_state(
                    ctx.paths, core.effective_run_id(ctx), stream_key, updated_state,
                    stream=str(sink),
                )
            except Exception as exc:
                core.debug_log(
                    f"usage-capture: state save failed transcript_path={transcript_path} "
                    f"agent_instance_id={agent_instance_id}",
                    exc,
                )

        return count
    except Exception as exc:
        core.debug_log(
            f"usage-capture: failed transcript_path={transcript_path} "
            f"agent_instance_id={agent_instance_id}",
            exc,
        )
        return 0
