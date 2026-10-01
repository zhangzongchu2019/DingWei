"""WorkPulse session envelope protocol helpers."""

from __future__ import annotations

import time
import uuid
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class AddressBook:
    session_name: str
    key_id: str
    bot_name: str = "UnifiedRobot"

    @property
    def self_addr(self) -> str:
        return session_addr(self.session_name, self.key_id)

    def feishu_addr(self, open_id: str, bot_name: str | None = None) -> str:
        return feishu_addr(open_id, self.key_id, bot_name or self.bot_name)


def session_addr(session_name: str, key_id: str) -> str:
    return f"{session_name}#{key_id}"


def feishu_addr(open_id: str, key_id: str, bot_name: str) -> str:
    return f"{open_id}#{key_id}#{bot_name}"


def envelope(to: str, body: str, from_addr: str, meta: dict[str, Any] | None = None) -> dict[str, Any]:
    return {
        "id": uuid.uuid4().hex,
        "to": to,
        "from": from_addr,
        "body": body,
        "ts": int(time.time()),
        "meta": meta or {},
    }


def required_env(name: str, env: dict[str, str]) -> str:
    value = env.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


# 临时兼容再导出：is_feishu_addr / reply_target / is_mirror_control 已搬到 routing.py（R3）。
# 用模块级 __getattr__ 延迟取，避免 protocol <-> routing 循环 import；V9 把这些逻辑上移到 Hub 后移除。
_MOVED_TO_ROUTING = ("is_feishu_addr", "reply_target", "is_mirror_control")


def __getattr__(name: str):
    if name in _MOVED_TO_ROUTING:
        from . import routing
        return getattr(routing, name)
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
