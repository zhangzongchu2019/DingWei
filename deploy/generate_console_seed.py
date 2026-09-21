#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""生成控制台种子文件 —— 取代手抄。

为什么有这个工具(2026-09-15,写在这里免得将来有人以为它可有可无):
  组长手抄了一份「本组四席」的种子,而业主名下实际 20 条(含他本周新加的评审人)。
  按那份种子上线,业主会**少看到 16 项,且不会有任何报错** ——「少列是无声的」。
  根因不是数错,是**名单被写死在配方里,而名单会变**。
  同一天里,同一份单子的计数还出现过「标题写 20、量化行写 4」——
  成因是改的时候只改了记得的那几处。
  ⇒ 这两类错的共同解法不是「更仔细」,是**让名单与计数出自同一次查询**。

🔴 设计约束(来自 cmd/workpulse/console_seed.go 的原注释,不可违反):
  种子文件是**显式声明**,运行时**绝不**从 member/seen_person/key_id/名字前缀推导归属。
  ⇒ 本工具只在**生成时**查询;产出的文件仍是逐条显式声明。
  **变的是名单从哪来,不是文件长什么样。**

🔴 凭据纪律:
  · 业主身份用 **sha256 摘要前 16 位**指定(非敏感),由本工具在库中解析为字面值。
    ⇒ **字面身份值不出现在命令行、不出现在日志、不出现在仓库。**
  · 产出文件含身份字面值 ⇒ 调用方须自行 chmod 0600,且**不得入仓**。
