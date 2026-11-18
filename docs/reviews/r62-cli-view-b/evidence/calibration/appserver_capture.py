#!/usr/bin/env python3
# S-R62-CLI-VIEW-B 工具行版式校准：直起 codex app-server --stdio（不经 helper），
# 最小 JSON-RPC 握手（initialize/initialized/thread/start/turn/start），捕获全部通知原文。
# 用法: appserver_capture.py <bin> <codex-home> <workdir> <provider-port> <out.jsonl>
import json, os, subprocess, sys, threading, time, queue

BIN, CHOME, WORK, PORT, OUT = sys.argv[1:6]
env = {
    "HOME": os.path.join(CHOME, "..", "home"),
    "PATH": "/usr/local/bin:/usr/bin:/bin",
    "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8",
    "CODEX_HOME": CHOME,
    "MOCK_API_KEY": "dummy-calibration-only",
}
os.makedirs(env["HOME"], exist_ok=True)
proc = subprocess.Popen([BIN, "app-server", "--stdio"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                        stderr=subprocess.PIPE, env=env, cwd=WORK, text=True, bufsize=1)
msgs = queue.Queue()
raw = open(OUT, "w")

def reader():
    for line in proc.stdout:
        line = line.strip()
        if not line:
            continue
        raw.write(line + "\n"); raw.flush()
        try:
            msgs.put(json.loads(line))
        except Exception:
            pass

def errreader():
    for line in proc.stderr:
        with open(OUT + ".stderr", "a") as f:
            f.write(line)

threading.Thread(target=reader, daemon=True).start()
threading.Thread(target=errreader, daemon=True).start()

def send(obj):
    proc.stdin.write(json.dumps(obj) + "\n"); proc.stdin.flush()

def wait_id(rid, timeout=30):
    end = time.time() + timeout
    while time.time() < end:
        try:
            m = msgs.get(timeout=max(0.1, end - time.time()))
        except queue.Empty:
            break
        if m.get("id") == rid:
            return m
    raise TimeoutError(f"no response for id={rid}")

send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"clientInfo": {"name": "cli-view-b-calibration", "version": "0"}, "capabilities": {"experimentalApi": True}}})
init = wait_id(1)
print("INIT", json.dumps(init.get("result", {}), ensure_ascii=False)[:200], flush=True)
send({"jsonrpc": "2.0", "method": "initialized"})
send({"jsonrpc": "2.0", "id": 2, "method": "thread/start", "params": {"cwd": WORK, "ephemeral": True, "approvalPolicy": "never", "sandbox": "danger-full-access", "model": "mock-model", "modelProvider": "mock"}})
ts = wait_id(2, 60)
thread = (ts.get("result") or {}).get("thread") or {}
tid = thread.get("id")
print("THREAD", tid, flush=True)
if not tid:
    print("THREAD_FAIL", json.dumps(ts, ensure_ascii=False)[:400], flush=True)
    proc.kill(); sys.exit(2)
send({"jsonrpc": "2.0", "id": 3, "method": "turn/start", "params": {"threadId": tid, "input": [{"type": "text", "text": "跑一下测试"}], "clientUserMessageId": "cal-1"}})
turn = wait_id(3, 60)
print("TURN", json.dumps(turn.get("result", {}), ensure_ascii=False)[:200], flush=True)
end = time.time() + 120
done = False
while time.time() < end and not done:
    try:
        m = msgs.get(timeout=1)
    except queue.Empty:
        continue
    method = m.get("method", "")
    if method in ("item/started", "item/completed"):
        item = (m.get("params") or {}).get("item") or {}
        print("ITEM", method, item.get("type"), json.dumps(item, ensure_ascii=False)[:300], flush=True)
    if method == "turn/completed":
        done = True
        print("TURN_COMPLETED", json.dumps(m.get("params", {}), ensure_ascii=False)[:300], flush=True)
proc.kill()
raw.close()
print("DONE", flush=True)
