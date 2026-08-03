"""reply-origin / loop-termination 契约的 helper 侧原语(冻结锚 a33e136)。

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

from typing import Any

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
