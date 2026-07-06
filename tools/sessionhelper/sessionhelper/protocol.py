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


def is_feishu_addr(addr: str) -> bool:
    return addr.count("#") == 2


def reply_target(inbound: dict[str, Any], book: AddressBook) -> tuple[str, dict[str, Any]]:
    """Return target address and meta for replying to an inbound envelope."""
    from_addr = str(inbound.get("from") or "")
    meta = inbound.get("meta") or {}
    source_bot = str(meta.get("source_bot_channel_id") or "").strip()
    rmeta: dict[str, Any] = {}
    if source_bot:
        rmeta["source_bot_channel_id"] = source_bot
        chat_type = str(meta.get("source_chat_type") or meta.get("chat_type") or "").strip()
        if chat_type:
            rmeta["source_chat_type"] = chat_type
        for key in ("source_open_id", "source_chat_id", "source_sender_openid"):
            value = str(meta.get(key) or "").strip()
            if value:
                rmeta[key] = value

    chat_type = str(meta.get("source_chat_type") or meta.get("chat_type") or "").strip()
    if chat_type == "group" and (meta.get("source_chat_id") or meta.get("group_chat_id")):
        chat_id = str(meta.get("source_chat_id") or meta.get("group_chat_id"))
        sender = str(meta.get("source_sender_openid") or meta.get("sender_open_id") or "").strip()
        if sender:
            rmeta["at"] = [sender]
        return book.feishu_addr(chat_id, source_bot or None), rmeta
    if chat_type == "personal" and source_bot:
        open_id = str(meta.get("source_open_id") or meta.get("source_sender_openid") or meta.get("source_chat_id") or "").strip()
        if open_id:
            return book.feishu_addr(open_id, source_bot), rmeta
    if meta.get("chat_type") == "group" and meta.get("group_chat_id"):
        rmeta: dict[str, Any] = {}
        if meta.get("sender_open_id"):
            rmeta["at"] = [meta["sender_open_id"]]
        return book.feishu_addr(str(meta["group_chat_id"])), rmeta
    return from_addr, {}


def is_mirror_control(env: dict[str, Any]) -> bool:
    meta = env.get("meta") or {}
    return meta.get("type") == "mirror_control"


def required_env(name: str, env: dict[str, str]) -> str:
    value = env.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value
