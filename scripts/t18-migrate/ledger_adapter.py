#!/usr/bin/env python3
"""T18 D5 adapter:task-ledger-imgsearch.json → import.py 的 tasks.json 格式。

task-ledger 件结构(coordinator 样本 08-04):
  {id, name, deps, designer/implementer/tester/qa_owner:{who,cli,llm},
   status, nodes:[{node_id,seq,deps}], notes}

映射决策(dry-run 输出供 coordinator 核):
  id→id;name→title;status→8态(映射表,未知=保守 proposed+告警)
  owner_session=implementer.who(主责;缺则 designer/qa_owner fallback)
  四职能→dispatch(role 映射 designer/implementer→executor,tester→reviewer,qa_owner→advisor;
                    原职能名+cli+llm 存 dispatch.note 保真)
  deps/nodes/notes/完整四职能矩阵→detail JSONB(零信息丢失)

用法:python3 ledger_adapter.py task-ledger-imgsearch.json > tasks.json
      (task-ledger 可为单件 dict 或件数组)
"""
import json
import sys

# task-ledger status → datad 8 态(§2 CHECK)
STATUS_MAP = {
    "done": "done", "completed": "done", "closed": "done",
    "in_progress": "in_progress", "doing": "in_progress", "wip": "in_progress",
    "todo": "proposed", "pending": "proposed", "proposed": "proposed", "new": "proposed",
    "dispatched": "dispatched", "assigned": "dispatched",
    "review": "review", "testing": "review", "qa": "review",
    "blocked": "blocked", "waiting": "waiting_gate", "waiting_gate": "waiting_gate",
    "archived": "archived",
}
# 职能 → dispatch role(§3 CHECK executor|reviewer|advisor)
ROLE_MAP = {"designer": "executor", "implementer": "executor",
            "tester": "reviewer", "qa_owner": "advisor"}
ROLES = ["designer", "implementer", "tester", "qa_owner"]


def warn(msg):
    print(f"WARN {msg}", file=sys.stderr)


def role_who(item, role):
    r = item.get(role)
    if isinstance(r, dict):
        return r.get("who", "")
    if isinstance(r, str):
        return r
    return ""


def convert(item):
    tid = item.get("id", "")
    status_raw = (item.get("status") or "").strip().lower()
    status = STATUS_MAP.get(status_raw)
    if status is None:
        warn(f"{tid}: 未知 status={status_raw!r}→保守设 proposed(请人工核)")
        status = "proposed"
    # owner_session:implementer 主责,缺则 designer→qa_owner fallback
    owner = role_who(item, "implementer") or role_who(item, "designer") or role_who(item, "qa_owner")
    if not owner:
        warn(f"{tid}: 无 implementer/designer/qa_owner,owner_session 空(import 会 422,请补)")
    # dispatch:每职能一条(去空),role 映射+职能名/cli/llm 存 note 保真
    dispatch = []
    for role in ROLES:
        r = item.get(role)
        if isinstance(r, dict) and r.get("who"):
            note = role
            if r.get("cli") or r.get("llm"):
                note += f"({r.get('cli','')}/{r.get('llm','')})"
            dispatch.append({"assignee": r["who"], "role": ROLE_MAP[role], "note": note})
    # detail:完整保真(四职能矩阵+deps+nodes+notes)
    detail = {"roles": {role: item[role] for role in ROLES if role in item}}
    for k in ("deps", "nodes", "notes"):
        if item.get(k):
            detail[k] = item[k]
    out = {"id": tid, "title": item.get("name", ""), "owner_session": owner,
           "status": status, "detail": detail}
    if dispatch:
        out["dispatch"] = dispatch
    return out


def main():
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    raw = json.load(open(sys.argv[1]))
    items = raw if isinstance(raw, list) else [raw]
    out = [convert(it) for it in items]
    print(json.dumps(out, ensure_ascii=False, indent=2))
    warn(f"转换 {len(out)} 件 → tasks.json(dry-run 核映射后喂 import.py --tasks)")


if __name__ == "__main__":
    main()
