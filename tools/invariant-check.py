#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""不变量巡检器 —— 把"出事后才做的对照"变成"每天自动做一遍"。

为什么有它（2026-09-15 一天的代价，写在这里免得将来有人以为它可有可无）:
  · 未鉴权的终端事件流**开着很久**,没有任何告警;
  · 现网 renderer 与 view_service.py **不是发版锚那一份**,组长花一下午在
    **现网根本没在跑的代码**里找原因 —— 而一条"三文件哈希 == 锚"的断言 5 秒就能答;
  · hub 自身版本号一度不可解析,**enforce 模式下会挡住所有客户端** —— 它没爆只因为开关没开。
⇒ 这些都不是"出了错",是**现实慢慢偏离了意图,而没有任何东西定期重新问一遍**。

🔴 三条设计硬线（缺一即退化为"看起来在巡检"）:
  1. **fail-closed**: 命令失败/超时/不可判定 ⇒ 记为【不成立】,**绝不记为通过**。
     "查不了" != "没问题"。
  2. **心跳**: 输出**最后一次成功巡检的时刻**;`--max-age` 超时即整体失败。
     **一个静静死掉的巡检器比没有更糟** —— 它让人以为"没告警就是没事"。
  3. **阳性对照**: 断言集里**必须**含至少一条【故意为假】的哨兵(sentinel),
     它若"通过"了,说明巡检器本身坏了 ⇒ 整体判失败。
