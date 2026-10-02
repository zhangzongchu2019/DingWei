#!/usr/bin/env python3
"""reply-origin/loop-termination e2e binary smoke:真 Hub + 两个真 sessionHelper。

链路(全真进程,无 mock):
  IPC(mcp_send 顶层四字段) → helper A → WSS → Hub RouteEnvelope(R4/R5/origin) →
  helper B 双闸(R2 expect_reply ⇒ 产 reply,carry-forward kind=reply+conversation_terminal) →
  Hub → helper A 双闸(R3 conversation_terminal ⇒ 终止,不再回) = A↔B 环被打断。

场景:
  S1 A--content/expect_reply-->B:B 自动回源到 A(T6),A 收到 reply 后 R3 终止(T1 e2e)。
  S2 A--kind=ack-->B:B1/control 短路,B 不产 reply(T7 e2e)。
  S3 A--kind=mirror-->B:R1,B 不产 reply(T3 e2e)。
  终局断言:静置后 A 恰一次终止、B 恰一次 reply、无环状增长。

可靠外壳(组长 R-AGENT-REPLY-CONTROL-IMPL-20261002154009,ANALYSIS-R-AGENT-REPLY-CONTROL 第四节第 6、7 格):
  第 6 格 S1 机械确认 B 收件、A 收回信都是 Hub 可靠外壳(type=delivery、ack_required=true,读两侧 inbox 库),
        A 收回信保留 conversation_terminal=true;修前 B 红在「suppress … control」,修后 A 绿在「suppress … conversation_terminal」。
  第 7 格 S4 两个「旧 helper」(启动时打补丁:无第二道闸、回信不带 terminal、一律 expect_reply)互回,
        Hub R4 在 max_hops 精确停:恰一条 R4 丢弃(hop=16 max=16)、两侧收件合计 16、静置后不增长。

🔴 环境隔离与机械闸(组长 R-SMOKE-IPC-HIJACK-20261002154248):helper 一律经 _smoke_isolation.spawn_helper 启动,
  IPC 路径与 HOME 不在本次临时目录内即退出、不启动;跑前跑后打印真 IPC 路径状态并断言未变。

harness(build/seed/wait)基座采 251dd64 的 source-bot smoke,场景全新。
"""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path

try:
    from _smoke_isolation import isolated_helper_env, print_real_ipc_state, spawn_helper
except ImportError:  # 作为 tests.reply_origin_binary_smoke 导入时
    from tests._smoke_isolation import isolated_helper_env, print_real_ipc_state, spawn_helper

ROOT = Path(__file__).resolve().parents[3]

KEY_ID = "FB-rig-2642"
KEY_SECRET = "wp_" + "52" * 32
# key 未绑成员 owner ⇒ 服务端不派生全名,注册名即短名
SESSION_A = "asess"
SESSION_B = "bsess"
ADDR_A = f"{SESSION_A}#{KEY_ID}"
ADDR_B = f"{SESSION_B}#{KEY_ID}"
# 第 7 格两只「旧 helper」
SESSION_C = "csess"
SESSION_D = "dsess"
ADDR_C = f"{SESSION_C}#{KEY_ID}"
ADDR_D = f"{SESSION_D}#{KEY_ID}"
DEFAULT_MAX_HOPS = 16  # internal/m8/reply_origin.go defaultMaxHops

# 旧 helper 模拟:进程内打补丁后照常 `python -m sessionhelper`。去掉 helper 第二道闸(control/terminal/hop/reply_policy),
# 回信不带 conversation_terminal、一律 expect_reply,hop 照常带回 ⇒ 双方每次都回,只剩 Hub R4 兜底。
OLD_HELPER_BOOT = r'''
import runpy
import sessionhelper.app as app
import sessionhelper.routing as routing

def classify(env):
    kind = str((env.get("meta") or {}).get("kind") or "")
    if kind in ("ack", "mirror"):
        return routing.ReplyDecision(False, kind)
    return routing.ReplyDecision(True, "ok")

def carry(env):
    meta = env.get("meta") or {}
    out = {"kind": "content", "reply_policy": "expect_reply", "hop_count": routing._hop_count(env)}
    if meta.get("conversation_id"):
        out["conversation_id"] = meta["conversation_id"]
    return out

app.classify_reply_decision = classify
app.reply_carryforward_meta = carry
runpy.run_module("sessionhelper", run_name="__main__", alter_sys=True)
'''


