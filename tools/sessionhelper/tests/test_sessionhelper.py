import unittest
import asyncio
import hashlib
import io
import json
import stat
import sqlite3
import subprocess
import tarfile
import tempfile
import time
from unittest import mock
from pathlib import Path

from sessionhelper.app import (
    COMM_SKILL_ACK,
    SessionHelper,
    agent_route_envelope,
    comm_skill_ack_envelope,
    contains_comm_skill_ack,
    is_broadcast_envelope,
    is_no_mirror_envelope,
    is_online_directory_text,
)
from sessionhelper.cli import (
    BRACKETED_PASTE_END,
    BRACKETED_PASTE_START,
    CLIAdapter,
    CLI_PROFILES,
    OpenCodeDBReader,
    TranscriptTailer,
    event_time,
    fresh_jsonl,
    opencode_time,
    parse_claude_event,
    parse_codex_event,
    slug_cwd,
    strip_markdown_code_fence,
    tmux_session_name,
    user_text_matches,
)
from sessionhelper.config import detect_full_session_name, load_config
from sessionhelper.llm import PROVIDERS
from sessionhelper.protocol import AddressBook, is_mirror_control, reply_target
from sessionhelper.provision import Provisioner, compare_versions


BASE_ENV = {
    "SH_SESSION_NAME": "home",
    "SH_KEY_ID": "FB-test",
    "SH_SECRET": "secret",
}

ROOT = Path(__file__).resolve().parents[1]