"""
import argparse, json, os, subprocess, sys, time

SENTINEL_ID = "sentinel-must-fail"

def ack_state(a, today):
    """已知缺陷的静默 —— 🔴 **必须带期限**。

    为什么要这个:某些断言在根因修好之前会**永远红**(例:开关只存在于容器创建参数,
    要修得先解决 T-112 镜像来源)。**一条永远红的断言会训练所有人无视整个巡检器** ——
    那比没有巡检器更糟。
    🔴 但"暂时忽略"极易变成"永远忘记",所以静默**必须有到期日**:
       · 未到期 ⇒ 记为【已知缺陷】,不拖垮整体,但**每轮都打印出来**(不许隐身)
       · 一旦到期 ⇒ **自动恢复为真失败**,而且**比普通失败更响**(它已经拖了太久)
    返回 "": 不适用 / "acked": 静默中 / "expired": 静默已到期
    """
    until = a.get("expected_fail_until")
    if not until:
        return ""
    return "acked" if today <= str(until) else "expired"


def run_one(a, timeout):
    """跑一条断言。返回 (ok, detail)。🔴 任何异常都归为不成立。"""
    try:
        # 🔴 显式用 bash:断言里会用到 ${v:0:8} 之类 bash 语法,
        #    而 shell=True 默认走 /bin/sh(dash) ⇒ "Bad substitution"。
        #    实测教训(2026-09-16):首版因此让一条哈希断言【两个方向都失败】——
        #    哈希对上也红。**它看起来在工作,其实恒红,等于没有这条断言。**
        p = subprocess.run(a["cmd"], shell=True, executable="/bin/bash",
                           capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        return False, "超时 %ss —— **超时不是通过**" % timeout
    except Exception as exc:                      # noqa: BLE001
        return False, "无法执行: %s" % exc
    if p.returncode != 0:
        tail = (p.stderr or p.stdout or "").strip().splitlines()
        return False, "退码=%d %s" % (p.returncode, tail[-1][:120] if tail else "")
    want = a.get("expect_stdout")
    if want is not None and want not in p.stdout:
        return False, "输出不含期望片段 %r" % want[:40]
    return True, "ok"

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--config", required=True, help="断言集(JSON)")
    ap.add_argument("--state", help="心跳状态文件;记录最后一次【全部成立】的时刻")
    ap.add_argument("--max-age", type=int, default=0,
                    help="秒;仅与 --heartbeat-only 合用:上次成功巡检超过该时长即失败")
    ap.add_argument("--heartbeat-only", action="store_true",
                    help="只问「巡检器还在不在跑」,不跑断言;消费方用它发现巡检器自己死了")
    ap.add_argument("--timeout", type=int, default=20, help="单条断言超时秒数")
    ap.add_argument("--json", action="store_true", help="输出机器可读 JSON")
    a = ap.parse_args()

    # 🔴 两件事必须分开问,混在一个退出码里会互相掩盖:
    #    ① 这一轮的结论  —— 断言成不成立(正常模式)
    #    ② 巡检器还在跑吗 —— 心跳新不新(--heartbeat-only)
    #    实测教训(2026-09-16):首版把二者揉在一起 ⇒ **第一次运行必然报「从未成功过」**,
    #    哪怕这一轮全部成立;而组长的 test_9 把该错误行为写成了断言,**测试锁死了 bug**。
    if a.heartbeat_only:
        if not a.state or not a.max_age:
            print("🔴 --heartbeat-only 需同时给 --state 与 --max-age"); return 3
        try:
            with open(a.state, encoding="utf-8") as fh:
                last = int(json.load(fh).get("last_ok", 0))
        except Exception:                          # noqa: BLE001
            last = 0
        if not last:
            print("🔴 巡检器**从未成功过**(无心跳) ⇒ 不得视为「没问题」"); return 2
        age = int(time.time()) - last
        if age > a.max_age:
            print("🔴 巡检**心跳过期**:距上次成功 %ds(阈值 %ds) ⇒ **时间戳不动本身就是告警**"
                  % (age, a.max_age)); return 2
        print("✅ 巡检器在跑:距上次成功 %ds" % age); return 0

    try:
        with open(a.config, encoding="utf-8") as fh:
            cfg = json.load(fh)
    except Exception as exc:                      # noqa: BLE001
        print("🔴 读不到断言集 %s (%s) ⇒ **本次巡检无法判定,记为失败**" % (a.config, exc))
        return 3
    asserts = cfg.get("assertions", [])
    if not asserts:
        print("🔴 断言集为空 ⇒ **空巡检不算通过**")
        return 3

    today = time.strftime("%Y-%m-%d", time.gmtime())
    results, failed, acked, expired = [], [], [], []
    for item in asserts:
        ok, detail = run_one(item, a.timeout)
        st = ack_state(item, today)
        rec = {"id": item["id"], "desc": item.get("desc", ""), "ok": ok, "detail": detail}
        if not ok and st == "acked":
            rec["acked_until"] = item["expected_fail_until"]
            rec["ack_reason"] = item.get("ack_reason", "")
            acked.append(item["id"])
        elif not ok and st == "expired":
            rec["ack_expired"] = item["expected_fail_until"]
            expired.append(item["id"]); failed.append(item["id"])
        elif not ok:
            failed.append(item["id"])
        # 🔴 静默期内【意外通过】也要说 —— 说明根因可能已修好,该把静默撤掉
        if ok and st in ("acked", "expired"):
            rec["ack_stale"] = True
        results.append(rec)

    # 🔴 阳性对照:哨兵【必须失败】。它若通过 ⇒ 巡检器本身坏了。
    sent = [r for r in results if r["id"] == SENTINEL_ID]
    if not sent:
        print("🔴 断言集缺少哨兵 %s ⇒ **无法证明巡检器本身有判别力**,记为失败" % SENTINEL_ID)
        return 3
    if sent[0]["ok"]:
        print("🔴 **哨兵通过了 —— 巡检器本身失效**(它连一条故意为假的断言都判成了成立)。")
        return 3
    failed = [f for f in failed if f != SENTINEL_ID]     # 哨兵失败是预期,不计入
    real = [r for r in results if r["id"] != SENTINEL_ID]

    now = int(time.time())
    # 🔵 正常模式【不】判心跳:这一轮刚跑完,心跳由本轮刷新。
    #    "巡检器是否还在跑"由消费方用 --heartbeat-only 单独问。
    stale = None

    if not failed and a.state:
        try:
            with open(a.state, "w", encoding="utf-8") as fh:
                json.dump({"last_ok": now, "count": len(real)}, fh)
        except Exception as exc:                   # noqa: BLE001
            print("⚠️ 心跳写入失败: %s" % exc)

    if a.json:
        print(json.dumps({"ok": not failed and stale is None, "failed": failed,
                          "acked": acked, "ack_expired": expired,
                          "stale_seconds": stale, "checked": len(real),
                          "results": real}, ensure_ascii=False))
    else:
        if stale is not None:
            print("🔴 巡检**心跳过期**:%s ⇒ **时间戳不动本身就是告警**"
                  % ("从未成功过" if stale < 0 else "距上次成功 %ds" % stale))
        if failed:
            print("🔴 当前有 %d 条断言**不成立**:" % len(failed))
            for r in real:
                if not r["ok"]:
                    print("     [%s] %s —— %s" % (r["id"], r["desc"], r["detail"]))
        else:
            print("✅ %d 条断言全部成立（哨兵已正确判失败，巡检器有判别力）" % len(real))
        for r in real:
            if r.get("acked_until"):
                print("   🟡 已知缺陷（静默至 %s）:[%s] %s —— %s"
                      % (r["acked_until"], r["id"], r["desc"], r.get("ack_reason", "")))
            if r.get("ack_expired"):
                print("   🔴🔴 **静默已于 %s 到期而问题仍在**:[%s] —— 它已经拖了太久,不能再缓"
                      % (r["ack_expired"], r["id"]))
            if r.get("ack_stale"):
                print("   🔵 [%s] 已通过,但仍挂着静默 ⇒ **请撤掉 expected_fail_until**"
                      % r["id"])
    return 0 if (not failed and stale is None) else 2

if __name__ == "__main__":
    sys.exit(main())
