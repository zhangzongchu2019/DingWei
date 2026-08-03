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

ROOT = Path(__file__).resolve().parents[3]

KEY_ID = "FB-rig-2642"
KEY_SECRET = "wp_" + "52" * 32
# key 未绑成员 owner ⇒ 服务端不派生全名,注册名即短名
SESSION_A = "asess"
SESSION_B = "bsess"
ADDR_A = f"{SESSION_A}#{KEY_ID}"
ADDR_B = f"{SESSION_B}#{KEY_ID}"


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


def helper_env(tmp: Path, session_short: str, ipc_sock: str | None, hub_ws: str) -> dict:
    env = {
        **os.environ,
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
    }
    if ipc_sock:
        env["SH_MCP_IPC_SOCK"] = ipc_sock
    return env


def main() -> None:
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

        ipc_sock = str(tmp / "a-mcp.sock")
        logs = {name: open(tmp / f"{name}.log", "w+") for name in ("hub", "helper_a", "helper_b")}
        hub = helper_a = helper_b = None
        try:
            hub = subprocess.Popen([binary], cwd=ROOT, env=hub_env, stdout=logs["hub"], stderr=subprocess.STDOUT)
            wait_http(hub_http + "/readyz")
            helper_a = subprocess.Popen(
                [sys.executable, "-m", "sessionhelper"], cwd=ROOT,
                env=helper_env(tmp, "asess", ipc_sock, hub_ws),
                stdout=logs["helper_a"], stderr=subprocess.STDOUT,
            )
            helper_b = subprocess.Popen(
                [sys.executable, "-m", "sessionhelper"], cwd=ROOT,
                env=helper_env(tmp, "bsess", None, hub_ws),
                stdout=logs["helper_b"], stderr=subprocess.STDOUT,
            )
            try:
                wait_session(db, SESSION_A)
                wait_session(db, SESSION_B)
                wait_in_log(logs["helper_a"], "[ipc_listener] listening")
            except Exception:
                for name in ("hub", "helper_a", "helper_b"):
                    print(f"----- {name} -----\n{read_log(logs[name])}", file=sys.stderr)
                raise

            # S1: content + expect_reply → B 回源到 A(T6),A 收 reply 后 R3 终止(T1)
            resp = ipc_send(ipc_sock, {
                "type": "mcp_send", "to": ADDR_B, "body": "e2e-ping-s1",
                "request_id": "s1", "reply_policy": "expect_reply", "kind": "content",
            })
            assert resp.get("ok") is True, resp
            b_log = wait_in_log(logs["helper_b"], f"[reply] to={ADDR_A}")
            assert "e2e-ping-s1" in b_log, b_log
            a_log = wait_in_log(logs["helper_a"], "[reply-origin] suppress reply")
            assert f"suppress reply from={ADDR_B}: conversation_terminal" in a_log, a_log

            # S2: kind=ack → B1/control,B 不产 reply(T7)
            resp = ipc_send(ipc_sock, {
                "type": "mcp_send", "to": ADDR_B, "body": "e2e-ping-s2",
                "request_id": "s2", "kind": "ack",
            })
            assert resp.get("ok") is True, resp
            wait_in_log(logs["helper_b"], f"suppress reply from={ADDR_A}: control")

            # S3: kind=mirror → R1,B 不产 reply(T3)
            resp = ipc_send(ipc_sock, {
                "type": "mcp_send", "to": ADDR_B, "body": "e2e-ping-s3",
                "request_id": "s3", "kind": "mirror",
            })
            assert resp.get("ok") is True, resp
            wait_in_log(logs["helper_b"], f"suppress reply from={ADDR_A}: mirror")

            # 终局:静置后无环状增长——B 恰 1 次 reply,A 恰 1 次终止,A 零 auto-reply
            time.sleep(2)
            a_log, b_log = read_log(logs["helper_a"]), read_log(logs["helper_b"])
            assert b_log.count(f"[reply] to={ADDR_A}") == 1, b_log
            assert a_log.count("suppress reply") == 1, a_log
            assert f"[reply] to={ADDR_B}" not in a_log, a_log
            print("reply-origin e2e binary smoke PASS: "
                  "S1 reply-to-origin+R3 terminal / S2 ack control / S3 mirror / no loop growth")
        finally:
            stop(helper_a)
            stop(helper_b)
            stop(hub)
            for stream in logs.values():
                stream.close()


if __name__ == "__main__":
    main()
