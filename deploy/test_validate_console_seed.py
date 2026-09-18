#!/usr/bin/env python3
"""校验器自身的阴性对照:每一条检查都要有一个能把它踩红的样本。

🔴 判据不是"校验器跑了",是**每条检查各自红在自己那一条上** ——
   否则六条检查里可能有几条从来没被触发过(今日 T-114 的教训:零断言与红错地方)。
"""
from __future__ import annotations

import copy
import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
VALIDATOR = os.path.join(HERE, "validate_console_seed.py")

GOOD = {
    "principals": [
        {"id": "owner-a", "provider": "feishu", "provider_issuer": "app1",
         "provider_subject": "ou_aaa", "display_name": "Owner", "active": True},
        {"id": "owner-b", "provider": "feishu", "provider_issuer": "app1",
         "provider_subject": "ou_bbb", "display_name": "Owner(2nd)", "active": True},
    ],
    "roles": [
        {"principal_id": "owner-a", "role": "owner", "owner_key": "tenant-owner", "active": True},
        {"principal_id": "owner-b", "role": "owner", "owner_key": "tenant-owner", "active": True},
    ],
    "agents": [
        {"id": "ag-1", "key_id": "K", "session_name": "zzc-one-2642", "owner_principal": "owner-a",
         "active": True, "grants": [{"principal_id": "owner-a", "permission": "interact"},
                                    {"principal_id": "owner-b", "permission": "interact"}]},
        {"id": "ag-2", "key_id": "K", "session_name": "zzc-two-2642", "owner_principal": "owner-a",
         "active": True, "grants": [{"principal_id": "owner-a", "permission": "interact"},
                                    {"principal_id": "owner-b", "permission": "interact"}]},
    ],
}


def run(seed: dict, agents: int = 2, owners=("owner-a", "owner-b")):
    path = os.path.join(tempfile.mkdtemp(prefix="seedval-"), "seed.json")
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(seed, fh, ensure_ascii=False)
    cmd = [sys.executable, VALIDATOR, path, "--expect-agents", str(agents)]
    for o in owners:
        cmd += ["--owner-principal", o]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=30)


class SeedValidatorTest(unittest.TestCase):

    def test_0_good_seed_passes(self):
        r = run(copy.deepcopy(GOOD))
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("SEED_OK", r.stdout)

    def _mutate(self, fn, expect: str, **kw):
        seed = copy.deepcopy(GOOD)
        fn(seed)
        r = run(seed, **kw)
        self.assertEqual(r.returncode, 2, "该样本本应被拒:\n" + r.stdout + r.stderr)
        self.assertIn(expect, r.stderr,
                      f"红了,但红错地方 —— 期望命中 {expect!r}:\n{r.stderr}")

    def test_1_placeholder_left_in(self):
        self._mutate(lambda s: s["agents"][0].update(key_id="REPLACE_WITH_KEY_ID"),
                     "仍含占位符")

    def test_2_active_omitted(self):
        self._mutate(lambda s: s["agents"][0].pop("active"), "缺 active")

    def test_3_key_only_no_session(self):
        """🔴 只给 key_id ⇒ 等于把该 key 下全部会话拉进来(本平台一个 key 数百个)。"""
        self._mutate(lambda s: s["agents"][0].pop("session_name"),
                     "只给 key_id 等于把该 key 下【全部】会话拉进来")

    def test_4_agent_count_below_external_expectation(self):
        """🔴 少写一条不会有任何运行期信号 —— 只能靠外部预期发现。"""
        self._mutate(lambda s: s["agents"].pop(), "≠ 外部预期")

    def test_5_owner_second_identity_missing(self):
        """🔴 业主第二条身份漏写 ⇒ 他从那条登录时左栏是空的,而他不会知道。"""
        def drop_second(s):
            s["principals"] = [p for p in s["principals"] if p["id"] != "owner-b"]
            s["roles"] = [r for r in s["roles"] if r["principal_id"] != "owner-b"]
            for a in s["agents"]:
                a["grants"] = [g for g in a["grants"] if g["principal_id"] != "owner-b"]
        self._mutate(drop_second, "业主有几条身份就必须声明几条")

    def test_6_owner_declared_but_never_granted(self):
        """声明了却没有任何 grant —— 比漏写更隐蔽,表里有行、左栏仍空。"""
        def strip_grants(s):
            for a in s["agents"]:
                a["grants"] = [g for g in a["grants"] if g["principal_id"] != "owner-b"]
        self._mutate(strip_grants, "没有任何 grant")

    def test_7_grant_points_at_undeclared_principal(self):
        self._mutate(lambda s: s["agents"][0]["grants"].append(
            {"principal_id": "ghost", "permission": "view"}), "未声明的 principal")

    def test_9_role_prefix_instead_of_full_name(self):
        """🔴 写成截短的角色前缀 ⇒ 声明到另一条(或不存在的)席位,且不会报错。

        实测依据:按去掉尾码的角色前缀 LIKE 匹配会捞到额外行,
        其中有 active=1 但已 5 天未动的 —— 正是"看起来像做完了"的来源。
        """
        self._mutate(lambda s: s["agents"][0].update(session_name="zzc-one"),
                     "不是完整会话名")

    def test_8_duplicate_session(self):
        self._mutate(lambda s: s["agents"][1].update(session_name="zzc-one-2642"), "重复")

    def test_10_owner_role_must_be_explicit(self):
        self._mutate(lambda s: s["roles"].pop(), "没有 active owner 角色")


if __name__ == "__main__":
    unittest.main()
