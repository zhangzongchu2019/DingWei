#!/usr/bin/env python3
"""预演工具的两类测试:① 行为 ② 🔴 防漂移(SQL 必须与生产侧逐字同)。"""
from __future__ import annotations

import os
import re
import sqlite3
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
TOOL = os.path.join(HERE, "preview_console_for_principal.py")
PROD_GO = os.path.join(ROOT, "internal", "store", "console_auth.go")

SCHEMA = """
CREATE TABLE console_principal(id TEXT PRIMARY KEY, provider TEXT, provider_issuer TEXT,
  provider_subject TEXT, display_name TEXT, active INTEGER);
CREATE TABLE console_agent(id TEXT PRIMARY KEY, key_id TEXT, session_name TEXT,
  owner_principal TEXT, active INTEGER);
CREATE TABLE console_agent_grant(principal_id TEXT, agent_id TEXT, permission TEXT, active INTEGER);
"""


def make_db(grants_for_b=True):
    path = os.path.join(tempfile.mkdtemp(prefix="prev-"), "w.db")
    con = sqlite3.connect(path)
    con.executescript(SCHEMA)
    for pid in ("owner-a", "owner-b"):
        con.execute("INSERT INTO console_principal VALUES(?,?,?,?,?,1)",
                    (pid, "feishu", "app", pid, "Owner"))
    for aid, sname in (("a1", "s-one"), ("a2", "s-two")):
        con.execute("INSERT INTO console_agent VALUES(?,?,?,?,1)", (aid, "K", sname, "owner-a"))
        con.execute("INSERT INTO console_agent_grant VALUES(?,?,?,1)", ("owner-a", aid, "interact"))
        if grants_for_b:
            con.execute("INSERT INTO console_agent_grant VALUES(?,?,?,1)", ("owner-b", aid, "interact"))
    con.commit()
    con.close()
    return path


def run(db, principals, expect=None, expect_empty=False, absent=()):
    cmd = [sys.executable, TOOL, db]
    for p in principals:
        cmd += ["--principal", p]
    if expect is not None:
        cmd += ["--expect-total", str(expect)]
    if expect_empty:
        cmd += ["--expect-empty"]
    for a in absent:
        cmd += ["--absent-principal", a]
    return subprocess.run(cmd, capture_output=True, text=True, timeout=30)


class PreviewTest(unittest.TestCase):

    def test_1_both_identities_see_the_same_full_set(self):
        r = run(make_db(True), ["owner-a", "owner-b"], expect=2)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("PREVIEW_OK", r.stdout)

    def test_2_second_identity_missing_grants_is_caught(self):
        """🔴 他从第二条身份登录会少看到 —— 而他不会知道。预演必须当场判死。"""
        r = run(make_db(False), ["owner-a", "owner-b"], expect=2)
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertIn("空", r.stderr)
        self.assertIn("不一致", r.stderr)

    def test_3_count_below_external_expectation_is_caught(self):
        r = run(make_db(True), ["owner-a"], expect=3)
        self.assertEqual(r.returncode, 2)
        self.assertIn("≠ 外部预期", r.stderr)

    def test_4_preview_sql_matches_production_verbatim(self):
        """🔴 防漂移:生产侧改了查询而这里没跟 ⇒ 预演会"验证"另一个东西。"""
        go = open(PROD_GO, encoding="utf-8").read()
        m = re.search(r"func \(s \*SQLite\) ListConsoleAgents.*?QueryContext\(ctx, `(.*?)`",
                      go, re.S)
        self.assertIsNotNone(m, "没能从生产代码里取出那条 SQL —— 取法本身失效了")
        prod = m.group(1)
        tool = open(TOOL, encoding="utf-8").read()
        m2 = re.search(r"LIST_SQL = \((.*?)\n\)", tool, re.S)
        self.assertIsNotNone(m2)
        mine = "".join(re.findall(r'"(.*?)"', m2.group(1))).encode().decode("unicode_escape")
        self.assertEqual(" ".join(mine.split()), " ".join(prod.split()),
                         "🔴 预演 SQL 与生产侧已漂移 —— 预演将验证另一个查询")


    # ── 🔴 阴格模式(2026-09-15 补) ─────────────────────────────────────────
    # 立项理由:本工具原先把"看到空"一律判失败,而验收判据的**阴格**要求的恰是
    # "看到空才算通过" ⇒ 验收人得把 PREVIEW_FAILED 读成「阴格通过」。
    # **需要反着读的判据迟早会被读错**,故给阴格一个自己的正向语义,并用下面两条钉住它。

    def test_5_expect_empty_passes_when_principal_sees_nothing(self):
        """阳性对照:无 grant 的身份在 --expect-empty 下必须【通过】。"""
        db = make_db(grants_for_b=False)
        r = run(db, ["owner-b"], expect_empty=True)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("PREVIEW_OK", r.stdout)
        self.assertIn("阴格通过", r.stdout)

    def test_6_expect_empty_fails_when_principal_sees_anything(self):
        """🔴 阴性对照:看得见 Agent 的身份在 --expect-empty 下必须【失败】。

        这一条是本模式的判别力所在 —— 若它也通过,说明 --expect-empty 恒真,
        那么"阴格已验"这句话在任何数据下都成立,等于没验。
        """
        db = make_db(grants_for_b=True)
        r = run(db, ["owner-a"], expect_empty=True)
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertIn("PREVIEW_FAILED", r.stdout)
        self.assertIn("不该看到任何 Agent", r.stderr)

    # ── 🔴 排除核(2026-09-15 补) ───────────────────────────────────────────
    # 登录链在 ResolveConsolePrincipal 未命中时即拒(不建 principal、不建会话),
    # 所以"不在表里"＝"登不进来" ⇒ 该格**不需要真人去登一次**。

    def test_7_absent_principal_passes_when_truly_absent(self):
        r = run(make_db(True), ["owner-a"], expect=2, absent=["ghost-not-seeded"])
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("排除核通过", r.stdout)

    def test_8_absent_principal_fails_when_present(self):
        """🔴 阴性对照:本该被排除的身份若在表里,必须判死 —— 它能登进控制台。"""
        r = run(make_db(True), ["owner-a"], expect=2, absent=["owner-b"])
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertIn("排除核失败", r.stderr)

    def test_9_expect_empty_and_expect_total_are_mutually_exclusive(self):
        """两个 flag 同给 ⇒ 退 3(用法错),🔴 且必须与"闸拦下"的 2 区分开。"""
        db = make_db(True)
        cmd = [sys.executable, TOOL, db, "--principal", "owner-a",
               "--expect-empty", "--expect-total", "2"]
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        self.assertEqual(r.returncode, 3, r.stdout + r.stderr)
        self.assertNotIn("PREVIEW_OK", r.stdout)


if __name__ == "__main__":
    unittest.main()
