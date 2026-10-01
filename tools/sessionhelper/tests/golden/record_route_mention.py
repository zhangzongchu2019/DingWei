"""在**当前检出的代码**上录制 R3 黄金输出（route_mention_golden.json）。

只经**旧位置**取函数（app / protocol / reply_origin），所以必须在 R3 改动之前的代码上跑；
文件头记录了录制时的 git 提交与三个被测源文件的 sha256，用来证明黄金来自旧代码、不是新代码重新生成。
用法：PYTHONPATH=<工作树> python tests/golden/record_route_mention.py
"""
import hashlib
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
sys.path.insert(0, str(HERE))
sys.path.insert(0, str(ROOT))

import route_mention_cases as C  # noqa: E402
from sessionhelper.app import agent_route_envelope  # noqa: E402
from sessionhelper.protocol import AddressBook, is_feishu_addr, reply_target  # noqa: E402
from sessionhelper.reply_origin import (  # noqa: E402
    classify_reply_decision, is_control_envelope, is_reserved_control_payload, reply_carryforward_meta)


def norm_env(env):
    if env is None:
        return None
    return {k: v for k, v in env.items() if k not in ("id", "ts")}


def book(d):
    return AddressBook(d["session_name"], d["key_id"], d["bot_name"])


def main():
    sha = lambda rel: hashlib.sha256((ROOT / rel).read_bytes()).hexdigest()
    head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    import inspect
    import re as _re
    src_fn = inspect.getsource(agent_route_envelope)
    m = _re.search(r're\.fullmatch\(r"([^"]+)", to\)', src_fn)
    assert m, "旧代码里应有内联的目标地址 fullmatch 字面量"
    from sessionhelper import app as _app
    regexes = {
        "LEGACY_AGENT_ROUTE_RE": [_app.LEGACY_AGENT_ROUTE_RE.pattern, _app.LEGACY_AGENT_ROUTE_RE.flags],
        "CONTROL_ROUTE_RE": [_app.CONTROL_ROUTE_RE.pattern, _app.CONTROL_ROUTE_RE.flags],
        "ROUTE_TARGET_PATTERN_inline": m.group(1),
        "ROUTE_BODY_MAX": _app.ROUTE_BODY_MAX,
    }
    out = {
        "regexes": regexes,
        "_recorded_from": {
            "git_head": head,
            "source_sha256": {r: sha(r) for r in ("sessionhelper/app.py", "sessionhelper/protocol.py",
                                                  "sessionhelper/reply_origin.py")},
            "note": "录制于 R3 改动之前的旧代码；改动后用同一组输入经新接口重放，逐条比对。",
        },
        "route": [{"text": C.compact(t), "out": C.compact(norm_env(agent_route_envelope(t, book(C.BOOK))))} for t in C.ROUTE_TEXTS],
        "route_book2": [{"text": C.compact(t), "out": C.compact(norm_env(agent_route_envelope(t, book(C.BOOK2))))} for t in C.ROUTE_TEXTS[:12]],
        "reply_target": [{"case": c, "out": list(reply_target(c["inbound"], book(c["book"])))} for c in C.REPLY_TARGET_CASES],
        "is_feishu_addr": [{"addr": a, "out": is_feishu_addr(a)} for a in C.FEISHU_ADDRS],
        "reply": [],
    }
    for env in C.REPLY_ENVS:
        d = classify_reply_decision(env)
        out["reply"].append({
            "env": env,
            "reserved": is_reserved_control_payload(env),
            "control": is_control_envelope(env),
            "decision": [d.allow, d.reason],
            "carry": reply_carryforward_meta(env),
        })
    (HERE / "route_mention_golden.json").write_text(
        json.dumps(out, ensure_ascii=False, indent=1, sort_keys=True), encoding="utf-8")
    n = len(out["route"]) + len(out["route_book2"]) + len(out["reply_target"]) + len(out["is_feishu_addr"]) + 4 * len(out["reply"])
    print(f"recorded {n} checks from {head[:8]}")


if __name__ == "__main__":
    main()