"""
import argparse, hashlib, json, os, sqlite3, sys

def die(msg, code=3):
    print("🔴 %s" % msg, file=sys.stderr); sys.exit(code)

def digest(s):
    return hashlib.sha256(s.encode()).hexdigest()[:16]

def resolve_principal(conn, want_digest, issuer):
    """按摘要在 seen_person 中定位 open_id。要求【活跃渠道】且【唯一命中】。"""
    rows = conn.execute(
        "SELECT open_id, bot_channel_id FROM seen_person WHERE bot_channel_id=?", (issuer,)
    ).fetchall()
    hits = [r for r in rows if digest(r[0]) == want_digest]
    if len(hits) == 0:
        die("摘要 %s 在 issuer=%s 下无命中。\n"
            "   ⇒ 不要换一个像的继续 —— 这说明「业主是哪条身份」本身没对齐。" % (want_digest, issuer))
    if len(hits) > 1:
        die("摘要 %s 命中 %d 条,无法判别。fail-closed。" % (want_digest, len(hits)))
    return hits[0][0]

def resolve_principal_id(conn, subject, issuer):
    """以库内既有身份配对为准取 id；不得用命令行位置重新给真人编号。"""
    try:
        rows = conn.execute(
            "SELECT id FROM console_principal "
            "WHERE provider='feishu' AND provider_issuer=? AND provider_subject=?",
            (issuer, subject),
        ).fetchall()
    except sqlite3.Error as exc:
        die("无法读取 console_principal 既有身份配对: %s" % exc)
    if len(rows) == 0:
        die("subject 摘要 %s 在 console_principal 中无既有 id。\n"
            "   ⇒ 本工具不按参数位置发明 id;请先显式建立并审核 principal 身份配对。"
            % digest(subject))
    if len(rows) > 1:
        die("subject 摘要 %s 在 console_principal 中命中 %d 个 id,无法判别。fail-closed。"
            % (digest(subject), len(rows)))
    return rows[0][0]

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", required=True, help="workpulse.db(只读打开)")
    ap.add_argument("--key-id", required=True, help="🔴 归属按【列值】判,不按名字后缀")
    ap.add_argument("--owner-key", required=True)
    ap.add_argument("--issuer", required=True, help="= bot_channel_id,不是显示名/app_id")
    ap.add_argument("--principal-digest", action="append", required=True,
                    metavar="SHA256_16", help="业主身份的 sha256 前16位,可重复")
    ap.add_argument("--permission", default="interact")
    ap.add_argument("--management-principal", action="append", default=[],
                    help="生成结果中要显式授予 management 的 principal id;其余为 owner")
    ap.add_argument("--out", required=True)
    ap.add_argument("--expect-agents", type=int, default=None,
                    help="外部预期条数;给了就必须相等,否则退 2(不迁就现状)")
    a = ap.parse_args()

    if not os.path.exists(a.db):
        die("数据库不存在: %s" % a.db)
    conn = sqlite3.connect("file:%s?mode=ro" % a.db, uri=True)

    # ---- principals:摘要 -> subject -> 库内既有 id，顺序不得改变身份配对 ----
    principals = []
    for d in a.principal_digest:
        subj = resolve_principal(conn, d, a.issuer)
        pid = resolve_principal_id(conn, subj, a.issuer)
        principals.append({"id": pid, "provider": "feishu", "provider_issuer": a.issuer,
                           "provider_subject": subj, "display_name": "Owner", "active": True})
    principals.sort(key=lambda p: p["id"])

    # ---- agents:按【列值】取,不按名字 ----
    rows = conn.execute(
        "SELECT session_name FROM session_endpoint "
        "WHERE active=1 AND key_id=? AND owner_key=? ORDER BY session_name",
        (a.key_id, a.owner_key)).fetchall()
    names = [r[0] for r in rows]
    if not names:
        die("按 key_id=%s / owner_key=%s 查出 0 条。fail-closed:空名单不产出文件。"
            % (a.key_id, a.owner_key), 2)

    if a.expect_agents is not None and len(names) != a.expect_agents:
        die("查出 %d 条,而外部预期 %d 条。\n"
            "   ⇒ 名单在出单之后变过。**这是要重新判断的事实,不是待对齐的参数。**\n"
            "   不得自行调整计数。" % (len(names), a.expect_agents), 2)

    agents = [{"id": "ag-%s" % n, "key_id": a.key_id, "session_name": n,
               "owner_principal": principals[0]["id"], "active": True,
               "grants": [{"principal_id": p["id"], "permission": a.permission}
                          for p in principals]}          # 🔴 每个 agent 对【每条】身份各一条
              for n in names]

    # Archival authority is an explicit owner role scoped to the same
    # owner_key as the generated Agent inventory. It is not inferred from a
    # shared key_id, session-name prefix, or a particular principal ID.
    roles = [{"principal_id": p["id"], "role": "owner",
              "owner_key": a.owner_key, "active": True}
             for p in principals]

    principal_ids = {p["id"] for p in principals}
    unknown_management = sorted(set(a.management_principal) - principal_ids)
    if unknown_management:
        die("management principal 不在本次生成集合: %s" % unknown_management, 2)
    management = set(a.management_principal)
    platform_roles = [{"principal_id": p["id"],
                       "role": "management" if p["id"] in management else "owner",
                       "active": True}
                      for p in principals]

    with open(a.out, "w", encoding="utf-8") as fh:
        json.dump({"principals": principals, "roles": roles,
                   "platform_roles": platform_roles, "agents": agents}, fh,
                  ensure_ascii=False, indent=2)
        fh.write("\n")
    os.chmod(a.out, 0o600)

    # ---- 回执:只印摘要与计数,**不印任何字面身份值** ----
    print("SEED_GENERATED")
    print("  principals : %d  (%s)" % (len(principals),
          ", ".join("%s=%s" % (p["id"], digest(p["provider_subject"])) for p in principals)))
    print("  agents     : %d" % len(agents))
    print("  grants     : %d  (= %d × %d)" % (len(agents) * len(principals), len(agents), len(principals)))
    print("  owner roles: %d" % len(roles))
    print("  platform roles: %d (management=%d)" % (len(platform_roles), len(management)))
    print("  🔴 下一步用这个数:  --expect-agents %d   /   --expect-total %d"
          % (len(agents), len(agents)))
    print("  ⚠️ 产出含身份字面值,已置 0600;**不得入仓**。")
    return 0

if __name__ == "__main__":
    sys.exit(main())
