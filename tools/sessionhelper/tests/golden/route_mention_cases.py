"""R3 黄金用例的**输入**（确定性生成，无随机、无时间）。

录制与重放共用这一份输入：录制脚本在**改动前的旧代码**上跑出 route_mention_golden.json，
重放用例在新接口上跑同一组输入，逐条与黄金比对。输入变了就必须在旧代码上重新录制。
"""
import hashlib
import itertools


def compact(obj, limit=1000):
    """超长字符串（如 10 万字符的正文边界用例）只存 长度＋sha256，录制与重放共用，避免黄金文件膨胀。"""
    if isinstance(obj, str) and len(obj) > limit:
        return f"<len={len(obj)} sha256={hashlib.sha256(obj.encode('utf-8')).hexdigest()}>"
    if isinstance(obj, dict):
        return {k: compact(v, limit) for k, v in obj.items()}
    if isinstance(obj, (list, tuple)):
        return [compact(v, limit) for v in obj]
    return obj

BOOK = {"session_name": "home", "key_id": "FB-2642", "bot_name": "UnifiedRobot"}
BOOK2 = {"session_name": "dev", "key_id": "FB-9", "bot_name": "OtherBot"}

# ---- A 出站路由解析：agent_route_envelope(text, book) ----
_CTRL = '[[DW_ROUTE_V1\n{}\n]]'
ROUTE_TEXTS = [
    # legacy：#名 / @属主#名
    "#alice hello", "#alice  hello world", "#a.b-c_1 body", "@zzc#agent1 hi", "@zzc#agent-1\nmultiline\nbody",
    "#alice", "#alice   ", "#alice\n", "@zzc hi", "@#x hi", "##x hi", "hello #alice hi", " #alice hi",
    "\n#alice hi\n", "#中文 hi", "#alice\thi", "#alice\nhi", "@a b#c hi", "@zzc#agent1", "@zzc#agent1  ",
    "#a hi\n\n", "#alice " + "x" * 200, "#alice\r\nhi", "# alice hi", "@zzc#a#b hi", "@zzc#a.b-c_d hi",
    "#alice [[DW_ROUTE_V1\n{}\n]]", "plain text no route", "", "   ", "\n\n",
    # control：合法
    _CTRL.format('{"to":"#alice","body":"hi"}'),
    _CTRL.format('{"to":"@zzc#agent1","body":"hi there"}'),
    _CTRL.format('{"body":"b","to":"#alice"}'),
    _CTRL.format('{"to":" #alice ","body":"  padded  "}'),
    _CTRL.format('{"to":"#alice","body":"多\\n行\\n正文"}'),
    _CTRL.format('{"to":"#alice","body":"' + "x" * 100000 + '"}'),
    "  " + _CTRL.format('{"to":"#alice","body":"hi"}') + "  ",
    "\n" + _CTRL.format('{"to":"#alice","body":"hi"}') + "\n",
    # control：非法 / 边界
    _CTRL.format('{"to":"#alice","body":"' + "x" * 100001 + '"}'),
    _CTRL.format('{"to":"#alice","body":""}'),
    _CTRL.format('{"to":"#alice","body":"   "}'),
    _CTRL.format('{"to":"#alice"}'),
    _CTRL.format('{"body":"hi"}'),
    _CTRL.format('{"to":"#alice","body":"hi","extra":1}'),
    _CTRL.format('{"to":1,"body":"hi"}'),
    _CTRL.format('{"to":"#alice","body":2}'),
    _CTRL.format('{"to":"alice","body":"hi"}'),
    _CTRL.format('{"to":"#al ice","body":"hi"}'),
    _CTRL.format('{"to":"@zzc agent","body":"hi"}'),
    _CTRL.format('{"to":"#","body":"hi"}'),
    _CTRL.format('[1,2]'), _CTRL.format('"str"'), _CTRL.format('null'), _CTRL.format('{bad json'),
    _CTRL.format(''), "[[DW_ROUTE_V1 {\"to\":\"#alice\",\"body\":\"hi\"} ]]",
    '[[DW_ROUTE_V1\n{"to":"#alice","body":"hi"}\n]] trailing', 'lead [[DW_ROUTE_V1\n{"to":"#alice","body":"hi"}\n]]',
    '[[DW_ROUTE_V1\r\n{"to":"#alice","body":"hi"}\r\n]]', '[[DW_ROUTE_V2\n{"to":"#alice","body":"hi"}\n]]',
    '[[dw_route_v1\n{"to":"#alice","body":"hi"}\n]]',
    # 非字符串输入
    None, 123, 0, [], {},
]

# ---- C1 回程寻址：reply_target(inbound, book) ----
_META_KEYS = {
    "source_bot_channel_id": "botX",
    "source_chat_type": "group",
    "chat_type": "personal",
    "source_chat_id": "oc_chat1",
    "group_chat_id": "oc_group1",
    "source_sender_openid": "ou_sender1",
    "sender_open_id": "ou_sender2",
    "source_open_id": "ou_open1",
}


