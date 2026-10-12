#!/usr/bin/env python3
"""Local-only frozen-package verification; no Hub, enrollment or AI login.

Usage: python scripts/verify-sessionhelper-frozen.py /path/to/sessionhelper
Creates only disposable private fixture state, config and a local Unix socket.
"""
import argparse
import asyncio
import hashlib
import json
import os
from pathlib import Path
import subprocess
import signal
import time
import sys
import tempfile
import threading
import tomllib


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    parser.add_argument("--desktop-app", type=Path, help="macOS selected application bundle")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    expected_version = (Path(__file__).resolve().parents[1] / "VERSION").read_text().strip()
    with tempfile.TemporaryDirectory(prefix="dwf-", dir="/tmp") as temp:
        root = Path(temp)
        state, codex = root / "state", root / "codex"
        codex.mkdir(mode=0o700)
        config = codex / "config.toml"
        before = b'model = "fixture-model"\n[mcp_servers.fixture_existing]\ncommand = "fixture-only"\n'
        config.write_bytes(before)
        config.chmod(0o600)
        env = dict(os.environ, CODEX_HOME=str(codex))
        # An MCP/version/setup invocation must not enroll using a caller's credentials.
        for key in ("SH_ENROLL", "SH_KEY_ID", "SH_SECRET"):
            env.pop(key, None)
        version = subprocess.run([str(binary), "--version"], env=env, text=True,
                                 capture_output=True, timeout=30, check=True)
        assert version.stdout.strip() == "sessionHelper " + expected_version
        answers = "\n".join(("1", "https://fixture.invalid", "fixtureowner", "fx", "fixtureagent", str(state), str(root), "feishu")) + "\n"
        setup_command = [str(binary), "--setup", "--configure-only"]
        if args.desktop_app:
            setup_command.extend(["--desktop-app", str(args.desktop_app)])
        for _ in range(2):
            subprocess.run(setup_command, input=answers,
                           env=env, text=True, capture_output=True, timeout=30, check=True)
        assert config.read_bytes().startswith(before)
        mcp = tomllib.loads(config.read_text())["mcp_servers"]["dingwei-fixtureagent"]
        assert mcp["command"] == str(binary) and mcp["args"] == ["--mcp"]
        assert "PYTHONPATH" not in mcp["env"]
        assert json.loads((state / "setup-profile.json").read_text())["SH_MODE"] == "desktop"
        assert (state / "setup-profile.json").stat().st_mode & 0o777 == 0o600
        assert json.loads((state / "setup-status.json").read_text())["native_auto_notify"] == "NOT_VERIFIED"
        assert not (state / "claim-pending.json").exists() and not (state / "credential.json").exists()
        # Real local inbox implementation with synthetic input, then frozen MCP reads it.
        sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools" / "sessionhelper"))
        from types import SimpleNamespace
        from sessionhelper.desktop import DesktopAdapter
        from sessionhelper.ipc_listener import serve_ipc
        inbox = DesktopAdapter(SimpleNamespace(inbox_db=str(root / "messages")))
        inbox.handle_delivery({"id": "fixture-only", "from": "fixture", "body": "fixture message"})
        socket_path = str(root / "fixture.sock")
        ready = threading.Event()
        loop = asyncio.new_event_loop()
        async def inbox_request(payload):
            return inbox.request(payload)
        async def forbid_send(*args):
            raise AssertionError("package smoke must not send to Hub")
        async def server():
            bound = asyncio.Event()
            task = asyncio.create_task(serve_ipc(socket_path, forbid_send, bound=bound, inbox_handler=inbox_request))
            await bound.wait()
            ready.set()
            await task
        task = loop.create_task(server())
        thread = threading.Thread(target=loop.run_forever, daemon=True)
        thread.start()
        try:
            assert ready.wait(5), "local fixture IPC did not start"
            requests = [
                {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "fixture", "version": "1"}}},
                {"jsonrpc": "2.0", "method": "notifications/initialized"},
                {"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
                {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "dw_inbox", "arguments": {"operation": "status"}}},
            ]
            mcp_env = dict(env, DINGWEI_MCP_IPC_SOCK=socket_path)
            result = subprocess.run([str(binary), "--mcp"], input="".join(json.dumps(r)+"\n" for r in requests),
                                    env=mcp_env, text=True, capture_output=True, timeout=30, check=True)
            responses = {r["id"]: r for r in map(json.loads, result.stdout.splitlines())}
            assert len(responses) == 3 and all("error" not in r for r in responses.values())
            assert responses[1]["result"]["serverInfo"]["version"] == expected_version
            assert {"dw_inbox", "send_message"} <= {t["name"] for t in responses[2]["result"]["tools"]}
            content = json.loads(responses[3]["result"]["content"][0]["text"])
            assert content["unread"] == 1 and content["native_auto_notify"] == "NOT_VERIFIED"
        finally:
            async def shutdown():
                task.cancel()
                await asyncio.gather(task, return_exceptions=True)
            asyncio.run_coroutine_threadsafe(shutdown(), loop).result(5)
            loop.call_soon_threadsafe(loop.stop)
            thread.join(5)
            loop.close()
        probe_command = [str(binary), "--desktop-probe"]
        if args.desktop_app:
            probe_command.extend(["--desktop-app", str(args.desktop_app)])
        probe = json.loads(subprocess.run(probe_command, env=env, text=True, capture_output=True, timeout=30, check=True).stdout)
        assert probe["account_state"] == "NOT_READ" and probe["native_auto_notify"] == "NOT_VERIFIED"
        pilot = subprocess.Popen([str(binary), "--desktop-pilot", "--profile", str(state / "setup-profile.json")],
                                 env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            report = None
            report_path = None
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                assert pilot.poll() is None, "frozen local pilot exited before ready"
                candidates = list(state.glob("desktop-pilot-*-report.json"))
                if candidates:
                    report_path = candidates[0]
                    report = json.loads(report_path.read_text())
                    if report["state"] == "IPC_READY":
                        break
                time.sleep(0.05)
            assert report and report["state"] == "IPC_READY", "frozen pilot not ready"
            requests = [
                {"jsonrpc": "2.0", "id": 1, "method": "initialize"},
                {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "dw_inbox", "arguments": {"operation": "read", "message_id": report["message_id"]}}},
                {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "send_message", "arguments": {"to": "desktop-pilot#LOCAL", "body": "DW_DESKTOP_OK", "in_reply_to": report["message_id"]}}},
            ]
            result = subprocess.run([str(binary), "--mcp"], input="".join(json.dumps(r)+"\n" for r in requests),
                                    env=dict(env, DINGWEI_MCP_IPC_SOCK=str(state / "dingwei-mcp.sock")), text=True, capture_output=True, timeout=30, check=True)
            responses = list(map(json.loads, result.stdout.splitlines()))
            assert len(responses) == 3 and all("error" not in r for r in responses)
            report = json.loads(report_path.read_text())
            assert report["read_count"] == report["reply_count"] == 1 and report["hub_connections"] == 0
        finally:
            pilot.send_signal(signal.SIGTERM)
            pilot.wait(timeout=10)
        assert pilot.returncode == 0 and not (state / "dingwei-mcp.sock").exists() and not (state / "desktop-pilot.lock").exists()
    print(json.dumps({"status": "FROZEN_LOCAL_SMOKE_PASS", "version": expected_version,
                      "bytes": binary.stat().st_size, "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                      "real_hub_or_ai_calls": 0, "native_desktop_runtime": "NOT_VERIFIED"}))


if __name__ == "__main__":
    main()
