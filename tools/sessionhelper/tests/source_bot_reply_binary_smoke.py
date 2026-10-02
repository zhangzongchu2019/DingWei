#!/usr/bin/env python3
"""Binary smoke for source-bot reply routing through a real Hub and sessionHelper."""

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
sys.path.insert(0, str(ROOT / "tools/sessionhelper"))

from sessionhelper.protocol import AddressBook, reply_target  # noqa: E402

try:
    from _smoke_isolation import isolated_helper_env, print_real_ipc_state, spawn_helper  # noqa: E402
except ImportError:  # 作为 tests.source_bot_reply_binary_smoke 导入时
    from tests._smoke_isolation import isolated_helper_env, print_real_ipc_state, spawn_helper  # noqa: E402


KEY_ID = "FB-rig-2642"
KEY_SECRET = "wp_" + "52" * 32
SESSION = "alice-echo-2642"
BOT_TOKEN = "rig-webhook-token"


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
        for channel_id, name in (("unifiedrobot", "UnifiedRobot"), ("fallback", "FallbackRobot")):
            conn.execute(
                "INSERT INTO bot_channel(id,name,app_id,verification_token,purpose,can_send,can_receive,active) "
                "VALUES(?,?,?,?,?,?,?,1)",
                (channel_id, name, "cli_" + channel_id, BOT_TOKEN, "general", 1, 1),
            )
        conn.execute(
            "INSERT INTO member(id,owner_key,display_name,feishu_open_id,role,active) VALUES(?,?,?,?,?,1)",
            ("member-alice", "alice", "Alice", "ou_rig", "member"),
        )
        conn.execute(
            "INSERT INTO chat_entity(id,bot_channel_id,type,feishu_id,display_name,bound_owner,active) "
            "VALUES(?,?,?,?,?,?,1)",
            ("unifiedrobot:personal:ou_rig", "unifiedrobot", "personal", "ou_rig", "Alice", "alice"),
        )
        conn.execute(
            "INSERT INTO chat_entity(id,bot_channel_id,type,feishu_id,display_name,bound_owner,active) "
            "VALUES(?,?,?,?,?,?,1)",
            ("fallback:personal:ou_fallback", "fallback", "personal", "ou_fallback", "Alice", "alice"),
        )
        conn.execute(
            "INSERT INTO registered_service(id,name,delivery_type,reply_mode,enabled) VALUES(?,?,?,?,1)",
            ("rig-helper", "rig-helper", "ws", "sync"),
        )
        conn.execute(
            "INSERT INTO service_api_key(id,service_id,key_hash,label,active,created_at) VALUES(?,?,?,?,1,?)",
            (KEY_ID, "rig-helper", hashlib.sha256(KEY_SECRET.encode()).hexdigest(), "rig", now),
        )
        conn.execute(
            "INSERT INTO api_key_account(api_key_id,chat_entity_id) VALUES(?,?)",
            (KEY_ID, "unifiedrobot:personal:ou_rig"),
        )
        conn.execute(
            "INSERT INTO api_key_account(api_key_id,chat_entity_id) VALUES(?,?)",
            (KEY_ID, "fallback:personal:ou_fallback"),
        )


def wait_session(db: Path, timeout: float = 8) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        with sqlite3.connect(db) as conn:
            row = conn.execute(
                "SELECT active FROM session_endpoint WHERE key_id=? AND session_name=?",
                (KEY_ID, SESSION),
            ).fetchone()
        if row == (1,):
            return
        time.sleep(0.1)
    with sqlite3.connect(db) as conn:
        rows = conn.execute("SELECT key_id,session_name,active FROM session_endpoint").fetchall()
    raise AssertionError(f"real helper did not register: endpoints={rows}")


def outbound_rows(db: Path) -> list[tuple[str, str, str]]:
    with sqlite3.connect(db) as conn:
        return conn.execute(
            "SELECT bot_channel_id,chat_entity_id,content_json FROM message "
            "WHERE direction='out' ORDER BY rowid"
        ).fetchall()


def wait_outbound(db: Path, marker: str, timeout: float = 8) -> tuple[str, str, str]:
    deadline = time.time() + timeout
    while time.time() < deadline:
        rows = outbound_rows(db)
        for row in rows:
            if marker in row[2]:
                return row
        time.sleep(0.1)
    raise AssertionError(f"missing outbound marker={marker!r}: {outbound_rows(db)}")