def go_binary() -> str:
    return shutil.which("go") or "/usr/local/go/bin/go"


def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def wait_http(url: str, timeout: float = 10) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=0.5) as response:
                if response.status < 500:
                    return
        except Exception:
            time.sleep(0.1)
    raise RuntimeError(f"service not ready: {url}")


def stop(proc: subprocess.Popen | None) -> None:
    if proc is None or proc.poll() is not None:
        return
    proc.terminate()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()


def seed(db: Path) -> None:
    now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    with sqlite3.connect(db) as conn:
        conn.execute(
            "INSERT INTO registered_service(id,name,delivery_type,reply_mode,enabled) VALUES(?,?,?,?,1)",
            ("rig-helper", "rig-helper", "ws", "sync"),
        )
        conn.execute(
            "INSERT INTO service_api_key(id,service_id,key_hash,label,active,created_at) VALUES(?,?,?,?,1,?)",
            (KEY_ID, "rig-helper", hashlib.sha256(KEY_SECRET.encode()).hexdigest(), "rig", now),
        )


def wait_session(db: Path, session: str, timeout: float = 8) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        with sqlite3.connect(db) as conn:
            row = conn.execute(
                "SELECT active FROM session_endpoint WHERE key_id=? AND session_name=?",
                (KEY_ID, session),
            ).fetchone()
        if row == (1,):
            return
        time.sleep(0.1)
    with sqlite3.connect(db) as conn:
        rows = conn.execute("SELECT key_id,session_name,active FROM session_endpoint").fetchall()
    raise AssertionError(f"helper {session} did not register: endpoints={rows}")


def ipc_send(sock_path: str, payload: dict) -> dict:
    """一条 newline-delimited JSON 请求→一条响应(shim 契约同款)。"""
    client = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    client.settimeout(5)
    try:
        client.connect(sock_path)
        client.sendall(json.dumps(payload, ensure_ascii=False).encode("utf-8") + b"\n")
        buf = b""
        while not buf.endswith(b"\n"):
            chunk = client.recv(4096)
            if not chunk:
                break
            buf += chunk
        return json.loads(buf.decode("utf-8"))
    finally:
        client.close()


def read_log(stream) -> str:
    stream.flush()
    stream.seek(0)
    return stream.read()


def wait_in_log(stream, marker: str, timeout: float = 8) -> str:
    deadline = time.time() + timeout
    while time.time() < deadline:
        text = read_log(stream)
        if marker in text:
            return text
        time.sleep(0.1)
    raise AssertionError(f"missing log marker={marker!r}:\n{read_log(stream)}")


def helper_env(tmp: Path, session_short: str, hub_ws: str) -> dict:
    return isolated_helper_env(tmp, session_short, {
        "PYTHONPATH": str(ROOT / "tools/sessionhelper"),
        "SH_SESSION_NAME": session_short,
        "SH_OWNER": "alice",
        "SH_KEY_ID": KEY_ID,
        "SH_SECRET": KEY_SECRET,
        "SH_WS_BASE": hub_ws,
        "SH_MODE": "echo",
        "SH_MIRROR_TO": "",
        "SH_COLLECT": "0",
        "SH_NO_DIRECTORY": "1",
        "SH_PROXY": "off",
        "SH_PROVISION_NO_EXIT": "1",
        "SH_INBOX_DB": str(tmp / f"helper-{session_short}-inbox.db"),
    })


def inbox_envelopes(tmp: Path, session_short: str) -> list[dict]:
    """helper 本地持久收件箱里每封可靠投递的信封(按收件序)。"""
    db = tmp / f"helper-{session_short}-inbox.db"
    if not db.exists():
        return []
    with sqlite3.connect(f"file:{db}?mode=ro", uri=True) as conn:
        rows = conn.execute("SELECT envelope_json FROM delivery_inbox ORDER BY received_at").fetchall()
    return [json.loads(r[0]) for r in rows]


def inbox_metas(tmp: Path, session_short: str) -> list[dict]:
    return [env.get("meta") or {} for env in inbox_envelopes(tmp, session_short)]


def is_reliable(meta: dict) -> bool:
    return (meta.get("type") == "delivery" and meta.get("ack_required") is True
            and meta.get("delivery_version") == 1 and bool(meta.get("delivery_id")))


