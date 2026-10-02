"""路由／提及判定的唯一接口（R3；为 V9 把这些判定上移到 Hub 铺路）。

客户端里"这段文本/这条信封该去哪、该回给谁、该不该回"的判定都收在本模块：

  A 出站路由解析   agent_route_envelope（CLI 输出文本 → 路由信封）及其三条正则/常量
  C 回程寻址与回复策略  reply_target / is_feishu_addr，以及 reply-origin / loop-termination 的判定
                   （is_reserved_control_payload / is_control_envelope / classify_reply_decision /
                    reply_carryforward_meta）

V9 的验收是"客户端路由正则命中 0"：路由正则只许出现在本文件（tests/test_route_regex_single_home.py 守着）。
本次是纯结构搬迁：函数体原样搬入，行为不变；黄金输出见 tests/golden/（录制自搬迁前的旧代码）。

  B 收件分类（R3-B）  这条信封要不要投递/镜像/忽略、属于哪类控制消息：
                   truthy_meta / is_broadcast_envelope / is_no_mirror_envelope / is_hub_notice_envelope /
                   is_online_directory_text / is_client_onboarding_text / is_mirror_control / is_provision_envelope，
                   以及按 meta.type 精确匹配的 8 个判定（is_*_envelope）。
                   recv_loop 的派发链**顺序即行为**，不动、不收；V9 会把整条链搬到 Hub。

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

from .hub_meta import HUB_META_TYPE_DELIVERY_FAILURE, HUB_META_TYPE_DELIVERY_RESULT
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


def is_reliable_delivery_wrapper(env: dict[str, Any]) -> bool:
    """Hub 可靠投递外壳(hub.go 覆写 type=delivery 并补 delivery_id/delivery_version=1/ack_required=true)。

    只认严格形状:畸形或伪造的 type=delivery 不享受下面的外壳例外。
    """
    meta = _meta(env)
    version = meta.get("delivery_version")
    return (meta.get("type") == "delivery" and type(version) is int and version == 1
            and meta.get("ack_required") is True and bool(str(meta.get("delivery_id") or "").strip()))


def is_reserved_control_payload(env: dict[str, Any]) -> bool:
    meta = _meta(env)
    # 组长 R-AGENT-REPLY-CONTROL(板 57f0e517,依 ANALYSIS-R-AGENT-REPLY-CONTROL 第二节):type=delivery 对严格可靠外壳
    # 只是传输外壳,忽略这一项;kind=ack、正文保留 token 照判,其后 terminal/hop/reply_policy 照判。
    if str(meta.get("type") or "") in _CONTROL_META_TYPES and not is_reliable_delivery_wrapper(env):
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


# ======================= B 收件分类（R3-B） =======================

def truthy_meta(value: object) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        return value.strip().lower() in {"1", "true", "yes", "y", "on"}
    return bool(value)


def is_broadcast_envelope(env: dict) -> bool:
    meta = env.get("meta") or {}
    return bool(str(meta.get("broadcast_dedup_key") or "").strip())


def is_no_mirror_envelope(env: dict) -> bool:
    """是否为不应进入会话的控制面信封。

    🔴 T-045:本函数此前把 no_mirror 也当作丢弃理由,导致 AMR 路由的 @ 提及被静默丢弃 ——
    帧到达了连接(coder 在 runtime.log 的 [recv] 里找到了原文、from 为发件方本体),
    但因这道判定在 delivery_inbox.accept 之前执行,它既不入库也不留痕,
    而 Hub 侧 RouteEnvelope 成功、计为 targets_dispatched。计数说成功、收件库是空的。

    no_mirror 的字面含义就是「不要镜像」,Hub 侧也只由 shouldSkipMirror 消费
    (决定要不要镜像到飞书等外部面),与「要不要投递给本会话」无关。
    把它读成「不要投递」是语义越界。

    改为只按 system 判定,安全性已逐个核对:Hub 侧每一个 no_mirror=true 的信封
    **都同时带 system=true** —— terminal_interrupt/resize/refresh、delivery_cancel、
    provision、mirror_control、agent_network_skill_push、online_directory、
    delivery_status、errorEnvelope,无一例外。
    唯二例外是 amr.go:583 的路由信封与本文件 agent_route_envelope 自己产的信封,
    而这两者**正是应当被投递的**。
    另:在线清单在本函数之前已由 is_online_directory_text 分支单独处理,不受影响。
    """
    meta = env.get("meta") or {}
    return truthy_meta(meta.get("system"))


# Hub 自发系统通知的 meta.type 集合。值来自 internal/model/model.go 的单一真源
# (tools/sessionhelper/gen_hub_meta.py 生成 hub_meta.py)。
# 🔴 不要在这里写字面量:tests/test_hub_notice_no_inject.py 有一格专门扫 sessionhelper/*.py(除 hub_meta.py)的源码,
#    命中字面量即红(派工单第三节:「不得在 helper 里另抄一份字面量 —— 两份必漂移」)。
HUB_NOTICE_META_TYPES = frozenset({HUB_META_TYPE_DELIVERY_FAILURE, HUB_META_TYPE_DELIVERY_RESULT})


def is_hub_notice_envelope(env: dict) -> bool:
    """是否为 Hub 自发的系统通知(投递失败回执 / AMR 路由结果通知)。

    🔴 S-HELPER-NOTICE-NO-INJECT:这两类信封此前【既不进 delivery_inbox、又会被注入 CLI】。
    原因是 dispatch 链上每一道判据都判否:
      · is_delivery 要 type=="delivery" + ack_required —— 它们两者都不带;
      · is_no_mirror_envelope 自 T-045 起只按 system 判 —— 它们不带 system=true
        (T-045 那次正是把 no_mirror 当丢弃理由,把该投的 AMR 提及静默丢掉、计数还说成功;
         所以这里【不能】靠给通知补 system/no_mirror 来解决,那会重演同一种越界)。
    ⇒ 落到兜底 process_inbound_message / buffer_if_busy ⇒ 注入 CLI ⇒ 产生新一轮。

    而新一轮必然再产生一条通知(有正文会通知;没正文也通知,见 amr.go 的 empty 分支),
    限速器 dedupKey 不含 DeliveryID/TurnID、窗口 1 分钟 ⇒ 约 60 轮/小时、零业务产出,
    且席位侧无法自己退出。这就是要断的环。

    🔴 处理方式是「留痕 + 不注入」(见 hub_notice_trace),不是丢弃:
    丢弃会把可见故障换成不可见故障。
    🔴 也不得改成写进 delivery_inbox —— inbox 的行会被 delivery_worker_loop claim 出来注入,
    等于把环换个地方接回去。判据③正是逐个量这三个出口(adapter.handle /
    pending_inbound / inbox)都为空。
    """
    meta = env.get("meta") or {}
    return str(meta.get("type") or "") in HUB_NOTICE_META_TYPES


def is_online_directory_text(text: str) -> bool:
    text = str(text or "")
    return ("【DingWei在线清单】" in text or "【DingWei 在线清单】" in text) and "**********" in text


def is_client_onboarding_text(text: str) -> bool:
    """上线指南文本识别。移植自部署版(T-048)。

    Hub 会在重连后重推《DingWei 客户端上线指南》。若不识别,它会作为普通正文
    被注入 CLI 对话 —— 每次重连刷一遍。部署版有此函数、仓内版没有,
    是 2026-09-10 T-048 逐行 diff 查出的三处「部署版独有、覆盖即丢」之一。
    """
    text = str(text or "")
    return "DingWei 客户端上线指南" in text and "sessionHelper 每分钟刷新" in text


def is_mirror_control(env: dict[str, Any]) -> bool:
    meta = env.get("meta") or {}
    return meta.get("type") == "mirror_control"


def is_delivery_envelope(env: dict) -> bool:
    meta = env.get("meta") or {}
    return meta.get("type") == "delivery" and bool(meta.get("ack_required"))


def is_delivery_cancel_envelope(env: dict) -> bool:
    return (env.get("meta") or {}).get("type") == "delivery_cancel"


def is_agent_network_skill_envelope(env: dict) -> bool:
    return (env.get("meta") or {}).get("type") == "agent_network_skill"


def is_terminal_input_envelope(env: dict) -> bool:
    return (env.get("meta") or {}).get("type") == "terminal_input"


def is_terminal_interrupt_envelope(env: dict) -> bool:
    return (env.get("meta") or {}).get("type") == "terminal_interrupt"


def is_terminal_resize_envelope(env: dict) -> bool:
    return (env.get("meta") or {}).get("type") == "terminal_resize"


def is_terminal_refresh_envelope(env: dict) -> bool:
    return (env.get("meta") or {}).get("type") == "terminal_refresh"


def is_file_capability_response_envelope(env: dict) -> bool:
    return (env.get("meta") or {}).get("type") == "file_capability_response"


def is_provision_envelope(env: dict) -> bool:
    return (env.get("meta") or {}).get("type") == "provision"