def _meta_variants():
    keys = list(_META_KEYS)
    out = [{}]
    # 单键、双键全组合（有序、确定）
    for r in (1, 2):
        for combo in itertools.combinations(keys, r):
            out.append({k: _META_KEYS[k] for k in combo})
    # 若干有代表性的完整形态
    out += [
        {"source_bot_channel_id": "botX", "source_chat_type": "group", "source_chat_id": "oc_chat1",
         "source_sender_openid": "ou_sender1"},
        {"source_bot_channel_id": "botX", "source_chat_type": "personal", "source_open_id": "ou_open1"},
        {"source_bot_channel_id": "botX", "chat_type": "personal", "source_sender_openid": "ou_sender1"},
        {"source_bot_channel_id": "botX", "chat_type": "personal", "source_chat_id": "oc_chat1"},
        {"chat_type": "group", "group_chat_id": "oc_group1", "sender_open_id": "ou_sender2"},
        {"chat_type": "group", "group_chat_id": "oc_group1"},
        {"source_chat_type": "group", "source_chat_id": "oc_chat1", "source_sender_openid": "ou_s",
         "chat_type": "group", "group_chat_id": "oc_group1", "sender_open_id": "ou_s2"},
        {"source_bot_channel_id": "  ", "source_chat_type": "group", "source_chat_id": "oc_chat1"},
        {"source_bot_channel_id": "botX", "source_chat_type": " group ", "source_chat_id": "oc_chat1"},
        {"source_bot_channel_id": "botX", "source_chat_type": "group", "source_chat_id": "oc_chat1",
         "source_sender_openid": "   "},
    ]
    return out


REPLY_TARGET_CASES = []
for _m in _meta_variants():
    for _from in ("peer#FB-2642", "ou_abc#FB-2642#botX"):
        REPLY_TARGET_CASES.append({"inbound": {"from": _from, "meta": _m}, "book": BOOK})
REPLY_TARGET_CASES += [
    {"inbound": {"from": "peer#FB-2642"}, "book": BOOK},
    {"inbound": {"from": "peer#FB-2642", "meta": None}, "book": BOOK},
    {"inbound": {}, "book": BOOK},
    {"inbound": {"meta": {"source_bot_channel_id": "botX", "source_chat_type": "group", "source_chat_id": "oc"}}, "book": BOOK2},
    {"inbound": {"from": "p#1", "meta": {"chat_type": "group", "group_chat_id": "g", "sender_open_id": "s"}}, "book": BOOK2},
]

# ---- C2 地址形态：is_feishu_addr ----
FEISHU_ADDRS = ["", "a", "a#b", "a#b#c", "a#b#c#d", "##", "a##", "#a#", "ou_1#FB-1#bot", "peer#FB-2642", "x#y#z "]

# ---- C3 回复策略：reply_origin 的判定 ----
_TYPES = [None, "delivery", "delivery_ack", "delivery_cancel", "delivery_cancel_ack", "mirror_control",
          "provision", "provision_ack", "agent_route", "other"]
_KINDS = [None, "content", "reply", "ack", "mirror"]
_POLICIES = [None, "expect_reply", "no_reply", "ack_only", "weird"]
_CLASSES = [None, "conversation", "control"]
_TERMINAL = [None, False, True, "false", 0, 1]
_HOPS = [None, 0, 3, "5", "x", 16, 17, -1]
_MAXHOPS = [None, 0, 4, "6", "y", 16]
_BODIES = ["", "plain", '{"mcs_dispatch_v1": 1}', 'x "mcs_receipt_v1" y', '"board_control"', '"delivery_ack"', "mcs_dispatch_v1"]


def _reply_cases():
    out = []
    # 全组合太大：对单一因素逐个扫（其余取中性值），再叠加若干交互
    base = {"type": None, "kind": None, "reply_policy": "expect_reply", "protocol_class": None,
            "conversation_terminal": None, "hop_count": None, "max_hops": None}

    def mk(meta_over, body="plain", extra=None):
        meta = {k: v for k, v in {**base, **meta_over}.items() if v is not None}
        env = {"from": "peer#FB-2642", "body": body, "meta": meta}
        if extra:
            env.update(extra)
        return env

    for t in _TYPES:
        out.append(mk({"type": t}))
    for k in _KINDS:
        out.append(mk({"kind": k}))
    for p in _POLICIES:
        out.append(mk({"reply_policy": p}))
    for c in _CLASSES:
        out.append(mk({"protocol_class": c}))
    for t in _TERMINAL:
        out.append(mk({"conversation_terminal": t}))
    for h in _HOPS:
        for m in _MAXHOPS:
            out.append(mk({"hop_count": h, "max_hops": m}))
    for b in _BODIES:
        out.append(mk({}, body=b))
    for t, k, p in itertools.product(_TYPES[:3] + ["other"], _KINDS, _POLICIES[:3]):
        out.append(mk({"type": t, "kind": k, "reply_policy": p}))
    # carry-forward 相关
    out += [
        mk({"conversation_id": "conv-1", "max_hops": 8, "hop_count": 2}, extra={"id": "m-1"}),
        mk({"conversation_id": "  conv-2  "}, extra={"id": "  m-2  "}),
        mk({"conversation_id": ""}, extra={"id": ""}),
        mk({}, extra={"id": None}),
        {"from": "peer#FB-2642", "body": "no meta"},
        {"from": "peer#FB-2642", "body": "meta none", "meta": None},
        {},
    ]
    return out


REPLY_ENVS = _reply_cases()