def send_webhook(hub_http: str, channel: str, sender: str, text: str, event_id: str) -> None:
    payload = {
        "schema": "2.0",
        "header": {"event_id": event_id, "event_type": "im.message.receive_v1", "token": BOT_TOKEN},
        "event": {
            "sender": {"sender_id": {"open_id": sender}},
            "message": {
                "message_id": event_id,
                "chat_type": "p2p",
                "chat_id": sender,
                "content": json.dumps({"text": f"#{SESSION} {text}"}),
            },
        },
    }
    request = urllib.request.Request(
        f"{hub_http}/webhook/{channel}", json.dumps(payload).encode(),
        {"Content-Type": "application/json"}, method="POST",
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        assert response.status in (200, 202), response.status


def main() -> None:
    before = print_real_ipc_state("before")
    port = free_port()
    hub_http = f"http://127.0.0.1:{port}"
    hub_ws = f"ws://127.0.0.1:{port}"
    with tempfile.TemporaryDirectory(prefix="dw-reply-origin-") as raw_tmp:
        tmp = Path(raw_tmp)
        binary, db = tmp / "workpulse", tmp / "hub.db"
        subprocess.run([shutil.which("go") or "/usr/local/go/bin/go", "build", "-o", binary, "./cmd/workpulse"], cwd=ROOT, check=True)
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

        logs = {name: open(tmp / f"{name}.log", "w+") for name in ("hub", "helper")}
        helper = hub = None
        try:
            hub = subprocess.Popen([binary], cwd=ROOT, env=hub_env, stdout=logs["hub"], stderr=subprocess.STDOUT)
            wait_http(hub_http + "/readyz")
            # 🔴 环境隔离＋IPC 路径机械闸(组长 R-SMOKE-IPC-HIJACK-20261002154248):不以 os.environ 整体为底
            helper_env = isolated_helper_env(tmp, "echo", {
                "PYTHONPATH": str(ROOT / "tools/sessionhelper"),
                "SH_SESSION_NAME": "echo",
                "SH_OWNER": "alice",
                "SH_KEY_ID": KEY_ID,
                "SH_SECRET": KEY_SECRET,
                "SH_WS_BASE": hub_ws,
                "SH_MODE": "echo",
                "SH_MIRROR_TO": "",
                "SH_COLLECT": "0",
                "SH_NO_DIRECTORY": "1",
                "SH_PROXY": "off",
                "SH_INBOX_DB": str(tmp / "helper-inbox.db"),
            })
            helper = spawn_helper(
                [sys.executable, "-m", "sessionhelper"], tmp, helper_env, cwd=ROOT,
                stdout=logs["helper"], stderr=subprocess.STDOUT,
            )
            try:
                wait_session(db)
            except Exception:
                for stream in logs.values():
                    stream.flush()
                    stream.seek(0)
                    print(stream.read(), file=sys.stderr)
                raise

            send_webhook(hub_http, "unifiedrobot", "ou_rig", "rig-ping-primary", "rig-primary-source")
            bot, entity, content = wait_outbound(db, "rig-ping-primary")
            assert bot == "unifiedrobot", (bot, entity, content)
            assert entity == "unifiedrobot:personal:ou_rig", (bot, entity, content)
            assert "rig-ping-primary" in content, content

            send_webhook(hub_http, "fallback", "ou_fallback", "rig-ping-fallback", "rig-fallback-source")
            bot, entity, content = wait_outbound(db, "rig-ping-fallback")
            assert bot == "fallback", (bot, entity, content)
            assert entity == "fallback:personal:ou_fallback", (bot, entity, content)
            assert "rig-ping-fallback" in content, content

            logs["helper"].flush()
            logs["helper"].seek(0)
            helper_log = logs["helper"].read()
            assert f"[reply] to=ou_rig#{KEY_ID}#unifiedrobot" in helper_log, helper_log
            assert f"[reply] to=ou_fallback#{KEY_ID}#fallback" in helper_log, helper_log

            fallback_to, fallback_meta = reply_target(
                {"from": f"ou_address#{KEY_ID}#FallbackRobot", "meta": {}},
                AddressBook("echo", KEY_ID, "UnifiedRobot"),
            )
            assert fallback_to == f"ou_address#{KEY_ID}#FallbackRobot", fallback_to
            assert fallback_meta == {}, fallback_meta
            print("reply-origin binary smoke PASS: primary source bot + address fallback")
        finally:
            stop(helper)
            stop(hub)
            for stream in logs.values():
                stream.close()
            after = print_real_ipc_state("after")
            assert after == before, f"real IPC path changed during smoke: {before} -> {after}"


if __name__ == "__main__":
    main()
