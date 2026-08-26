#!/usr/bin/env python3
"""M2 负例组:wkr.py 工人侧半边可执行用例(tester 冻结件 m2-negative-cases.md N4/N5a)。

W1=N4a 错投拒(exit1+MISROUTE+零 send 调用) / W2=N4b 正投 ack(exit0+回执纯JSON可解析)
W3=N5a 工人侧半边:同信封重发 done 幂等可行(恰一执行的账面半边在 board 包 TestTCN5a)

用 fake send.py 捕获外呼(不真发);wkr.py 路径经 WKR 环境变量注入,
默认 /home/devo48/work/heartbeat/wkr.py(契约0 权威路径)。全过 exit 0。
"""
import json
import os
import subprocess
import sys
import tempfile

WKR = os.environ.get("WKR", "/home/devo48/work/heartbeat/wkr.py")


def run_case(envelope, mode, to, me, extra=None):
    """跑一次 wkr.py,返回 (exit_code, stderr, sent_bodies)。"""
    d = tempfile.mkdtemp(prefix="m2neg-")
    cap = os.path.join(d, "sent.jsonl")
    fake = os.path.join(d, "send.py")
    with open(fake, "w") as f:
        f.write(
            "#!/usr/bin/env python3\n"
            "import sys\n"
            "args = sys.argv[1:]\n"
            "body = open(args[args.index('--file')+1]).read() if '--file' in args else ' '.join(args[1:])\n"
            "open(%r, 'a').write(body.rstrip() + chr(10))\n"
            "print('sent to=' + args[0])\n" % cap
        )
    env_file = os.path.join(d, "env.json")
    with open(env_file, "w") as f:
        json.dump(envelope, f, ensure_ascii=False)
    # wkr.py 的 SEND 常量指向 ~/.dingwei/send.py:用 HOME 重定向到 fake(零改 wkr 本体)
    dingwei = os.path.join(d, ".dingwei")
    os.makedirs(dingwei)
    os.rename(fake, os.path.join(dingwei, "send.py"))
    env = dict(os.environ, HOME=d, SH_FULL_NAME=me)
    cmd = ["python3", WKR, mode, env_file, "--to", to] + (extra or [])
    r = subprocess.run(cmd, capture_output=True, text=True, env=env)
    sent = []
    if os.path.exists(cap):
        sent = [l for l in open(cap).read().splitlines() if l.strip()]
    return r.returncode, r.stderr, sent


def main():
    fails = 0
    envelope = {"kind": "mcs_dispatch_v1", "node_id": "n1", "generation": 1,
                "rid": "rid-1", "payload": {"cmd": "run"},
                "from": "zzc-scheduler-2642", "to": "zzc-worker-b-2642"}

    # W1=N4a 错投:to≠自身 → exit1 + stderr MISROUTE + 零外呼
    code, err, sent = run_case(envelope, "ack", "zzc-scheduler-2642", "zzc-worker-a-2642")
    ok = code == 1 and "MISROUTE" in err and not sent
    print(("PASS" if ok else "FAIL"), "W1-N4a", f"exit={code} misroute={'MISROUTE' in err} sent={len(sent)}")
    fails += 0 if ok else 1

    # W2=N4b 正投:to=自身 → exit0 + 回执纯 JSON(kind/phase/rid 齐)
    good = dict(envelope, to="zzc-worker-a-2642")
    code, err, sent = run_case(good, "ack", "zzc-scheduler-2642", "zzc-worker-a-2642")
    ok = code == 0 and len(sent) == 1
    if ok:
        try:
            rc = json.loads(sent[0])  # 纯 JSON:整行必须可解析(N12 禁尾时戳)
            ok = (rc["kind"] == "mcs_receipt_v1" and rc["phase"] == "processing"
                  and rc["rid"] == "rid-1" and rc["from"] == "zzc-worker-a-2642")
        except (ValueError, KeyError):
            ok = False
    print(("PASS" if ok else "FAIL"), "W2-N4b", f"exit={code} sent={len(sent)}")
    fails += 0 if ok else 1

    # W3=N5a 工人侧半边:同信封重发 done 两次均 exit0(重发回执可行,不重执行 payload 由
    # 会话协议担保;board 侧恰一=TestTCN5a)
    code1, _, s1 = run_case(good, "done", "zzc-scheduler-2642", "zzc-worker-a-2642",
                            ["--ref", "/tmp"])
    code2, _, s2 = run_case(good, "done", "zzc-scheduler-2642", "zzc-worker-a-2642",
                            ["--ref", "/tmp"])
    ok = code1 == 0 and code2 == 0 and len(s1) == 1 and len(s2) == 1
    if ok:
        r1, r2 = json.loads(s1[0]), json.loads(s2[0])
        ok = r1 == r2 and r1["phase"] == "done" and r1["ok"] is True
    print(("PASS" if ok else "FAIL"), "W3-N5a", f"exit={code1},{code2} 重发回执一致={ok}")
    fails += 0 if ok else 1

    print(f"{3 - fails}/3 PASS")
    sys.exit(1 if fails else 0)


if __name__ == "__main__":
    main()
