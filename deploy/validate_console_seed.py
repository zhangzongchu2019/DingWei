#!/usr/bin/env python3
"""T-113 种子文件校验器 —— 在【导入之前】拦住会让控制台悄悄出错的写法。

🔴 为什么需要它:种子写错**不会报错**,只会让业主的左栏少几项或多几项 ——
   而"少了几项"他看不出来(他不知道本该有几项),"多了几项"是越权看到别人的会话。
   ⇒ **这一类错误没有任何运行期信号,只能在导入前拦。**

判据全部 fail-closed:任何一条不满足即退 2 并说明,不输出"警告"了事。

用法:
    validate_console_seed.py <seed.json> --expect-agents N [--owner-principal ID ...]

🔴 `--expect-agents` 与 `--owner-principal` 是**必填的外部预期** ——
   不从文件自身推导。理由:**判据不得取自被校验对象本身**,
   否则"文件说自己有 4 个 agent,于是校验它有 4 个 agent"是恒真的。
"""
from __future__ import annotations

import argparse
import json
import re
import sys

# 🔴 平台会话名形态 = <owner_key>-<短名>-<key 末 4 位>（入口自身的报错文案即此规则）。
#    要求全名等值,是为了挡住"截短的角色前缀"这一类:
#    实测 `LIKE 'zzc-prodops%'`(去掉尾码)会捞到额外行,其中**有 active=1 但已 5 天未动的**。
#    种子里若写成角色前缀,声明的就不是那条席位 —— 而且不会报错。
SESSION_RE = re.compile(r"^[a-z0-9]+-[A-Za-z0-9_.]+-[0-9a-f]{4}$")
PERMISSIONS = {"view", "interact"}
PLACEHOLDER = re.compile(r"REPLACE_WITH|CHANGEME|TODO|<.*>", re.I)


def fail(msg: str) -> None:
    print(f"SEED_INVALID: {msg}", file=sys.stderr)
    raise SystemExit(2)


# 🔴 必须与 cmd/workpulse/console_seed.go 的 consoleSeed 结构体保持一致。
#    生产解析器带 dec.DisallowUnknownFields() ⇒ **任何未知字段都会让 workpulse 启动失败**。
#    防漂移由 test_unknown_field_set_matches_production 钉住。
ALLOWED_TOP = {"principals", "agents"}
ALLOWED_PRINCIPAL = {"id", "provider", "provider_issuer", "provider_subject",
                     "display_name", "active"}
ALLOWED_AGENT = {"id", "key_id", "session_name", "owner_principal", "active", "grants"}
ALLOWED_GRANT = {"principal_id", "permission"}


