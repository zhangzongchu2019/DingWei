#!/usr/bin/env python3
"""把「巡检期望值」与「仓内真实文件」焊在一起。

🔴 为什么要有这个测试（组长 2026-09-18 立的通则）：
    期望值/不变式的更新，**归属于使它失效的那次改动**，不归属于发版。
而在此之前它靠的是「有人记得在发版当天回写」——
`9239443c` 那次提交的标题自己写着「**我写下规矩的下一版就自己忘了**」。

⇒ 本测试让这件事不再依赖记性：**改了被钉住的文件而没回写期望值，这里就红。**
它替代不了发版当天的 ⑬ 闸（那条在真实产物上重算），它只是把**仓内**这一半提前到改动发生的那一刻。
"""
import json
import os
import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
INVARIANTS = ROOT / "deploy" / "invariants-dingwei-gray.json"

# 巡检项 id -> 仓内对应文件。只登记「期望值就是仓内某个文件哈希」的那几条；
# 期望值来自部署期环境变量的（如 workpulse 二进制）不在此列。
RETIRED = {
    "renderer-matches-anchor",
    "view-service-matches-anchor",
    "page-store-matches-anchor",
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


def reject_retired(checks):
    restored = RETIRED & checks.keys()
    if restored:
        raise AssertionError(f"硬编码断言被加回: {restored}")


class ExpectedHashesTest(unittest.TestCase):
    def setUp(self):
        self.checks = {c["id"]: c for c in iter_checks(json.loads(INVARIANTS.read_text()))}

    def test_hardcoded_repo_file_expectations_stay_retired(self):
        reject_retired(self.checks)

    def test_reverse_mutation_restoring_each_hardcoded_assertion_is_rejected(self):
        for retired in RETIRED:
            with self.subTest(retired=retired), self.assertRaisesRegex(AssertionError, "硬编码断言被加回"):
                reject_retired({**self.checks, retired: {"id": retired, "cmd": "true"}})

    def test_manifest_expectation_is_injected_and_fails_closed(self):
        check = self.checks["view-service-manifest-matches-release"]
        self.assertIn("${DW_EXPECT_VS_MANIFEST_SHA:?需在部署时写入期望的 view-service manifest 摘要}", check["cmd"])
        self.assertIn("sha256sum /app/vs/MANIFEST.sha256", check["cmd"])
        self.assertIn("取不到 /app/vs/MANIFEST.sha256(记为不成立)", check["cmd"])

    def test_manifest_expectation_has_positive_wrong_and_missing_poles(self):
        cmd = self.checks["view-service-manifest-matches-release"]["cmd"]
        with tempfile.TemporaryDirectory() as raw:
            manifest = pathlib.Path(raw) / "MANIFEST.sha256"
            manifest.write_text("digest  view-service/file\n")
            digest = __import__("hashlib").sha256(manifest.read_bytes()).hexdigest()
            probe = cmd.replace("/app/vs/MANIFEST.sha256", str(manifest))
            env = dict(os.environ, DW_EXPECT_VS_MANIFEST_SHA=digest)
            self.assertEqual(subprocess.run(["bash", "-c", probe], env=env, capture_output=True).returncode, 0)
            env["DW_EXPECT_VS_MANIFEST_SHA"] = "0" * 64
            self.assertNotEqual(subprocess.run(["bash", "-c", probe], env=env, capture_output=True).returncode, 0)
            manifest.unlink()
            missing = subprocess.run(["bash", "-c", probe], env=env, capture_output=True, text=True)
            self.assertNotEqual(missing.returncode, 0)
            self.assertIn("取不到", missing.stdout)


if __name__ == "__main__":
    unittest.main()
