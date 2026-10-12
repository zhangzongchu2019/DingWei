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
import shlex
import select
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
        env = {key: value for key, value in os.environ.items()
               if not key.startswith(("SH_", "DW_", "CODEX_", "CLAUDE_", "OPENCODE_", "DINGWEI_"))}
        env["CODEX_HOME"] = str(codex)
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
            configured = subprocess.run(setup_command, input=answers,
                           env=env, text=True, capture_output=True, timeout=30, check=True)
            startup = next(line for line in configured.stdout.splitlines() if line.startswith("启动： "))
            startup_args = shlex.split(startup[len("启动： "):])
            assert startup_args == [str(binary), "--run-profile", str(state / "setup-profile.json")]
        # Execute the actual printed command with readiness deliberately absent.
        # It must reach the profile guard, not enroll/connect or treat -m as a
        # frozen executable option. Restore only the synthetic status afterwards.
        status_path = state / "setup-status.json"
        status_bytes = status_path.read_bytes()
        status_path.unlink()
        try:
            refused = subprocess.run(startup_args, env=env, text=True, capture_output=True, timeout=30)
            assert refused.returncode != 0
            assert "finish the installer tool-readiness step" in refused.stderr
        finally:
            sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools" / "sessionhelper"))
            from sessionhelper.enroll import write_private_file
            write_private_file(str(status_path), status_bytes.decode())
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
            assert {"dw_inbox", "send_message", "dw_wait_message", "dw_reply_message"} <= {t["name"] for t in responses[2]["result"]["tools"]}
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

        # Exercise the frozen process's pending tool, not a source-module stand-in.
        old_reports = set(state.glob("desktop-pilot-*-report.json"))
        pilot = subprocess.Popen([str(binary), "--desktop-pilot", "--message-after-wait", "--profile", str(state / "setup-profile.json")],
                                 env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        client = None
        try:
            deadline = time.monotonic() + 15
            report = None
            while time.monotonic() < deadline:
                assert pilot.poll() is None, "frozen delayed pilot exited before ready"
                candidates = set(state.glob("desktop-pilot-*-report.json")) - old_reports
                if candidates:
                    report_path = candidates.pop()
                    report = json.loads(report_path.read_text())
                    if report["state"] == "IPC_READY": break
                time.sleep(.05)
            assert report and report["state"] == "IPC_READY"
            assert not report["fixture_posted"]
            client = subprocess.Popen([str(binary), "--mcp"], env=dict(env, DINGWEI_MCP_IPC_SOCK=str(state / "dingwei-mcp.sock")),
                                      stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
            def call(ident, method, params=None):
                packet = {"jsonrpc":"2.0", "id":ident, "method":method}
                if params is not None: packet["params"] = params
                client.stdin.write(json.dumps(packet)+"\n"); client.stdin.flush()
                assert select.select([client.stdout], [], [], 15)[0], "frozen MCP response timeout"
                result = json.loads(client.stdout.readline())
                assert result["id"] == ident
                return result
            assert "error" not in call(1, "initialize")
            packet = call(2, "tools/call", {"name":"dw_wait_message", "arguments":{"wait_seconds":5}})
            result = json.loads(packet["result"]["content"][0]["text"])
            assert result["state"] == "message" and result["message"]["id"] == report["message_id"]
            assert result["model_execution_verified"] is False
            assert "error" in call(3, "tools/call", {"name":"dw_reply_message", "arguments":{"message_id":report["message_id"],"body":"DW_DESKTOP_OK"}})
            packet = call(4, "tools/call", {"name":"send_message", "arguments":{"to":"desktop-pilot#LOCAL","body":"DW_DESKTOP_OK","in_reply_to":report["message_id"]}})
            assert json.loads(packet["result"]["content"][0]["text"])["sent_to_hub"] is False
            report = json.loads(report_path.read_text())
            assert report["fixture_posted"] and report["wait_return_count"] == report["reply_count"] == 1
            assert report["hub_connections"] == 0
        finally:
            if client is not None:
                client.stdin.close()
                try: client.wait(timeout=10)
                except subprocess.TimeoutExpired: client.kill(); client.wait(timeout=5)
                client.stdout.close()
            if pilot.poll() is None: pilot.send_signal(signal.SIGTERM)
            pilot.wait(timeout=10)
        assert pilot.returncode == 0 and not (state / "dingwei-mcp.sock").exists() and not (state / "desktop-pilot.lock").exists()
    print(json.dumps({"status": "FROZEN_LOCAL_SMOKE_PASS", "version": expected_version,
                      "bytes": binary.stat().st_size, "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                      "real_hub_or_ai_calls": 0, "native_desktop_runtime": "NOT_VERIFIED"}))


if __name__ == "__main__":
    main()
