"""路由／提及判定的唯一接口（R3；为 V9 把这些判定上移到 Hub 铺路）。

客户端里"这段文本/这条信封该去哪、该回给谁、该不该回"的判定都收在本模块：

  A 出站路由解析   agent_route_envelope（CLI 输出文本 → 路由信封）及其三条正则/常量
  C 回程寻址与回复策略  reply_target / is_feishu_addr，以及 reply-origin / loop-termination 的判定
                   （is_reserved_control_payload / is_control_envelope / classify_reply_decision /
                    reply_carryforward_meta）

V9 的验收是"客户端路由正则命中 0"：路由正则只许出现在本文件（tests/test_route_regex_single_home.py 守着）。
本次是纯结构搬迁：函数体原样搬入，行为不变；黄金输出见 tests/golden/（录制自搬迁前的旧代码）。

B 收件分类（is_no_mirror_envelope / is_broadcast_envelope / 在线清单 / 上线指南 / mirror_control 等）
不在本次范围，另做第二件。

reply-origin 原文档（冻结锚 a33e136）：
reply-origin / loop-termination 契约的 helper 侧原语(冻结锚 a33e136)。

对应 Go 侧 internal/m8/reply_origin.go。本模块只做纯判定 + 回程 meta 构造:
  - is_control_envelope: B3 classifier(mcs/ack/board/delivery_ack ⇒ control)。
  - classify_reply_decision: helper 双闸第二道(§10)——生成 reply 前 honor
    control(B1 ack 不回)/kind=mirror(R1)/conversation_terminal(R3)/hop 兜底(R4)/
    reply_policy(R2,仅 expect_reply 才产 content-reply)。
  - reply_carryforward_meta: 生成的 auto-reply 带 kind=reply + conversation_terminal=True
    (R3=回复默认终态,打断 A↔B 正反馈)+ hop_count/conversation_id carry-forward
    (hub 每 transit 递增,使 A↔B 环累计到 max_hops 被 R4 兜底)+ in_reply_to(业务关联,非 ACK)。

冻结语义:ACK 绝不创建/消费/计入 conversation reply obligation 或事件(dev56 M5 硬边界)——
本模块对 control/ack 一律 allow=False,且从不读/写 ack 的 conversation 字段。
"""

from __future__ import annotations

import json
import re
from typing import TYPE_CHECKING, Any

from .protocol import envelope

if TYPE_CHECKING:
    from .protocol import AddressBook


# ======================= A 出站路由解析 =======================

LEGACY_AGENT_ROUTE_RE = re.compile(r"^(#[A-Za-z0-9_.-]+|@[^\s#]+#[A-Za-z0-9_.-]+)\s+(.+)$", re.S)
CONTROL_ROUTE_RE = re.compile(r"^\s*\[\[DW_ROUTE_V1\s*\n(?P<json>.*?)\n\]\]\s*$", re.S)
# 目标地址（control 路由的 to 字段）：与 LEGACY 前缀同形。原先内联在 agent_route_envelope 里的 re.fullmatch 字面量。
ROUTE_TARGET_RE = re.compile(r"#[A-Za-z0-9_.-]+|@[^\s#]+#[A-Za-z0-9_.-]+")
ROUTE_BODY_MAX = 100_000


def agent_route_envelope(text: str, book: AddressBook) -> dict | None:
    text = str(text or "").strip()
    control = CONTROL_ROUTE_RE.match(text)
    if control:
        try:
            payload = json.loads(control.group("json"))
        except (TypeError, ValueError, json.JSONDecodeError):
            return None
        if not isinstance(payload, dict) or set(payload) != {"to", "body"}:
            return None
        if not isinstance(payload.get("to"), str) or not isinstance(payload.get("body"), str):
            return None
        to = payload["to"].strip()
        body = payload["body"].strip()
        if not ROUTE_TARGET_RE.fullmatch(to):
            return None
        if not body or len(body) > ROUTE_BODY_MAX:
            return None
        return envelope(to, body, book.self_addr, {"type": "agent_route", "no_mirror": True})
    match = LEGACY_AGENT_ROUTE_RE.match(text)
    if not match:
        return None
    to, body = match.group(1).strip(), match.group(2).strip()
    if not body:
        return None
    return envelope(to, body, book.self_addr, {"type": "agent_route", "no_mirror": True})


# ======================= C 回程寻址 =======================

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


# ======================= C 回复策略（reply-origin / loop-termination） =======================

# ---- Meta 字段键(与 Go 冻结命名一致) ----
META_PROTOCOL_CLASS = "protocol_class"
META_KIND = "kind"
META_REPLY_POLICY = "reply_policy"
META_CONVERSATION_TERMINAL = "conversation_terminal"
META_CONVERSATION_ID = "conversation_id"
META_IN_REPLY_TO = "in_reply_to"
META_REPLY_TO = "reply_to"
META_ORIGIN_KIND = "origin_kind"
META_ORIGIN_ADDR = "origin_addr"
META_HOP_COUNT = "hop_count"
META_MAX_HOPS = "max_hops"
META_DEDUP_KEY = "dedup_key"