class SessionHelperTest(unittest.TestCase):
    def test_load_config_required_and_defaults(self):
        cfg = load_config(BASE_ENV)
        self.assertEqual(cfg.session_name, "home")
        self.assertEqual(cfg.key_id, "FB-test")
        self.assertEqual(cfg.mode, "echo")
        self.assertEqual(cfg.cli_launch, "")
        self.assertEqual(cfg.cli_ready_timeout, 90.0)
        self.assertEqual(cfg.cli_reply_wait, 45.0)
        self.assertTrue(cfg.collect)
        self.assertFalse(cfg.async_reply)
        self.assertFalse(cfg.producer)
        self.assertFalse(cfg.no_directory)
        self.assertTrue(cfg.agent_route)
        self.assertEqual(cfg.target_group, "")
        self.assertEqual(cfg.target_bot, "")
        self.assertEqual(cfg.opencode_db, "")
        self.assertEqual(cfg.ws_url, "ws://127.0.0.1:8791/ws/session/home?key_id=FB-test&os=linux")

    def test_ws_url_reports_tool_and_model_when_configured(self):
        cfg = load_config(dict(BASE_ENV, SH_TOOL="CODEX", SH_MODEL="gpt-5.5", SH_SESSION_FULL="sh-home-e0d12642"))
        self.assertEqual(
            cfg.ws_url,
            "ws://127.0.0.1:8791/ws/session/home?key_id=FB-test&tool=CODEX&os=linux&model=gpt-5.5&full_session_name=sh-home-e0d12642",
        )

    def test_ws_url_reports_producer_target_group(self):
        cfg = load_config(dict(BASE_ENV, SH_PRODUCER="1", SH_TARGET_GROUP="oc_ai", SH_TARGET_BOT="bot-test"))
        self.assertTrue(cfg.producer)
        self.assertEqual(cfg.target_group, "oc_ai")
        self.assertEqual(cfg.target_bot, "bot-test")
        self.assertEqual(cfg.ws_url, "ws://127.0.0.1:8791/ws/session/home?key_id=FB-test&os=linux&producer=1&target_group=oc_ai&target_bot=bot-test")

    def test_ws_url_reports_mirror_to(self):
        cfg = load_config(dict(BASE_ENV, SH_MIRROR_TO="ou_u1#FB-test#is3-Connector"))
        self.assertEqual(cfg.ws_url, "ws://127.0.0.1:8791/ws/session/home?key_id=FB-test&os=linux&mirror_to=ou_u1%23FB-test%23is3-Connector")

    def test_ws_url_reports_no_directory(self):
        cfg = load_config(dict(BASE_ENV, SH_NO_DIRECTORY="1"))
        self.assertTrue(cfg.no_directory)
        self.assertEqual(cfg.ws_url, "ws://127.0.0.1:8791/ws/session/home?key_id=FB-test&os=linux&no_directory=1")

    def test_detect_full_session_name_prefers_tmux_session(self):
        with mock.patch("sessionhelper.config.subprocess.check_output", return_value="sh-developer-e0d12642\n"):
            got = detect_full_session_name("developer", dict(BASE_ENV, TMUX="/tmp/tmux-1000/default,1,0", SH_SESSION_FULL="fallback"))
        self.assertEqual(got, "sh-developer-e0d12642")

    def test_detect_full_session_name_falls_back_to_env_then_short_name(self):
        self.assertEqual(detect_full_session_name("developer", dict(BASE_ENV, SH_SESSION_FULL="sh-developer-e0d12642")), "sh-developer-e0d12642")
        self.assertEqual(detect_full_session_name("developer", dict(BASE_ENV)), "developer")

    def test_cli_launch_env_and_tmux_session_name(self):
        env = dict(BASE_ENV, SH_CLI_LAUNCH="tmux")
        cfg = load_config(env)
        self.assertEqual(cfg.cli_launch, "tmux")
        name = tmux_session_name("home/dev", "FB-test-key-0000")
        self.assertTrue(name.startswith("sh-home-dev-"))
        self.assertIn("key-0000", name)

    def test_opencode_profile_uses_sqlite_reply_source(self):
        cfg = load_config(dict(BASE_ENV, SH_CLI="opencode", SH_OPENCODE_DB="/tmp/opencode.db"))
        self.assertEqual(cfg.opencode_db, "/tmp/opencode.db")
        self.assertEqual(CLI_PROFILES["opencode"].output_source, "opencode_db")
        self.assertEqual(CLI_PROFILES["opencode"].launch, "pty")
        self.assertEqual(CLI_PROFILES["claude"].launch, "pty")
        self.assertEqual(CLI_PROFILES["codex"].launch, "pty")

    def test_cli_launch_failure_returns_not_ready_reply(self):
        cfg = load_config(
            dict(
                BASE_ENV,
                SH_MODE="cli",
                SH_CLI="claude",
                SH_CLI_LAUNCH="pty",
                SH_CLI_CMD="false",
                SH_CLI_READY_TIMEOUT="0.2",
                SH_CLI_LAUNCH_RETRIES="1",
            )
        )
        reply = CLIAdapter(cfg).handle({"body": "hello"})
        self.assertIn("CLI未就绪", reply)

    def test_cli_async_reply_transcript_confirms_user_without_sync_reply(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="claude", SH_ASYNC_REPLY="1"))
        self.assertTrue(cfg.async_reply)
        adapter = CLIAdapter(cfg)
        adapter.ready.set()
        adapter.transcript = object()
        calls = {"confirm": 0, "collect": 0}
        adapter.start = lambda: True

        def fake_confirm(prompt):
            calls["confirm"] += 1
            self.assertEqual(prompt, "长耗时任务")
            return "ok"

        def fake_collect(_prompt):
            calls["collect"] += 1
            return ("ok", "不应同步返回")

        adapter.inject_and_confirm_transcript_user = fake_confirm
        adapter.inject_and_collect_transcript_reply = fake_collect
        self.assertEqual(adapter.handle({"body": "长耗时任务"}), "")
        self.assertEqual(calls, {"confirm": 1, "collect": 0})

    def test_cli_sync_reply_default_still_collects_transcript_reply(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="claude"))
        adapter = CLIAdapter(cfg)
        adapter.ready.set()
        adapter.transcript = object()
        calls = {"confirm": 0, "collect": 0}
        adapter.start = lambda: True
        adapter.inject_and_confirm_transcript_user = lambda _prompt: calls.__setitem__("confirm", calls["confirm"] + 1) or "ok"

        def fake_collect(prompt):
            calls["collect"] += 1
            self.assertEqual(prompt, "普通任务")
            return ("ok", "同步答案")

        adapter.inject_and_collect_transcript_reply = fake_collect
        self.assertEqual(adapter.handle({"body": "普通任务"}), "同步答案")
        self.assertEqual(calls, {"confirm": 0, "collect": 1})

    def test_cli_async_reply_pty_injects_without_collect_output(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="aider", SH_ASYNC_REPLY="1"))
        adapter = CLIAdapter(cfg)
        adapter.ready.set()
        calls = {"inject": 0, "collect": 0}
        adapter.start = lambda: True

        def fake_inject(prompt):
            calls["inject"] += 1
            self.assertEqual(prompt, "异步命令")

        def fake_collect(_wait):
            calls["collect"] += 1
            return ["不应收集"]

        adapter.inject_text = fake_inject
        adapter.collect_output = fake_collect
        self.assertEqual(adapter.handle({"body": "异步命令"}), "")
        self.assertEqual(calls, {"inject": 1, "collect": 0})

    def test_tmux_multiline_injection_uses_bracketed_paste_then_enter(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="aider", SH_TOOL="AIDER"))
        adapter = CLIAdapter(cfg)
        adapter.launch_mode = "tmux"
        calls = []

        def fake_run(args, check=False, **kwargs):
            calls.append(args)
            return subprocess.CompletedProcess(args, 0)

        with mock.patch("sessionhelper.cli.subprocess.run", side_effect=fake_run), mock.patch("sessionhelper.cli.time.sleep", return_value=None):
            adapter.inject_text("第一行\n第二行")

        sent = [call[-1] for call in calls if call[:3] == ["tmux", "send-keys", "-t"] and "-l" in call]
        self.assertEqual(sent, [BRACKETED_PASTE_START, "第一行\n第二行", BRACKETED_PASTE_END])
        self.assertEqual(calls[-1][-1], "Enter")

    def test_pty_multiline_injection_uses_bracketed_paste(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="opencode", SH_TOOL="OPENCODE"))
        adapter = CLIAdapter(cfg)
        adapter.launch_mode = "pty"
        sent = []

        class FakeChild:
            def send(self, text):
                sent.append(text)

        adapter.child = FakeChild()
        with mock.patch("sessionhelper.cli.time.sleep", return_value=None):
            adapter.inject_text("a\nb")
        self.assertEqual(sent, [f"{BRACKETED_PASTE_START}a\nb{BRACKETED_PASTE_END}", "\r"])

    def test_unsupported_multiline_injection_flattens_to_single_submit(self):
        cfg = load_config(dict(BASE_ENV, SH_MODE="cli", SH_CLI="gemini", SH_TOOL="GEMINI"))
        adapter = CLIAdapter(cfg)
        self.assertEqual(adapter.prepare_input_text("a\n\nb"), "a / b")

    def test_group_reply_targets_chat_and_at_sender(self):
        book = AddressBook("home", "FB-test", "UnifiedRobot")
        to, meta = reply_target(
            {
                "from": "ou_alice#FB-test#UnifiedRobot",
                "meta": {
                    "chat_type": "group",
                    "group_chat_id": "oc_group",
                    "sender_open_id": "ou_alice",
                },
            },
            book,
        )
        self.assertEqual(to, "oc_group#FB-test#UnifiedRobot")
        self.assertEqual(meta, {"at": ["ou_alice"]})

    def test_reply_target_preserves_source_bot_context(self):
        book = AddressBook("home", "FB-test", "UnifiedRobot")
        to, meta = reply_target(
            {
                "from": "ou_u1#FB-test#UnifiedRobot",
                "meta": {
                    "source_bot_channel_id": "is3-Connector",
                    "source_chat_type": "personal",
                    "source_open_id": "ou_u1",
                    "source_chat_id": "ou_u1",
                },
            },
            book,
        )
        self.assertEqual(to, "ou_u1#FB-test#is3-Connector")
        self.assertEqual(
            meta,
            {
                "source_bot_channel_id": "is3-Connector",
                "source_chat_type": "personal",
                "source_open_id": "ou_u1",
                "source_chat_id": "ou_u1",
            },
        )

        to, meta = reply_target(
            {
                "from": "ou_sender#FB-test#UnifiedRobot",
                "meta": {
                    "source_bot_channel_id": "is3-Connector",
                    "source_chat_type": "group",
                    "source_chat_id": "oc_team",
                    "source_sender_openid": "ou_sender",
                },
            },
            book,
        )
        self.assertEqual(to, "oc_team#FB-test#is3-Connector")
        self.assertEqual(
            meta,
            {
                "source_bot_channel_id": "is3-Connector",
                "source_chat_type": "group",
                "source_chat_id": "oc_team",
                "source_sender_openid": "ou_sender",
                "at": ["ou_sender"],
            },
        )

    def test_comm_skill_ack_envelope(self):
        book = AddressBook("home", "FB-test", "UnifiedRobot")
        self.assertTrue(contains_comm_skill_ack(f"ok {COMM_SKILL_ACK}"))
        env = comm_skill_ack_envelope(book)
        self.assertEqual(env["to"], "workpulse#FB-test")
        self.assertEqual(env["from"], "home#FB-test")
        self.assertEqual(env["body"], COMM_SKILL_ACK)
        self.assertEqual(env["meta"]["type"], "agent_network_skill_ack")

    def test_agent_route_envelope_accepts_selector_output(self):
        book = AddressBook("home", "FB-test", "UnifiedRobot")
        env = agent_route_envelope("#developer 请核对X", book)
        self.assertEqual(env["to"], "#developer")
        self.assertEqual(env["from"], "home#FB-test")
        self.assertEqual(env["body"], "请核对X")
        self.assertEqual(env["meta"]["type"], "agent_route")

        env = agent_route_envelope("@u2#tester 请复测", book)
        self.assertEqual(env["to"], "@u2#tester")
        self.assertEqual(env["body"], "请复测")

        self.assertIsNone(agent_route_envelope("普通回答", book))
        self.assertIsNone(agent_route_envelope("# 中文标题", book))

    def test_agent_network_skill_injects_without_handle(self):
        class FakeAdapter:
            name = "cli"

            def __init__(self):
                self.injected = []
                self.handled = []

            def start(self):
                return True

            def inject_text(self, text):
                self.injected.append(text)

            def handle(self, env):
                self.handled.append(env)
                return "should not happen"

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli")))
        fake = FakeAdapter()
        helper.adapter = fake
        asyncio.run(helper.inject_agent_network_skill({"body": "指南", "meta": {"type": "agent_network_skill"}}))
        self.assertEqual(fake.injected, ["指南"])
        self.assertEqual(fake.handled, [])

    def test_terminal_input_writes_cli_adapter(self):
        class FakeAdapter:
            def __init__(self):
                self.inputs = []

            def write_terminal_input(self, data):
                self.inputs.append(data)

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli")))
        fake = FakeAdapter()
        helper.adapter = fake
        self.assertTrue(helper.is_terminal_input({"meta": {"type": "terminal_input"}}))
        asyncio.run(helper.handle_terminal_input({"body": "abc\r", "meta": {"type": "terminal_input"}}))
        self.assertEqual(fake.inputs, ["abc\r"])

    def test_recv_loop_filters_online_directory_without_handle(self):
        class FakeWS:
            def __init__(self, env):
                self.env = env
                self.done = False
                self.sent = []

            def __aiter__(self):
                return self

            async def __anext__(self):
                if self.done:
                    raise StopAsyncIteration
                self.done = True
                return json.dumps(self.env, ensure_ascii=False)

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            def __init__(self):
                self.handled = []

            def handle(self, env):
                self.handled.append(env)
                return "should not run"

        body = "\n**********\n【DingWei在线清单】同账号在线AI会话\n1. #home\n**********\n"
        self.assertTrue(is_online_directory_text(body))
        env = {"from": "workpulse#FB-test", "to": "home#FB-test", "body": body, "meta": {"type": "online_directory", "no_mirror": True}}
        self.assertTrue(is_no_mirror_envelope(env))
        helper = SessionHelper(load_config(BASE_ENV))
        fake = FakeAdapter()
        helper.adapter = fake
        ws = FakeWS(env)

        asyncio.run(helper.recv_loop(ws))

        self.assertEqual(fake.handled, [])
        self.assertEqual(ws.sent, [])

    def test_recv_loop_duplicate_agent_skill_acks_without_reinject(self):
        class FakeWS:
            def __init__(self, env):
                self.env = env
                self.done = False
                self.sent = []

            def __aiter__(self):
                return self

            async def __anext__(self):
                if self.done:
                    raise StopAsyncIteration
                self.done = True
                return json.dumps(self.env, ensure_ascii=False)

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            name = "cli"

            def __init__(self):
                self.injected = []

            def start(self):
                return True

            def inject_text(self, text):
                self.injected.append(text)

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli")))
        fake = FakeAdapter()
        helper.adapter = fake
        helper.comm_skill_installed = True
        ws = FakeWS({"from": "workpulse#FB-test", "to": "home#FB-test", "body": "指南", "meta": {"type": "agent_network_skill"}})

        asyncio.run(helper.recv_loop(ws))

        self.assertEqual(fake.injected, [])
        self.assertEqual(len(ws.sent), 1)
        self.assertEqual(ws.sent[0]["meta"]["type"], "agent_network_skill_ack")

    def test_busy_buffer_queues_busy_messages_and_drains_fifo(self):
        class FakeWS:
            def __init__(self, envs):
                self.envs = list(envs)
                self.sent = []

            def __aiter__(self):
                return self

            async def __anext__(self):
                if not self.envs:
                    raise StopAsyncIteration
                return json.dumps(self.envs.pop(0), ensure_ascii=False)

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            name = "cli"

            def __init__(self):
                self.idle = False
                self.handled = []

            def is_idle(self):
                return self.idle

            def handle(self, env):
                self.handled.append(env["body"])
                return ""

        envs = [
            {"from": "peer#FB-test", "to": "home#FB-test", "body": "one", "meta": {}},
            {"from": "peer#FB-test", "to": "home#FB-test", "body": "two", "meta": {}},
        ]
        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli", SH_BUSY_BUFFER_MAX="10", SH_CLI_SETTLE_SECONDS="0.1")))
        fake = FakeAdapter()
        helper.adapter = fake
        ws = FakeWS(envs)
        asyncio.run(helper.recv_loop(ws))
        self.assertEqual(fake.handled, [])
        self.assertEqual([item["body"] for item in helper.pending_inbound], ["one", "two"])
        self.assertEqual(len(ws.sent), 1)
        self.assertEqual(ws.sent[0]["to"], "peer#FB-test")
        self.assertIn("忙", ws.sent[0]["body"])

        async def drain_once():
            fake.idle = True
            task = asyncio.create_task(helper.pending_drain_loop(ws))
            deadline = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < deadline and helper.pending_inbound:
                await asyncio.sleep(0.05)
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass

        asyncio.run(drain_once())
        self.assertEqual(fake.handled, ["one", "two"])
        self.assertEqual(len(helper.pending_inbound), 0)
        self.assertEqual(helper.busy_acked_from, set())

    def test_busy_buffer_drops_oldest_when_full_and_skips_special_messages(self):
        class FakeAdapter:
            name = "cli"

            def is_idle(self):
                return False

        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MODE="cli", SH_BUSY_BUFFER_MAX="2")))
        helper.adapter = FakeAdapter()
        ws = FakeWS()

        async def run_case():
            for body in ["one", "two", "three"]:
                await helper.buffer_if_busy(ws, {"from": "peer#FB-test", "to": "home#FB-test", "body": body, "meta": {}})

        asyncio.run(run_case())
        self.assertEqual([item["body"] for item in helper.pending_inbound], ["two", "three"])
        self.assertEqual(len(ws.sent), 1)

    def test_provision_rejects_bad_source_and_audits(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            result = Provisioner(cfg).handle(
                {
                    "from": "attacker#FB-test",
                    "to": "home#FB-test",
                    "meta": {"type": "provision", "system": True, "action": "install_skill", "target": "x", "version": "1", "url": "https://ts.wegoab.com/x", "sha256": "0" * 64},
                }
            )
            self.assertFalse(result.ok)
            self.assertIn("source denied", result.message)
            with sqlite3.connect(Path(tmp) / "audit.db") as conn:
                rows = conn.execute("SELECT action, ok, source FROM provision_audit").fetchall()
            self.assertEqual(rows, [("install_skill", 0, "attacker#FB-test")])

    def test_provision_validates_host_and_sha_before_download(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            base = {
                "from": "workpulse#FB-test",
                "to": "home#FB-test",
                "meta": {"type": "provision", "system": True, "action": "install_skill", "target": "x", "version": "1", "url": "https://evil.example/x", "sha256": "0" * 64},
            }
            result = Provisioner(cfg).handle(base)
            self.assertFalse(result.ok)
            self.assertIn("host not allowed", result.message)
            bad_sha = json.loads(json.dumps(base))
            bad_sha["meta"]["url"] = "https://ts.wegoab.com/x"
            bad_sha["meta"]["sha256"] = "abc"
            result = Provisioner(cfg).handle(bad_sha)
            self.assertFalse(result.ok)
            self.assertIn("invalid sha256", result.message)

    def test_provision_idempotent_version_skips_download(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            provisioner = Provisioner(cfg)
            provisioner.version_file = Path(tmp) / "versions.json"
            provisioner.record_version("install_skill", "demo", "2.0")
            env = {
                "from": "workpulse#FB-test",
                "to": "home#FB-test",
                "meta": {"type": "provision", "system": True, "action": "install_skill", "target": "demo", "version": "1.0", "url": "https://ts.wegoab.com/x", "sha256": "0" * 64},
            }
            with mock.patch.object(provisioner, "download_and_verify", side_effect=AssertionError("should not download")):
                result = provisioner.handle(env)
            self.assertTrue(result.ok)
            self.assertEqual(result.message, "already installed")

    def test_provision_install_skill_from_verified_tar(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            home.mkdir()
            artifact = Path(tmp) / "skill.tar"
            src = Path(tmp) / "src"
            src.mkdir()
            (src / "SKILL.md").write_text("demo skill", encoding="utf-8")
            with tarfile.open(artifact, "w") as tf:
                tf.add(src / "SKILL.md", arcname="SKILL.md")
            cfg = load_config(dict(BASE_ENV, SH_CLI="codex", SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            with mock.patch.dict("os.environ", {"HOME": str(home), "CODEX_HOME": str(home / ".codex")}):
                provisioner = Provisioner(cfg)
                provisioner.version_file = Path(tmp) / "versions.json"
                env = {
                    "from": "workpulse#FB-test",
                    "to": "home#FB-test",
                    "meta": {"type": "provision", "system": True, "action": "install_skill", "target": "demo", "version": "1.0", "url": "https://ts.wegoab.com/skill.tar", "sha256": "0" * 64},
                }
                with mock.patch.object(provisioner, "download_and_verify", return_value=artifact):
                    result = provisioner.handle(env)
                self.assertTrue(result.ok, result.message)
                self.assertEqual((home / ".codex" / "skills" / "demo" / "SKILL.md").read_text(encoding="utf-8"), "demo skill")

    def test_provision_install_mcp_merges_json_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            home.mkdir()
            artifact = Path(tmp) / "mcp.json"
            artifact.write_text(json.dumps({"command": "demo-mcp", "args": ["--stdio"], "env": {"TOKEN": "${DEMO_TOKEN}"}}), encoding="utf-8")
            cfg = load_config(dict(BASE_ENV, SH_CLI="claude", SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            with mock.patch.dict("os.environ", {"HOME": str(home)}):
                provisioner = Provisioner(cfg)
                provisioner.version_file = Path(tmp) / "versions.json"
                env = {
                    "from": "workpulse#FB-test",
                    "to": "home#FB-test",
                    "meta": {"type": "provision", "system": True, "action": "install_mcp", "target": "demo", "version": "1.0", "url": "https://ts.wegoab.com/mcp.json", "sha256": "0" * 64},
                }
                with mock.patch.object(provisioner, "download_and_verify", return_value=artifact):
                    result = provisioner.handle(env)
                self.assertTrue(result.ok, result.message)
                data = json.loads((home / ".claude.json").read_text(encoding="utf-8"))
                self.assertEqual(data["mcpServers"]["demo"]["command"], "demo-mcp")
                self.assertEqual(data["mcpServers"]["demo"]["env"]["TOKEN"], "${DEMO_TOKEN}")

    def test_provision_install_mcp_writes_codex_toml_snippet(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            codex_home = home / ".codex"
            home.mkdir()
            artifact = Path(tmp) / "mcp.json"
            artifact.write_text(json.dumps({"mcpServers": {"demo": {"command": "demo-mcp", "args": ["--stdio"]}}}), encoding="utf-8")
            cfg = load_config(dict(BASE_ENV, SH_CLI="codex", SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            with mock.patch.dict("os.environ", {"HOME": str(home), "CODEX_HOME": str(codex_home)}):
                provisioner = Provisioner(cfg)
                provisioner.version_file = Path(tmp) / "versions.json"
                env = {
                    "from": "workpulse#FB-test",
                    "to": "home#FB-test",
                    "meta": {"type": "provision", "system": True, "action": "install_mcp", "target": "demo", "version": "1.0", "url": "https://ts.wegoab.com/mcp.json", "sha256": "0" * 64},
                }
                with mock.patch.object(provisioner, "download_and_verify", return_value=artifact):
                    result = provisioner.handle(env)
                self.assertTrue(result.ok, result.message)
                text = (codex_home / "config.toml").read_text(encoding="utf-8")
                self.assertIn('# dingwei-mcp:demo:1.0', text)
                self.assertIn('[mcp_servers."demo"]', text)
                self.assertIn('command = "demo-mcp"', text)

    def test_provision_download_requires_matching_sha256(self):
        class FakeResponse:
            def __init__(self, chunks):
                self.chunks = list(chunks)

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self, _size):
                if not self.chunks:
                    return b""
                return self.chunks.pop(0)

        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            provisioner = Provisioner(cfg)
            expected = hashlib.sha256(b"abcdef").hexdigest()
            with mock.patch("sessionhelper.provision.urllib.request.urlopen", return_value=FakeResponse([b"abc", b"def"])):
                path = provisioner.download_and_verify("https://ts.wegoab.com/artifact", expected)
            self.assertEqual(path.read_bytes(), b"abcdef")
            with mock.patch("sessionhelper.provision.urllib.request.urlopen", return_value=FakeResponse([b"bad"])):
                with self.assertRaises(Exception):
                    provisioner.download_and_verify("https://ts.wegoab.com/artifact", expected)

    def test_provision_rejects_tar_escape_and_links(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            provisioner = Provisioner(cfg)
            escape_tar = Path(tmp) / "escape.tar"
            with tarfile.open(escape_tar, "w") as tf:
                info = tarfile.TarInfo("../escape.txt")
                payload = b"x"
                info.size = len(payload)
                tf.addfile(info, io.BytesIO(payload))
            with self.assertRaises(Exception):
                provisioner.extract_package(escape_tar, Path(tmp) / "out")

            link_tar = Path(tmp) / "link.tar"
            with tarfile.open(link_tar, "w") as tf:
                info = tarfile.TarInfo("link")
                info.type = tarfile.SYMTYPE
                info.linkname = "/tmp/escape"
                tf.addfile(info)
            with self.assertRaises(Exception):
                provisioner.extract_package(link_tar, Path(tmp) / "out2")

    def test_provision_update_self_rolls_back_stale_pending_update(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            root = Path(tmp) / "sessionhelper"
            backup = Path(tmp) / "backup"
            home.mkdir()
            root.mkdir()
            backup.mkdir()
            (root / "app.py").write_text("new", encoding="utf-8")
            (backup / "app.py").write_text("old", encoding="utf-8")
            cfg = load_config(dict(BASE_ENV, SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db")))
            with mock.patch.dict("os.environ", {"HOME": str(home), "SH_PROVISION_NO_EXIT": "1"}):
                provisioner = Provisioner(cfg, package_root=str(root))
                provisioner.update_state_file.parent.mkdir(parents=True, exist_ok=True)
                provisioner.update_state_file.write_text(
                    json.dumps({"version": "future", "backup": str(backup), "status": "pending", "ts": int(time.time()) - 100}),
                    encoding="utf-8",
                )
                self.assertTrue(provisioner.rollback_stale_update_if_needed(timeout_seconds=1))
                self.assertEqual((root / "app.py").read_text(encoding="utf-8"), "old")

    def test_provision_restart_strategy_is_platform_specific(self):
        cfg = load_config(BASE_ENV)
        provisioner = Provisioner(cfg)
        with mock.patch("sessionhelper.provision.detect_os", return_value="linux"):
            self.assertEqual(provisioner.restart_strategy(), "cron_guard")
        with mock.patch("sessionhelper.provision.detect_os", return_value="macos"):
            self.assertEqual(provisioner.restart_strategy(), "launchd")

    def test_provision_ack_is_sent_from_recv_loop(self):
        class FakeWS:
            def __init__(self, env):
                self.env = env
                self.done = False
                self.sent = []

            def __aiter__(self):
                return self

            async def __anext__(self):
                if self.done:
                    raise StopAsyncIteration
                self.done = True
                return json.dumps(self.env, ensure_ascii=False)

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        helper = SessionHelper(load_config(BASE_ENV))
        env = {"from": "workpulse#FB-test", "to": "home#FB-test", "body": "", "meta": {"type": "provision", "system": True, "action": "install_skill", "target": "x", "version": "1", "url": "https://evil.example/x", "sha256": "0" * 64}}
        ws = FakeWS(env)
        asyncio.run(helper.recv_loop(ws))
        self.assertEqual(len(ws.sent), 1)
        self.assertEqual(ws.sent[0]["to"], "workpulse#FB-test")
        self.assertEqual(ws.sent[0]["meta"]["type"], "provision_ack")
        self.assertFalse(ws.sent[0]["meta"]["ok"])

    def test_compare_versions_handles_numbers_and_hashes(self):
        self.assertGreater(compare_versions("2.0", "1.9"), 0)
        self.assertLess(compare_versions("abc", "bcd"), 0)

    def test_mirror_loop_sends_comm_skill_ack_on_marker(self):
        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            def __init__(self):
                self.events = [("assistant", f"收到 {COMM_SKILL_ACK}")]

            def start(self):
                return True

            def next_mirror_event(self, _timeout):
                if self.events:
                    return self.events.pop(0)
                return None

        async def run_case():
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_COLLECT="1", SH_MIRROR_TO="ou_u1#FB-test#UnifiedRobot")))
            helper.adapter = FakeAdapter()
            ws = FakeWS()
            task = asyncio.create_task(helper.mirror_loop(ws))
            deadline = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < deadline and not ws.sent:
                await asyncio.sleep(0.05)
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass
            return ws.sent

        sent = asyncio.run(run_case())
        self.assertEqual(len(sent), 1)
        self.assertEqual(sent[0]["to"], "workpulse#FB-test")
        self.assertEqual(sent[0]["body"], COMM_SKILL_ACK)
        self.assertEqual(sent[0]["meta"]["type"], "agent_network_skill_ack")

    def test_mirror_control_updates_state_without_adapter(self):
        helper = SessionHelper(load_config(BASE_ENV))
        env = {
            "meta": {
                "type": "mirror_control",
                "enabled": True,
                "mirror_to": "oc_group#FB-test#UnifiedRobot",
            }
        }
        self.assertTrue(is_mirror_control(env))
        helper.apply_mirror_control(env)
        self.assertTrue(helper.mirror.enabled)
        self.assertEqual(helper.mirror.to, "oc_group#FB-test#UnifiedRobot")

    def test_mirror_control_without_target_keeps_configured_mirror_to(self):
        helper = SessionHelper(load_config(dict(BASE_ENV, SH_MIRROR_TO="ou_u1#FB-test#is3-Connector")))
        helper.apply_mirror_control({"meta": {"type": "mirror_control", "enabled": True}})
        self.assertTrue(helper.mirror.enabled)
        self.assertEqual(helper.mirror.to, "ou_u1#FB-test#is3-Connector")

        helper.apply_mirror_control({"meta": {"type": "mirror_control", "enabled": False, "mirror_to": ""}})
        self.assertFalse(helper.mirror.enabled)
        self.assertEqual(helper.mirror.to, "")

    def test_online_directory_broadcast_is_filtered_from_cli(self):
        class FakeWS:
            def __init__(self, env):
                self.env = env
                self.done = False
                self.sent = []

            def __aiter__(self):
                return self

            async def __anext__(self):
                if self.done:
                    raise StopAsyncIteration
                self.done = True
                return json.dumps(self.env, ensure_ascii=False)

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            def __init__(self, events=()):
                self.seen = []
                self.events = list(events)

            def handle(self, env):
                self.seen.append(env.get("body"))
                return ""

            def start(self):
                return True

            def next_mirror_event(self, _timeout):
                if self.events:
                    return self.events.pop(0)
                return None

        async def run_mirror(helper, ws):
            task = asyncio.create_task(helper.mirror_loop(ws))
            deadline = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < deadline and helper.adapter.events:
                await asyncio.sleep(0.05)
            await asyncio.sleep(0.35)
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass

        async def run_case():
            helpers = []
            for i, session in enumerate(["home", "developer", "review"]):
                env = {
                    "from": "workpulse#FB-test",
                    "to": f"{session}#FB-test",
                    "body": "系统广播内容",
                    "meta": {
                        "type": "online_directory",
                        "system": True,
                        "no_mirror": True,
                        "broadcast_dedup_key": "broadcast:online_directory:alice:m1",
                        "mirror_primary": i == 0,
                    },
                }
                cfg = load_config(dict(BASE_ENV, SH_SESSION_NAME=session, SH_MIRROR_TO="ou_alice#FB-test#UnifiedRobot", SH_COLLECT="0"))
                helper = SessionHelper(cfg)
                helper.adapter = FakeAdapter()
                recv_ws = FakeWS(env)
                await helper.recv_loop(recv_ws)
                helpers.append((helper, recv_ws))
            mirror_sends = []
            for helper, _recv_ws in helpers:
                mirror_ws = FakeWS({})
                await run_mirror(helper, mirror_ws)
                mirror_sends.extend(mirror_ws.sent)
            return [helper.adapter.seen for helper, _ in helpers], mirror_sends

        injected, mirrored = asyncio.run(run_case())
        self.assertEqual(injected, [[], [], []])
        self.assertEqual(mirrored, [])

    def test_is_broadcast_envelope_requires_dedup_key(self):
        env = {
            "body": "广播内容",
            "meta": {"type": "online_directory", "broadcast_dedup_key": "broadcast:1"},
        }
        self.assertTrue(is_broadcast_envelope(env))
        self.assertFalse(is_broadcast_envelope({"body": "点对点", "meta": {"system": True, "no_mirror": True}}))

    def test_mirror_loop_keeps_point_to_point_and_ai_output_mirrors(self):
        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        class FakeAdapter:
            def __init__(self, events):
                self.events = list(events)

            def start(self):
                return True

            def next_mirror_event(self, _timeout):
                if self.events:
                    return self.events.pop(0)
                return None

        async def run_one(events, remembered=""):
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_MIRROR_TO="ou_alice#FB-test#UnifiedRobot", SH_COLLECT="0")))
            if remembered:
                helper.remember_broadcast_mirror_decision(
                    {"body": remembered, "meta": {"broadcast_dedup_key": "broadcast:test", "mirror_primary": False}}
                )
            helper.adapter = FakeAdapter(events)
            ws = FakeWS()
            task = asyncio.create_task(helper.mirror_loop(ws))
            deadline = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < deadline and helper.adapter.events:
                await asyncio.sleep(0.05)
            await asyncio.sleep(0.35)
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass
            return ws.sent

        point_to_point = asyncio.run(run_one([("user", "点对点输入")]))
        ai_output = asyncio.run(run_one([("claude", "各自 AI 输出")], remembered="广播输入"))
        mirrored_normal = asyncio.run(run_one([("claude", "正常任务回复")]))

        self.assertEqual(len(point_to_point), 1)
        self.assertIn("点对点输入", point_to_point[0]["body"])
        self.assertEqual(len(ai_output), 1)
        self.assertIn("各自 AI 输出", ai_output[0]["body"])
        self.assertEqual(len(mirrored_normal), 1)
        self.assertEqual(mirrored_normal[0]["to"], "ou_alice#FB-test#UnifiedRobot")
        self.assertIn("正常任务回复", mirrored_normal[0]["body"])

    def test_producer_envelope_targets_group_and_marks_no_mirror(self):
        helper = SessionHelper(load_config(dict(BASE_ENV, SH_PRODUCER="1", SH_TARGET_GROUP="oc_group", SH_TARGET_BOT="bot-test")))
        env = helper.producer_envelope("hello", role="alert")
        self.assertEqual(env["to"], "oc_group#FB-test#UnifiedRobot")
        self.assertEqual(env["from"], "home#FB-test")
        self.assertEqual(env["body"], "hello")
        self.assertEqual(env["meta"]["producer"], True)
        self.assertEqual(env["meta"]["target_group"], "oc_group")
        self.assertEqual(env["meta"]["target_bot"], "bot-test")
        self.assertEqual(env["meta"]["role"], "alert")
        self.assertEqual(env["meta"]["no_mirror"], True)

    def test_producer_requires_target_group(self):
        helper = SessionHelper(load_config(dict(BASE_ENV, SH_PRODUCER="1")))
        with self.assertRaises(RuntimeError):
            helper.producer_envelope("hello")

    def test_producer_stdin_loop_sends_lines_to_group(self):
        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        async def run_case():
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_PRODUCER="1", SH_TARGET_GROUP="oc_group")))
            ws = FakeWS()
            lines = iter(["hello\n", ""])

            async def fake_to_thread(_func):
                return next(lines)

            with mock.patch("sessionhelper.app.asyncio.to_thread", side_effect=fake_to_thread):
                await helper.producer_stdin_loop(ws)
            return ws.sent

        sent = asyncio.run(run_case())
        self.assertEqual(len(sent), 1)
        self.assertEqual(sent[0]["to"], "oc_group#FB-test#UnifiedRobot")
        self.assertEqual(sent[0]["body"], "hello")
        self.assertEqual(sent[0]["meta"]["no_mirror"], True)

    def test_producer_stdin_loop_sends_multiple_lines_then_exits_on_eof(self):
        class FakeWS:
            def __init__(self):
                self.sent = []

            async def send(self, payload):
                self.sent.append(json.loads(payload))

        async def run_case():
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_PRODUCER="1", SH_TARGET_GROUP="oc_group")))
            ws = FakeWS()
            lines = iter(["one\n", "\n", "two\n", ""])

            async def fake_to_thread(_func):
                return next(lines)

            with mock.patch("sessionhelper.app.asyncio.to_thread", side_effect=fake_to_thread):
                await helper.producer_stdin_loop(ws)
            return ws.sent

        sent = asyncio.run(run_case())
        self.assertEqual([item["body"] for item in sent], ["one", "two"])

    def test_producer_run_forever_exits_after_one_run_once(self):
        async def run_case():
            helper = SessionHelper(load_config(dict(BASE_ENV, SH_PRODUCER="1", SH_TARGET_GROUP="oc_group")))
            calls = {"n": 0}

            async def fake_run_once():
                calls["n"] += 1

            helper._run_once = fake_run_once
            class FakeWebsockets:
                ConnectionClosedError = RuntimeError

            with mock.patch.dict("sys.modules", {"websockets": FakeWebsockets}):
                await helper.run_forever()
            return calls["n"]

        self.assertEqual(asyncio.run(run_case()), 1)

    def test_reply_body_uses_cross_member_prefix(self):
        helper = SessionHelper(load_config(BASE_ENV))
        body = helper.reply_body({"meta": {"reply_prefix": "【UserTwo·u2】"}}, "干净答案")
        self.assertEqual(body, "【UserTwo·u2】干净答案")

    def test_provider_defaults_include_required_names(self):
        for name in ["deepseek", "qwen", "kimi", "minimax", "glm", "openai", "claude", "gemini"]:
            self.assertIn(name, PROVIDERS)
            self.assertTrue(PROVIDERS[name].base_url)
            self.assertTrue(PROVIDERS[name].model)

    def test_claude_transcript_parser_extracts_clean_text(self):
        event = {
            "type": "assistant",
            "message": {"content": [{"type": "text", "text": "干净答案"}]},
        }
        self.assertEqual(parse_claude_event(event), ("claude", "干净答案"))

    def test_codex_transcript_parser_extracts_agent_message(self):
        event = {
            "type": "event_msg",
            "payload": {"type": "agent_message", "message": "codex answer"},
        }
        self.assertEqual(parse_codex_event(event), ("codex", "codex answer"))

    def test_codex_transcript_parser_extracts_user_message(self):
        event = {
            "type": "response_item",
            "payload": {
                "type": "message",
                "role": "user",
                "content": [{"type": "input_text", "text": "hello codex"}],
            },
        }
        self.assertEqual(parse_codex_event(event), ("user", "hello codex"))

    def test_transcript_tailer_reads_only_new_events(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "session.jsonl"
            path.write_text(json.dumps({"type": "assistant", "message": {"content": [{"type": "text", "text": "old"}]}}) + "\n")
            tailer = TranscriptTailer("claude", path)
            with path.open("a", encoding="utf-8") as f:
                f.write(json.dumps({"type": "assistant", "message": {"content": [{"type": "text", "text": "new"}]}}) + "\n")
            self.assertEqual(tailer.read_events(), [("claude", "new")])

    def test_transcript_tailer_can_read_existing_events_for_new_session(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "session.jsonl"
            path.write_text(json.dumps({"type": "user", "message": {"content": "hello"}}) + "\n")
            tailer = TranscriptTailer("claude", path, from_end=False)
            self.assertEqual(tailer.read_events(), [("user", "hello")])

    def test_cli_reinjects_when_transcript_user_turn_missing(self):
        cfg = load_config(
            dict(
                BASE_ENV,
                SH_MODE="cli",
                SH_CLI="claude",
                SH_CLI_USER_ACK_WAIT="0.01",
                SH_CLI_REPLY_WAIT="0.01",
            )
        )
        adapter = CLIAdapter(cfg)
        counter = {"injects": 0}

        class FakeTranscript:
            sent = False

            def read_events(self):
                if counter["injects"] < 2:
                    return []
                if self.sent:
                    return []
                self.sent = True
                return [("user", "冷启动第一条"), ("claude", "干净答案")]

        adapter.transcript = FakeTranscript()
        adapter.inject_text = lambda text: counter.__setitem__("injects", counter["injects"] + 1)
        adapter.transcript_retry_delay = lambda attempt: 0

        status, reply = adapter.inject_and_collect_transcript_reply("冷启动第一条")
        self.assertEqual((status, reply), ("ok", "干净答案"))
        self.assertEqual(counter["injects"], 2)

    def test_opencode_db_reader_collects_finished_text_reply(self):
        with tempfile.TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "opencode.db"
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                CREATE TABLE message(id TEXT, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT);
                CREATE TABLE part(id TEXT, message_id TEXT, session_id TEXT, time_created INTEGER, data TEXT);
                """
            )
            now = 1_783_000_000_000
            conn.execute(
                "INSERT INTO message VALUES(?,?,?,?,?)",
                ("m-user", "sess-1", now, now, json.dumps({"role": "user"})),
            )
            conn.execute(
                "INSERT INTO part VALUES(?,?,?,?,?)",
                ("p-user", "m-user", "sess-1", now, json.dumps({"type": "text", "text": "请总结"})),
            )
            conn.execute(
                "INSERT INTO message VALUES(?,?,?,?,?)",
                ("m-assistant", "sess-1", now + 1000, now + 1000, json.dumps({"role": "assistant", "finish": True})),
            )
            conn.execute(
                "INSERT INTO part VALUES(?,?,?,?,?)",
                ("p-reason", "m-assistant", "sess-1", now + 1001, json.dumps({"type": "reasoning", "text": "skip"})),
            )
            conn.execute(
                "INSERT INTO part VALUES(?,?,?,?,?)",
                ("p-text", "m-assistant", "sess-1", now + 1002, json.dumps({"type": "text", "text": "```markdown\n干净答案\n```"})),
            )
            conn.commit()
            conn.close()

            reader = OpenCodeDBReader(str(db_path))
            session = reader.find_user_message("请总结", now / 1000 - 0.1)
            self.assertEqual(session, ("sess-1", now / 1000))
            self.assertEqual(reader.find_finished_assistant_reply("sess-1", now / 1000), "干净答案")

    def test_opencode_helpers_parse_time_and_strip_fence(self):
        self.assertEqual(opencode_time(1_783_000_000_000), 1_783_000_000)
        self.assertEqual(strip_markdown_code_fence("```go\nfmt.Println(1)\n```"), "fmt.Println(1)")

    def test_slug_cwd_is_stable_path_fragment(self):
        self.assertIn("tmp-demo", slug_cwd("/tmp/demo"))

    def test_fresh_jsonl_rejects_old_first_timestamp(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "old.jsonl"
            path.write_text(json.dumps({"timestamp": "2026-07-01T00:00:00Z", "type": "mode"}) + "\n")
            self.assertEqual(fresh_jsonl([path], 1782867600), [])

    def test_event_time_parses_iso_and_user_text_matches(self):
        self.assertEqual(event_time({"timestamp": "2026-07-01T00:00:00Z"}), 1782864000)
        self.assertTrue(user_text_matches("hello   world", "prefix hello world suffix"))

    def test_run_sh_is_portable_bootstrap(self):
        path = ROOT / "run.sh"
        self.assertTrue(path.exists())
        mode = stat.S_IMODE(path.stat().st_mode)
        self.assertTrue(mode & stat.S_IXUSR, oct(mode))
        subprocess.run(["bash", "-n", str(path)], check=True)
        text = path.read_text(encoding="utf-8")
        for forbidden in ("declare -A", "${var,,}", "sed -i"):
            self.assertNotIn(forbidden, text)
        self.assertIn("chmod 600", text)
        self.assertIn("trap cleanup_tmux EXIT", text)
        self.assertIn("SH_CLI_READY_TIMEOUT=${SH_CLI_READY_TIMEOUT:-90}", text)
        self.assertIn("SH_CLI_REPLY_WAIT=${SH_CLI_REPLY_WAIT:-45}", text)
        self.assertIn("SH_COLLECT=${SH_COLLECT:-1}", text)
        self.assertIn("SH_ASYNC_REPLY=${SH_ASYNC_REPLY:-0}", text)
        self.assertIn("SH_TARGET_GROUP=${SH_TARGET_GROUP:-}", text)
        self.assertIn("SH_TARGET_BOT=${SH_TARGET_BOT:-}", text)
        self.assertIn("SH_PRODUCER=${SH_PRODUCER:-0}", text)
        self.assertIn("SH_SECRET", text)
        self.assertNotIn("PLACEHOLDER_SECRET@", text)
        self.assertNotIn("test-backend.com", text)


if __name__ == "__main__":
    unittest.main()
