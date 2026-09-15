#!/usr/bin/env python3
"""只读预演:导入种子【之后】、业主登录【之前】,先看他会看到什么。

🔴 为什么需要它:没有它,验证方式只有"让业主自己登录一次" ——
   而他看到空白左栏时**并不知道本该有几项**,所以那不构成验证。
   本工具把"我们以为种子对了"变成"我们能把他将看到的列出来"。

🔴 判据取自【与生产同一条 SQL】,并有一条防漂移测试钉住它 ——
   若生产侧的查询改了而这里没跟,测试会红。
   (直接复制 SQL 会漂移;调用生产代码则不独立;故取"同 SQL ＋ 漂移即红"。)

用法:
    # 阳格:业主的每条身份都应看到 N 条,且各条身份看到的集合必须一致
    preview_console_for_principal.py <db> --principal <id> [--principal <id> ...] --expect-total N

    # 🔴 阴格:某身份【必须看到 0 条】—— 看到任何一条即失败
    preview_console_for_principal.py <db> --expect-empty --principal <id>

    # 🔴 排除核:某些身份【必须根本不在 console_principal 表里】(登录链在此即拒,不建会话)
    preview_console_for_principal.py <db> --principal <业主id> --expect-total 4 \
                                      --absent-principal <被排除id> [--absent-principal ...]

🔴 为什么要有 --expect-empty:本工具原先把"看到空"一律判为失败(rc=2)。
   而验收判据的**阴格**要求的恰恰是"看到空才算通过" ——
   照原语义,验收人得把 `PREVIEW_FAILED` 读成「阴格通过」。
   **一个需要反着读的判据,迟早会被读错**,故给阴格一个自己的正向语义。
"""
from __future__ import annotations

import argparse
import sqlite3
import sys

# 🔴 必须与 internal/store/console_auth.go 的 ListConsoleAgents 逐字一致。
#    防漂移由 test_preview_matches_production_sql 钉住。
LIST_SQL = (
    "SELECT DISTINCT a.id,a.key_id,a.session_name,a.owner_principal,a.active "
    "FROM console_agent a\n\t\tJOIN console_agent_grant g ON g.agent_id=a.id "
    "JOIN console_principal p ON p.id=g.principal_id\n\t\t"
    "WHERE p.id=? AND p.active=1 AND a.active=1 AND g.active=1 "
    "ORDER BY a.session_name,a.key_id"
)


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("db")
    ap.add_argument("--principal", action="append", required=True)
    ap.add_argument("--expect-total", type=int,
                    help="🔴 外部预期:每个 principal 应看到的条数(不从库推导)")
    ap.add_argument("--expect-empty", action="store_true",
                    help="🔴 阴格:这些 principal 必须看到 0 条;看到任何一条即失败")
    ap.add_argument("--absent-principal", action="append", default=[],
                    help="🔴 排除核:这些 principal 必须【不存在于 console_principal 表】")
    args = ap.parse_args()

    if args.expect_empty and args.expect_total is not None:
        print("🔴 --expect-empty 与 --expect-total 互斥:一个要求看到 0 条,"
              "一个要求看到 N 条,同时给出说明调用方没想清楚要验哪一格。", file=sys.stderr)
        raise SystemExit(3)

    con = sqlite3.connect(f"file:{args.db}?mode=ro", uri=True)
    rc = 0
    seen_sets = []
    for pid in args.principal:
        rows = con.execute(LIST_SQL, (pid,)).fetchall()
        names = [r[2] for r in rows]
        seen_sets.append(set(names))
        print(f"principal={pid} count={len(rows)}")
        for r in rows:
            print(f"    session={r[2]} key={r[1]} owner={r[3]}")
        if args.expect_empty:
            # 🔴 阴格:空 = 通过,非空 = 失败(与阳格语义相反,故单独一支,不与下面共用)
            if rows:
                print(f"    🔴 阴格失败:该身份【不该看到任何 Agent】,实际看到 {len(rows)} 条",
                      file=sys.stderr)
                rc = 2
            else:
                print(f"    ✅ 阴格通过:0 条")
        else:
            if not rows:
                print(f"    🔴 空 —— 该身份登录后左栏是空的", file=sys.stderr)
                rc = 2
            if args.expect_total is not None and len(rows) != args.expect_total:
                print(f"    🔴 条数 {len(rows)} ≠ 外部预期 {args.expect_total}", file=sys.stderr)
                rc = 2

    # 🔴 业主的多条身份必须看到**同一个全集** —— 否则他从某条登录会少看到,
    #    而他不会知道少了什么(v0.1 不做 principal 合并,靠双 grant 达成)。
    if (not args.expect_empty) and len(seen_sets) > 1 and any(s != seen_sets[0] for s in seen_sets[1:]):
        print("🔴 多条身份看到的集合【不一致】 —— 他从其中一条登录会少看到东西:",
              file=sys.stderr)
        for pid, s in zip(args.principal, seen_sets):
            print(f"    {pid}: {sorted(s)}", file=sys.stderr)
        rc = 2

    # 🔴 排除核:被排除的身份必须【根本不在表里】。
    #    登录链在 ResolveConsolePrincipal 未命中时即拒(不建 principal、不建会话),
    #    所以"不在表里"就等于"登不进来" —— 这一格因此【不需要真人去登一次】。
    for pid in args.absent_principal:
        n = con.execute("SELECT COUNT(*) FROM console_principal WHERE id=?", (pid,)).fetchone()[0]
        if n:
            print(f"🔴 排除核失败:principal={pid} 【存在于表中】({n} 行),"
                  f"它本应被显式排除 —— 该身份能登进控制台。", file=sys.stderr)
            rc = 2
        else:
            print(f"✅ 排除核通过:principal={pid} 不在 console_principal 表中(登录链会拒)")

    print("PREVIEW_OK" if rc == 0 else "PREVIEW_FAILED")
    raise SystemExit(rc)


if __name__ == "__main__":
    main()