# ---- 枚举值 ----
PROTOCOL_CONVERSATION = "conversation"
PROTOCOL_CONTROL = "control"

KIND_CONTENT = "content"
KIND_REPLY = "reply"
KIND_ACK = "ack"
KIND_MIRROR = "mirror"

REPLY_POLICY_EXPECT = "expect_reply"
REPLY_POLICY_NO_REPLY = "no_reply"
REPLY_POLICY_ACK_ONLY = "ack_only"

DEFAULT_MAX_HOPS = 16

_RESERVED_CONTROL_PAYLOAD_TOKENS = (
    '"mcs_dispatch_v1"',
    '"mcs_receipt_v1"',
    '"board_control"',
    '"delivery_ack"',
)
_CONTROL_META_TYPES = frozenset(
    {
        "delivery",
        "delivery_ack",
        "delivery_cancel",
        "delivery_cancel_ack",
        "mirror_control",
        "provision",
        "provision_ack",
    }
)


def _meta(env: dict[str, Any]) -> dict[str, Any]:
    return env.get("meta") or {}


def _hop_count(env: dict[str, Any]) -> int:
    try:
        return int(_meta(env).get(META_HOP_COUNT) or 0)
    except (TypeError, ValueError):
        return 0


def _max_hops(env: dict[str, Any]) -> int:
    try:
        m = int(_meta(env).get(META_MAX_HOPS) or 0)
    except (TypeError, ValueError):
        m = 0
    return m if m > 0 else DEFAULT_MAX_HOPS


def is_reserved_control_payload(env: dict[str, Any]) -> bool:
    meta = _meta(env)
    if str(meta.get("type") or "") in _CONTROL_META_TYPES:
        return True
    if str(meta.get(META_KIND) or "") == KIND_ACK:
        return True
    body = str(env.get("body") or "")
    return any(tok in body for tok in _RESERVED_CONTROL_PAYLOAD_TOKENS)


def is_control_envelope(env: dict[str, Any]) -> bool:
    """B3:显式 control,或 payload 命中 reserved-control。显式 conversation 撞 reserved = 仍归 control(spoof)。"""
    if is_reserved_control_payload(env):
        return True
    return str(_meta(env).get(META_PROTOCOL_CLASS) or "") == PROTOCOL_CONTROL


class ReplyDecision:
    __slots__ = ("allow", "reason")

    def __init__(self, allow: bool, reason: str) -> None:
        self.allow = allow
        self.reason = reason

    def __repr__(self) -> str:  # pragma: no cover
        return f"ReplyDecision(allow={self.allow}, reason={self.reason!r})"


def classify_reply_decision(inbound: dict[str, Any]) -> ReplyDecision:
    """helper 第二道闸:决定是否为 inbound 生成 session-to-session auto-reply。

    顺序(硬停在前):control/ack → 不回(B1);kind=mirror → 不回(R1);
    conversation_terminal → 不回(R3);hop 达 max → 不回(R4 兜底);
    reply_policy != expect_reply → 不回(R2,默认保守=不产 auto-reply,不制造新噪声)。
    """
    meta = _meta(inbound)
    if is_control_envelope(inbound):
        return ReplyDecision(False, "control")  # B1: ack/control 绝不生成 reply
    if str(meta.get(META_KIND) or "") == KIND_MIRROR:
        return ReplyDecision(False, "mirror")  # R1
    if bool(meta.get(META_CONVERSATION_TERMINAL)):
        return ReplyDecision(False, "conversation_terminal")  # R3:回复默认不被回复
    if _hop_count(inbound) >= _max_hops(inbound):
        return ReplyDecision(False, "hop_backstop")  # R4 第二道闸
    if str(meta.get(META_REPLY_POLICY) or "") != REPLY_POLICY_EXPECT:
        return ReplyDecision(False, "no_expect_reply")  # R2:仅 expect_reply 才产 content-reply
    return ReplyDecision(True, "ok")


def reply_carryforward_meta(inbound: dict[str, Any]) -> dict[str, Any]:
    """构造 auto-reply 的 reply-origin/termination meta。

    - kind=reply、conversation_terminal=True(R3:auto-reply 默认终态,打断 A↔B)。
    - hop_count carry-forward(hub 每 transit 递增,使环累计触 R4);conversation_id 继承。
    - in_reply_to=inbound id(业务关联,非 ACK;ACK 走独立 ack_for/delivery_id)。
    """
    meta = _meta(inbound)
    out: dict[str, Any] = {
        META_KIND: KIND_REPLY,
        META_CONVERSATION_TERMINAL: True,
        META_HOP_COUNT: _hop_count(inbound),
    }
    conv_id = str(meta.get(META_CONVERSATION_ID) or "").strip()
    if conv_id:
        out[META_CONVERSATION_ID] = conv_id
    max_hops = meta.get(META_MAX_HOPS)
    if max_hops:
        out[META_MAX_HOPS] = max_hops
    inbound_id = str(inbound.get("id") or "").strip()
    if inbound_id:
        out[META_IN_REPLY_TO] = inbound_id
    return out
