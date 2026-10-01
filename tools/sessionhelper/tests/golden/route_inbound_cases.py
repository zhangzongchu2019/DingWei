"""R3-B 黄金用例的**输入**（确定性生成，无随机、无时间）：收件分类判定。

录制与重放共用这一份输入；输入变了就必须在旧代码上重新录制。
"""
import hashlib
import itertools


def compact(obj, limit=1000):
    if isinstance(obj, str) and len(obj) > limit:
        return f"<len={len(obj)} sha256={hashlib.sha256(obj.encode('utf-8')).hexdigest()}>"
    if isinstance(obj, dict):
        return {k: compact(v, limit) for k, v in obj.items()}
    if isinstance(obj, (list, tuple)):
        return [compact(v, limit) for v in obj]
    return obj


# truthy_meta 的取值全集（含各种真/假字符串、数字、容器）
TRUTHY_VALUES = [True, False, None, 0, 1, 2, -1, 0.0, 1.5, "", " ", "0", "1", "true", "TRUE", "True", " true ", "yes", "Y", "y",
                 "on", "ON", "off", "false", "no", "n", "2", "enabled", [], [0], {}, {"a": 1}, ()]

# meta.type 全集：每个 is_* 判定认的 type、Hub 通知 type、近似/大小写/类型异常的干扰项
TYPES = [
    "delivery", "delivery_ack", "delivery_cancel", "delivery_cancel_ack", "agent_network_skill", "agent_network_skill_ack",
    "terminal_input", "terminal_interrupt", "terminal_resize", "terminal_refresh", "file_capability_response",
    "file_capability_request", "mirror_control", "provision", "provision_ack", "delivery_failure", "delivery_result",
    "online_directory", "agent_route", "collect", "", " ", "Delivery", "DELIVERY", "delivery ", " delivery", "provisions",
    "mirror_control2", "terminal", None, 0, 123, ["delivery"], {"t": "delivery"}, True,
]

ACK_REQUIRED = [True, False, None, 0, 1, "", "false", "0", "true", [], [0], {}]

BROADCAST_KEYS = [None, "", " ", "k", " k ", "\n", 0, 123, False, True, "0"]

TEXTS = [
    "", None, 123, "plain", "【DingWei在线清单】同账号在线AI会话\n1. #home\n**********", "【DingWei 在线清单】x **********",
    "【DingWei在线清单】只有标题没有星号", "只有星号 **********", "**********\n【DingWei在线清单】", "【DingWei在线清单】【DingWei 在线清单】**********",
    "前缀【DingWei在线清单】中间**********后缀", "【dingwei在线清单】**********", "【DingWei在线清单】*********",
    "DingWei 客户端上线指南\nsessionHelper 每分钟刷新", "DingWei 客户端上线指南", "sessionHelper 每分钟刷新",
    "…DingWei 客户端上线指南…sessionHelper 每分钟刷新…", "DingWei客户端上线指南 sessionHelper 每分钟刷新",
    "DingWei 客户端上线指南 sessionHelper每分钟刷新", "sessionHelper 每分钟刷新 DingWei 客户端上线指南",
    "x" * 2000, ["a"], {"b": 1},
]


def _meta_variants():
    out = [None, {}]
    out += [{"type": t} for t in TYPES]
    out += [{"type": "delivery", "ack_required": v} for v in ACK_REQUIRED]
    out += [{"type": "delivery_cancel", "ack_required": True}]
    out += [{"system": v} for v in TRUTHY_VALUES]
    out += [{"no_mirror": v} for v in (True, False, "true", 1)]
    out += [{"system": True, "no_mirror": True}, {"system": False, "no_mirror": True}, {"system": "yes", "no_mirror": False}]
    out += [{"broadcast_dedup_key": v} for v in BROADCAST_KEYS]
    out += [{"broadcast_dedup_key": "k", "mirror_primary": v} for v in TRUTHY_VALUES[:14]]
    out += [{"type": t, "system": True} for t in ("delivery", "delivery_failure", "provision", "terminal_input")]
    out += [{"type": "delivery_failure", "system": v} for v in (False, "0", None)]
    out += [{"type": "delivery_result", "no_mirror": True}]
    return out


ENVS = []
for _m in _meta_variants():
    env = {"from": "peer#FB-2642", "to": "home#FB-2642", "body": "b"}
    if _m is not None:
        env["meta"] = _m
    ENVS.append(env)
ENVS += [{}, {"meta": None}, {"meta": []}, {"meta": ""}]

FUNCS = [
    "is_broadcast_envelope", "is_no_mirror_envelope", "is_hub_notice_envelope", "is_mirror_control", "is_provision_envelope",
    "is_delivery", "is_delivery_cancel", "is_agent_network_skill", "is_terminal_input", "is_terminal_interrupt",
    "is_terminal_resize", "is_terminal_refresh", "is_file_capability_response",
]
TEXT_FUNCS = ["is_online_directory_text", "is_client_onboarding_text"]