def start_helper(tmp: Path, session_short: str, hub_ws: str, log, old_helper: bool = False):
    env = helper_env(tmp, session_short, hub_ws)
    cmd = [sys.executable, "-c", OLD_HELPER_BOOT] if old_helper else [sys.executable, "-m", "sessionhelper"]
    proc = spawn_helper(cmd, tmp, env, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT)
    return proc, env["SH_MCP_IPC_SOCK"]


def scenario_s1_to_s3(tmp: Path, hub_ws: str, db: Path, logs: dict) -> None:
    helper_a, ipc_a = start_helper(tmp, SESSION_A, hub_ws, logs["helper_a"])
    helper_b, _ = start_helper(tmp, SESSION_B, hub_ws, logs["helper_b"])
    try:
        wait_session(db, SESSION_A)
        wait_session(db, SESSION_B)
        wait_in_log(logs["helper_a"], "[ipc_listener] listening")

        # S1(第 6 格): content + expect_reply → B 回源到 A(T6),A 收 reply 后 R3 终止(T1)
        resp = ipc_send(ipc_a, {
            "type": "mcp_send", "to": ADDR_B, "body": "e2e-ping-s1",
            "request_id": "s1", "reply_policy": "expect_reply", "kind": "content",
        })
        assert resp.get("ok") is True, resp
        try:
            b_log = wait_in_log(logs["helper_b"], f"[reply] to={ADDR_A}")
        except AssertionError:
            b_log = read_log(logs["helper_b"])
            if f"suppress reply from={ADDR_A}: control" in b_log:
                raise AssertionError(f"S1 RED: B suppress reply from={ADDR_A}: control "
                                     "(reliable delivery wrapper judged control)") from None
            raise
        # helper 日志不打正文(日志卫生),正文从 B 的收件箱取
        assert [e.get("body") for e in inbox_envelopes(tmp, SESSION_B)] == ["e2e-ping-s1"], inbox_envelopes(tmp, SESSION_B)
        a_log = wait_in_log(logs["helper_a"], "[reply-origin] suppress reply")
        assert f"suppress reply from={ADDR_B}: conversation_terminal" in a_log, a_log
        b_in, a_in = inbox_metas(tmp, SESSION_B), inbox_metas(tmp, SESSION_A)
        assert len(b_in) == 1 and is_reliable(b_in[0]) and b_in[0].get("reply_policy") == "expect_reply", b_in
        assert len(a_in) == 1 and is_reliable(a_in[0]), a_in
        assert a_in[0].get("kind") == "reply" and a_in[0].get("conversation_terminal") is True, a_in
        print(f"[smoke] S1 B inbox meta type={b_in[0]['type']} ack_required={b_in[0]['ack_required']} "
              f"reply_policy={b_in[0]['reply_policy']}; A inbox meta type={a_in[0]['type']} "
              f"kind={a_in[0]['kind']} conversation_terminal={a_in[0]['conversation_terminal']}", flush=True)

        # S2: kind=ack → B1/control,B 不产 reply(T7)
        resp = ipc_send(ipc_a, {
            "type": "mcp_send", "to": ADDR_B, "body": "e2e-ping-s2",
            "request_id": "s2", "kind": "ack",
        })
        assert resp.get("ok") is True, resp
        wait_in_log(logs["helper_b"], f"suppress reply from={ADDR_A}: control")

        # S3: kind=mirror → R1,B 不产 reply(T3)
        resp = ipc_send(ipc_a, {
            "type": "mcp_send", "to": ADDR_B, "body": "e2e-ping-s3",
            "request_id": "s3", "kind": "mirror",
        })
        assert resp.get("ok") is True, resp
        wait_in_log(logs["helper_b"], f"suppress reply from={ADDR_A}: mirror")

        # 终局:静置后无环状增长——B 恰 1 次 reply,A 恰 1 次终止,A 零 auto-reply;两侧 inbox 无新增循环件
        time.sleep(2)
        a_log, b_log = read_log(logs["helper_a"]), read_log(logs["helper_b"])
        assert b_log.count(f"[reply] to={ADDR_A}") == 1, b_log
        assert a_log.count("suppress reply") == 1, a_log
        assert f"[reply] to={ADDR_B}" not in a_log, a_log
        assert len(inbox_metas(tmp, SESSION_A)) == 1, inbox_metas(tmp, SESSION_A)
        assert len(inbox_metas(tmp, SESSION_B)) == 3, inbox_metas(tmp, SESSION_B)  # s1、s2、s3 各一
    finally:
        stop(helper_a)
        stop(helper_b)


