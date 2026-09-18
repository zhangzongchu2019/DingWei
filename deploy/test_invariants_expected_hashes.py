#!/usr/bin/env python3
"""把「巡检期望值」与「仓内真实文件」焊在一起。

🔴 为什么要有这个测试（组长 2026-09-18 立的通则）：
    期望值/不变式的更新，**归属于使它失效的那次改动**，不归属于发版。
而在此之前它靠的是「有人记得在发版当天回写」——
`9239443c` 那次提交的标题自己写着「**我写下规矩的下一版就自己忘了**」。

⇒ 本测试让这件事不再依赖记性：**改了被钉住的文件而没回写期望值，这里就红。**
它替代不了发版当天的 ⑬ 闸（那条在真实产物上重算），它只是把**仓内**这一半提前到改动发生的那一刻。
"""
import hashlib
import json
import pathlib
import re
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
INVARIANTS = ROOT / "deploy" / "invariants-dingwei-gray.json"

# 巡检项 id -> 仓内对应文件。只登记「期望值就是仓内某个文件哈希」的那几条；
# 期望值来自部署期环境变量的（如 workpulse 二进制）不在此列。
PINNED = {
    "renderer-matches-anchor": "view-service/renderer-canary-sse.html",
    "view-service-matches-anchor": "view-service/view_service.py",
    "page-store-matches-anchor": "view-service/view_page_store.py",
}


def iter_checks(node):
    if isinstance(node, dict):
        if "id" in node and "cmd" in node:
            yield node
        for value in node.values():
            yield from iter_checks(value)
    elif isinstance(node, list):
        for value in node:
            yield from iter_checks(value)


class ExpectedHashesTest(unittest.TestCase):
    def setUp(self):
        self.checks = {c["id"]: c for c in iter_checks(json.loads(INVARIANTS.read_text()))}

    def test_every_pinned_expectation_matches_the_repo_file(self):
        for check_id, rel in PINNED.items():
            with self.subTest(check=check_id):
                self.assertIn(check_id, self.checks, f"{check_id} 不在巡检清单里（被删了？）")
                match = re.search(r"want=\"?([0-9a-f]{8,64})", self.checks[check_id]["cmd"])
                self.assertIsNotNone(match, f"{check_id} 的 cmd 里取不到 want=")
                want = match.group(1)
                digest = hashlib.sha256((ROOT / rel).read_bytes()).hexdigest()
                self.assertEqual(
                    digest[: len(want)], want,
                    f"\n🔴 {rel} 变了但 {check_id} 的期望值没回写。"
                    f"\n   仓内实际 sha256 = {digest}"
                    f"\n   巡检 want       = {want}"
                    f"\n   改动者请在【同一次改动里】回写，不要留给发版当天。",
                )

    def test_registry_covers_every_repo_file_pin(self):
        """防止新增一条钉住仓内文件的巡检项、却忘了登记进 PINNED。"""
        for check in self.checks.values():
            cmd = check["cmd"]
            if "sha256sum /app/" not in cmd or "want=\"${" in cmd:
                continue
            self.assertIn(
                check["id"], PINNED,
                f"\n🔴 巡检项 {check['id']} 钉的是文件哈希，但没登记到 PINNED ——"
                f"\n   它将不被本测试保护，会退回『靠人记得回写』。",
            )


if __name__ == "__main__":
    unittest.main()
