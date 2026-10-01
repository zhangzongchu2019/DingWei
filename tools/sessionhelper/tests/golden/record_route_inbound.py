"""在**当前检出的代码**上录制 R3-B 黄金输出（route_inbound_golden.json）。

只经**旧位置**取函数（app / protocol / provision，以及 SessionHelper 的实例判定方法，self 传 None——
它们都不碰 self），所以必须在 R3-B 改动之前的代码上跑；文件头记录录制时的 git head 与被测源文件 sha256，
证明黄金来自旧代码、不是新代码重新生成。输入见 route_inbound_cases.py。
用法：PYTHONPATH=<工作树> python tests/golden/record_route_inbound.py
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

import route_inbound_cases as C  # noqa: E402
from sessionhelper import app, protocol, provision  # noqa: E402


def old_fn(name):
    if name in ("is_mirror_control",):
        return getattr(protocol, name)
    if name == "is_provision_envelope":
        return getattr(provision, name)
    if name in ("is_delivery", "is_delivery_cancel", "is_agent_network_skill", "is_terminal_input",
                "is_terminal_interrupt", "is_terminal_resize", "is_terminal_refresh", "is_file_capability_response"):
        f = getattr(app.SessionHelper, name)
        return lambda env: f(None, env)  # 实例方法不碰 self
    return getattr(app, name)


def call(fn, *args):
    try:
        return {"v": fn(*args)}
    except Exception as exc:  # 异常也是行为：新旧必须同样抛
        return {"raises": type(exc).__name__}


def main():
    sha = lambda rel: hashlib.sha256((ROOT / rel).read_bytes()).hexdigest()
    head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    out = {
        "_recorded_from": {
            "git_head": head,
            "source_sha256": {r: sha(r) for r in ("sessionhelper/app.py", "sessionhelper/protocol.py",
                                                  "sessionhelper/provision.py", "sessionhelper/routing.py")},
            "hub_notice_types": sorted(app.HUB_NOTICE_META_TYPES),
            "note": "录制于 R3-B 改动之前的代码；改动后用同一组输入经新接口重放，逐条比对。",
        },
        "truthy": [{"value": repr(v), "out": call(app.truthy_meta, v)} for v in C.TRUTHY_VALUES],
        "env": [{"env": C.compact(e), **{n: call(old_fn(n), e) for n in C.FUNCS}} for e in C.ENVS],
        "text": [{"text": C.compact(t), **{n: call(getattr(app, n), t) for n in C.TEXT_FUNCS}} for t in C.TEXTS],
    }
    (HERE / "route_inbound_golden.json").write_text(
        json.dumps(out, ensure_ascii=False, indent=1, sort_keys=True), encoding="utf-8")
    n = len(out["truthy"]) + len(C.FUNCS) * len(out["env"]) + len(C.TEXT_FUNCS) * len(out["text"])
    print(f"recorded {n} checks from {head[:8]}")


if __name__ == "__main__":
    main()
