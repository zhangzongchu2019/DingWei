#!/usr/bin/env python3
"""P1 test Agent (real client enrollment code + a bare /ws/agent connection, no real helper).

  enroll --hub URL --owner USER --state DIR   prints PAIRING_CODE=<code>, waits for approval, saves the credential
  serve  --hub URL --state DIR                connects /ws/agent with the saved credential, prints every envelope as a
                                              JSON line, and executes JSON commands read from stdin:
                                              {"op":"http","method":"GET","path":"/api/...","cap":"<capability>"}
                                              -> prints {"http":{"status":N,"body":"..."}}
Credential values never reach stdout.
"""
import argparse
import asyncio
import json
import os
import sys
import urllib.error
import urllib.request

sys.path.insert(0, os.path.join(os.environ.get("P1_REPO", "."), "tools", "sessionhelper"))
from sessionhelper import enroll as E  # noqa: E402


def emit(obj):
    print(json.dumps(obj, ensure_ascii=False), flush=True)


def cmd_enroll(a):
    os.makedirs(a.state, exist_ok=True)
    cred = E.enroll(
        state_dir=a.state, owner=a.owner, transport=E.UrllibTransport(a.hub),
        show=lambda code: print("PAIRING_CODE=" + code, flush=True),
        notify=lambda m: print("NOTIFY=" + m, flush=True), poll_interval=E.MIN_POLL_INTERVAL,
    )
    E.save_credential(a.state, cred)
    print("ENROLLED agent_name=" + cred.agent_name, flush=True)


def http_call(hub, method, path, cred, cap):
    req = urllib.request.Request(hub.rstrip("/") + path, method=method)
    req.add_header("Authorization", "Bearer " + cred.secret)
    if cap:
        req.add_header("X-DW-Console-Capability", cap)
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")


async def cmd_serve(a):
    import websockets
    cred = E.load_credential(a.state)
    if not cred:
        raise SystemExit("no credential in state dir")
    url = a.hub.replace("http://", "ws://").replace("https://", "wss://").rstrip("/") + "/ws/agent"
    loop = asyncio.get_running_loop()
    async with websockets.connect(url, additional_headers={"Authorization": "Bearer " + cred.secret}) as ws:
        emit({"online": cred.agent_name})

        async def reader():
            async for raw in ws:
                try:
                    emit({"envelope": json.loads(raw)})
                except ValueError:
                    emit({"raw": str(raw)[:200]})

        async def commands():
            while True:
                line = await loop.run_in_executor(None, sys.stdin.readline)
                if not line:
                    return
                try:
                    c = json.loads(line)
                except ValueError:
                    continue
                if c.get("op") == "http":
                    st, body = await loop.run_in_executor(
                        None, http_call, a.hub, c.get("method", "GET"), c["path"], cred, c.get("cap", ""))
                    emit({"http": {"status": st, "body": body}})

        await asyncio.gather(reader(), commands())


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name in ("enroll", "serve"):
        p = sub.add_parser(name)
        p.add_argument("--hub", required=True)
        p.add_argument("--state", required=True)
        if name == "enroll":
            p.add_argument("--owner", required=True)
    args = ap.parse_args()
    if args.cmd == "enroll":
        cmd_enroll(args)
    else:
        asyncio.run(cmd_serve(args))
