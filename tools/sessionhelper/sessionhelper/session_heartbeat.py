"""Correlated session heartbeat self-report over the authenticated Hub WS.

R60-③: heartbeat_report used to reach datad through the T18 vsapi pipe. That
transport cannot work in the shipped runtime (no aiohttp in
requirements-runtime.lock) and the new platform does not run datad at all, so
the helper raised and the shim folded a definite failure into
helper_rejected/outcome unknown. The report now rides the existing session
connection as a reserved control subtype, mirroring delivery_status_query_v1:
one nonce-correlated reply, never a resend, and unknown (never a fabricated
success) when no reply arrives.
"""
import asyncio
import json
import re
import secrets

from .protocol import envelope, session_addr

CAPABILITY_HEADER = "X-DW-Session-Heartbeat"
CAPABILITY_VALUE = "session-heartbeat/v1"
REQUEST_TYPE = "session_heartbeat_v1"
RESULT_TYPE = "session_heartbeat_result_v1"
SCHEMA = "session-heartbeat/v1"
STATUSES = ("active", "busy", "blocked", "idle")
TEXT_FIELDS = ("task", "eta", "blocker")
TEXT_MAX_RUNES = 128
# Fixed reply contract. Unknown Hub fields cannot leak through this channel.
RESULT_FIELDS = ("ok", "status", "retry_after_ms", "recorded_at_ms")
MAX_IN_FLIGHT = 8
# IPC transport keys are not tool arguments; the handler strips exactly these.
IPC_ONLY_KEYS = frozenset({"type", "request_id"})
_NONCE = re.compile(r"[0-9a-f]{32}")


def heartbeat_payload(args) -> dict:
    """Validate tool arguments into the fixed wire schema; no extra keys."""
    if not isinstance(args, dict) or set(args) - {"status", *TEXT_FIELDS}:
        raise ValueError("invalid heartbeat arguments")
    status = args.get("status")
    if not isinstance(status, str) or status.strip() not in STATUSES:
        raise ValueError("status must be active/busy/blocked/idle")
    payload = {"schema": SCHEMA, "status": status.strip()}
    for field in TEXT_FIELDS:
        text = args.get(field)
        if text is None:
            continue
        if not isinstance(text, str):
            raise ValueError(f"{field} must be a string")
        text = text.strip()
        if not text:
            continue
        if len(text) > TEXT_MAX_RUNES:
            raise ValueError(f"{field} exceeds {TEXT_MAX_RUNES} chars")
        if any(ord(char) < 0x20 or ord(char) == 0x7F for char in text):
            raise ValueError(f"{field} must not contain control characters")
        payload[field] = text
    return payload


def heartbeat_wire_payload(args) -> dict:
    """Validate an IPC heartbeat request into the wire payload."""
    if isinstance(args, dict):
        args = {key: value for key, value in args.items() if key not in IPC_ONLY_KEYS}
    return heartbeat_payload(args)


class SessionHeartbeatReporter:
    """Nonce-correlated self-report; a missing reply stays unknown."""

    def __init__(self, session_names):
        self.session_names = frozenset(name for name in session_names if name)
        self.pending = {}

    @staticmethod
    def capable(ws) -> bool:
        """Only a Hub that advertised the subtype may receive it.

        An old Hub would route an unknown control subtype as a conversation
        message, so absence of the capability header means "do not send".
        """
        headers = getattr(getattr(ws, "response", None), "headers", None) or {}
        return headers.get(CAPABILITY_HEADER) == CAPABILITY_VALUE

    def _valid_hub_address(self, env) -> bool:
        source = str(env.get("from") or "").split("#")
        target = str(env.get("to") or "").split("#")
        return (len(source) == 2 and len(target) == 2 and source[0] == "workpulse" and
                target[0] in self.session_names and bool(source[1]) and source[1] == target[1])

    def receive(self, ws, env) -> bool:
        meta = env.get("meta") if isinstance(env.get("meta"), dict) else {}
        if not isinstance(meta, dict) or meta.get("type") != RESULT_TYPE:
            return False
        # Control replies never become inbox or model input, even when unmatched.
        nonce = meta.get("nonce")
        entry = self.pending.get(nonce) if isinstance(nonce, str) and _NONCE.fullmatch(nonce) else None
        if entry is None:
            return True
        connection, future = entry
        result = meta.get("result")
        if (connection is ws and not future.done() and isinstance(result, dict) and
                self._valid_hub_address(env)):
            future.set_result({key: result[key] for key in RESULT_FIELDS if key in result})
        return True

    async def report(self, ws, source_addr, key_id, payload, *, timeout=4):
        if not self.capable(ws):
            return {"ok": False, "status": "unsupported"}
        if len(self.pending) >= MAX_IN_FLIGHT:
            return {"ok": False, "status": "busy"}
        nonce = secrets.token_hex(16)
        future = asyncio.get_running_loop().create_future()
        self.pending[nonce] = (ws, future)
        frame = envelope(session_addr("workpulse", key_id), json.dumps(payload, ensure_ascii=False),
                         source_addr, {"type": REQUEST_TYPE, "nonce": nonce, "no_mirror": True})
        try:
            async with asyncio.timeout(timeout):
                await ws.send(json.dumps(frame, ensure_ascii=False))
                return await future
        except Exception:
            # No reply never proves the Hub did not record the report, and a
            # self-report is never worth an automatic resend.
            return {"ok": False, "status": "unknown"}
        finally:
            self.pending.pop(nonce, None)
            if not future.done():
                future.cancel()
            elif not future.cancelled():
                future.exception()

    def abort(self, ws):
        """Connection loss resolves waiters as unknown; it never resends."""
        for connection, future in list(self.pending.values()):
            if connection is ws and not future.done():
                future.set_result({"ok": False, "status": "unknown"})
