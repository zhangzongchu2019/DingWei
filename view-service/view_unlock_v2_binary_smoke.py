#!/usr/bin/env python3
"""Level-1 page-unlock v2 smoke using real Hub, View Service and sessionHelper processes."""

from __future__ import annotations

import hashlib
import hmac
import json
import os
import socket
import secrets
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.request
import urllib.error
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
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status, json.loads(response.read() or b"{}")
    except urllib.error.HTTPError as error:
        return error.code, {}


def signed_post(url, path, payload, secret, target, direction):
    raw = json.dumps(payload, separators=(",", ":")).encode()
    timestamp, nonce = str(int(time.time())), secrets.token_hex(16)
    direction_key = hmac.new(secret, direction.encode(), hashlib.sha256).digest()
    canonical = "\n".join(("POST", path, timestamp, nonce, hashlib.sha256(raw).hexdigest())).encode()
    signature = hmac.new(direction_key, canonical, hashlib.sha256).hexdigest()
    request = urllib.request.Request(url + path, raw, {
        "Content-Type": "application/json", "X-DW-VS-Target": target,
        "X-DW-Timestamp": timestamp, "X-DW-Nonce": nonce,
        "X-DW-Request-ID": str(payload.get("request_id", "")), "X-DW-Signature": signature,
    }, method="POST")
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code


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


def wait_unlock_reply(db, event_id, minimum_count, timeout=8):
    deadline = time.time() + timeout
    while time.time() < deadline:
        with sqlite3.connect(db) as conn:
            rows = conn.execute("SELECT content_json FROM message WHERE direction='out' ORDER BY created_at DESC").fetchall()
        text = "\n".join(row[0] or "" for row in rows)
        if text.count("已解锁页面输入") >= minimum_count:
            return text
        time.sleep(.1)
    raise AssertionError(f"missing successful unlock reply for {event_id}")


def wait_view_delivery_processed(db, helper_db, request_id, timeout=15):
    deadline = time.time() + timeout
    while time.time() < deadline:
        with sqlite3.connect(db) as conn:
            hub_row = conn.execute(
                "SELECT id,state,provenance FROM session_delivery WHERE source_message_id=?",
                (request_id,),
            ).fetchone()
        if hub_row:
            with sqlite3.connect(helper_db) as conn:
                helper_row = conn.execute(
                    "SELECT state,envelope_json FROM delivery_inbox "
                    "WHERE session_name=? AND delivery_id=?",
                    ("reviewer", hub_row[0]),
                ).fetchone()
            if hub_row[1] == "processed" and helper_row and helper_row[0] == "processed":
                provenance = json.loads(hub_row[2])
                envelope = json.loads(helper_row[1])
                assert provenance == {
                    "kind": "human", "display_name": "人工",
                    "attested_by": "hub", "trust_decision": "trusted",
                }, provenance
                assert "_hub_ingress_channel" not in (envelope.get("meta") or {}), envelope
                return hub_row[0]
        time.sleep(.1)
    raise AssertionError(f"View delivery did not reach processed in Hub + real helper: request_id={request_id}")


def session_connection_sequence(db):
    with sqlite3.connect(db) as conn:
        row = conn.execute(
            "SELECT conn_seq FROM session_endpoint WHERE key_id=? AND session_name=?",
            (KEY_ID, SESSION),
        ).fetchone()
    return int(row[0]) if row else -1


def wait_session_online(db, after_conn_seq, timeout=15):
    deadline = time.time() + timeout
    while time.time() < deadline:
        with sqlite3.connect(db) as conn:
            row = conn.execute(
                "SELECT active,conn_seq FROM session_endpoint WHERE key_id=? AND session_name=?",
                (KEY_ID, SESSION),
            ).fetchone()
        if row and row[0] == 1 and int(row[1]) > after_conn_seq:
            return
        time.sleep(.1)
    raise AssertionError("real sessionHelper did not reconnect after Hub restart")


