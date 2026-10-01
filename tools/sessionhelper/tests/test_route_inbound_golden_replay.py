"""R3-B：收件分类判定"新旧逐条一致"——重放黄金输出。

黄金（tests/golden/route_inbound_golden.json）录制于 R3-B 改动之前的代码（文件头 _recorded_from 记录了
git head 与源文件 sha256；录制脚本只经旧位置取函数）。这里用同一组输入，分别经
  ① 新接口 sessionhelper.routing，
  ② 临时兼容再导出（app / protocol / provision 的旧 import 路径；SessionHelper 的实例判定方法），
重放，逐条（含"抛什么异常"）与黄金比对。
"""
import json
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE / "golden"))

import route_inbound_cases as C  # noqa: E402
from sessionhelper import app, protocol, provision, routing  # noqa: E402

GOLDEN = json.loads((HERE / "golden" / "route_inbound_golden.json").read_text(encoding="utf-8"))

# 实例判定在 routing 里叫 is_*_envelope
ROUTING_NAME = {
    "is_delivery": "is_delivery_envelope", "is_delivery_cancel": "is_delivery_cancel_envelope",
    "is_agent_network_skill": "is_agent_network_skill_envelope", "is_terminal_input": "is_terminal_input_envelope",
    "is_terminal_interrupt": "is_terminal_interrupt_envelope", "is_terminal_resize": "is_terminal_resize_envelope",
    "is_terminal_refresh": "is_terminal_refresh_envelope", "is_file_capability_response": "is_file_capability_response_envelope",
}


def call(fn, *args):
    try:
        return {"v": fn(*args)}
    except Exception as exc:
        return {"raises": type(exc).__name__}


def provider_routing(name):
    return getattr(routing, ROUTING_NAME.get(name, name))


def provider_legacy(name):
    if name == "is_mirror_control":
        return protocol.is_mirror_control
    if name == "is_provision_envelope":
        return provision.is_provision_envelope
    if name in ROUTING_NAME:
        f = getattr(app.SessionHelper, name)  # 实例方法（现为对 routing 的委托），不碰 self
        return lambda env: f(None, env)
    return getattr(app, name)


class RouteInboundGoldenReplayTest(unittest.TestCase):
    def test_golden_covers_every_input_and_has_discriminating_power(self):
        self.assertEqual(len(GOLDEN["env"]), len(C.ENVS))
        self.assertEqual(len(GOLDEN["text"]), len(C.TEXTS))
        self.assertEqual(len(GOLDEN["truthy"]), len(C.TRUTHY_VALUES))
        self.assertRegex(GOLDEN["_recorded_from"]["git_head"], r"^[0-9a-f]{40}$")
        # 反向对照：每个判定在黄金里既有真又有假，否则任何实现都能蒙过
        for name in C.FUNCS:
            vals = {r[name].get("v") for r in GOLDEN["env"]}
            self.assertEqual(vals, {True, False}, name)
        for name in C.TEXT_FUNCS:
            self.assertEqual({r[name].get("v") for r in GOLDEN["text"]}, {True, False}, name)
        self.assertEqual({r["out"].get("v") for r in GOLDEN["truthy"]}, {True, False})

    def test_replay_matches_golden_through_both_providers(self):
        for label, get in (("routing", provider_routing), ("legacy-reexports", provider_legacy)):
            with self.subTest(provider=label):
                for env, rec in zip(C.ENVS, GOLDEN["env"]):
                    for name in C.FUNCS:
                        self.assertEqual(call(get(name), env), rec[name], f"{name} {rec['env']}")
                for text, rec in zip(C.TEXTS, GOLDEN["text"]):
                    for name in C.TEXT_FUNCS:
                        fn = getattr(routing, name) if label == "routing" else getattr(app, name)
                        self.assertEqual(call(fn, text), rec[name], f"{name} {rec['text']!r}")
                tm = routing.truthy_meta if label == "routing" else app.truthy_meta
                for value, rec in zip(C.TRUTHY_VALUES, GOLDEN["truthy"]):
                    self.assertEqual(call(tm, value), rec["out"], rec["value"])

    def test_hub_notice_types_unchanged_and_old_paths_are_the_same_objects(self):
        self.assertEqual(sorted(routing.HUB_NOTICE_META_TYPES), GOLDEN["_recorded_from"]["hub_notice_types"])
        self.assertIs(app.HUB_NOTICE_META_TYPES, routing.HUB_NOTICE_META_TYPES)
        for n in ("truthy_meta", "is_broadcast_envelope", "is_no_mirror_envelope", "is_hub_notice_envelope",
                  "is_online_directory_text", "is_client_onboarding_text", "is_mirror_control", "is_provision_envelope"):
            self.assertIs(getattr(app, n), getattr(routing, n), n)
        self.assertIs(protocol.is_mirror_control, routing.is_mirror_control)
        self.assertIs(provision.is_provision_envelope, routing.is_provision_envelope)


if __name__ == "__main__":
    unittest.main()
