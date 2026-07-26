"""Read-only normalization for native CLI usage frames.

The probe is deliberately fail-safe: partial token accounting is evidence, not
an authoritative context percentage.  Callers may trigger rotation only from a
``known`` sample with a verified max-context source.
"""

from __future__ import annotations

from dataclasses import asdict, dataclass
from typing import Any


@dataclass(frozen=True)
class UsageProbeResult:
    provider: str
    state: str
    session_id: str = ""
    model: str = ""
    source: str = ""
    normalized_used_tokens: int | None = None
    max_context: int | None = None
    raw_value: dict[str, Any] | None = None
    raw_kind: str = ""
    confidence: str = "unknown"
    reason: str = ""

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


def _non_negative_int(value: Any) -> int | None:
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        return None
    return value


def probe_claude_frame(frame: dict[str, Any], *, session_id: str = "", model: str = "") -> UsageProbeResult:
    """Inspect one Claude stream-json frame without inventing max-context.

    ``assistant.message.usage`` is retained as partial native evidence.  It is
    not promoted to a context snapshot until an authenticated probe proves the
    semantics and supplies an authoritative max-context value.
    """

    if frame.get("type") == "system" and frame.get("subtype") == "init":
        return UsageProbeResult(
            provider="claude",
            state="unknown",
            session_id=str(frame.get("session_id") or ""),
            model=str(frame.get("model") or ""),
            source="claude.stream-json.system.init",
            raw_kind="system_init",
            confidence="partial",
            reason="usage_not_present",
        )

    if frame.get("type") != "assistant":
        return UsageProbeResult(
            provider="claude",
            state="unknown",
            session_id=session_id,
            model=model,
            source="claude.stream-json",
            raw_kind=str(frame.get("type") or "unknown"),
            confidence="unknown",
            reason="not_an_assistant_usage_frame",
        )

    message = frame.get("message")
    usage = message.get("usage") if isinstance(message, dict) else None
    if not isinstance(usage, dict):
        return UsageProbeResult(
            provider="claude",
            state="unknown",
            session_id=str(frame.get("session_id") or session_id),
            model=model,
            source="claude.stream-json.assistant.message.usage",
            raw_kind="assistant_message_usage",
            confidence="unknown",
            reason="usage_missing",
        )

    token_fields = (
        "input_tokens",
        "cache_creation_input_tokens",
        "cache_read_input_tokens",
        "output_tokens",
    )
    parsed = {name: _non_negative_int(usage.get(name)) for name in token_fields}
    if any(value is None for value in parsed.values()):
        reason = "invalid_token_fields"
    else:
        reason = "max_context_unavailable_and_context_semantics_unverified"
    return UsageProbeResult(
        provider="claude",
        state="unknown",
        session_id=str(frame.get("session_id") or session_id),
        model=str(message.get("model") or model) if isinstance(message, dict) else model,
        source="claude.stream-json.assistant.message.usage",
        raw_value={name: usage.get(name) for name in token_fields},
        raw_kind="assistant_message_usage",
        confidence="partial" if reason.startswith("max_context") else "unknown",
        reason=reason,
    )
