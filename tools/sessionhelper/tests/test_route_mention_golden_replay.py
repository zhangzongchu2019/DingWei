"""R3：路由／提及判定"新旧逐条一致"——重放黄金输出。

黄金（tests/golden/route_mention_golden.json）录制于 R3 改动之前的旧代码（文件头 _recorded_from 记录了
当时的 git head 与源文件 sha256；录制脚本只经旧位置取函数）。这里用**同一组输入**，分别经
  ① 新接口 sessionhelper.routing，
  ② 临时兼容再导出（app / protocol / reply_origin 的旧 import 路径），
重放，逐条与黄金比对。
"""
import json
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE / "golden"))

import route_mention_cases as C  # noqa: E402
from sessionhelper import app, protocol, reply_origin, routing  # noqa: E402

GOLDEN = json.loads((HERE / "golden" / "route_mention_golden.json").read_text(encoding="utf-8"))


def norm_env(env):
    return None if env is None else {k: v for k, v in env.items() if k not in ("id", "ts")}


def book(d):
    return protocol.AddressBook(d["session_name"], d["key_id"], d["bot_name"])


class RouteMentionGoldenReplayTest(unittest.TestCase):
    # 每个 provider 给出：agent_route_envelope / reply_target / is_feishu_addr / 回复策略四件
    PROVIDERS = {
        "routing": (routing.agent_route_envelope, routing.reply_target, routing.is_feishu_addr,
                    routing.is_reserved_control_payload, routing.is_control_envelope,
                    routing.classify_reply_decision, routing.reply_carryforward_meta),
        "legacy-reexports": (app.agent_route_envelope, protocol.reply_target, protocol.is_feishu_addr,
                             reply_origin.is_reserved_control_payload, reply_origin.is_control_envelope,
                             reply_origin.classify_reply_decision, reply_origin.reply_carryforward_meta),
    }

    def test_golden_covers_every_input(self):
        self.assertEqual(len(GOLDEN["route"]), len(C.ROUTE_TEXTS))
        self.assertEqual(len(GOLDEN["reply_target"]), len(C.REPLY_TARGET_CASES))
        self.assertEqual(len(GOLDEN["is_feishu_addr"]), len(C.FEISHU_ADDRS))
        self.assertEqual(len(GOLDEN["reply"]), len(C.REPLY_ENVS))
        self.assertRegex(GOLDEN["_recorded_from"]["git_head"], r"^[0-9a-f]{40}$")

    def test_replay_matches_golden_through_every_provider(self):
        for name, (route, target, is_feishu, reserved, control, decide, carry) in self.PROVIDERS.items():
            with self.subTest(provider=name):
                for text, rec in zip(C.ROUTE_TEXTS, GOLDEN["route"]):
                    self.assertEqual(C.compact(norm_env(route(text, book(C.BOOK)))), rec["out"], rec["text"])
                for text, rec in zip(C.ROUTE_TEXTS[:12], GOLDEN["route_book2"]):
                    self.assertEqual(C.compact(norm_env(route(text, book(C.BOOK2)))), rec["out"], rec["text"])
                for case, rec in zip(C.REPLY_TARGET_CASES, GOLDEN["reply_target"]):
                    self.assertEqual(list(target(case["inbound"], book(case["book"]))), rec["out"], case)
                for addr, rec in zip(C.FEISHU_ADDRS, GOLDEN["is_feishu_addr"]):
                    self.assertEqual(is_feishu(addr), rec["out"], addr)
                for env, rec in zip(C.REPLY_ENVS, GOLDEN["reply"]):
                    d = decide(env)
                    self.assertEqual(
                        (reserved(env), control(env), [d.allow, d.reason], carry(env)),
                        (rec["reserved"], rec["control"], rec["decision"], rec["carry"]), env)

    def test_golden_has_discriminating_power(self):
        # 反向对照：黄金不能是"全 None/全 False"的退化输出，否则任何实现都能过。
        self.assertGreater(sum(1 for r in GOLDEN["route"] if r["out"] is not None), 15)
        self.assertGreater(sum(1 for r in GOLDEN["route"] if r["out"] is None), 15)
        self.assertEqual({r["decision"][1] for r in GOLDEN["reply"]},
                         {"control", "mirror", "conversation_terminal", "hop_backstop", "no_expect_reply", "ok"})
        self.assertGreater(len({json.dumps(r["out"], sort_keys=True) for r in GOLDEN["reply_target"]}), 8)

    def test_old_import_paths_are_the_same_objects_as_routing(self):
        for n in ("agent_route_envelope", "CONTROL_ROUTE_RE", "LEGACY_AGENT_ROUTE_RE", "ROUTE_BODY_MAX",
                  "reply_target", "is_feishu_addr", "classify_reply_decision", "reply_carryforward_meta"):
            self.assertIs(getattr(app, n), getattr(routing, n), n)
        for n in ("reply_target", "is_feishu_addr"):
            self.assertIs(getattr(protocol, n), getattr(routing, n), n)
        for n in ("classify_reply_decision", "is_control_envelope", "is_reserved_control_payload",
                  "reply_carryforward_meta", "ReplyDecision", "KIND_ACK", "META_KIND", "DEFAULT_MAX_HOPS"):
            self.assertIs(getattr(reply_origin, n), getattr(routing, n), n)


if __name__ == "__main__":
    unittest.main()
