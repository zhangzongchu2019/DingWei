#!/usr/bin/env python3
"""Level-1 page-unlock v2 smoke using real Hub, View Service and sessionHelper processes."""

from __future__ import annotations

import hashlib
import json
import os
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SESSION = "alice-reviewer-2642"
KEY_ID = "FB-smoke-2642"
KEY_SECRET = "wp_" + "42" * 32
BOT_TOKEN = "smoke-webhook-token"


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def wait_http(url, timeout=10):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=.5) as response:
                if response.status < 500:
                    return
        except Exception:
            time.sleep(.1)
    raise RuntimeError(f"service not ready: {url}")


def post(url, payload):
    request = urllib.request.Request(url, json.dumps(payload).encode(),
                                     {"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(request, timeout=5) as response:
        return response.status, json.loads(response.read() or b"{}")


def stop(proc):
    if proc and proc.poll() is None:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()


def seed(db):
    now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    with sqlite3.connect(db) as conn:
        conn.execute("INSERT INTO bot_channel(id,name,app_id,verification_token,purpose,can_send,can_receive,active) VALUES(?,?,?,?,?,?,?,?)",
                     ("dev", "UnifiedRobot", "cli_smoke", BOT_TOKEN, "general", 1, 1, 1))
        conn.execute("INSERT INTO chat_entity(id,bot_channel_id,type,feishu_id,display_name,bound_owner,active) VALUES(?,?,?,?,?,?,1)",
                     ("dev:personal:ou_alice", "dev", "personal", "ou_alice", "Alice", "alice"))
        conn.execute("INSERT INTO member(id,owner_key,display_name,feishu_open_id,role,active) VALUES(?,?,?,?,?,1)",
                     ("member-alice", "alice", "Alice", "ou_alice", "member"))
        conn.execute("INSERT INTO registered_service(id,name,delivery_type,reply_mode,enabled) VALUES(?,?,?,?,1)",
                     ("smoke-helper", "smoke-helper", "ws", "sync"))
        conn.execute("INSERT INTO service_api_key(id,service_id,key_hash,label,active,created_at) VALUES(?,?,?,?,1,?)",
                     (KEY_ID, "smoke-helper", hashlib.sha256(KEY_SECRET.encode()).hexdigest(), "smoke", now))
        conn.execute("INSERT INTO api_key_account(api_key_id,chat_entity_id) VALUES(?,?)",
                     (KEY_ID, "dev:personal:ou_alice"))


def webhook_unlock(hub_url, code, event_id):
    payload = {
        "schema": "2.0",
        "header": {"event_id": event_id, "event_type": "im.message.receive_v1", "token": BOT_TOKEN},
        "event": {"sender": {"sender_id": {"open_id": "ou_alice"}}, "message": {
            "message_id": event_id, "chat_type": "p2p", "chat_id": "ou_alice",
            "content": json.dumps({"text": f"#unlock {code}"}),
        }},
    }
    return post(f"{hub_url}/webhook/dev", payload)[0]


def wait_unlocked(vs_url, page, timeout=8):
    deadline = time.time() + timeout
    while time.time() < deadline:
        _, state = post(f"{vs_url}/page/resume", {"page_id": page["page_id"], "page_token": page["page_token"]})
        if state.get("can_write"):
            return
        time.sleep(.1)
    raise AssertionError("page did not become unlocked")


def main():
    public_port, internal_port, vs_port = free_port(), free_port(), free_port()
    hub_url = f"http://127.0.0.1:{public_port}"
    vs_url = f"http://127.0.0.1:{vs_port}"
    with tempfile.TemporaryDirectory(prefix="dw-v2-smoke-") as tmp_raw:
        tmp = Path(tmp_raw)
        binary, db = tmp / "workpulse", tmp / "hub.db"
        forward, reverse = tmp / "hub-to-vs.key", tmp / "vs-to-hub.key"
        forward.write_text("f" * 48); reverse.write_text("r" * 48)
        subprocess.run(["/usr/local/go/bin/go", "build", "-o", binary, "./cmd/workpulse"], cwd=ROOT, check=True)
        hub_env = {**os.environ, "WP_DB_PATH": str(db), "WP_DATA_DIR": str(tmp / "data"),
                   "WP_ADDR": f"127.0.0.1:{public_port}", "WP_INTERNAL_ADDR": f"127.0.0.1:{internal_port}",
                   "WP_ADMIN_INIT_PASSWORD": "smoke", "FEISHU_TRANSPORT": "fake"}
        bootstrap = subprocess.Popen([binary], cwd=ROOT, env=hub_env, stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT)
        wait_http(f"{hub_url}/readyz"); stop(bootstrap); seed(db)
        routing = tmp / "view-routing.json"
        routing.write_text(json.dumps({"sessions": {SESSION: "canary"}, "users": {}, "default": "legacy-xterm",
            "targets": {"legacy-xterm": {"builtin": True}, "canary": {"url": vs_url, "view_unlock_v2": True,
                "hub_to_vs_secret_file": str(forward), "vs_to_hub_secret_file": str(reverse)}}}))
        hub_env["VIEW_ROUTING_CONFIG"] = str(routing)
        vs_env = {**os.environ, "VS_PORT": str(vs_port), "VS_SESSION": SESSION,
                  "VS_VIEW_UNLOCK_V2": "1", "VS_TARGET_NAME": "canary",
                  "VS_HUB_INTERNAL": f"http://127.0.0.1:{internal_port}", "VS_HUB_PUBLIC": hub_url,
                  "VS_HUB_TO_VS_SECRET_FILE": str(forward), "VS_TO_HUB_SECRET_FILE": str(reverse),
                  "VS_PAGE_DB": str(tmp / "pages.db"), "VS_RENDERER": str(ROOT / "view-service/renderer-canary-sse.html")}
        logs = {name: open(tmp / f"{name}.log", "w+") for name in ("hub", "vs", "helper")}
        hub = vs = helper = None
        try:
            vs = subprocess.Popen([sys.executable, "view_service.py"], cwd=ROOT / "view-service", env=vs_env,
                                  stdout=logs["vs"], stderr=subprocess.STDOUT)
            hub = subprocess.Popen([binary], cwd=ROOT, env=hub_env, stdout=logs["hub"], stderr=subprocess.STDOUT)
            wait_http(f"{hub_url}/readyz"); wait_http(f"{vs_url}/status")
            cli_marker = tmp / "cli-output.txt"
            cli_program = tmp / "echo_cli.py"
            cli_program.write_text(
                "import os,pathlib,sys,tty\ntty.setraw(0)\nprint('>', flush=True)\n"
                f"marker=pathlib.Path({str(cli_marker)!r})\n"
                "buf=b''\nwhile True:\n"
                " buf += os.read(0,1024)\n"
                " if b'LEVEL1\\r' in buf:\n"
                "  marker.write_text('SMOKE_OUTPUT:LEVEL1')\n"
                "  print('SMOKE_OUTPUT:LEVEL1', flush=True)\n"
                "  buf=b''\n")
            helper_env = {**os.environ, "PYTHONPATH": str(ROOT / "tools/sessionhelper"), "SH_SESSION_NAME": "reviewer",
                "SH_SESSION_FULL": SESSION, "SH_OWNER": "alice", "SH_KEY_ID": KEY_ID, "SH_SECRET": KEY_SECRET,
                "SH_WS_BASE": hub_url.replace("http", "ws"), "SH_MODE": "cli", "SH_CLI_LAUNCH": "pty",
                "SH_CLI_CMD": f"{sys.executable} -u {cli_program}",
                "SH_PROXY": "off", "SH_INBOX_DB": str(tmp / "helper-inbox.db")}
            helper = subprocess.Popen([sys.executable, "-m", "sessionhelper"], cwd=ROOT, env=helper_env,
                                      stdout=logs["helper"], stderr=subprocess.STDOUT)
            time.sleep(1)
            _, page = post(f"{vs_url}/page", {})
            assert webhook_unlock(hub_url, page["code"], "unlock-first") == 202
            wait_unlocked(vs_url, page)
            request_id = "a" * 32
            _, result = post(f"{vs_url}/input", {"text": "LEVEL1", "page_id": page["page_id"],
                                                   "page_token": page["page_token"], "request_id": request_id})
            assert result.get("ok"), result
            deadline = time.time() + 5
            while time.time() < deadline and not cli_marker.exists():
                time.sleep(.05)
            cli_output_ok = cli_marker.exists() and cli_marker.read_text() == "SMOKE_OUTPUT:LEVEL1"
            stop(helper)
            helper = subprocess.Popen([sys.executable, "-m", "sessionhelper"], cwd=ROOT, env=helper_env,
                                      stdout=logs["helper"], stderr=subprocess.STDOUT)
            time.sleep(1)
            assert webhook_unlock(hub_url, page["code"], "unlock-after-churn") == 202
            wait_unlocked(vs_url, page)
            time.sleep(.5); logs["hub"].flush(); logs["hub"].seek(0)
            assert "未找到页面码" not in logs["hub"].read()
            assert cli_output_ok, "CLI output did not return through Hub events"
            print("LEVEL1 PASS: wiring + CLI output + churn first-unlock")
        except Exception:
            for name, stream in logs.items():
                stream.flush(); stream.seek(0)
                print(f"--- {name}.log ---\n{stream.read()}", file=sys.stderr)
            raise
        finally:
            stop(helper); stop(hub); stop(vs)
            for stream in logs.values(): stream.close()


if __name__ == "__main__":
    main()
