#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""generate_console_seed.py 的判别性测试。

🔴 每条都必须有【阴性对照】:工具"没报错"不等于"判据有效"。
   本组 2026-09-15 的教训:一个恒真的判据比没有判据更糟 —— 它给出虚假的放行。
"""
import hashlib, json, os, sqlite3, subprocess, sys, tempfile, unittest

HERE = os.path.dirname(os.path.abspath(__file__))
TOOL = os.path.join(HERE, "generate_console_seed.py")
OWNER_KEY, KEY_ID, ISSUER = "zzc", "FB-zzc-devteam-e0d12642", "greyTestRobot"
SUBJ_A, SUBJ_B, SUBJ_OTHER = "ou_aaa_owner_entry_a", "ou_bbb_owner_entry_b", "ou_ccc_someone_else"

def dg(s): return hashlib.sha256(s.encode()).hexdigest()[:16]

def make_db(path, agents, persons):
    c = sqlite3.connect(path)
    c.execute("CREATE TABLE session_endpoint(session_name TEXT, key_id TEXT, owner_key TEXT, active INT)")
    c.executemany("INSERT INTO session_endpoint VALUES(?,?,?,?)", agents)
    c.execute("CREATE TABLE seen_person(open_id TEXT, bot_channel_id TEXT)")
    c.executemany("INSERT INTO seen_person VALUES(?,?)", persons)
    c.commit(); c.close()

class T(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp(); self.db = os.path.join(self.d, "w.db")
        self.out = os.path.join(self.d, "seed.json")
        make_db(self.db, [
            ("zzc-opus50ts-2642",  KEY_ID, OWNER_KEY, 1),
            ("zzc-prodops-2642",   KEY_ID, OWNER_KEY, 1),
            ("zzc-stale-2642",     KEY_ID, OWNER_KEY, 1),   # 已停 6 天但 active=1 ⇒ 应包含(授权≠存活)
            ("zzc-inactive-2642",  KEY_ID, OWNER_KEY, 0),   # active=0 ⇒ 应排除
            ("systemtaskintl-secops71-7094", "FB-sec-ops-local", "systemtaskintl", 1),  # 别人的 ⇒ 排除
            # 🔴 排他性夹具:key_id【正确】但 owner_key【不对】——
            #    只有 owner_key 这一条判据能排除它。
            #    2026-09-15 变异实测发现:缺了这一行,拆掉 owner_key 判据后 test_1 仍绿
            #    ⇒ 夹具分不清「owner_key 起作用」与「key_id 起作用」(两层冗余,只拆一层)。
            ("zzc-wrongowner-2642", KEY_ID, "someone_else", 1),
        ], [
            (SUBJ_A, ISSUER), (SUBJ_B, ISSUER),
            (SUBJ_B, "unifiedrobot"),      # 🔴 同一人在【停用渠道】下的入口 ⇒ 不得被选中
            (SUBJ_OTHER, ISSUER),
        ])

    def run_tool(self, *extra, digests=(SUBJ_A, SUBJ_B), expect=None):
        cmd = [sys.executable, TOOL, "--db", self.db, "--key-id", KEY_ID,
               "--owner-key", OWNER_KEY, "--issuer", ISSUER, "--out", self.out]
        for s in digests: cmd += ["--principal-digest", dg(s)]
        if expect is not None: cmd += ["--expect-agents", str(expect)]
        cmd += list(extra)
        p = subprocess.run(cmd, capture_output=True, text=True)
        return p.returncode, p.stdout + p.stderr

    # ---------- 阳性对照 ----------
    def test_1_happy_path(self):
        rc, out = self.run_tool(expect=3)
        self.assertEqual(rc, 0, out)
        d = json.load(open(self.out))
        names = [a["session_name"] for a in d["agents"]]
        self.assertEqual(names, ["zzc-opus50ts-2642", "zzc-prodops-2642", "zzc-stale-2642"])
        self.assertNotIn("systemtaskintl-secops71-7094", names)   # 按列值排除,非按后缀
        self.assertNotIn("zzc-wrongowner-2642", names)            # 🔴 只有 owner_key 能排除它
        self.assertNotIn("zzc-inactive-2642", names)
        self.assertEqual(len(d["principals"]), 2)
        # 🔴 每个 agent 对【每条】身份各一条 —— 少一条则业主用另一身份登录时看不到它
        for a in d["agents"]:
            self.assertEqual(sorted(g["principal_id"] for g in a["grants"]),
                             ["owner-entry-a", "owner-entry-b"])
        self.assertEqual(d["roles"], [
            {"principal_id": "owner-entry-a", "role": "owner", "owner_key": OWNER_KEY, "active": True},
            {"principal_id": "owner-entry-b", "role": "owner", "owner_key": OWNER_KEY, "active": True},
        ])

    def test_2_schema_matches_go_struct(self):
        """🔴 生产解析器带 DisallowUnknownFields():多一个键就起不来。"""
        self.assertEqual(self.run_tool(expect=3)[0], 0)
        d = json.load(open(self.out))
        self.assertEqual(set(d.keys()), {"principals", "roles", "agents"})
        for p in d["principals"]:
            self.assertEqual(set(p), {"id","provider","provider_issuer","provider_subject","display_name","active"})
        for a in d["agents"]:
            self.assertEqual(set(a), {"id","key_id","session_name","owner_principal","active","grants"})
            for g in a["grants"]:
                self.assertEqual(set(g), {"principal_id","permission"})
        for role in d["roles"]:
            self.assertEqual(set(role), {"principal_id", "role", "owner_key", "active"})

    def test_10_every_owner_identity_gets_explicit_scoped_role(self):
        self.assertEqual(self.run_tool(expect=3)[0], 0)
        d = json.load(open(self.out))
        self.assertEqual(
            sorted((r["principal_id"], r["role"], r["owner_key"], r["active"])
                   for r in d["roles"]),
            [("owner-entry-a", "owner", OWNER_KEY, True),
             ("owner-entry-b", "owner", OWNER_KEY, True)],
        )

    def test_3_file_is_0600(self):
        self.assertEqual(self.run_tool(expect=3)[0], 0)
        self.assertEqual(oct(os.stat(self.out).st_mode & 0o777), oct(0o600))

    def test_4_no_literal_identity_on_stdout(self):
        """🔴 回执只许出现摘要,不许出现字面身份值。"""
        rc, out = self.run_tool(expect=3)
        self.assertEqual(rc, 0)
        for subj in (SUBJ_A, SUBJ_B):
            self.assertNotIn(subj, out)
            self.assertIn(dg(subj), out)          # 阳性:摘要在

    # ---------- 阴性对照 ----------
    def test_5_count_mismatch_blocks(self):
        """名单变了就停,不迁就现状。"""
        rc, out = self.run_tool(expect=99)
        self.assertEqual(rc, 2)
        self.assertIn("不得自行调整计数", out)
        self.assertFalse(os.path.exists(self.out))   # 🔴 拦下时不得留半成品

    def test_6_unknown_digest_blocks(self):
        rc, out = self.run_tool(digests=(SUBJ_A, "ou_not_in_db"))
        self.assertEqual(rc, 3)
        self.assertIn("无命中", out)

    def test_7_inactive_channel_identity_not_picked(self):
        """🔴 SUBJ_B 在 unifiedrobot(停用)下也有一条;按 issuer 过滤后不得命中它。"""
        rc, out = self.run_tool(expect=3)
        self.assertEqual(rc, 0, out)
        d = json.load(open(self.out))
        self.assertEqual(len(d["principals"]), 2)     # 不因重复入口而变成 3 条

    def test_8_empty_roster_blocks(self):
        rc, out = self.run_tool("--owner-key", "nobody", expect=None)
        self.assertEqual(rc, 2)
        self.assertIn("空名单不产出文件", out)

    def test_9_ambiguous_digest_blocks(self):
        """同一摘要在同一 issuer 下命中多条 ⇒ fail-closed,不许挑一个。"""
        c = sqlite3.connect(self.db)
        c.execute("INSERT INTO seen_person VALUES(?,?)", (SUBJ_A, ISSUER))  # 制造重复
        c.commit(); c.close()
        rc, out = self.run_tool()
        self.assertEqual(rc, 3)
        self.assertIn("无法判别", out)

if __name__ == "__main__":
    unittest.main(verbosity=2)
