"""Session-local FIFO and lifecycle events shared by all human command sources."""

from __future__ import annotations

import threading
import time
import uuid
from collections import deque
from dataclasses import dataclass
from typing import Literal

from .protocol import is_feishu_addr


CommandSource = Literal["user", "agent", "feishu"]
QueueState = Literal["queued", "admitting", "admitted", "completed", "interrupted", "failed"]


@dataclass
class QueuedCommand:
    delivery_id: str
    queue_seq: int
    source: CommandSource
    display_name: str
    text: str
    envelope: dict
    enqueued_at: float
    open_id: str = ""
    bot_channel_id: str = ""
    state: QueueState = "queued"
    turn_id: str = ""


@dataclass(frozen=True)
class EnqueueResult:
    state: Literal["queued", "duplicate", "rejected"]
    command: QueuedCommand | None
    event: dict | None
    reason: str = ""


class UnifiedCommandQueue:
    """One lock, one FIFO and one current command for a SessionHelper process.

    Events are emitted at most once for each ``(delivery_id, lifecycle)``.  A
    duplicate Hub delivery returns the original command without allocating a
    second queue sequence.
    """

    def __init__(self, capacity: int = 100, *, clock=time.time):
        self.capacity = max(1, int(capacity))
        self._clock = clock
        self._lock = threading.RLock()
        self._pending: deque[QueuedCommand] = deque()
        self._by_delivery: dict[str, QueuedCommand] = {}
        self._emitted: set[tuple[str, str]] = set()
        self._rejected_emitted: set[str] = set()
        self._current: QueuedCommand | None = None
        self._next_seq = 1

    def enqueue(self, env: dict, *, source: CommandSource | None = None) -> EnqueueResult:
        text = str(env.get("body") or "")
        delivery_id = self._delivery_id(env, source)
        with self._lock:
            existing = self._by_delivery.get(delivery_id)
            if existing is not None:
                return EnqueueResult("duplicate", existing, None)
            if self.depth >= self.capacity:
                event = None
                if delivery_id not in self._rejected_emitted:
                    self._rejected_emitted.add(delivery_id)
                    event = self._rejected_event(delivery_id, text, env, source)
                return EnqueueResult(
                    "rejected",
                    None,
                    event,
                    "queue_full",
                )
            source, display_name, open_id, bot_channel_id = source_fields(env, source)
            command = QueuedCommand(
                delivery_id=delivery_id,
                queue_seq=self._next_seq,
                source=source,
                display_name=display_name,
                open_id=open_id,
                bot_channel_id=bot_channel_id,
                text=text,
                envelope=env,
                enqueued_at=self._clock(),
            )
            self._next_seq += 1
            self._pending.append(command)
            self._by_delivery[delivery_id] = command
            event = self._lifecycle_event(command, "queued")
            return EnqueueResult("queued", command, event)

    def start_next(self) -> QueuedCommand | None:
        with self._lock:
            if self._current is not None or not self._pending:
                return None
            self._current = self._pending.popleft()
            self._current.state = "admitting"
            return self._current

    def admit(self, delivery_id: str, turn_id: str) -> dict | None:
        turn_id = str(turn_id or "").strip()
        if not turn_id:
            return None
        with self._lock:
            command = self._current
            if command is None or command.delivery_id != delivery_id:
                return None
            command.turn_id = turn_id
            command.state = "admitted"
            return self._lifecycle_event(command, "admitted")

    def finish(self, delivery_id: str, outcome: Literal["completed", "interrupted", "failed"]) -> QueuedCommand | None:
        with self._lock:
            command = self._current
            if command is None or command.delivery_id != delivery_id:
                return None
            command.state = outcome
            self._current = None
            return command

    @property
    def current(self) -> QueuedCommand | None:
        with self._lock:
            return self._current

    def interrupt_target(self, delivery_id: str, turn_id: str) -> QueuedCommand | None:
        """Atomically validate an interrupt against the authoritative current turn."""
        with self._lock:
            command = self._current
            if command is None or command.delivery_id != delivery_id:
                return None
            if not command.turn_id or command.turn_id != turn_id:
                return None
            return command

    @property
    def pending(self) -> tuple[QueuedCommand, ...]:
        with self._lock:
            return tuple(self._pending)

    @property
    def depth(self) -> int:
        return len(self._pending) + (1 if self._current is not None else 0)

    def _lifecycle_event(self, command: QueuedCommand, lifecycle: Literal["queued", "admitted"]) -> dict | None:
        key = (command.delivery_id, lifecycle)
        if key in self._emitted:
            return None
        self._emitted.add(key)
        data = {
            "lifecycle": lifecycle,
            "source": command.source,
            "display_name": command.display_name,
            "open_id": command.open_id,
            "bot_channel_id": command.bot_channel_id,
            "queue_seq": command.queue_seq,
            "enqueued_at": command.enqueued_at,
        }
        if lifecycle == "admitted":
            data["admitted_at"] = self._clock()
        return {
            "kind": "user_input",
            "text": command.text,
            "data": data,
            "session_id": "",
            "cursor": f"queue:{command.queue_seq:020d}:{lifecycle}",
            "delivery_id": command.delivery_id,
            "turn_id": command.turn_id if lifecycle == "admitted" else "",
        }

    def _rejected_event(
        self, delivery_id: str, text: str, env: dict, source: CommandSource | None
    ) -> dict:
        source, display_name, open_id, bot_channel_id = source_fields(env, source)
        return {
            "kind": "state_change",
            "text": text,
            "data": {
                "change": "queue_rejected",
                "reason": "queue_full",
                "source": source,
                "display_name": display_name,
                "open_id": open_id,
                "bot_channel_id": bot_channel_id,
            },
            "session_id": "",
            "cursor": "",
            "delivery_id": delivery_id,
            "turn_id": "",
        }

    @staticmethod
    def _delivery_id(env: dict, source: CommandSource | None) -> str:
        meta = env.get("meta") or {}
        raw = str(meta.get("delivery_id") or meta.get("id") or env.get("id") or "").strip()
        if raw:
            return raw if raw.startswith("dw-") else f"dw-{raw}"
        prefix = "dw-view" if source == "user" or meta.get("source_kind") == "user" else "dw"
        return f"{prefix}-{uuid.uuid4().hex}"


def source_fields(env: dict, explicit: CommandSource | None = None) -> tuple[CommandSource, str, str, str]:
    meta = env.get("meta") or {}
    from_addr = str(env.get("from") or "").strip()
    raw_source = explicit or str(meta.get("source_kind") or meta.get("source") or "").strip()
    if raw_source in ("user", "agent", "feishu"):
        source: CommandSource = raw_source  # type: ignore[assignment]
    else:
        source = "feishu" if is_feishu_addr(from_addr) else "agent"

    bot_channel_id = str(meta.get("source_bot_channel_id") or "").strip()
    open_id = ""
    if source == "feishu":
        chat_type = str(meta.get("source_chat_type") or meta.get("chat_type") or "").strip()
        if chat_type == "group":
            open_id = str(meta.get("source_sender_openid") or meta.get("sender_open_id") or "").strip()
        else:
            open_id = str(
                meta.get("source_open_id")
                or meta.get("source_sender_openid")
                or meta.get("sender_open_id")
                or from_addr.split("#", 1)[0]
            ).strip()
    fallback = "用户" if source == "user" else (open_id if source == "feishu" else from_addr.split("#", 1)[0])
    display_name = str(meta.get("source_display_name") or meta.get("display_name") or fallback).strip() or fallback
    return source, display_name, open_id, bot_channel_id
