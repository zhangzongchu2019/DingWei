"""reply-origin / loop-termination 判定——已搬到 routing.py（R3）。

临时兼容再导出：保留旧 import 路径（app.py、tests 在用），逻辑与文档见 routing.py；
V9 把判定上移到 Hub 后移除本文件。
"""

from .routing import (  # noqa: F401
    DEFAULT_MAX_HOPS,
    KIND_ACK,
    KIND_CONTENT,
    KIND_MIRROR,
    KIND_REPLY,
    META_CONVERSATION_ID,
    META_CONVERSATION_TERMINAL,
    META_DEDUP_KEY,
    META_HOP_COUNT,
    META_IN_REPLY_TO,
    META_KIND,
    META_MAX_HOPS,
    META_ORIGIN_ADDR,
    META_ORIGIN_KIND,
    META_PROTOCOL_CLASS,
    META_REPLY_POLICY,
    META_REPLY_TO,
    PROTOCOL_CONTROL,
    PROTOCOL_CONVERSATION,
    REPLY_POLICY_ACK_ONLY,
    REPLY_POLICY_EXPECT,
    REPLY_POLICY_NO_REPLY,
    ReplyDecision,
    _CONTROL_META_TYPES,
    _RESERVED_CONTROL_PAYLOAD_TOKENS,
    _hop_count,
    _max_hops,
    _meta,
    classify_reply_decision,
    is_control_envelope,
    is_reserved_control_payload,
    reply_carryforward_meta,
)