def scenario_s4_hub_r4(tmp: Path, hub_ws: str, db: Path, logs: dict) -> None:
    helper_c, ipc_c = start_helper(tmp, SESSION_C, hub_ws, logs["helper_c"], old_helper=True)
    helper_d, _ = start_helper(tmp, SESSION_D, hub_ws, logs["helper_d"], old_helper=True)
    try:
        wait_session(db, SESSION_C)
        wait_session(db, SESSION_D)
        wait_in_log(logs["helper_c"], "[ipc_listener] listening")
        resp = ipc_send(ipc_c, {
            "type": "mcp_send", "to": ADDR_D, "body": "e2e-ping-s4",
            "request_id": "s4", "reply_policy": "expect_reply", "kind": "content",
        })
        assert resp.get("ok") is True, resp
        hub_log = wait_in_log(logs["hub"], "R4 hop 兜底丢弃", timeout=60)
        time.sleep(3)  # 静置:Hub 丢弃之后不得再有投递
        hub_log = read_log(logs["hub"])
        drops = [ln for ln in hub_log.splitlines() if "R4 hop 兜底丢弃" in ln]
        assert len(drops) == 1, drops
        assert f"hop={DEFAULT_MAX_HOPS} max={DEFAULT_MAX_HOPS}" in drops[0], drops
        c_in, d_in = inbox_metas(tmp, SESSION_C), inbox_metas(tmp, SESSION_D)
        hops = sorted(int(m.get("hop_count") or 0) for m in c_in + d_in)
        assert hops == list(range(1, DEFAULT_MAX_HOPS + 1)), hops
        assert all(is_reliable(m) and not m.get("conversation_terminal") for m in c_in + d_in), c_in + d_in
        replies = read_log(logs["helper_c"]).count("[reply] to=") + read_log(logs["helper_d"]).count("[reply] to=")
        assert replies == DEFAULT_MAX_HOPS, replies  # 16 封回信:15 封投到、第 16 封被 Hub R4 丢
        print(f"[smoke] S4 Hub R4: deliveries={len(hops)} hops=1..{hops[-1]} replies_sent={replies} "
              f"drop='{drops[0].split('[reply-origin] ')[-1]}'", flush=True)
    finally:
        stop(helper_c)
        stop(helper_d)


def main() -> None:
    before = print_real_ipc_state("before")
    port = free_port()
    hub_http = f"http://127.0.0.1:{port}"
    hub_ws = f"ws://127.0.0.1:{port}"
    with tempfile.TemporaryDirectory(prefix="dw-reply-origin-e2e-") as raw_tmp:
        tmp = Path(raw_tmp)
        binary, db = tmp / "workpulse", tmp / "hub.db"
        subprocess.run([go_binary(), "build", "-o", binary, "./cmd/workpulse"], cwd=ROOT, check=True)
        hub_env = {
            **os.environ,
            "WP_DB_PATH": str(db),
            "WP_DATA_DIR": str(tmp / "data"),
            "WP_ADDR": f"127.0.0.1:{port}",
            "WP_ADMIN_INIT_PASSWORD": "rig",
            "FEISHU_TRANSPORT": "fake",
        }
        bootstrap = subprocess.Popen([binary], cwd=ROOT, env=hub_env, stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT)
        wait_http(hub_http + "/readyz")
        stop(bootstrap)
        seed(db)

        names = ("hub", "helper_a", "helper_b", "helper_c", "helper_d")
        logs = {name: open(tmp / f"{name}.log", "w+") for name in names}
        hub = None
        try:
            hub = subprocess.Popen([binary], cwd=ROOT, env=hub_env, stdout=logs["hub"], stderr=subprocess.STDOUT)
            wait_http(hub_http + "/readyz")
            try:
                scenario_s1_to_s3(tmp, hub_ws, db, logs)
                scenario_s4_hub_r4(tmp, hub_ws, db, logs)
            except Exception:
                for name in names:
                    print(f"----- {name} -----\n{read_log(logs[name])[-6000:]}", file=sys.stderr)
                raise
            print("reply-origin e2e binary smoke PASS: "
                  "S1 reliable reply-to-origin+R3 terminal / S2 ack control / S3 mirror / no loop growth / "
                  "S4 Hub R4 exact stop")
        finally:
            stop(hub)
            for stream in logs.values():
                stream.close()
            after = print_real_ipc_state("after")
            assert after == before, f"real IPC path changed during smoke: {before} -> {after}"


if __name__ == "__main__":
    main()