def main():
    public_port, internal_port, vs_port = free_port(), free_port(), free_port()
    hub_url = f"http://127.0.0.1:{public_port}"
    vs_url = f"http://127.0.0.1:{vs_port}"
    with tempfile.TemporaryDirectory(prefix="dw-v2-smoke-") as tmp_raw:
        tmp = Path(tmp_raw)
        binary, db = tmp / "workpulse", tmp / "hub.db"
        forward, reverse = tmp / "hub-to-vs.key", tmp / "vs-to-hub.key"
        forward_secret, reverse_secret = b"f" * 48, b"r" * 48
        forward.write_bytes(forward_secret); reverse.write_bytes(reverse_secret)
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
                  "VS_HUB_INTERNAL": f"http://127.0.0.1:{internal_port}",
                  "VS_HUB_TO_VS_SECRET_FILE": str(forward), "VS_TO_HUB_SECRET_FILE": str(reverse),
                  "VS_PAGE_DB": str(tmp / "pages.db")}
        logs = {name: open(tmp / f"{name}.log", "w+") for name in ("hub", "vs", "helper")}
        hub = vs = helper = None
        try:
            vs = subprocess.Popen([sys.executable, "view_service.py"], cwd=ROOT / "view-service", env=vs_env,
                                  stdout=logs["vs"], stderr=subprocess.STDOUT)
            hub = subprocess.Popen([binary], cwd=ROOT, env=hub_env, stdout=logs["hub"], stderr=subprocess.STDOUT)
            wait_http(f"{hub_url}/readyz"); wait_http(f"{vs_url}/status")
            assert post(f"{hub_url}/api/terminal/mock-unlock", {"code": "0000", "session_name": SESSION})[0] == 404
            status, fixed_page = post(f"{vs_url}/page?session=attacker-manager-0000", {})
            assert status == 201 and fixed_page.get("code", "").startswith(SESSION + "-"), fixed_page
            cli_marker = tmp / "cli-output.txt"
            cli_program = tmp / "echo_cli.py"
            cli_program.write_text(
                "import os,pathlib,sys,tty\ntty.setraw(0)\nprint('>', flush=True)\n"
                f"marker=pathlib.Path({str(cli_marker)!r})\n"
                "buf=b''; count=0\nwhile True:\n"
                " buf += os.read(0,1024)\n"
                " if b'LEVEL1\\r' in buf:\n"
                "  count += buf.count(b'LEVEL1\\r')\n"
                "  marker.write_text(str(count))\n"
                "  print('SMOKE_OUTPUT:LEVEL1', flush=True)\n"
                "  buf=b''\n")
            helper_inbox = tmp / "helper-inbox.db"
            helper_env = {**os.environ, "PYTHONPATH": str(ROOT / "tools/sessionhelper"), "SH_SESSION_NAME": "reviewer",
                "SH_OWNER": "alice", "SH_KEY_ID": KEY_ID, "SH_SECRET": KEY_SECRET,
                "SH_WS_BASE": hub_url.replace("http", "ws"), "SH_MODE": "cli", "SH_CLI_LAUNCH": "pty",
                "SH_CLI_CMD": f"{sys.executable} -u {cli_program}",
                "SH_PROXY": "off", "SH_INBOX_DB": str(helper_inbox)}
            helper = subprocess.Popen([sys.executable, "-m", "sessionhelper"], cwd=ROOT, env=helper_env,
                                      stdout=logs["helper"], stderr=subprocess.STDOUT)
            time.sleep(1)
            with sqlite3.connect(db) as conn:
                endpoint = conn.execute(
                    "SELECT session_name, full_session_name, owner_key FROM session_endpoint WHERE key_id=?",
                    (KEY_ID,),
                ).fetchone()
            assert endpoint == (SESSION, "", "alice"), endpoint
            _, wiring_page = post(f"{vs_url}/page", {})
            unlock_body = {"command_id": "a1-unlock", "request_id": "a1-unlock", "session": SESSION,
                "code": wiring_page["code"], "owner_key": "alice", "sender_open_id": "ou_alice",
                "granted_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
            assert signed_post(vs_url, "/internal/control/view-page/unlock", unlock_body,
                               forward_secret, "canary", "hub-to-view-service/v1") == 200
            wait_unlocked(vs_url, wiring_page)
            reverse_body = {"session": SESSION, "page_id": wiring_page["page_id"],
                            "request_id": "1" * 32, "text": "A1_CONFIG"}
            assert signed_post(f"http://127.0.0.1:{internal_port}", f"/internal/view-v2/{SESSION}/input",
                               reverse_body, reverse_secret, "canary", "view-service-to-hub/v1") == 200
            _, page = post(f"{vs_url}/page", {})
            assert webhook_unlock(hub_url, page["code"], "unlock-first") == 202
            wait_unlocked(vs_url, page)
            wait_unlock_reply(db, "unlock-first", 1)
            request_id = "a" * 32
            _, result = post(f"{vs_url}/input", {"text": "LEVEL1", "page_id": page["page_id"],
                                                   "page_token": page["page_token"], "request_id": request_id})
            assert result.get("ok"), result
            delivery_id = wait_view_delivery_processed(db, helper_inbox, request_id)
            deadline = time.time() + 15
            while time.time() < deadline and not cli_marker.exists():
                time.sleep(.05)
            time.sleep(.3)
            cli_output_ok = cli_marker.exists() and cli_marker.read_text() == "1"

            # Production replacement restarts Hub and drops every WS. Wait for
            # the real helper to reconnect before accepting the next View input.
            old_conn_seq = session_connection_sequence(db)
            stop(hub)
            hub = subprocess.Popen([binary], cwd=ROOT, env=hub_env, stdout=logs["hub"], stderr=subprocess.STDOUT)
            wait_http(f"{hub_url}/readyz")
            wait_session_online(db, old_conn_seq)
            restart_request_id = "b" * 32
            _, restart_result = post(f"{vs_url}/input", {"text": "LEVEL1", "page_id": page["page_id"],
                "page_token": page["page_token"], "request_id": restart_request_id})
            assert restart_result.get("ok"), restart_result
            restart_delivery_id = wait_view_delivery_processed(db, helper_inbox, restart_request_id)
            deadline = time.time() + 15
            while time.time() < deadline and cli_marker.read_text() != "2":
                time.sleep(.05)
            cli_output_ok = cli_output_ok and cli_marker.read_text() == "2"

            stop(helper)
            helper = subprocess.Popen([sys.executable, "-m", "sessionhelper"], cwd=ROOT, env=helper_env,
                                      stdout=logs["helper"], stderr=subprocess.STDOUT)
            time.sleep(1)
            assert webhook_unlock(hub_url, page["code"], "unlock-after-churn") == 202
            wait_unlocked(vs_url, page)
            wait_unlock_reply(db, "unlock-after-churn", 2)
            time.sleep(.5); logs["hub"].flush(); logs["hub"].seek(0)
            assert "未找到页面码" not in logs["hub"].read()
            assert cli_output_ok, "CLI output did not return through Hub events"
            route_data = json.loads(routing.read_text())
            route_data["targets"]["canary"]["view_unlock_v2"] = False
            time.sleep(1.1)
            routing.write_text(json.dumps(route_data))
            assert signed_post(f"http://127.0.0.1:{internal_port}", f"/internal/view-v2/{SESSION}/input",
                               {**reverse_body, "request_id": "2" * 32}, reverse_secret, "canary",
                               "view-service-to-hub/v1") == 404
            stop(vs)
            off_env = {**vs_env, "VS_VIEW_UNLOCK_V2": "0"}
            vs = subprocess.Popen([sys.executable, "view_service.py"], cwd=ROOT / "view-service", env=off_env,
                                  stdout=logs["vs"], stderr=subprocess.STDOUT)
            wait_http(f"{vs_url}/status")
            assert signed_post(vs_url, "/internal/control/view-page/unlock",
                               {**unlock_body, "command_id": "off", "request_id": "off"},
                               forward_secret, "canary", "hub-to-view-service/v1") == 404
            print(f"LEVEL1 PASS: VS /input + Hub provenance + real helper processed delivery={delivery_id} "
                  f"+ Hub-restart recovery delivery={restart_delivery_id} + CLI output + helper churn first-unlock")
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