def check_unknown_fields(seed) -> None:
    """🔴 本检查的立项缘由（2026-09-15，实测，不是设想）：

    组长把真实种子在本地跑了一次，workpulse **启动失败**：
        parse console seed: json: unknown field "_note"
    而**本校验器当时对同一份文件判 `SEED_OK` 且退 0**。

    ⇒ 运行单第 2 步写的是「须 SEED_OK 且退 0，非 0 立即停」
      ⇒ **照该流程，校验器会放行，然后第 4 步重启 workpulse 时现网起不来。**

    🔴 根因是**判据不排他**：本校验器此前的通过，
    **不能区分「这份文件能被生产解析器加载」与「这份文件会让服务崩」。**
    生产解析器带 `DisallowUnknownFields()`，而本校验器只按需取字段、对多余字段视而不见
    —— **两个解析器对同一份文件给出相反结论。**
    （同族：仓内曾长期存在两条构建路径，产物必然不同而不一致不报错。）
    """
    unknown_top = sorted(set(seed) - ALLOWED_TOP)
    if unknown_top:
        fail("顶层存在未知字段 %s —— 生产解析器带 DisallowUnknownFields(),"
             "**workpulse 会启动失败**。允许的顶层字段只有 %s。"
             "（注释/说明请写进运行单，不要写进种子文件。）"
             % (unknown_top, sorted(ALLOWED_TOP)))
    for i, pr in enumerate(seed.get("principals") or []):
        u = sorted(set(pr) - ALLOWED_PRINCIPAL)
        if u:
            fail("principals[%d] 存在未知字段 %s ⇒ workpulse 会启动失败" % (i, u))
    for i, ag in enumerate(seed.get("agents") or []):
        u = sorted(set(ag) - ALLOWED_AGENT)
        if u:
            fail("agents[%d] 存在未知字段 %s ⇒ workpulse 会启动失败" % (i, u))
        for j, g in enumerate(ag.get("grants") or []):
            u = sorted(set(g) - ALLOWED_GRANT)
            if u:
                fail("agents[%d].grants[%d] 存在未知字段 %s ⇒ workpulse 会启动失败" % (i, j, u))


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("seed")
    ap.add_argument("--expect-agents", type=int, required=True,
                    help="外部预期的 agent 条数(不从文件推导)")
    ap.add_argument("--owner-principal", action="append", required=True,
                    help="业主的 principal id;🔴 业主有几条身份就给几个,少给即判失败")
    args = ap.parse_args()

    try:
        seed = json.loads(open(args.seed, encoding="utf-8").read())
        
        # 🔴 第一道:未知字段 —— 它决定 workpulse 能不能【启动】,优先于一切语义检查
        check_unknown_fields(seed)
    except Exception as exc:                     # noqa: BLE001
        fail(f"无法解析 {args.seed}: {exc}")

    principals = seed.get("principals") or []
    agents = seed.get("agents") or []
    if not principals or not agents:
        fail("principals 与 agents 都不得为空")

    pids = [p.get("id") for p in principals]
    if len(set(pids)) != len(pids):
        fail(f"principal id 重复: {pids}")

    # ① 占位符必须已被替换 —— 样例文件原样导入是最容易发生的一种错
    blob = json.dumps(seed, ensure_ascii=False)
    if PLACEHOLDER.search(blob):
        fail("仍含占位符(REPLACE_WITH/TODO/<...>) —— 这是样例文件,不是部署文件")

    # ② active 必须显式;零值不得承担"未配置"
    for kind, rows in (("principal", principals), ("agent", agents)):
        for row in rows:
            if "active" not in row:
                fail(f"{kind} {row.get('id')!r} 缺 active —— 必须显式声明,不得省略")
            if not isinstance(row["active"], bool):
                fail(f"{kind} {row.get('id')!r} 的 active 必须是布尔")

    # ③ 🔴 每个 agent 必须按 (key_id, session_name) 逐条声明
    #    缺 session_name ⇒ 等于按 key 批量拉入;本平台一个 key 覆盖数百个会话
    seen: set[tuple[str, str]] = set()
    for a in agents:
        kid, sname = a.get("key_id"), a.get("session_name")
        if not kid or not sname:
            fail(f"agent {a.get('id')!r} 必须同时给 key_id 与 session_name —— "
                 f"只给 key_id 等于把该 key 下【全部】会话拉进来")
        if not SESSION_RE.match(sname):
            fail(f"agent {a.get('id')!r} 的 session_name {sname!r} 不是完整会话名 —— "
                 f"须为 <owner>-<短名>-<4位尾码> 全名;写成角色前缀会声明到另一条(或不存在的)席位,"
                 f"且不会报错")
        if (kid, sname) in seen:
            fail(f"(key_id, session_name) 重复: {sname}")
        seen.add((kid, sname))
        if a.get("owner_principal") not in pids:
            fail(f"agent {a.get('id')!r} 的 owner_principal 不在 principals 内")
        grants = a.get("grants") or []
        if not grants:
            fail(f"agent {a.get('id')!r} 无显式 grant")
        for g in grants:
            if g.get("principal_id") not in pids:
                fail(f"agent {a.get('id')!r} 的 grant 指向未声明的 principal")
            if g.get("permission") not in PERMISSIONS:
                fail(f"agent {a.get('id')!r} 的 permission 非法: {g.get('permission')!r}")

    # ④ 🔴 条数须等于【外部】预期 —— 防"少写一条没人看得出来"
    if len(agents) != args.expect_agents:
        fail(f"agent 条数 {len(agents)} ≠ 外部预期 {args.expect_agents} —— "
             f"少写一条不会有任何运行期信号")

    # ⑤ 🔴 业主每一条身份都必须出现,且都要有 grant
    #    业主是多条 open_id;只写一条 ⇒ 他从另一条登录时左栏是空的,而他不会知道
    granted = {g.get("principal_id") for a in agents for g in (a.get("grants") or [])}
    for owner in args.owner_principal:
        if owner not in pids:
            fail(f"业主 principal {owner!r} 未在 principals 内声明 —— "
                 f"业主有几条身份就必须声明几条")
        if owner not in granted:
            fail(f"业主 principal {owner!r} 没有任何 grant —— "
                 f"他从这条身份登录时左栏会是空的,而他不会知道少了什么")

    print(f"SEED_OK principals={len(principals)} agents={len(agents)} "
          f"owner_principals={len(args.owner_principal)}")


if __name__ == "__main__":
    main()
