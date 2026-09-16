#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""invariant-check.py 的判别性测试。
🔴 重点不是"它能不能跑",是"它会不会把【不该通过的】判成通过"。
"""
import json, os, subprocess, sys, tempfile, unittest
HERE = os.path.dirname(os.path.abspath(__file__))
TOOL = os.path.join(HERE, "invariant-check.py")

def cfg(*items):
    fd, p = tempfile.mkstemp(suffix=".json"); os.close(fd)
    with open(p, "w", encoding="utf-8") as fh:
        json.dump({"assertions": list(items)}, fh)
    return p

SENT = {"id": "sentinel-must-fail", "desc": "哨兵", "cmd": "false"}
OK   = {"id": "a-ok",   "desc": "恒真", "cmd": "true"}
BAD  = {"id": "a-bad",  "desc": "恒假", "cmd": "false"}

def run(conf, *extra):
    p = subprocess.run([sys.executable, TOOL, "--config", conf, *extra],
                       capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr

class T(unittest.TestCase):
    def test_1_all_pass(self):
        rc, out = run(cfg(SENT, OK))
        self.assertEqual(rc, 0, out)
        self.assertIn("全部成立", out)

    def test_2_one_fails_blocks(self):
        rc, out = run(cfg(SENT, OK, BAD))
        self.assertEqual(rc, 2, out)
        self.assertIn("a-bad", out)

    # ---------- 🔴 巡检器自身的失效,下面这些才是重点 ----------
    def test_3_missing_sentinel_is_failure(self):
        """没有哨兵 ⇒ 无法证明有判别力 ⇒ 不得判通过。"""
        rc, out = run(cfg(OK))
        self.assertEqual(rc, 3, out)
        self.assertIn("缺少哨兵", out)

    def test_4_sentinel_passing_means_checker_broken(self):
        """🔴 哨兵若通过 ⇒ 巡检器坏了 ⇒ 整体必须失败(而不是报"全绿")。"""
        rc, out = run(cfg({"id": "sentinel-must-fail", "desc": "被改成恒真的哨兵", "cmd": "true"}, OK))
        self.assertEqual(rc, 3, out)
        self.assertIn("巡检器本身失效", out)

    def test_5_empty_config_is_failure(self):
        """空断言集不算通过 —— 否则"删光断言"就能让巡检永远绿。
        🔴 排他:必须断言**是被"断言集为空"拦下的**,不是被"缺哨兵"顺带拦下的。
        变异实测:首版只断言 rc==3,把"空集检查"拆掉后仍绿(实为缺哨兵那条在起作用)。"""
        rc, out = run(cfg())
        self.assertEqual(rc, 3, out)
        self.assertIn("断言集为空", out)

    def test_6_unreadable_config_is_failure(self):
        """🔴 读不到配置 = 查不了,不是没问题。"""
        rc, out = run("/nonexistent/x.json")
        self.assertEqual(rc, 3, out)
        self.assertIn("无法判定", out)

    def test_7_timeout_is_failure_not_pass(self):
        """🔴 超时必须记为不成立。"""
        rc, out = run(cfg(SENT, {"id": "slow", "desc": "卡住", "cmd": "sleep 5"}), "--timeout", "1")
        self.assertEqual(rc, 2, out)
        self.assertIn("超时", out)

    def test_8_crashing_command_is_failure(self):
        """命令不存在(shell 退 127)必须记为不成立。"""
        rc, out = run(cfg(SENT, {"id": "boom", "desc": "命令不存在", "cmd": "definitely-not-a-command-xyz"}))
        self.assertEqual(rc, 2, out)

    def test_8b_subprocess_exception_is_failure(self):
        """🔴 排他:上一条走的是"退码非 0",**不经过异常分支**。
        变异实测:把 `except Exception -> return True` 之后 test_8 仍绿 ——
        因为 shell 把"命令不存在"变成了退码 127,Python 从不抛异常。
        本例传一个**非字符串 cmd**,强制 subprocess 抛异常,专测那条分支。"""
        rc, out = run(cfg(SENT, {"id": "boom2", "desc": "非法 cmd 类型", "cmd": 12345}))
        self.assertEqual(rc, 2, out)
        self.assertIn("boom2", out)

    # ---------- 心跳 ----------
    def test_9_first_run_all_pass_is_ok_not_stale(self):
        """🔴 本用例是【更正】:首版断言"首次运行必须报过期",**那是把 bug 写成了断言**。
        这一轮明明全部成立,却因"先判过期、后写心跳"而失败 —— 第一次运行必然报错。
        正确语义:正常模式只回答"这一轮结论",心跳由本轮刷新。"""
        fd, st = tempfile.mkstemp(); os.close(fd); os.unlink(st)
        rc, out = run(cfg(SENT, OK), "--state", st, "--max-age", "60")
        self.assertEqual(rc, 0, out)
        self.assertTrue(os.path.exists(st), "成功一轮必须写下心跳")

    def test_9b_heartbeat_only_never_ran_is_failure(self):
        """🔴 "巡检器还在不在跑"由消费方单独问;从未成功过 ⇒ 不得视为没问题。"""
        fd, st = tempfile.mkstemp(); os.close(fd); os.unlink(st)
        rc, out = run(cfg(SENT, OK), "--state", st, "--max-age", "60", "--heartbeat-only")
        self.assertEqual(rc, 2, out)
        self.assertIn("从未成功过", out)

    def test_10_heartbeat_written_then_fresh(self):
        fd, st = tempfile.mkstemp(); os.close(fd); os.unlink(st)
        run(cfg(SENT, OK), "--state", st)                 # 先写一次心跳
        self.assertTrue(os.path.exists(st))
        rc, out = run(cfg(SENT, OK), "--state", st, "--max-age", "3600", "--heartbeat-only")
        self.assertEqual(rc, 0, out)

    def test_11_stale_heartbeat_blocks(self):
        """🔴 心跳过期即告警 —— 一个静静死掉的巡检器比没有更糟。
        🔴 排他:必须用 --heartbeat-only 问,否则会与"断言结论"互相掩盖。"""
        fd, st = tempfile.mkstemp(); os.close(fd)
        with open(st, "w", encoding="utf-8") as fh:
            json.dump({"last_ok": 1}, fh)                 # 1970 年
        rc, out = run(cfg(SENT, OK), "--state", st, "--max-age", "60", "--heartbeat-only")
        self.assertEqual(rc, 2, out)
        self.assertIn("心跳过期", out)

    def test_12_expect_stdout_is_checked(self):
        rc, _ = run(cfg(SENT, {"id": "s", "desc": "输出不符", "cmd": "echo hello", "expect_stdout": "world"}))
        self.assertEqual(rc, 2)

    # ---------- 🔴 已知缺陷的静默:必须带期限,且到期自动变响 ----------
    def test_13_acked_failure_does_not_block(self):
        """静默期内的已知缺陷:不拖垮整体,但**必须仍被打印**(不许隐身)。"""
        rc, out = run(cfg(SENT, {"id": "known", "desc": "已知缺陷", "cmd": "false",
                                 "expected_fail_until": "2099-01-01",
                                 "ack_reason": "等 T-112"}))
        self.assertEqual(rc, 0, out)
        self.assertIn("已知缺陷", out)
        self.assertIn("known", out)          # 🔴 静默 != 隐身

    def test_14_expired_ack_blocks_loudly(self):
        """🔴 静默一旦到期,必须自动恢复为真失败 —— 否则"暂时忽略"就成了"永远忘记"。"""
        rc, out = run(cfg(SENT, {"id": "overdue", "desc": "拖太久的已知缺陷", "cmd": "false",
                                 "expected_fail_until": "2000-01-01"}))
        self.assertEqual(rc, 2, out)
        self.assertIn("到期", out)
        self.assertIn("overdue", out)

    def test_15_ack_on_passing_assertion_is_flagged(self):
        """🔵 静默挂着但断言已通过 ⇒ 提醒撤掉,防止静默无限期躺着。"""
        rc, out = run(cfg(SENT, {"id": "fixed", "desc": "其实已修好", "cmd": "true",
                                 "expected_fail_until": "2099-01-01"}))
        self.assertEqual(rc, 0, out)
        self.assertIn("请撤掉", out)

    def test_16_ack_does_not_silence_sentinel(self):
        """🔴 排他:不得用 expected_fail_until 把【哨兵】静默掉 ——
        那等于关掉巡检器的自检。哨兵失败本就是预期,静默它毫无意义;
        但若有人给哨兵加了静默并改成恒真,整体仍须失败。"""
        rc, out = run(cfg({"id": "sentinel-must-fail", "desc": "被静默且被改成恒真的哨兵",
                           "cmd": "true", "expected_fail_until": "2099-01-01"},
                          {"id": "x", "desc": "ok", "cmd": "true"}))
        self.assertEqual(rc, 3, out)
        self.assertIn("巡检器本身失效", out)

if __name__ == "__main__":
    unittest.main(verbosity=2)
