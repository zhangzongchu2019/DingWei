#!/usr/bin/env python3
"""T18 D5 迁移导入(设计冻结版 §5 双写窗):现有散置账 → datad VS API。

三类迁入物(§5):
  ①coordinator 手工台账 → PUT /v1/tasks(+POST /v1/dispatch)
  ②hb.py/各会话心跳 state.json → POST /v1/sessions/{me}/heartbeat + PUT state
  ③在线清单 → PUT /v1/sessions/{name}/state

设计铁律:datad=唯一入口,本脚本走 API 不直连 PG(迁移也守单入口)。
幂等:tasks 用 If-Match(首次免锁);重跑安全(乐观锁/CAS 挡重复)。
双写窗:本脚本为"一次性导入"半边;"持续双写"半边=各会话 MCP 自报(上线后天然写),
       旧账窗内不停=回滚零成本(§5)。

用法:
  DATAD_URL=https://dingwei.wegoab.com/datad DATAD_TOKEN=<coordinator token> \
    python3 import.py --tasks tasks.json [--sessions online.json] [--dry-run]

tasks.json 格式(coordinator 台账导出,数组):
  [{"id":"T17","title":"...","owner_session":"zzc-devo48-2642","status":"done",
    "anchor":"f9ff717","eta":"","dispatch":[{"assignee":"zzc-claudek3-2642","role":"reviewer"}]}]
online.json 格式(在线清单导出):
  [{"session":"zzc-devo48-2642","state":"active"}]
"""
import argparse
import json
import os
import sys
import urllib.error
import urllib.request

BASE = os.environ.get("DATAD_URL", "").rstrip("/")
TOKEN = os.environ.get("DATAD_TOKEN", "")


def die(msg):
    print(f"ERR {msg}", file=sys.stderr)
    sys.exit(1)


def call(method, path, body=None, if_match=None):
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", "Bearer " + TOKEN)
    req.add_header("Content-Type", "application/json")
    if if_match is not None:
        req.add_header("If-Match", if_match)
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            raw = r.read().decode()
            return r.status, json.loads(raw) if raw.strip() else None
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()


def import_tasks(tasks, dry):
    ok = skip = fail = 0
    for t in tasks:
        dispatch = t.pop("dispatch", [])
        if dry:
            print(f"  [dry] PUT task {t['id']} status={t.get('status')}")
            ok += 1
            continue
        # 幂等:先 GET 判在否→在则带 If-Match,不在则首建免锁
        st, cur = call("GET", f"/v1/tasks/{t['id']}")
        if_match = cur.get("updated_at") if st == 200 and isinstance(cur, dict) else None
        st, resp = call("PUT", f"/v1/tasks/{t['id']}", t, if_match=if_match)
        if st == 200:
            ok += 1
            for d in dispatch:
                d["task_id"] = t["id"]
                call("POST", "/v1/dispatch", d)
        elif st == 409:
            skip += 1  # 并发/已迁,幂等跳过
            print(f"  skip {t['id']}: 409(已存在)")
        else:
            fail += 1
            print(f"  FAIL {t['id']}: {st} {resp}")
    return ok, skip, fail


def import_config(entries, dry):
    ok = skip = fail = 0
    for e in entries:
        if dry:
            print(f"  [dry] PUT config {e['key']}")
            ok += 1
            continue
        st, cur = call("GET", f"/v1/config/{e['key']}")
        gen = str(cur["generation"]) if st == 200 and isinstance(cur, dict) else "0"
        st, resp = call("PUT", f"/v1/config/{e['key']}", e["value"], if_match=gen)
        if st == 200:
            ok += 1
        elif st == 412:
            skip += 1  # 并发/已迁
        else:
            fail += 1
            print(f"  FAIL config {e['key']}: {st} {resp}")
    return ok, skip, fail


def import_sessions(sessions, dry):
    ok = fail = 0
    for s in sessions:
        if dry:
            print(f"  [dry] PUT session {s['session']} state={s.get('state')}")
            ok += 1
            continue
        # 注:session state=行级自报域,导入需 coordinator token 亦不可代写他人行(§4)。
        # 故此路仅供【各会话自迁】或【datad 加一次性 import 豁免】——见 §5 注。
        st, resp = call("PUT", f"/v1/sessions/{s['session']}/state", {"state": s["state"]})
        if st == 200:
            ok += 1
        else:
            fail += 1
            print(f"  FAIL {s['session']}: {st} {resp}(自报域行级:coordinator 也不能代写他人 state)")
    return ok, fail


def main():
    if not BASE or not TOKEN:
        die("DATAD_URL+DATAD_TOKEN env 必填")
    ap = argparse.ArgumentParser()
    ap.add_argument("--tasks")
    ap.add_argument("--config")
    ap.add_argument("--sessions")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    if args.tasks:
        with open(args.tasks) as f:
            ok, skip, fail = import_tasks(json.load(f), args.dry_run)
        print(f"tasks: ok={ok} skip={skip} fail={fail}")
        if fail:
            sys.exit(1)
    if args.config:
        with open(args.config) as f:
            ok, skip, fail = import_config(json.load(f), args.dry_run)
        print(f"config: ok={ok} skip={skip} fail={fail}")
        if fail:
            sys.exit(1)
    if args.sessions:
        with open(args.sessions) as f:
            ok, fail = import_sessions(json.load(f), args.dry_run)
        print(f"sessions: ok={ok} fail={fail}")
        if fail:
            print("注:自报域行级限制=session state 建议各会话自迁,非批量导入;见 §5")
    print("导入完成" + (" (dry-run)" if args.dry_run else ""))


if __name__ == "__main__":
    main()
